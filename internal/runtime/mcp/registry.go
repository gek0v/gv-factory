// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ServerEntry holds an MCP client and its metadata in the registry.
type ServerEntry struct {
	Name    string
	Client  *Client
	Timeout time.Duration
}

// RegistryConfig provides configuration for MCPToolRegistry.
type RegistryConfig struct {
	DefaultTimeout  time.Duration
	MaxTimeout      time.Duration
	MaxConcurrent   int
}

// MCPToolRegistry manages multiple MCP server connections and routes tool calls.
type MCPToolRegistry struct {
	mu            sync.RWMutex
	servers       map[string]*ServerEntry
	toolToServer  map[string]string // tool name -> server name
	defaultTimeout time.Duration
	maxTimeout    time.Duration
	sem           chan struct{} // Concurrency limiter
}

// NewMCPToolRegistry initializes a new MCPToolRegistry.
func NewMCPToolRegistry(cfg RegistryConfig) *MCPToolRegistry {
	defaultTimeout := cfg.DefaultTimeout
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Second
	}
	maxTimeout := cfg.MaxTimeout
	if maxTimeout <= 0 {
		maxTimeout = 5 * time.Minute
	}
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 50
	}

	return &MCPToolRegistry{
		servers:        make(map[string]*ServerEntry),
		toolToServer:   make(map[string]string),
		defaultTimeout: defaultTimeout,
		maxTimeout:     maxTimeout,
		sem:            make(chan struct{}, maxConcurrent),
	}
}

// RegisterServer registers an MCP client under a logical server name.
func (r *MCPToolRegistry) RegisterServer(name string, client *Client, timeout time.Duration) error {
	if name == "" {
		return fmt.Errorf("server name cannot be empty")
	}
	if client == nil {
		return fmt.Errorf("client cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if timeout <= 0 {
		timeout = r.defaultTimeout
	} else if timeout > r.maxTimeout {
		timeout = r.maxTimeout
	}

	r.servers[name] = &ServerEntry{
		Name:    name,
		Client:  client,
		Timeout: timeout,
	}

	return nil
}

// UnregisterServer unregisters a server and removes its tool mappings.
func (r *MCPToolRegistry) UnregisterServer(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.servers[name]
	if !ok {
		return ErrServerNotFound
	}

	delete(r.servers, name)

	// Clean up tool mappings
	for toolName, sName := range r.toolToServer {
		if sName == name {
			delete(r.toolToServer, toolName)
		}
	}

	_ = entry.Client.Close()
	return nil
}

// ListTools discovers tools across all registered servers and caches tool-to-server routing.
func (r *MCPToolRegistry) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	r.mu.RLock()
	servers := make([]*ServerEntry, 0, len(r.servers))
	for _, s := range r.servers {
		servers = append(servers, s)
	}
	r.mu.RUnlock()

	var allTools []ToolDefinition
	newMappings := make(map[string]string)

	for _, s := range servers {
		tCtx, cancel := context.WithTimeout(ctx, s.Timeout)
		tools, err := s.Client.ListTools(tCtx)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("failed to list tools for server %q: %w", s.Name, err)
		}

		for _, tool := range tools {
			allTools = append(allTools, tool)
			newMappings[tool.Name] = s.Name
		}
	}

	r.mu.Lock()
	for k, v := range newMappings {
		r.toolToServer[k] = v
	}
	r.mu.Unlock()

	return allTools, nil
}

// CallTool invokes a tool by name, routing the request to the owning MCP server.
func (r *MCPToolRegistry) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolResult, error) {
	r.mu.RLock()
	serverName, ok := r.toolToServer[name]
	r.mu.RUnlock()

	if !ok {
		// If not cached, attempt discovery once
		if _, err := r.ListTools(ctx); err != nil {
			return nil, err
		}
		r.mu.RLock()
		serverName, ok = r.toolToServer[name]
		r.mu.RUnlock()

		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrToolNotFound, name)
		}
	}

	r.mu.RLock()
	server, exists := r.servers[serverName]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("%w: server %s for tool %s", ErrServerNotFound, serverName, name)
	}

	// Concurrency limiting
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	callTimeout := server.Timeout
	if callTimeout <= 0 {
		callTimeout = r.defaultTimeout
	}

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	return server.Client.CallTool(callCtx, name, arguments)
}

// Close closes all registered servers.
func (r *MCPToolRegistry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error
	for _, entry := range r.servers {
		if err := entry.Client.Close(); err != nil {
			lastErr = err
		}
	}
	r.servers = make(map[string]*ServerEntry)
	r.toolToServer = make(map[string]string)
	return lastErr
}
