// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/gateway"
)

// GateEventBus defines messaging capabilities required for gate event distribution.
type GateEventBus interface {
	Publish(ctx context.Context, topic string, ev gateway.Event) error
	SubscribeTaskEvents(ctx context.Context, topic string, ch chan gateway.Event) (func(), error)
}

// ApprovalTokenProvider defines methods to create and validate cryptographically signed HMAC approval tokens.
type ApprovalTokenProvider interface {
	GenerateApprovalToken(runID string, expiresAt time.Time) string
	ValidateApprovalToken(token string) (string, bool)
}

// LocalToolHandler defines an in-process tool implementation.
type LocalToolHandler func(ctx context.Context, arguments map[string]any) (*ToolResult, error)

type localToolEntry struct {
	def     ToolDefinition
	handler LocalToolHandler
}

// ServerEntry holds an MCP client and its metadata in the registry.
type ServerEntry struct {
	Name    string
	Client  *Client
	Timeout time.Duration
}

// RegistryConfig provides configuration for MCPToolRegistry.
type RegistryConfig struct {
	DefaultTimeout     time.Duration
	MaxTimeout         time.Duration
	MaxConcurrent      int
	DefaultGateTimeout time.Duration
	AuthService        ApprovalTokenProvider
	Gateway            GateEventBus
	AuditLogger        auth.AuditLogger
}

type gateResolution struct {
	decision string
}

// MCPToolRegistry manages multiple MCP server connections and routes tool calls.
type MCPToolRegistry struct {
	mu                 sync.RWMutex
	servers            map[string]*ServerEntry
	localTools         map[string]localToolEntry
	toolToServer       map[string]string // tool name -> server name
	toolDefinitions    map[string]ToolDefinition
	policies           []ToolSecurityPolicy
	defaultTimeout     time.Duration
	maxTimeout         time.Duration
	defaultGateTimeout time.Duration
	sem                chan struct{} // Concurrency limiter
	authService        ApprovalTokenProvider
	gateway            GateEventBus
	auditLogger        auth.AuditLogger
	gateResolvers      sync.Map // key: token (or run_id) -> chan gateResolution
	gwUnsub            func()
	closeCh            chan struct{}
	closeOnce          sync.Once
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
	defaultGateTimeout := cfg.DefaultGateTimeout
	if defaultGateTimeout <= 0 {
		defaultGateTimeout = 5 * time.Minute
	}
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 50
	}

	r := &MCPToolRegistry{
		servers:            make(map[string]*ServerEntry),
		localTools:         make(map[string]localToolEntry),
		toolToServer:       make(map[string]string),
		toolDefinitions:    make(map[string]ToolDefinition),
		policies:           make([]ToolSecurityPolicy, 0),
		defaultTimeout:     defaultTimeout,
		maxTimeout:         maxTimeout,
		defaultGateTimeout: defaultGateTimeout,
		sem:                make(chan struct{}, maxConcurrent),
		authService:        cfg.AuthService,
		gateway:            cfg.Gateway,
		auditLogger:        cfg.AuditLogger,
		closeCh:            make(chan struct{}),
	}

	if r.gateway != nil {
		resCh := make(chan gateway.Event, 50)
		unsub, err := r.gateway.SubscribeTaskEvents(context.Background(), gateway.TopicGateResolved, resCh)
		if err == nil {
			r.gwUnsub = unsub
			go r.listenGateResolutions(resCh)
		}
	}

	return r
}

func (r *MCPToolRegistry) listenGateResolutions(ch chan gateway.Event) {
	for {
		select {
		case <-r.closeCh:
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			p := ev.Payload
			decision, _ := p["decision"].(string)
			token, _ := p["token"].(string)
			runID := ev.RunID
			if runID == "" {
				runID, _ = p["run_id"].(string)
			}

			res := gateResolution{decision: decision}

			// Dispatch by token first
			if token != "" {
				if chVal, ok := r.gateResolvers.Load(token); ok {
					if rCh, ok := chVal.(chan gateResolution); ok {
						select {
						case rCh <- res:
						default:
						}
					}
				}
			}

			// Also dispatch by runID
			if runID != "" {
				if chVal, ok := r.gateResolvers.Load(runID); ok {
					if rCh, ok := chVal.(chan gateResolution); ok {
						select {
						case rCh <- res:
						default:
						}
					}
				}
			}
		}
	}
}

// SetToolPolicy adds or updates a security policy for tools.
func (r *MCPToolRegistry) SetToolPolicy(policy ToolSecurityPolicy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policies = append(r.policies, policy)
}

// RegisterProtectedTool registers a specific tool as requiring human-in-the-loop approval.
func (r *MCPToolRegistry) RegisterProtectedTool(toolName string, timeout time.Duration) {
	r.SetToolPolicy(ToolSecurityPolicy{
		NamePattern:      toolName,
		RequiresApproval: true,
		ApprovalTimeout:  timeout,
		Description:      fmt.Sprintf("Protection rule for %s", toolName),
	})
}

// IsApprovalRequired returns whether a tool requires human-in-the-loop gate approval, and its timeout.
func (r *MCPToolRegistry) IsApprovalRequired(toolName string) (bool, time.Duration) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check explicit tool definition first
	if def, ok := r.toolDefinitions[toolName]; ok && def.RequiresApproval {
		timeout := def.ApprovalTimeout
		if timeout <= 0 {
			timeout = r.defaultGateTimeout
		}
		return true, timeout
	}

	// Check policies in reverse order (most recent first)
	for i := len(r.policies) - 1; i >= 0; i-- {
		p := r.policies[i]
		if p.Matches(toolName) {
			timeout := p.ApprovalTimeout
			if timeout <= 0 {
				timeout = r.defaultGateTimeout
			}
			return p.RequiresApproval, timeout
		}
	}

	return false, 0
}

// RegisterLocalTool registers an in-process tool handler with the registry.
func (r *MCPToolRegistry) RegisterLocalTool(def ToolDefinition, handler LocalToolHandler) error {
	name := strings.TrimSpace(def.Name)
	if name == "" {
		return fmt.Errorf("local tool name cannot be empty")
	}
	if handler == nil {
		return fmt.Errorf("local tool handler cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	def.Name = name
	r.localTools[name] = localToolEntry{
		def:     def,
		handler: handler,
	}
	r.toolDefinitions[name] = def
	return nil
}

// UnregisterLocalTool removes an in-process tool from the registry.
func (r *MCPToolRegistry) UnregisterLocalTool(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.localTools[name]; !ok {
		return fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}

	delete(r.localTools, name)
	delete(r.toolDefinitions, name)
	return nil
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
	localDefs := make([]ToolDefinition, 0, len(r.localTools))
	for _, lt := range r.localTools {
		localDefs = append(localDefs, lt.def)
	}
	r.mu.RUnlock()

	var allTools []ToolDefinition
	allTools = append(allTools, localDefs...)
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
	for _, tool := range allTools {
		r.toolDefinitions[tool.Name] = tool
	}
	r.mu.Unlock()

	return allTools, nil
}

// CallTool invokes a tool by name, routing the request to the owning MCP server,
// and enforcing Human-in-the-Loop decision gates when policies require approval.
func (r *MCPToolRegistry) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolResult, error) {
	ec, _ := ExecutionContextFromContext(ctx)
	return r.CallToolWithContext(ctx, ec, name, arguments)
}

// CallToolWithContext invokes a tool with an explicit ExecutionContext.
func (r *MCPToolRegistry) CallToolWithContext(ctx context.Context, ec ExecutionContext, name string, arguments map[string]any) (*ToolResult, error) {
	requiresApproval, gateTimeout := r.IsApprovalRequired(name)
	if requiresApproval {
		if ec.RunID == "" {
			return nil, ErrMissingExecutionContext
		}
		if r.authService == nil {
			return nil, ErrMissingAuthService
		}
		if r.gateway == nil {
			return nil, ErrMissingGateway
		}

		if err := r.interceptToolCall(ctx, ec, name, arguments, gateTimeout); err != nil {
			return nil, err
		}
	}

	r.mu.RLock()
	localEntry, isLocal := r.localTools[name]
	r.mu.RUnlock()

	if isLocal {
		select {
		case r.sem <- struct{}{}:
			defer func() { <-r.sem }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		callTimeout := r.defaultTimeout
		callCtx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()

		return localEntry.handler(callCtx, arguments)
	}

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

func (r *MCPToolRegistry) interceptToolCall(
	ctx context.Context,
	ec ExecutionContext,
	name string,
	arguments map[string]any,
	gateTimeout time.Duration,
) error {
	expiresAt := time.Now().Add(gateTimeout)
	token := r.authService.GenerateApprovalToken(ec.RunID, expiresAt)
	tokenHMACPrefix := auth.ExtractTokenPrefix(token)

	// Register resolver channels
	resCh := make(chan gateResolution, 2)
	r.gateResolvers.Store(token, resCh)
	r.gateResolvers.Store(ec.RunID, resCh)
	defer func() {
		r.gateResolvers.Delete(token)
		r.gateResolvers.Delete(ec.RunID)
	}()

	now := time.Now().UTC()
	// Emit gate.approval_required
	err := r.gateway.Publish(ctx, gateway.TopicGateApprovalRequired, gateway.Event{
		Type:      gateway.TopicGateApprovalRequired,
		RunID:     ec.RunID,
		Timestamp: now.UnixMilli(),
		Payload: map[string]any{
			"run_id":     ec.RunID,
			"tool_name":  name,
			"arguments":  arguments,
			"token":      token,
			"timeout_ms": gateTimeout.Milliseconds(),
			"expires_at": expiresAt.Unix(),
			"caller_id":  ec.CallerID,
			"session_id": ec.SessionID,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to emit gate approval event: %w", err)
	}

	// Emit initial pending gate.audit
	r.recordAudit(auth.AuditRecord{
		Timestamp:       now,
		RunID:           ec.RunID,
		Decision:        "PENDING",
		TokenHMACPrefix: tokenHMACPrefix,
		Status:          "PENDING",
		Details:         fmt.Sprintf("Tool %q requested gate approval", name),
	})

	// Wait for resolution, timeout, or context cancellation
	timer := time.NewTimer(gateTimeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		r.recordAudit(auth.AuditRecord{
			Timestamp:       time.Now().UTC(),
			RunID:           ec.RunID,
			Decision:        "CANCELLED",
			TokenHMACPrefix: tokenHMACPrefix,
			Status:          auth.AuditStatusExpired,
			Details:         fmt.Sprintf("Tool %q approval cancelled by context", name),
		})
		return ctx.Err()

	case <-timer.C:
		r.recordAudit(auth.AuditRecord{
			Timestamp:       time.Now().UTC(),
			RunID:           ec.RunID,
			Decision:        "TIMEOUT",
			TokenHMACPrefix: tokenHMACPrefix,
			Status:          auth.AuditStatusExpired,
			Details:         fmt.Sprintf("Tool %q approval timed out after %v", name, gateTimeout),
		})
		return ErrToolApprovalTimeout

	case res := <-resCh:
		decision := strings.ToLower(strings.TrimSpace(res.decision))
		if decision == "approved" {
			r.recordAudit(auth.AuditRecord{
				Timestamp:       time.Now().UTC(),
				RunID:           ec.RunID,
				Decision:        "approved",
				TokenHMACPrefix: tokenHMACPrefix,
				Status:          auth.AuditStatusSuccess,
				Details:         fmt.Sprintf("Tool %q approved for execution", name),
			})
			return nil
		}

		r.recordAudit(auth.AuditRecord{
			Timestamp:       time.Now().UTC(),
			RunID:           ec.RunID,
			Decision:        "rejected",
			TokenHMACPrefix: tokenHMACPrefix,
			Status:          "REJECTED",
			Details:         fmt.Sprintf("Tool %q rejected by human operator", name),
		})
		return ErrToolExecutionRejected
	}
}

func (r *MCPToolRegistry) recordAudit(rec auth.AuditRecord) {
	if r.auditLogger != nil {
		r.auditLogger.Record(rec)
	}
	if r.gateway != nil {
		_ = r.gateway.Publish(context.Background(), gateway.TopicGateAudit, gateway.Event{
			Type:      gateway.TopicGateAudit,
			RunID:     rec.RunID,
			Timestamp: rec.Timestamp.UnixMilli(),
			Payload: map[string]any{
				"timestamp":         rec.Timestamp.Format(time.RFC3339Nano),
				"run_id":            rec.RunID,
				"decision":          rec.Decision,
				"token_hmac_prefix": rec.TokenHMACPrefix,
				"status":            string(rec.Status),
				"details":           rec.Details,
			},
		})
	}
}

// Close closes all registered servers and cleans up subscriptions.
func (r *MCPToolRegistry) Close() error {
	r.closeOnce.Do(func() {
		close(r.closeCh)
		if r.gwUnsub != nil {
			r.gwUnsub()
		}
	})

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
	r.toolDefinitions = make(map[string]ToolDefinition)
	return lastErr
}
