// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Client represents an active connection to an MCP server.
type Client struct {
	transport Transport

	nextID     atomic.Uint64
	pendingMu  sync.Mutex
	pending    map[string]chan *Response
	notifCh    chan *Response

	serverInfo ImplementationInfo
	serverCaps ServerCapabilities

	closeOnce sync.Once
	closed    chan struct{}
	errChan   chan error
}

// ClientOptions configures the MCP client.
type ClientOptions struct {
	ClientInfo ImplementationInfo
	Timeout    time.Duration
}

// NewClient wraps a transport into an active MCP client and starts its background receiver.
func NewClient(transport Transport) *Client {
	c := &Client{
		transport: transport,
		pending:   make(map[string]chan *Response),
		notifCh:   make(chan *Response, 100),
		closed:    make(chan struct{}),
		errChan:   make(chan error, 1),
	}

	go c.readLoop()

	return c
}

// readLoop reads incoming messages from the transport and routes them.
func (c *Client) readLoop() {
	for {
		select {
		case <-c.closed:
			return
		default:
		}

		resp, err := c.transport.Receive(context.Background())
		if err != nil {
			c.closeWithError(err)
			return
		}

		if resp == nil {
			continue
		}

		if resp.ID == nil {
			// Notification
			select {
			case c.notifCh <- resp:
			default:
			}
			continue
		}

		idKey := resp.ID.String()
		c.pendingMu.Lock()
		ch, exists := c.pending[idKey]
		if exists {
			delete(c.pending, idKey)
		}
		c.pendingMu.Unlock()

		if exists && ch != nil {
			ch <- resp
			close(ch)
		}
	}
}

func (c *Client) closeWithError(err error) {
	c.closeOnce.Do(func() {
		close(c.closed)
		select {
		case c.errChan <- err:
		default:
		}

		// Fail all pending requests
		c.pendingMu.Lock()
		for _, ch := range c.pending {
			close(ch)
		}
		c.pending = make(map[string]chan *Response)
		c.pendingMu.Unlock()

		_ = c.transport.Close()
	})
}

// Close closes the client and underlying transport.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)

		c.pendingMu.Lock()
		for _, ch := range c.pending {
			close(ch)
		}
		c.pending = make(map[string]chan *Response)
		c.pendingMu.Unlock()

		_ = c.transport.Close()
	})
	return nil
}

// Request sends a request and waits for its response matching the ID.
func (c *Client) Request(ctx context.Context, method string, params any) (*Response, error) {
	select {
	case <-c.closed:
		return nil, ErrClientClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	idNum := c.nextID.Add(1)
	reqID := NewIntID(idNum)

	var paramsRaw json.RawMessage
	if params != nil {
		pBytes, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("mcp client: failed to marshal params: %w", err)
		}
		paramsRaw = pBytes
	}

	req := &Request{
		JSONRPC: JSONRPCVersion,
		ID:      reqID,
		Method:  method,
		Params:  paramsRaw,
	}

	ch := make(chan *Response, 1)
	c.pendingMu.Lock()
	c.pending[reqID.String()] = ch
	c.pendingMu.Unlock()

	if err := c.transport.Send(ctx, req); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, reqID.String())
		c.pendingMu.Unlock()
		return nil, err
	}

	select {
	case <-c.closed:
		return nil, ErrClientClosed
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, reqID.String())
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok || resp == nil {
			return nil, ErrTransportClosed
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp, nil
	}
}

// Notify sends a one-way notification without waiting for a response.
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	select {
	case <-c.closed:
		return ErrClientClosed
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	var paramsRaw json.RawMessage
	if params != nil {
		pBytes, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("mcp client: failed to marshal params: %w", err)
		}
		paramsRaw = pBytes
	}

	req := &Request{
		JSONRPC: JSONRPCVersion,
		ID:      nil, // Notification has no ID
		Method:  method,
		Params:  paramsRaw,
	}

	return c.transport.Send(ctx, req)
}

// Initialize performs the MCP initialization handshake.
func (c *Client) Initialize(ctx context.Context, clientInfo ImplementationInfo) (*InitializeResult, error) {
	if clientInfo.Name == "" {
		clientInfo.Name = "gv-factory-mcp-client"
	}
	if clientInfo.Version == "" {
		clientInfo.Version = "1.0.0"
	}

	params := InitializeParams{
		ProtocolVersion: LatestProtocolVersion,
		Capabilities:    ClientCapabilities{},
		ClientInfo:      clientInfo,
	}

	resp, err := c.Request(ctx, MethodInitialize, params)
	if err != nil {
		return nil, fmt.Errorf("mcp init handshake failed: %w", err)
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("mcp failed to unmarshal initialize result: %w", err)
	}

	c.serverInfo = result.ServerInfo
	c.serverCaps = result.Capabilities

	// Send notifications/initialized as required by the MCP specification
	if err := c.Notify(ctx, MethodInitialized, map[string]any{}); err != nil {
		return nil, fmt.Errorf("mcp failed to send initialized notification: %w", err)
	}

	return &result, nil
}

// Ping checks server health.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Request(ctx, MethodPing, map[string]any{})
	return err
}

// ListTools retrieves the tools exposed by the server.
func (c *Client) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	resp, err := c.Request(ctx, MethodToolsList, map[string]any{})
	if err != nil {
		return nil, err
	}

	var result ListToolsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tools/list result: %w", err)
	}

	return result.Tools, nil
}

// CallTool executes a tool on the server with the given arguments.
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolResult, error) {
	params := CallToolParams{
		Name:      name,
		Arguments: arguments,
	}

	resp, err := c.Request(ctx, MethodToolsCall, params)
	if err != nil {
		return nil, err
	}

	var result ToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tools/call result: %w", err)
	}

	return &result, nil
}

// ServerInfo returns the cached server info from initialization.
func (c *Client) ServerInfo() ImplementationInfo {
	return c.serverInfo
}

// ServerCapabilities returns the cached server capabilities.
func (c *Client) ServerCapabilities() ServerCapabilities {
	return c.serverCaps
}
