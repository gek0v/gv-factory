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

type mockTransport struct {
	sendFunc    func(ctx context.Context, req *Request) error
	receiveFunc func(ctx context.Context) (*Response, error)
	closeFunc   func() error
}

func (m *mockTransport) Send(ctx context.Context, req *Request) error {
	if m.sendFunc != nil {
		return m.sendFunc(ctx, req)
	}
	return nil
}

func (m *mockTransport) Receive(ctx context.Context) (*Response, error) {
	if m.receiveFunc != nil {
		return m.receiveFunc(ctx)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (m *mockTransport) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func TestClientLifecycleAndMethods(t *testing.T) {
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
				// Generate mock response according to method
				switch req.Method {
				case MethodInitialize:
					res := InitializeResult{
						ProtocolVersion: LatestProtocolVersion,
						ServerInfo: ImplementationInfo{
							Name:    "unit-test-server",
							Version: "2.0",
						},
						Capabilities: ServerCapabilities{},
					}
					b, _ := json.Marshal(res)
					respChan <- &Response{
						JSONRPC: JSONRPCVersion,
						ID:      req.ID,
						Result:  b,
					}
				case MethodPing:
					respChan <- &Response{
						JSONRPC: JSONRPCVersion,
						ID:      req.ID,
						Result:  json.RawMessage(`{}`),
					}
				case MethodToolsList:
					res := ListToolsResult{
						Tools: []ToolDefinition{
							{
								Name:        "calculator",
								Description: "Performs math",
							},
						},
					}
					b, _ := json.Marshal(res)
					respChan <- &Response{
						JSONRPC: JSONRPCVersion,
						ID:      req.ID,
						Result:  b,
					}
				case MethodToolsCall:
					res := ToolResult{
						Content: []ContentBlock{
							{Type: "text", Text: "42"},
						},
					}
					b, _ := json.Marshal(res)
					respChan <- &Response{
						JSONRPC: JSONRPCVersion,
						ID:      req.ID,
						Result:  b,
					}
				case "error_method":
					respChan <- &Response{
						JSONRPC: JSONRPCVersion,
						ID:      req.ID,
						Error: &RPCError{
							Code:    CodeInternalError,
							Message: "mock internal error",
						},
					}
				}
			}()
			return nil
		},
	}

	client := NewClient(tr)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 1. Initialize
	initRes, err := client.Initialize(ctx, ImplementationInfo{})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if initRes.ServerInfo.Name != "unit-test-server" {
		t.Errorf("Unexpected server name: %s", initRes.ServerInfo.Name)
	}
	if client.ServerInfo().Name != "unit-test-server" {
		t.Errorf("Cached server info mismatch: %s", client.ServerInfo().Name)
	}
	_ = client.ServerCapabilities()

	// 2. Ping
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}

	// 3. ListTools
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "calculator" {
		t.Fatalf("ListTools mismatch: %+v", tools)
	}

	// 4. CallTool
	toolRes, err := client.CallTool(ctx, "calculator", map[string]any{"op": "add"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if toolRes.TextContent() != "42" {
		t.Errorf("Unexpected call result: %s", toolRes.TextContent())
	}

	// 5. Error Response
	_, err = client.Request(ctx, "error_method", nil)
	if err == nil {
		t.Fatalf("Expected error response")
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Errorf("Expected RPCError, got %T: %v", err, err)
	}
}

func TestClientContextTimeout(t *testing.T) {
	tr := &mockTransport{
		receiveFunc: func(ctx context.Context) (*Response, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
		sendFunc: func(ctx context.Context, req *Request) error {
			// Don't respond to simulate timeout
			return nil
		},
	}

	client := NewClient(tr)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Request(ctx, MethodPing, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected DeadlineExceeded, got: %v", err)
	}
}

func TestClientNotificationsAndClose(t *testing.T) {
	respChan := make(chan *Response, 5)
	tr := &mockTransport{
		receiveFunc: func(ctx context.Context) (*Response, error) {
			r, ok := <-respChan
			if !ok {
				return nil, ErrTransportClosed
			}
			return r, nil
		},
		sendFunc: func(ctx context.Context, req *Request) error {
			return nil
		},
	}

	client := NewClient(tr)

	// Send notification from transport to client
	respChan <- &Response{
		JSONRPC: JSONRPCVersion,
		ID:      nil, // Notification
	}

	time.Sleep(10 * time.Millisecond)

	ctx := context.Background()
	// Notify from client
	if err := client.Notify(ctx, "custom/notify", map[string]string{"foo": "bar"}); err != nil {
		t.Fatalf("Notify failed: %v", err)
	}

	// Close client
	if err := client.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Try Request on closed client
	if _, err := client.Request(ctx, MethodPing, nil); !errors.Is(err, ErrClientClosed) {
		t.Errorf("Expected ErrClientClosed, got: %v", err)
	}

	// Try Notify on closed client
	if err := client.Notify(ctx, "test", nil); !errors.Is(err, ErrClientClosed) {
		t.Errorf("Expected ErrClientClosed, got: %v", err)
	}

	// Test close with error
	client2 := NewClient(tr)
	client2.closeWithError(errors.New("simulated fatal error"))
	if _, err := client2.Request(ctx, MethodPing, nil); !errors.Is(err, ErrClientClosed) {
		t.Errorf("Expected ErrClientClosed, got: %v", err)
	}
}
