// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/gateway"
)

type failGateway struct {
	GateEventBus
}

func (f *failGateway) Publish(ctx context.Context, topic string, ev gateway.Event) error {
	return errors.New("network failure on publish")
}

func TestMCPToolRegistry_GatewayPublishFailure(t *testing.T) {
	authSvc := auth.NewService("test-key")
	realGW := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer realGW.Close()

	reg := NewMCPToolRegistry(RegistryConfig{
		AuthService: authSvc,
		Gateway:     &failGateway{GateEventBus: realGW},
	})
	defer reg.Close()

	mockClient := createMockClient([]ToolDefinition{
		{Name: "protected_cmd", RequiresApproval: true, ApprovalTimeout: time.Second},
	}, nil)
	_ = reg.RegisterServer("srv", mockClient, time.Second)
	_, _ = reg.ListTools(context.Background())

	// Call protected tool
	ctx := WithExecutionContext(context.Background(), ExecutionContext{RunID: "run-pub-fail"})
	_, err := reg.CallTool(ctx, "protected_cmd", nil)
	if err == nil || !errors.Is(err, errors.New("failed to emit gate approval event")) && err.Error() != "failed to emit gate approval event: network failure on publish" {
		t.Fatalf("expected network failure, got %v", err)
	}
}

func TestMCPToolRegistry_ToolDefinitionApproval(t *testing.T) {
	authSvc := auth.NewService("test-key")
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	reg := NewMCPToolRegistry(RegistryConfig{
		AuthService: authSvc,
		Gateway:     gw,
	})
	defer reg.Close()

	mockClient := createMockClient([]ToolDefinition{
		{Name: "custom_protected", RequiresApproval: true, ApprovalTimeout: 5 * time.Second},
	}, func(name string, args map[string]any) (*ToolResult, error) {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: "done"}}}, nil
	})
	_ = reg.RegisterServer("srv", mockClient, time.Second)

	// ListTools populates toolDefinitions
	_, err := reg.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	// Test policy with empty pattern and description
	emptyPatternPolicy := ToolSecurityPolicy{
		NamePattern: "",
	}
	if emptyPatternPolicy.Matches("any_tool") {
		t.Errorf("empty pattern should not match")
	}

	// Tool with default gate timeout
	reg.SetToolPolicy(ToolSecurityPolicy{
		NamePattern:      "default_timeout_tool",
		RequiresApproval: true,
	})
	isReq, defaultTimeout := reg.IsApprovalRequired("default_timeout_tool")
	if !isReq || defaultTimeout <= 0 {
		t.Fatalf("expected isReq=true with default timeout, got %v, %v", isReq, defaultTimeout)
	}

	// Unregistered tool approval check
	isReq, _ = reg.IsApprovalRequired("completely_unregistered_tool")
	if isReq {
		t.Fatalf("expected isReq=false for unregistered tool")
	}

	// Tool with custom malformed pattern ending in *
	malformedPolicy := ToolSecurityPolicy{
		NamePattern:      "[a-*",
		RequiresApproval: true,
	}
	if !malformedPolicy.Matches("[a-test") {
		t.Errorf("malformed prefix pattern should match via prefix fallback")
	}
	if malformedPolicy.Matches("other_tool") {
		t.Errorf("malformed prefix pattern should not match unrelated tool")
	}
}

func TestClient_EdgeCases(t *testing.T) {
	mockClient := createMockClient(nil, nil)

	// Context canceled on Request
	ctxCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := mockClient.Request(ctxCanceled, "test", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Request, got %v", err)
	}

	// Unmarshalable params on Request
	_, err = mockClient.Request(context.Background(), "test", make(chan int))
	if err == nil {
		t.Fatalf("expected marshal error on Request")
	}

	// Context canceled on Notify
	err = mockClient.Notify(ctxCanceled, "test", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Notify, got %v", err)
	}

	// Unmarshalable params on Notify
	err = mockClient.Notify(context.Background(), "test", make(chan int))
	if err == nil {
		t.Fatalf("expected marshal error on Notify")
	}

	// Closed client Request and Notify
	_ = mockClient.Close()
	_, err = mockClient.Request(context.Background(), "test", nil)
	if !errors.Is(err, ErrClientClosed) {
		t.Fatalf("expected ErrClientClosed on Request, got %v", err)
	}

	err = mockClient.Notify(context.Background(), "test", nil)
	if !errors.Is(err, ErrClientClosed) {
		t.Fatalf("expected ErrClientClosed on Notify, got %v", err)
	}
}

func TestHTTPTransport_ReceiveErrors(t *testing.T) {
	trans := &HTTPTransport{
		closed:     make(chan struct{}),
		eventsChan: make(chan *Response),
		errChan:    make(chan error, 1),
	}

	// Test error channel
	expectedErr := errors.New("stream failure")
	trans.errChan <- expectedErr
	_, err := trans.Receive(context.Background())
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected stream failure, got %v", err)
	}

	// Test context done
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = trans.Receive(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// Test transport closed
	close(trans.closed)
	_, err = trans.Receive(context.Background())
	if !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("expected ErrTransportClosed, got %v", err)
	}
}
