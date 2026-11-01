// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func createMockClient(tools []ToolDefinition, callHandler func(name string, args map[string]any) (*ToolResult, error)) *Client {
	respChan := make(chan *Response, 10)
	tr := &mockTransport{
		receiveFunc: func(ctx context.Context) (*Response, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case r, ok := <-respChan:
				if !ok {
					return nil, ErrTransportClosed
				}
				return r, nil
			}
		},
		sendFunc: func(ctx context.Context, req *Request) error {
			if req.IsNotification() {
				return nil
			}
			go func() {
				if req.Method == MethodToolsList {
					res := ListToolsResult{Tools: tools}
					b, _ := json.Marshal(res)
					respChan <- &Response{
						JSONRPC: JSONRPCVersion,
						ID:      req.ID,
						Result:  b,
					}
				} else if req.Method == MethodToolsCall {
					var p CallToolParams
					_ = json.Unmarshal(req.Params, &p)
					var res *ToolResult
					var err error
					if callHandler != nil {
						res, err = callHandler(p.Name, p.Arguments)
					}
					if err != nil {
						respChan <- &Response{
							JSONRPC: JSONRPCVersion,
							ID:      req.ID,
							Error: &RPCError{
								Code:    CodeInternalError,
								Message: err.Error(),
							},
						}
					} else {
						b, _ := json.Marshal(res)
						respChan <- &Response{
							JSONRPC: JSONRPCVersion,
							ID:      req.ID,
							Result:  b,
						}
					}
				}
			}()
			return nil
		},
	}
	return NewClient(tr)
}

func TestMCPToolRegistry(t *testing.T) {
	registry := NewMCPToolRegistry(RegistryConfig{
		DefaultTimeout: 2 * time.Second,
		MaxTimeout:     5 * time.Second,
		MaxConcurrent:  5,
	})
	defer registry.Close()

	// Server 1 with tool "git_status"
	client1 := createMockClient(
		[]ToolDefinition{
			{Name: "git_status", Description: "Returns git status"},
		},
		func(name string, args map[string]any) (*ToolResult, error) {
			return &ToolResult{
				Content: []ContentBlock{{Type: "text", Text: "clean"}},
			}, nil
		},
	)

	// Server 2 with tool "linter"
	client2 := createMockClient(
		[]ToolDefinition{
			{Name: "linter", Description: "Lints code"},
		},
		func(name string, args map[string]any) (*ToolResult, error) {
			return &ToolResult{
				Content: []ContentBlock{{Type: "text", Text: "0 issues found"}},
			}, nil
		},
	)

	// Register servers
	if err := registry.RegisterServer("git-server", client1, time.Second); err != nil {
		t.Fatalf("RegisterServer 1 failed: %v", err)
	}
	if err := registry.RegisterServer("lint-server", client2, time.Second); err != nil {
		t.Fatalf("RegisterServer 2 failed: %v", err)
	}

	ctx := context.Background()

	// List Tools
	tools, err := registry.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("Expected 2 tools, got %d", len(tools))
	}

	// Call tool 1
	res1, err := registry.CallTool(ctx, "git_status", nil)
	if err != nil {
		t.Fatalf("CallTool git_status failed: %v", err)
	}
	if res1.TextContent() != "clean" {
		t.Errorf("Unexpected tool 1 result: %s", res1.TextContent())
	}

	// Call tool 2
	res2, err := registry.CallTool(ctx, "linter", nil)
	if err != nil {
		t.Fatalf("CallTool linter failed: %v", err)
	}
	if res2.TextContent() != "0 issues found" {
		t.Errorf("Unexpected tool 2 result: %s", res2.TextContent())
	}

	// Call non-existent tool
	_, err = registry.CallTool(ctx, "unknown_tool", nil)
	if !errors.Is(err, ErrToolNotFound) {
		t.Errorf("Expected ErrToolNotFound, got: %v", err)
	}

	// Unregister Server
	if err := registry.UnregisterServer("git-server"); err != nil {
		t.Fatalf("UnregisterServer failed: %v", err)
	}

	// Calling tool from unregistered server
	_, err = registry.CallTool(ctx, "git_status", nil)
	if err == nil {
		t.Errorf("Expected error after unregistering server")
	}

	// Test max timeout capping
	if err := registry.RegisterServer("srv-capped", client2, 100*time.Hour); err != nil {
		t.Fatalf("RegisterServer capped failed: %v", err)
	}

	// Test CallTool timeout context
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := registry.CallTool(cancelCtx, "git_status", nil); err == nil {
		t.Errorf("Expected context canceled error")
	}

	// CallTool with explicit ExecutionContext
	ecExplicit := ExecutionContext{RunID: "run-explicit"}
	resExplicit, err := registry.CallToolWithContext(context.Background(), ecExplicit, "linter", nil)
	if err != nil || resExplicit.TextContent() != "0 issues found" {
		t.Fatalf("CallToolWithContext failed: %v", err)
	}

	// Close registry twice to test closeOnce
	if err := registry.Close(); err != nil {
		t.Fatalf("registry.Close failed: %v", err)
	}
}

func TestRegistryValidationAndDefaults(t *testing.T) {
	registry := NewMCPToolRegistry(RegistryConfig{})

	// Empty name
	if err := registry.RegisterServer("", nil, 0); err == nil {
		t.Errorf("Expected error for empty server name")
	}

	// Nil client
	if err := registry.RegisterServer("srv", nil, 0); err == nil {
		t.Errorf("Expected error for nil client")
	}
}
