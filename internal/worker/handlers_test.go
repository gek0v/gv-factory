// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/sandbox"
	pdk "github.com/gv-factory/gv-factory/plugins-sdk/go"
)

// mockWasmCaller implements WasmCaller for testing
type mockWasmCaller struct {
	callFunc    func(ctx context.Context, pluginRef string, functionName string, input []byte) ([]byte, error)
	callPDKFunc func(ctx context.Context, pluginRef string, functionName string, req pdk.Context) (*pdk.Result, error)
}

func (m *mockWasmCaller) Call(ctx context.Context, pluginRef string, functionName string, input []byte) ([]byte, error) {
	if m.callFunc != nil {
		return m.callFunc(ctx, pluginRef, functionName, input)
	}
	return []byte("mock output"), nil
}

func (m *mockWasmCaller) CallPDK(ctx context.Context, pluginRef string, functionName string, req pdk.Context) (*pdk.Result, error) {
	if m.callPDKFunc != nil {
		return m.callPDKFunc(ctx, pluginRef, functionName, req)
	}
	return &pdk.Result{
		Success: true,
		Output:  "pdk output",
		Data:    map[string]any{"key": "val"},
	}, nil
}

// mockSandboxManager implements SandboxManager for testing
type mockSandboxManager struct {
	worktreePath  string
	env           *sandbox.Environment
	provisionFunc func(ctx context.Context, runID string, opts sandbox.WorktreeOptions) (*sandbox.Environment, error)
	recycleCalled bool
	destroyCalled bool
}

func (m *mockSandboxManager) ProvisionWorktreeWithOptions(ctx context.Context, runID string, opts sandbox.WorktreeOptions) (*sandbox.Environment, error) {
	if m.provisionFunc != nil {
		return m.provisionFunc(ctx, runID, opts)
	}
	env := &sandbox.Environment{
		RunID:        runID,
		WorktreePath: m.worktreePath,
		Branch:       opts.Branch,
	}
	m.env = env
	return env, nil
}

func (m *mockSandboxManager) GetWorktreePath(runID string) (string, bool) {
	if m.worktreePath != "" {
		return m.worktreePath, true
	}
	return "", false
}

func (m *mockSandboxManager) GetEnvironment(runID string) (*sandbox.Environment, bool) {
	if m.env != nil {
		return m.env, true
	}
	return nil, false
}

func (m *mockSandboxManager) RecycleWorktree(ctx context.Context, runID string) error {
	m.recycleCalled = true
	return nil
}

func (m *mockSandboxManager) Destroy(ctx context.Context, runID string) error {
	m.destroyCalled = true
	return nil
}

// mockCommandExecutor implements CommandExecutor for testing
type mockCommandExecutor struct {
	executeFunc func(ctx context.Context, dir string, name string, args []string, env []string) (string, string, int, error)
}

func (m *mockCommandExecutor) ExecuteCommand(ctx context.Context, dir string, name string, args []string, env []string) (string, string, int, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, dir, name, args, env)
	}
	return "mock stdout", "", 0, nil
}

// --- Tests for WasmPluginHandler ---

func TestWasmPluginHandler_NilCaller(t *testing.T) {
	handler := NewWasmPluginHandler(nil)
	task := &TaskMessage{
		TaskID:   "t1",
		RunID:    "r1",
		StepID:   "s1",
		TaskType: TaskTypeWasmPlugin,
	}
	_, err := handler.HandleTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error with nil caller, got nil")
	}
}

func TestWasmPluginHandler_EmptyPayload(t *testing.T) {
	caller := &mockWasmCaller{}
	handler := NewWasmPluginHandler(caller)

	task := &TaskMessage{
		TaskID:   "t-empty",
		RunID:    "r-empty",
		StepID:   "s-empty",
		TaskType: TaskTypeWasmPlugin,
		Payload:  nil,
	}
	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error for empty payload: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success for empty payload no-op, got %v", res.Success)
	}
}

func TestWasmPluginHandler_InvalidJSONPayload(t *testing.T) {
	caller := &mockWasmCaller{}
	handler := NewWasmPluginHandler(caller)

	task := &TaskMessage{
		TaskID:   "t-invalid",
		RunID:    "r-invalid",
		StepID:   "s-invalid",
		TaskType: TaskTypeWasmPlugin,
		Payload:  json.RawMessage(`{not-valid-json`),
	}
	_, err := handler.HandleTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error for invalid JSON payload, got nil")
	}
}

func TestWasmPluginHandler_CallPDK_Success(t *testing.T) {
	var capturedRef, capturedFn string
	var capturedReq pdk.Context

	caller := &mockWasmCaller{
		callPDKFunc: func(ctx context.Context, pluginRef string, functionName string, req pdk.Context) (*pdk.Result, error) {
			capturedRef = pluginRef
			capturedFn = functionName
			capturedReq = req
			return &pdk.Result{
				Success: true,
				Output:  "pdk completed successfully",
				Data:    map[string]any{"computed": 42},
			}, nil
		},
	}

	handler := NewWasmPluginHandler(caller)
	payload, _ := json.Marshal(WasmPluginPayload{
		PluginURI:  "plugins/calc.wasm",
		Entrypoint: "execute",
		Configs:    map[string]string{"precision": "high"},
		Arguments:  map[string]any{"rate": "1.5"},
	})

	task := &TaskMessage{
		TaskID:   "t-pdk",
		RunID:    "r-pdk",
		StepID:   "s-pdk",
		TaskType: TaskTypeWasmPlugin,
		Payload:  payload,
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got false")
	}
	if res.Output != "pdk completed successfully" {
		t.Fatalf("unexpected output: %s", res.Output)
	}
	if res.Data["computed"] != 42 {
		t.Fatalf("unexpected data: %v", res.Data)
	}
	if capturedRef != "plugins/calc.wasm" || capturedFn != "execute" {
		t.Fatalf("unexpected captured ref/fn: %s, %s", capturedRef, capturedFn)
	}
	if capturedReq.Configs["precision"] != "high" || capturedReq.Configs["rate"] != "1.5" {
		t.Fatalf("unexpected captured configs: %v", capturedReq.Configs)
	}
}

func TestWasmPluginHandler_RawCall_Success(t *testing.T) {
	var capturedInput []byte
	caller := &mockWasmCaller{
		callFunc: func(ctx context.Context, pluginRef string, functionName string, input []byte) ([]byte, error) {
			capturedInput = input
			return []byte(`{"result":"raw_ok"}`), nil
		},
	}

	handler := NewWasmPluginHandler(caller)
	payload, _ := json.Marshal(WasmPluginPayload{
		PluginRef: "echo.wasm",
		Function:  "echo",
		Input:     json.RawMessage(`{"ping":"pong"}`),
	})

	task := &TaskMessage{
		TaskID:   "t-raw",
		RunID:    "r-raw",
		StepID:   "s-raw",
		TaskType: TaskTypeWasmPlugin,
		Payload:  payload,
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got false")
	}
	if !strings.Contains(res.Output, "raw_ok") {
		t.Fatalf("unexpected output: %s", res.Output)
	}
	if string(capturedInput) != `{"ping":"pong"}` {
		t.Fatalf("unexpected input captured: %s", string(capturedInput))
	}
}

func TestWasmPluginHandler_TransientTimeout_Retryable(t *testing.T) {
	caller := &mockWasmCaller{
		callPDKFunc: func(ctx context.Context, pluginRef string, functionName string, req pdk.Context) (*pdk.Result, error) {
			return nil, context.DeadlineExceeded
		},
	}

	handler := NewWasmPluginHandler(caller)
	payload, _ := json.Marshal(WasmPluginPayload{
		PluginURI: "timeout.wasm",
	})
	task := &TaskMessage{
		TaskID:   "t-to",
		RunID:    "r-to",
		StepID:   "s-to",
		TaskType: TaskTypeWasmPlugin,
		Payload:  payload,
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsRetryable(err) {
		t.Fatalf("expected RetryableError, got %T: %v", err, err)
	}
	if res != nil && res.Success {
		t.Fatalf("expected failed result")
	}
}

func TestWasmPluginHandler_PDKResultFailure(t *testing.T) {
	caller := &mockWasmCaller{
		callPDKFunc: func(ctx context.Context, pluginRef string, functionName string, req pdk.Context) (*pdk.Result, error) {
			return &pdk.Result{
				Success: false,
				Output:  "plugin execution failed: division by zero",
			}, nil
		},
	}

	handler := NewWasmPluginHandler(caller)
	payload, _ := json.Marshal(WasmPluginPayload{
		PluginURI: "math.wasm",
	})
	task := &TaskMessage{
		TaskID:   "t-fail",
		RunID:    "r-fail",
		StepID:   "s-fail",
		TaskType: TaskTypeWasmPlugin,
		Payload:  payload,
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Fatalf("expected failed result, got success")
	}
	if res.Retryable {
		t.Fatalf("expected non-retryable for logical failure")
	}
}

func TestWasmPluginHandler_FatalPluginError(t *testing.T) {
	caller := &mockWasmCaller{
		callPDKFunc: func(ctx context.Context, pluginRef string, functionName string, req pdk.Context) (*pdk.Result, error) {
			return nil, errors.New("wasm module not found: invalid.wasm")
		},
	}

	handler := NewWasmPluginHandler(caller)
	payload, _ := json.Marshal(WasmPluginPayload{
		PluginURI: "invalid.wasm",
	})
	task := &TaskMessage{
		TaskID:   "t-err",
		RunID:    "r-err",
		StepID:   "s-err",
		TaskType: TaskTypeWasmPlugin,
		Payload:  payload,
	}

	_, err := handler.HandleTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if IsRetryable(err) {
		t.Fatalf("fatal error should not be retryable")
	}
}

// --- Tests for GitSandboxHandler ---

func TestGitSandboxHandler_NilManager(t *testing.T) {
	handler := NewGitSandboxHandler(nil)
	task := &TaskMessage{
		TaskID:   "t-nil",
		RunID:    "r-nil",
		StepID:   "s-nil",
		TaskType: TaskTypeGitSandbox,
	}
	_, err := handler.HandleTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error with nil sandbox manager, got nil")
	}
}

func TestGitSandboxHandler_EmptyPayload(t *testing.T) {
	mgr := &mockSandboxManager{worktreePath: "/tmp/wt1"}
	handler := NewGitSandboxHandler(mgr)

	task := &TaskMessage{
		TaskID:   "t-empty-sb",
		RunID:    "r-empty-sb",
		StepID:   "s-empty-sb",
		TaskType: TaskTypeGitSandbox,
		Payload:  nil,
	}
	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success for empty payload no-op")
	}
}

func TestGitSandboxHandler_InvalidJSONPayload(t *testing.T) {
	mgr := &mockSandboxManager{worktreePath: "/tmp/wt1"}
	handler := NewGitSandboxHandler(mgr)

	task := &TaskMessage{
		TaskID:   "t-inv-sb",
		RunID:    "r-inv-sb",
		StepID:   "s-inv-sb",
		TaskType: TaskTypeGitSandbox,
		Payload:  json.RawMessage(`{invalid`),
	}
	_, err := handler.HandleTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error for invalid json, got nil")
	}
}

func TestGitSandboxHandler_ExecuteInExistingWorktree(t *testing.T) {
	mgr := &mockSandboxManager{worktreePath: "/workspaces/project/wt-123"}
	var capturedDir, capturedCmd string
	var capturedArgs []string
	var capturedEnv []string

	exec := &mockCommandExecutor{
		executeFunc: func(ctx context.Context, dir string, name string, args []string, env []string) (string, string, int, error) {
			capturedDir = dir
			capturedCmd = name
			capturedArgs = args
			capturedEnv = env
			return "go test passed\nOK", "", 0, nil
		},
	}

	handler := NewGitSandboxHandler(mgr, WithCommandExecutor(exec))
	payload, _ := json.Marshal(GitSandboxPayload{
		Command:   "go",
		Arguments: []string{"test", "./..."},
		Environment: map[string]string{
			"CGO_ENABLED": "0",
			"API_KEY":     "supersecret123",
		},
		Secrets: []string{"supersecret123"},
	})

	task := &TaskMessage{
		TaskID:   "t-sb-run",
		RunID:    "r-sb-run",
		StepID:   "s-sb-run",
		TaskType: TaskTypeGitSandbox,
		Payload:  payload,
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got false: %s", res.Error)
	}
	if capturedDir != "/workspaces/project/wt-123" {
		t.Fatalf("expected dir /workspaces/project/wt-123, got %s", capturedDir)
	}
	if capturedCmd != "go" || len(capturedArgs) != 2 || len(capturedEnv) == 0 {
		t.Fatalf("unexpected cmd/args/env: %s, %v, %v", capturedCmd, capturedArgs, capturedEnv)
	}
	if res.Data["exit_code"] != 0 {
		t.Fatalf("expected exit_code 0, got %v", res.Data["exit_code"])
	}
}

func TestGitSandboxHandler_AutoProvisionWorktree(t *testing.T) {
	var provisionedID string
	mgr := &mockSandboxManager{
		provisionFunc: func(ctx context.Context, runID string, opts sandbox.WorktreeOptions) (*sandbox.Environment, error) {
			provisionedID = runID
			return &sandbox.Environment{
				RunID:        runID,
				WorktreePath: "/tmp/auto-provisioned",
				Branch:       "feat/auto",
			}, nil
		},
	}

	exec := &mockCommandExecutor{
		executeFunc: func(ctx context.Context, dir string, name string, args []string, env []string) (string, string, int, error) {
			return "done", "", 0, nil
		},
	}

	handler := NewGitSandboxHandler(mgr, WithCommandExecutor(exec))
	payload, _ := json.Marshal(GitSandboxPayload{
		Command: "git",
		Args:    []string{"status"},
		Branch:  "feat/auto",
	})

	task := &TaskMessage{
		TaskID:   "t-auto",
		RunID:    "run-auto-1",
		StepID:   "s-auto",
		TaskType: TaskTypeGitSandbox,
		Payload:  payload,
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success")
	}
	if provisionedID != "run-auto-1" {
		t.Fatalf("expected provisionedID run-auto-1, got %s", provisionedID)
	}
}

func TestGitSandboxHandler_CommandFailure_ExitCode(t *testing.T) {
	mgr := &mockSandboxManager{worktreePath: "/tmp/wt"}
	exec := &mockCommandExecutor{
		executeFunc: func(ctx context.Context, dir string, name string, args []string, env []string) (string, string, int, error) {
			return "", "FAIL: test timed out", 1, errors.New("exit status 1")
		},
	}

	handler := NewGitSandboxHandler(mgr, WithCommandExecutor(exec))
	payload, _ := json.Marshal(GitSandboxPayload{
		Command: "go",
		Args:    []string{"test", "./..."},
	})
	task := &TaskMessage{
		TaskID:   "t-exit1",
		RunID:    "r-exit1",
		StepID:   "s-exit1",
		TaskType: TaskTypeGitSandbox,
		Payload:  payload,
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected handler error (expected result with Success=false): %v", err)
	}
	if res.Success {
		t.Fatalf("expected Success=false on non-zero exit code")
	}
	if res.Data["exit_code"] != 1 {
		t.Fatalf("expected exit_code 1, got %v", res.Data["exit_code"])
	}
}

func TestGitSandboxHandler_RuntimeMetadataEnrichment(t *testing.T) {
	mgr := &mockSandboxManager{worktreePath: "/tmp/wt"}
	exec := &mockStreamingExecutor{
		executeStreamFunc: func(ctx context.Context, dir, name string, args, env []string, stdout, stderr io.Writer) (int, error) {
			_, _ = stdout.Write([]byte("container run completed\n"))
			return 0, nil
		},
		metaFunc: func() map[string]any {
			return map[string]any{
				"sandbox_runtime": "oci",
				"runtime_engine":  "podman",
				"image":           "alpine:3.20",
				"resource_limits": map[string]any{
					"cpu_limit": "2.0",
				},
			}
		},
	}

	handler := NewGitSandboxHandler(mgr, WithCommandExecutor(exec))
	payload, _ := json.Marshal(GitSandboxPayload{
		Command: "echo",
		Args:    []string{"hello"},
	})
	task := &TaskMessage{
		TaskID:   "t-meta",
		RunID:    "r-meta",
		StepID:   "s-meta",
		TaskType: TaskTypeGitSandbox,
		Payload:  payload,
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Data["sandbox_runtime"] != "oci" {
		t.Errorf("expected sandbox_runtime oci, got %v", res.Data["sandbox_runtime"])
	}
	if res.Data["runtime_engine"] != "podman" {
		t.Errorf("expected runtime_engine podman, got %v", res.Data["runtime_engine"])
	}
	if res.Data["image"] != "alpine:3.20" {
		t.Errorf("expected image alpine:3.20, got %v", res.Data["image"])
	}
}

type mockStreamingExecutor struct {
	executeStreamFunc func(ctx context.Context, dir, name string, args, env []string, stdout, stderr io.Writer) (int, error)
	metaFunc          func() map[string]any
}

func (m *mockStreamingExecutor) ExecuteCommand(ctx context.Context, dir string, name string, args []string, env []string) (string, string, int, error) {
	var out, errOut bytes.Buffer
	code, err := m.ExecuteCommandStreaming(ctx, dir, name, args, env, &out, &errOut)
	return out.String(), errOut.String(), code, err
}

func (m *mockStreamingExecutor) ExecuteCommandStreaming(ctx context.Context, dir string, name string, args []string, env []string, stdoutWriter io.Writer, stderrWriter io.Writer) (int, error) {
	if m.executeStreamFunc != nil {
		return m.executeStreamFunc(ctx, dir, name, args, env, stdoutWriter, stderrWriter)
	}
	return 0, nil
}

func (m *mockStreamingExecutor) GetRuntimeMetadata() map[string]any {
	if m.metaFunc != nil {
		return m.metaFunc()
	}
	return nil
}

func TestGitSandboxHandler_TransientLock_Retryable(t *testing.T) {
	mgr := &mockSandboxManager{worktreePath: "/tmp/wt"}
	exec := &mockCommandExecutor{
		executeFunc: func(ctx context.Context, dir string, name string, args []string, env []string) (string, string, int, error) {
			return "", "fatal: Unable to create '.git/index.lock': File exists.", 128, errors.New("exit status 128")
		},
	}

	handler := NewGitSandboxHandler(mgr, WithCommandExecutor(exec))
	payload, _ := json.Marshal(GitSandboxPayload{
		Command: "git",
		Args:    []string{"commit", "-m", "foo"},
	})
	task := &TaskMessage{
		TaskID:   "t-lock",
		RunID:    "r-lock",
		StepID:   "s-lock",
		TaskType: TaskTypeGitSandbox,
		Payload:  payload,
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected retryable error on git index.lock collision, got nil")
	}
	if !IsRetryable(err) {
		t.Fatalf("expected RetryableError, got %T: %v", err, err)
	}
	if res != nil && res.Success {
		t.Fatalf("expected non-success result")
	}
}

func TestGitSandboxHandler_MaskSecrets(t *testing.T) {
	mgr := &mockSandboxManager{worktreePath: "/tmp/wt"}
	exec := &mockCommandExecutor{
		executeFunc: func(ctx context.Context, dir string, name string, args []string, env []string) (string, string, int, error) {
			return "Connecting with token my-super-secret-token-12345", "", 0, nil
		},
	}

	handler := NewGitSandboxHandler(mgr, WithCommandExecutor(exec))
	payload, _ := json.Marshal(GitSandboxPayload{
		Command: "curl",
		Args:    []string{"https://api.internal"},
		Secrets: []string{"my-super-secret-token-12345"},
	})
	task := &TaskMessage{
		TaskID:   "t-sec",
		RunID:    "r-sec",
		StepID:   "s-sec",
		TaskType: TaskTypeGitSandbox,
		Payload:  payload,
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(res.Output, "my-super-secret-token-12345") {
		t.Fatalf("output contains unmasked secret: %s", res.Output)
	}
	if !strings.Contains(res.Output, "***") {
		t.Fatalf("expected masked secret (***) in output: %s", res.Output)
	}
}

func TestGitSandboxHandler_StreamingWithPublisher(t *testing.T) {
	mgr := &mockSandboxManager{worktreePath: "/tmp/wt"}
	collector := &mockChunkEventCollector{}

	exec := &mockCommandExecutor{
		executeFunc: func(ctx context.Context, dir string, name string, args []string, env []string) (string, string, int, error) {
			return "step 1: downloading\nstep 2: done\n", "warning: non-fatal\n", 0, nil
		},
	}

	handler := NewGitSandboxHandler(
		mgr,
		WithCommandExecutor(exec),
		WithSandboxPublisher(collector),
		WithSandboxWorkerID("worker-streaming"),
	)

	payload, _ := json.Marshal(GitSandboxPayload{
		Command: "make",
		Args:    []string{"build"},
		Secrets: []string{"secret-val"},
	})
	task := &TaskMessage{
		TaskID:   "t-stream",
		RunID:    "r-stream",
		StepID:   "s-stream",
		TaskType: TaskTypeGitSandbox,
		Payload:  payload,
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected task to succeed")
	}

	events := collector.getEvents()
	if len(events) < 2 {
		t.Fatalf("expected at least 2 log chunk events, got %d", len(events))
	}

	for _, ev := range events {
		if ev.Type != gateway.TopicWorkerLogs {
			t.Errorf("expected topic %s, got %s", gateway.TopicWorkerLogs, ev.Type)
		}
		if ev.RunID != "r-stream" || ev.TaskID != "t-stream" {
			t.Errorf("unexpected IDs in event: %+v", ev)
		}
		if ev.Payload["worker_id"] != "worker-streaming" {
			t.Errorf("expected worker_id 'worker-streaming', got %v", ev.Payload["worker_id"])
		}
	}

	if !strings.Contains(res.Output, "step 1: downloading") {
		t.Errorf("expected full output in TaskResult, got %s", res.Output)
	}
}

func TestGitSandboxHandler_Options(t *testing.T) {
	mgr := &mockSandboxManager{worktreePath: "/tmp/wt"}
	exec := &mockCommandExecutor{}
	h := NewGitSandboxHandler(
		mgr,
		WithCommandExecutor(exec),
		WithSandboxChunkSize(512),
		WithSandboxWorkerID("worker-custom"),
	)
	if h.chunkSize != 512 {
		t.Errorf("expected chunkSize 512, got %d", h.chunkSize)
	}
	if h.workerID != "worker-custom" {
		t.Errorf("expected workerID 'worker-custom', got %q", h.workerID)
	}
}

func TestGitSandboxHandler_RecycleAfter(t *testing.T) {
	mgr := &mockSandboxManager{worktreePath: "/tmp/wt"}
	exec := &mockCommandExecutor{}

	handler := NewGitSandboxHandler(mgr, WithCommandExecutor(exec))
	payload, _ := json.Marshal(GitSandboxPayload{
		Command:      "echo",
		Args:         []string{"hi"},
		RecycleAfter: true,
	})
	task := &TaskMessage{
		TaskID:   "t-rec",
		RunID:    "r-rec",
		StepID:   "s-rec",
		TaskType: TaskTypeGitSandbox,
		Payload:  payload,
	}

	_, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mgr.recycleCalled {
		t.Fatal("expected RecycleWorktree to be called when RecycleAfter is true")
	}
}

// --- Tests for AgentStepHandler ---

func TestAgentStepHandler_EmptyPayload(t *testing.T) {
	handler := NewAgentStepHandler(AgentStepConfig{})
	task := &TaskMessage{
		TaskID:   "t-ag-empty",
		RunID:    "r-ag-empty",
		StepID:   "s-ag-empty",
		TaskType: TaskTypeAgentStep,
		Payload:  nil,
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success for empty agent step payload")
	}
}

func TestAgentStepHandler_StructuredStep_Success(t *testing.T) {
	handler := NewAgentStepHandler(AgentStepConfig{})
	payload, _ := json.Marshal(AgentStepPayload{
		StepName: "spec_design",
		Role:     "spec_writer",
		Action:   "plan",
		Prompt:   "Design SDD for issue #86",
		Context: map[string]any{
			"issue_id": 86,
		},
	})
	task := &TaskMessage{
		TaskID:   "t-ag-spec",
		RunID:    "r-ag-spec",
		StepID:   "s-ag-spec",
		TaskType: TaskTypeAgentStep,
		Payload:  payload,
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success")
	}
	if res.Data["role"] != "spec_writer" || res.Data["action"] != "plan" {
		t.Fatalf("unexpected data: %v", res.Data)
	}
}

func TestAgentStepHandler_StreamingLogs(t *testing.T) {
	collector := &mockChunkEventCollector{}
	handler := NewAgentStepHandler(AgentStepConfig{
		Publisher: collector,
		WorkerID:  "agent-worker-1",
	})

	payload, _ := json.Marshal(AgentStepPayload{
		StepName: "code_review",
		Role:     "reviewer",
		Action:   "validate",
	})
	task := &TaskMessage{
		TaskID:   "t-agent-stream",
		RunID:    "r-agent-stream",
		StepID:   "s-agent-stream",
		TaskType: TaskTypeAgentStep,
		Payload:  payload,
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success")
	}

	events := collector.getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 chunk event emitted for agent step, got %d", len(events))
	}

	ev := events[0]
	if ev.Type != gateway.TopicWorkerLogs {
		t.Errorf("expected topic %s, got %s", gateway.TopicWorkerLogs, ev.Type)
	}
	if ev.Payload["worker_id"] != "agent-worker-1" {
		t.Errorf("expected worker_id 'agent-worker-1', got %v", ev.Payload["worker_id"])
	}
	if !strings.Contains(ev.Payload["chunk"].(string), "agent step [reviewer:validate]") {
		t.Errorf("unexpected chunk content: %v", ev.Payload["chunk"])
	}
}

func TestAgentStepHandler_CustomExecutor(t *testing.T) {
	called := false
	exec := &mockAgentStepExecutor{
		execFunc: func(ctx context.Context, step AgentStepPayload, task *TaskMessage) (*TaskResult, error) {
			called = true
			return &TaskResult{
				Success: true,
				Output:  "custom agent step execution completed",
				Data:    map[string]any{"custom": true},
			}, nil
		},
	}

	handler := NewAgentStepHandler(AgentStepConfig{Executor: exec})
	payload, _ := json.Marshal(AgentStepPayload{
		Role:   "reviewer",
		Action: "validate",
	})
	task := &TaskMessage{
		TaskID:   "t-cust",
		RunID:    "r-cust",
		StepID:   "s-cust",
		TaskType: TaskTypeAgentStep,
		Payload:  payload,
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected custom executor to be called")
	}
	if res.Output != "custom agent step execution completed" {
		t.Fatalf("unexpected output: %s", res.Output)
	}
}

type mockAgentStepExecutor struct {
	execFunc func(ctx context.Context, step AgentStepPayload, task *TaskMessage) (*TaskResult, error)
}

func (m *mockAgentStepExecutor) ExecuteStep(ctx context.Context, step AgentStepPayload, task *TaskMessage) (*TaskResult, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, step, task)
	}
	return &TaskResult{Success: true}, nil
}

func TestAgentStepHandler_TimeoutExceeded(t *testing.T) {
	exec := &mockAgentStepExecutor{
		execFunc: func(ctx context.Context, step AgentStepPayload, task *TaskMessage) (*TaskResult, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(100 * time.Millisecond):
				return &TaskResult{Success: true}, nil
			}
		},
	}
	handler := NewAgentStepHandler(AgentStepConfig{Executor: exec})
	payload, _ := json.Marshal(AgentStepPayload{
		TimeoutMS: 10,
	})
	task := &TaskMessage{
		TaskID:   "t-to",
		RunID:    "r-to",
		StepID:   "s-to",
		TaskType: TaskTypeAgentStep,
		Payload:  payload,
	}

	_, err := handler.HandleTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected timeout context error, got nil")
	}
}

func TestAgentStepHandler_InvalidJSON(t *testing.T) {
	handler := NewAgentStepHandler(AgentStepConfig{})
	task := &TaskMessage{
		TaskID:   "t-inv",
		RunID:    "r-inv",
		StepID:   "s-inv",
		TaskType: TaskTypeAgentStep,
		Payload:  json.RawMessage(`{not-json`),
	}
	_, err := handler.HandleTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error on invalid JSON, got nil")
	}
}

func TestGitSandboxHandler_ProvisionFailure(t *testing.T) {
	mgr := &mockSandboxManager{
		provisionFunc: func(ctx context.Context, runID string, opts sandbox.WorktreeOptions) (*sandbox.Environment, error) {
			return nil, errors.New("provisioning failed: disk full")
		},
	}
	handler := NewGitSandboxHandler(mgr)
	payload, _ := json.Marshal(GitSandboxPayload{
		Command: "echo",
		Args:    []string{"fail"},
	})
	task := &TaskMessage{
		TaskID:   "t-prov-fail",
		RunID:    "r-prov-fail",
		StepID:   "s-prov-fail",
		TaskType: TaskTypeGitSandbox,
		Payload:  payload,
	}
	_, err := handler.HandleTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected error on provisioning failure, got nil")
	}
}

func TestGitSandboxHandler_TransientProvisionFailure_Retryable(t *testing.T) {
	mgr := &mockSandboxManager{
		provisionFunc: func(ctx context.Context, runID string, opts sandbox.WorktreeOptions) (*sandbox.Environment, error) {
			return nil, errors.New("failed: could not lock config file .git/config.lock")
		},
	}
	handler := NewGitSandboxHandler(mgr)
	payload, _ := json.Marshal(GitSandboxPayload{
		Command: "echo",
	})
	task := &TaskMessage{
		TaskID:   "t-prov-lock",
		RunID:    "r-prov-lock",
		StepID:   "s-prov-lock",
		TaskType: TaskTypeGitSandbox,
		Payload:  payload,
	}
	_, err := handler.HandleTask(context.Background(), task)
	if err == nil {
		t.Fatal("expected retryable error on git config lock failure, got nil")
	}
	if !IsRetryable(err) {
		t.Fatalf("expected RetryableError, got %T: %v", err, err)
	}
}

func TestGitSandboxHandler_CleanLocks(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sb-clean-locks-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mgr := &mockSandboxManager{worktreePath: tempDir}
	exec := &mockCommandExecutor{
		executeFunc: func(ctx context.Context, dir string, name string, args []string, env []string) (string, string, int, error) {
			return "cleaned", "", 0, nil
		},
	}

	handler := NewGitSandboxHandler(mgr, WithCommandExecutor(exec))
	payload, _ := json.Marshal(GitSandboxPayload{
		Command:    "git",
		Args:       []string{"status"},
		CleanLocks: true,
	})
	task := &TaskMessage{
		TaskID:   "t-clean-locks",
		RunID:    "r-clean-locks",
		StepID:   "s-clean-locks",
		TaskType: TaskTypeGitSandbox,
		Payload:  payload,
	}

	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success")
	}
}

func TestDefaultCommandExecutor_RealCommand_NonZeroExit(t *testing.T) {
	exec := &DefaultCommandExecutor{}
	// Run command that exits non-zero, e.g. "go fakecommandthatdoesnotexist"
	_, stderr, exitCode, err := exec.ExecuteCommand(context.Background(), os.TempDir(), "go", []string{"invalid_command_xyz"}, nil)
	if err == nil {
		t.Fatal("expected error for invalid go subcommand")
	}
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit code, got %d", exitCode)
	}
	if stderr == "" {
		t.Fatal("expected stderr to contain error output")
	}
}

func TestDefaultCommandExecutor_EnvAndDir(t *testing.T) {
	exec := &DefaultCommandExecutor{}
	stdout, _, exitCode, err := exec.ExecuteCommand(context.Background(), os.TempDir(), "go", []string{"env", "GOPATH"}, []string{"FOO=BAR"})
	if err != nil {
		t.Fatalf("go env GOPATH failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if stdout == "" {
		t.Fatal("expected non-empty output")
	}
}

func TestWasmPluginHandler_NilResultFromPDK(t *testing.T) {
	caller := &mockWasmCaller{
		callPDKFunc: func(ctx context.Context, pluginRef string, functionName string, req pdk.Context) (*pdk.Result, error) {
			return nil, nil
		},
	}
	handler := NewWasmPluginHandler(caller)
	payload, _ := json.Marshal(WasmPluginPayload{
		PluginURI: "empty_res.wasm",
	})
	task := &TaskMessage{
		TaskID:   "t-nilres",
		RunID:    "r-nilres",
		StepID:   "s-nilres",
		TaskType: TaskTypeWasmPlugin,
		Payload:  payload,
	}
	res, err := handler.HandleTask(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success on nil pdk result fallback")
	}
}
