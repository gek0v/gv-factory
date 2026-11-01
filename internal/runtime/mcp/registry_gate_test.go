// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package mcp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/gateway"
)

func setupTestMCPWithGate(t *testing.T) (*MCPToolRegistry, *auth.Service, *gateway.Gateway, *Client) {
	t.Helper()
	authSvc := auth.NewService("test-mcp-secret-key-123")
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})

	mockClient := createMockClient(
		[]ToolDefinition{
			{Name: "safe_tool", Description: "Safe read operation"},
			{Name: "exec_shell", Description: "Protected command execution"},
			{Name: "fs_delete", Description: "Protected file deletion"},
			{Name: "fs_write", Description: "Protected file write"},
		},
		func(name string, args map[string]any) (*ToolResult, error) {
			return &ToolResult{
				Content: []ContentBlock{{Type: "text", Text: "executed: " + name}},
			}, nil
		},
	)

	reg := NewMCPToolRegistry(RegistryConfig{
		DefaultTimeout:     2 * time.Second,
		MaxTimeout:         5 * time.Second,
		DefaultGateTimeout: 3 * time.Second,
		AuthService:        authSvc,
		Gateway:            gw,
		AuditLogger:        authSvc.AuditLogger(),
	})

	if err := reg.RegisterServer("main-server", mockClient, 2*time.Second); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	return reg, authSvc, gw, mockClient
}

func TestMCPToolRegistry_SafeToolExecution(t *testing.T) {
	reg, _, gw, _ := setupTestMCPWithGate(t)
	defer reg.Close()

	// Subscribe to gate.approval_required to ensure it is NOT called
	approvalCh := make(chan gateway.Event, 10)
	unsub, err := gw.SubscribeTaskEvents(context.Background(), gateway.TopicGateApprovalRequired, approvalCh)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	defer unsub()

	ctx := context.Background()
	res, err := reg.CallTool(ctx, "safe_tool", nil)
	if err != nil {
		t.Fatalf("CallTool safe_tool failed: %v", err)
	}
	if res.TextContent() != "executed: safe_tool" {
		t.Errorf("unexpected result: %s", res.TextContent())
	}

	select {
	case ev := <-approvalCh:
		t.Fatalf("safe tool should not trigger gate.approval_required, received: %v", ev)
	case <-time.After(100 * time.Millisecond):
		// Success: no event emitted
	}
}

func TestMCPToolRegistry_MissingExecutionContext(t *testing.T) {
	reg, _, _, _ := setupTestMCPWithGate(t)
	defer reg.Close()

	reg.RegisterProtectedTool("exec_shell", time.Second)

	// Call protected tool without ExecutionContext
	ctx := context.Background()
	_, err := reg.CallTool(ctx, "exec_shell", nil)
	if !errors.Is(err, ErrMissingExecutionContext) {
		t.Fatalf("expected ErrMissingExecutionContext, got: %v", err)
	}

	// Call with empty RunID
	ctxWithEmptyRunID := WithExecutionContext(ctx, ExecutionContext{RunID: ""})
	_, err = reg.CallTool(ctxWithEmptyRunID, "exec_shell", nil)
	if !errors.Is(err, ErrMissingExecutionContext) {
		t.Fatalf("expected ErrMissingExecutionContext for empty RunID, got: %v", err)
	}
}

func TestMCPToolRegistry_MissingAuthOrGateway(t *testing.T) {
	mockClient := createMockClient(
		[]ToolDefinition{
			{Name: "exec_shell", Description: "Protected command execution"},
		},
		func(name string, args map[string]any) (*ToolResult, error) {
			return &ToolResult{Content: []ContentBlock{{Type: "text", Text: "ok"}}}, nil
		},
	)

	// Registry without AuthService
	regNoAuth := NewMCPToolRegistry(RegistryConfig{
		Gateway: gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true}),
	})
	defer regNoAuth.Close()
	_ = regNoAuth.RegisterServer("srv", mockClient, time.Second)
	regNoAuth.RegisterProtectedTool("exec_shell", time.Second)

	ctx := WithExecutionContext(context.Background(), ExecutionContext{RunID: "run-1"})
	_, err := regNoAuth.CallTool(ctx, "exec_shell", nil)
	if !errors.Is(err, ErrMissingAuthService) {
		t.Fatalf("expected ErrMissingAuthService, got: %v", err)
	}

	// Registry without Gateway
	regNoGW := NewMCPToolRegistry(RegistryConfig{
		AuthService: auth.NewService("secret"),
	})
	defer regNoGW.Close()
	_ = regNoGW.RegisterServer("srv", mockClient, time.Second)
	regNoGW.RegisterProtectedTool("exec_shell", time.Second)

	_, err = regNoGW.CallTool(ctx, "exec_shell", nil)
	if !errors.Is(err, ErrMissingGateway) {
		t.Fatalf("expected ErrMissingGateway, got: %v", err)
	}
}

func TestMCPToolRegistry_ProtectedTool_Approved(t *testing.T) {
	reg, authSvc, gw, _ := setupTestMCPWithGate(t)
	defer reg.Close()

	// Protect tool using wildcard policy
	reg.SetToolPolicy(ToolSecurityPolicy{
		NamePattern:      "fs_*",
		RequiresApproval: true,
		ApprovalTimeout:  2 * time.Second,
	})

	runID := "run-approved-123"
	ec := ExecutionContext{
		RunID:     runID,
		CallerID:  "agent-reviewer",
		SessionID: "sess-abc",
	}
	ctx := WithExecutionContext(context.Background(), ec)

	approvalCh := make(chan gateway.Event, 10)
	unsub, err := gw.SubscribeTaskEvents(context.Background(), gateway.TopicGateApprovalRequired, approvalCh)
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	defer unsub()

	// Launch CallTool in background goroutine
	doneCh := make(chan struct{})
	var callResult *ToolResult
	var callErr error

	go func() {
		defer close(doneCh)
		callResult, callErr = reg.CallTool(ctx, "fs_write", map[string]any{"path": "/tmp/test.txt"})
	}()

	// Wait for gate.approval_required
	var approvalEv gateway.Event
	select {
	case approvalEv = <-approvalCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for gate.approval_required")
	}

	if approvalEv.RunID != runID {
		t.Fatalf("expected runID %s, got %s", runID, approvalEv.RunID)
	}
	token, ok := approvalEv.Payload["token"].(string)
	if !ok || token == "" {
		t.Fatalf("missing token in payload: %v", approvalEv.Payload)
	}

	// Verify token is cryptographically valid
	tokenRunID, valid := authSvc.ValidateApprovalToken(token)
	if !valid || tokenRunID != runID {
		t.Fatalf("invalid HMAC token generated: valid=%v, tokenRunID=%s", valid, tokenRunID)
	}

	// Simulate external resolution -> publish gate.resolved (decision: approved)
	err = gw.Publish(context.Background(), gateway.TopicGateResolved, gateway.Event{
		Type:      gateway.TopicGateResolved,
		RunID:     runID,
		Timestamp: time.Now().UnixMilli(),
		Payload: map[string]any{
			"run_id":   runID,
			"token":    token,
			"decision": "approved",
		},
	})
	if err != nil {
		t.Fatalf("failed to publish gate.resolved: %v", err)
	}

	// Wait for CallTool to complete
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("CallTool did not unblock after approved resolution")
	}

	if callErr != nil {
		t.Fatalf("CallTool failed: %v", callErr)
	}
	if callResult.TextContent() != "executed: fs_write" {
		t.Fatalf("unexpected result: %s", callResult.TextContent())
	}
}

func TestMCPToolRegistry_ProtectedTool_Rejected(t *testing.T) {
	reg, _, gw, _ := setupTestMCPWithGate(t)
	defer reg.Close()

	reg.RegisterProtectedTool("exec_shell", 2*time.Second)

	runID := "run-rejected-456"
	ec := ExecutionContext{
		RunID:     runID,
		CallerID:  "agent-danger",
		SessionID: "sess-xyz",
	}
	ctx := WithExecutionContext(context.Background(), ec)

	approvalCh := make(chan gateway.Event, 10)
	unsub, err := gw.SubscribeTaskEvents(context.Background(), gateway.TopicGateApprovalRequired, approvalCh)
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	defer unsub()

	doneCh := make(chan struct{})
	var callResult *ToolResult
	var callErr error

	go func() {
		defer close(doneCh)
		callResult, callErr = reg.CallTool(ctx, "exec_shell", map[string]any{"cmd": "rm -rf /"})
	}()

	var approvalEv gateway.Event
	select {
	case approvalEv = <-approvalCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for gate.approval_required")
	}

	token := approvalEv.Payload["token"].(string)

	// Publish rejected decision
	err = gw.Publish(context.Background(), gateway.TopicGateResolved, gateway.Event{
		Type:      gateway.TopicGateResolved,
		RunID:     runID,
		Timestamp: time.Now().UnixMilli(),
		Payload: map[string]any{
			"run_id":   runID,
			"token":    token,
			"decision": "rejected",
		},
	})
	if err != nil {
		t.Fatalf("publish rejected failed: %v", err)
	}

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("CallTool did not unblock after rejected resolution")
	}

	if !errors.Is(callErr, ErrToolExecutionRejected) {
		t.Fatalf("expected ErrToolExecutionRejected, got: %v", callErr)
	}
	if callResult != nil {
		t.Fatalf("expected nil result on rejection, got: %v", callResult)
	}
}

func TestMCPToolRegistry_ProtectedTool_Timeout(t *testing.T) {
	reg, _, _, _ := setupTestMCPWithGate(t)
	defer reg.Close()

	// Short timeout: 150ms
	reg.RegisterProtectedTool("exec_shell", 150*time.Millisecond)

	runID := "run-timeout-789"
	ctx := WithExecutionContext(context.Background(), ExecutionContext{RunID: runID})

	start := time.Now()
	_, err := reg.CallTool(ctx, "exec_shell", nil)
	duration := time.Since(start)

	if !errors.Is(err, ErrToolApprovalTimeout) {
		t.Fatalf("expected ErrToolApprovalTimeout, got: %v", err)
	}
	if duration < 100*time.Millisecond {
		t.Fatalf("duration too short for timeout: %v", duration)
	}
}

func TestMCPToolRegistry_ProtectedTool_ContextCancellation(t *testing.T) {
	reg, _, _, _ := setupTestMCPWithGate(t)
	defer reg.Close()

	reg.RegisterProtectedTool("exec_shell", 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	ctx = WithExecutionContext(ctx, ExecutionContext{RunID: "run-cancel"})

	doneCh := make(chan struct{})
	var callErr error

	go func() {
		defer close(doneCh)
		_, callErr = reg.CallTool(ctx, "exec_shell", nil)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-doneCh:
	case <-time.After(time.Second):
		t.Fatal("CallTool did not abort on context cancellation")
	}

	if !errors.Is(callErr, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", callErr)
	}
}

func TestMCPToolRegistry_ConcurrentApprovals(t *testing.T) {
	reg, _, gw, _ := setupTestMCPWithGate(t)
	defer reg.Close()

	reg.RegisterProtectedTool("exec_shell", 3*time.Second)

	concurrency := 10
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			runID := fmt.Sprintf("run-concurrent-%d", idx)
			ctx := WithExecutionContext(context.Background(), ExecutionContext{RunID: runID})

			// Goroutine to resolve after slight delay
			go func() {
				// We need to wait a bit then resolve
				time.Sleep(30 * time.Millisecond)
				_ = gw.Publish(context.Background(), gateway.TopicGateResolved, gateway.Event{
					Type:      gateway.TopicGateResolved,
					RunID:     runID,
					Timestamp: time.Now().UnixMilli(),
					Payload: map[string]any{
						"run_id":   runID,
						"decision": "approved",
					},
				})
			}()

			res, err := reg.CallTool(ctx, "exec_shell", nil)
			if err != nil {
				t.Errorf("worker %d failed: %v", idx, err)
				return
			}
			if res.TextContent() != "executed: exec_shell" {
				t.Errorf("worker %d unexpected result: %s", idx, res.TextContent())
			}
		}(i)
	}

	wg.Wait()
}
