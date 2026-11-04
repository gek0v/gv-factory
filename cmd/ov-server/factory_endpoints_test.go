// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/agent"
	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/sandbox"
	"github.com/gv-factory/gv-factory/internal/sandbox/merger"
	"github.com/gv-factory/gv-factory/internal/store"
)

// mockFactoryRunner implements FactoryPipelineRunner for unit tests.
type mockFactoryRunner struct {
	runFn     func(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error)
	callCount int32
}

func (m *mockFactoryRunner) RunPipeline(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error) {
	atomic.AddInt32(&m.callCount, 1)
	if m.runFn != nil {
		return m.runFn(ctx, task)
	}

	// Default behavior: emit stage events if hook is present in metadata
	if task.Metadata != nil {
		if hook, ok := task.Metadata["event_hook"].(func(string, any)); ok && hook != nil {
			hook("spec_generated", &agent.SpecDocument{Title: task.Title})
			hook("build_completed", &agent.BuildResult{TaskID: task.TaskID, Success: true})
			hook("review_decided", &agent.ApprovalDecision{DecisionType: agent.DecisionApproved})
		}
	}

	outcome := &agent.PipelineOutcome{
		TaskID:          task.TaskID,
		Success:         true,
		Decision:        agent.DecisionApproved,
		TotalIterations: 1,
		Duration:        50 * time.Millisecond,
	}

	if task.Metadata != nil {
		if hook, ok := task.Metadata["event_hook"].(func(string, any)); ok && hook != nil {
			hook("pipeline_completed", outcome)
		}
	}

	return outcome, nil
}

func TestFactoryEndpoints_PostRun_Validation(t *testing.T) {
	gw := gateway.NewGateway()
	runner := &mockFactoryRunner{}
	mux := http.NewServeMux()
	RegisterFactoryEndpoints(mux, gw, runner)

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{
			name:       "empty_body",
			method:     http.MethodPost,
			path:       "/api/v1/factory/run",
			body:       "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid_json",
			method:     http.MethodPost,
			path:       "/api/v1/factory/run",
			body:       "{invalid-json",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing_task_id",
			method:     http.MethodPost,
			path:       "/api/v1/factory/run",
			body:       `{"title": "Implement auth"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "whitespace_task_id",
			method:     http.MethodPost,
			path:       "/api/v1/factory/run",
			body:       `{"task_id": "   ", "title": "Implement auth"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing_title",
			method:     http.MethodPost,
			path:       "/api/v1/factory/run",
			body:       `{"task_id": "task-101"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "whitespace_title",
			method:     http.MethodPost,
			path:       "/api/v1/factory/run",
			body:       `{"task_id": "task-101", "title": " \t\n "}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "method_not_allowed_get",
			method:     http.MethodGet,
			path:       "/api/v1/factory/run",
			body:       "",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "method_not_allowed_delete",
			method:     http.MethodDelete,
			path:       "/api/v1/factory/run",
			body:       "",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d. Body: %s", tc.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestFactoryEndpoints_PostRun_Sync_Success(t *testing.T) {
	gw := gateway.NewGateway()
	runner := &mockFactoryRunner{}
	mux := http.NewServeMux()
	tracker := RegisterFactoryEndpoints(mux, gw, runner)

	payload := `{
		"task_id": "task-sync-1",
		"title": "Build Cache System",
		"description": "In-memory LRU cache",
		"labels": ["backend", "performance"],
		"constraints": ["Zero allocation in get path"],
		"metadata": {"coverage_target": 0.95}
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/factory/run?async=false", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK for sync run, got %d. Body: %s", w.Code, w.Body.String())
	}

	var outcome agent.PipelineOutcome
	if err := json.NewDecoder(w.Body).Decode(&outcome); err != nil {
		t.Fatalf("failed to decode outcome: %v", err)
	}

	if outcome.TaskID != "task-sync-1" {
		t.Errorf("expected TaskID 'task-sync-1', got %q", outcome.TaskID)
	}
	if !outcome.Success {
		t.Errorf("expected Success=true")
	}
	if outcome.Decision != agent.DecisionApproved {
		t.Errorf("expected DecisionApproved, got %s", outcome.Decision)
	}

	// Verify tracker recorded completed status
	record, found := tracker.Get("task-sync-1")
	if !found {
		t.Fatal("expected run in tracker")
	}
	if record.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", record.Status)
	}
	if record.Outcome == nil || record.Outcome.TaskID != "task-sync-1" {
		t.Errorf("expected recorded outcome")
	}
}

func TestFactoryEndpoints_PostRun_Sync_Failure(t *testing.T) {
	gw := gateway.NewGateway()
	runner := &mockFactoryRunner{
		runFn: func(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error) {
			outcome := &agent.PipelineOutcome{
				TaskID:       task.TaskID,
				Success:      false,
				Decision:     agent.DecisionFailed,
				ErrorMessage: "build failed",
			}
			return outcome, errors.New("pipeline failed")
		},
	}

	mux := http.NewServeMux()
	tracker := RegisterFactoryEndpoints(mux, gw, runner)

	payload := `{"task_id": "task-sync-fail", "title": "Failing Pipeline"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/factory/run?async=0", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 with failed outcome, got %d. Body: %s", w.Code, w.Body.String())
	}

	var outcome agent.PipelineOutcome
	if err := json.NewDecoder(w.Body).Decode(&outcome); err != nil {
		t.Fatalf("failed to decode outcome: %v", err)
	}
	if outcome.Success {
		t.Error("expected Success=false")
	}

	record, found := tracker.Get("task-sync-fail")
	if !found {
		t.Fatal("expected run in tracker")
	}
	if record.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", record.Status)
	}
}

func TestFactoryEndpoints_PostRun_Async_Lifecycle(t *testing.T) {
	gw := gateway.NewGateway()
	doneChan := make(chan struct{})

	runner := &mockFactoryRunner{
		runFn: func(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error) {
			<-doneChan
			return &agent.PipelineOutcome{
				TaskID:          task.TaskID,
				Success:         true,
				Decision:        agent.DecisionApproved,
				TotalIterations: 1,
			}, nil
		},
	}

	mux := http.NewServeMux()
	RegisterFactoryEndpoints(mux, gw, runner)

	// 1. Submit async run (default async=true)
	payload := `{"task_id": "task-async-1", "title": "Async Task Run"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/factory/run", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected HTTP 202 Accepted, got %d. Body: %s", w.Code, w.Body.String())
	}

	var asyncResp struct {
		RunID  string `json:"run_id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(w.Body).Decode(&asyncResp); err != nil {
		t.Fatalf("failed to decode async response: %v", err)
	}

	if asyncResp.RunID != "task-async-1" {
		t.Errorf("expected RunID 'task-async-1', got %q", asyncResp.RunID)
	}
	if asyncResp.Status != "running" {
		t.Errorf("expected status 'running', got %q", asyncResp.Status)
	}

	// 2. Query status while still running
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/factory/runs/task-async-1", nil)
	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 on running status query, got %d", getW.Code)
	}
	var runningRecord FactoryRunResponse
	if err := json.NewDecoder(getW.Body).Decode(&runningRecord); err != nil {
		t.Fatalf("failed to decode record: %v", err)
	}
	if runningRecord.Status != "running" {
		t.Errorf("expected status 'running', got %q", runningRecord.Status)
	}

	// 3. Release runner and wait for completion
	close(doneChan)
	time.Sleep(50 * time.Millisecond)

	// 4. Query status after completion
	getReq2 := httptest.NewRequest(http.MethodGet, "/api/v1/factory/runs/task-async-1", nil)
	getW2 := httptest.NewRecorder()
	mux.ServeHTTP(getW2, getReq2)

	if getW2.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 on completed status query, got %d", getW2.Code)
	}
	var completedRecord FactoryRunResponse
	if err := json.NewDecoder(getW2.Body).Decode(&completedRecord); err != nil {
		t.Fatalf("failed to decode completed record: %v", err)
	}
	if completedRecord.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", completedRecord.Status)
	}
	if completedRecord.Outcome == nil || !completedRecord.Outcome.Success {
		t.Errorf("expected valid successful outcome in completed record")
	}
}

func TestFactoryEndpoints_GetRun_NotFound_And_Methods(t *testing.T) {
	gw := gateway.NewGateway()
	mux := http.NewServeMux()
	RegisterFactoryEndpoints(mux, gw, &mockFactoryRunner{})

	// 1. Not Found for unknown run_id
	req := httptest.NewRequest(http.MethodGet, "/api/v1/factory/runs/unknown-run-999", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected HTTP 404 for unknown run, got %d", w.Code)
	}

	// 2. Empty run ID returns 404
	reqEmpty := httptest.NewRequest(http.MethodGet, "/api/v1/factory/runs/", nil)
	wEmpty := httptest.NewRecorder()
	mux.ServeHTTP(wEmpty, reqEmpty)

	if wEmpty.Code != http.StatusNotFound {
		t.Fatalf("expected HTTP 404 for empty run ID, got %d", wEmpty.Code)
	}

	// 3. POST on runs/:id is Method Not Allowed
	reqPost := httptest.NewRequest(http.MethodPost, "/api/v1/factory/runs/task-1", nil)
	wPost := httptest.NewRecorder()
	mux.ServeHTTP(wPost, reqPost)

	if wPost.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected HTTP 405 Method Not Allowed, got %d", wPost.Code)
	}
}

func TestFactoryEndpoints_LiveEventBroadcasting(t *testing.T) {
	gw := gateway.NewGateway()

	var eventsMu sync.Mutex
	receivedStages := make(map[string]bool)

	// Subscribe to the 4 pipeline stage events
	stages := []string{"spec_generated", "build_completed", "review_decided", "pipeline_completed"}
	for _, stage := range stages {
		ch := make(chan gateway.Event, 10)
		unsub, err := gw.SubscribeTaskEvents(context.Background(), stage, ch)
		if err != nil {
			t.Fatalf("failed to subscribe to %s: %v", stage, err)
		}
		defer unsub()

		stName := stage
		go func() {
			for ev := range ch {
				eventsMu.Lock()
				receivedStages[stName] = true
				eventsMu.Unlock()
				_ = ev
			}
		}()
	}

	runner := &mockFactoryRunner{}
	mux := http.NewServeMux()
	RegisterFactoryEndpoints(mux, gw, runner)

	payload := `{"task_id": "task-events-broadcast", "title": "Verify Event Broadcast"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/factory/run?async=false", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}

	// Give a moment for subscribers to receive
	time.Sleep(50 * time.Millisecond)

	eventsMu.Lock()
	defer eventsMu.Unlock()

	for _, stage := range stages {
		if !receivedStages[stage] {
			t.Errorf("expected live event broadcast for stage %q, but was not received", stage)
		}
	}
}

func TestFactoryEndpoints_ServerMuxIntegration(t *testing.T) {
	gw := gateway.NewGateway()
	runner := &mockFactoryRunner{}

	serverHandler := NewServerMuxWithRunner(nil, gw, nil, nil, nil, nil, nil, nil, runner)

	payload := `{"task_id": "task-mux-test", "title": "Mux Integration Run"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/factory/run?async=false", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	serverHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 through NewServerMuxWithRunner, got %d. Body: %s", w.Code, w.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/factory/runs/task-mux-test", nil)
	getW := httptest.NewRecorder()
	serverHandler.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 on GET through server mux, got %d", getW.Code)
	}
}

func TestFactoryEndpoints_TrackerCoverage(t *testing.T) {
	// 1. Nil tracker safety
	var nilTracker *FactoryRunTracker
	nilTracker.Set(nil)
	nilTracker.Set(&FactoryRunResponse{RunID: "x"})
	if _, ok := nilTracker.Get("x"); ok {
		t.Error("expected false for nil tracker Get")
	}
	if list := nilTracker.List(); len(list) != 0 {
		t.Errorf("expected empty list for nil tracker, got %d", len(list))
	}

	// 2. Real tracker operations
	tracker := NewFactoryRunTracker()
	tracker.Set(nil)
	if list := tracker.List(); len(list) != 0 {
		t.Errorf("expected 0 runs initially, got %d", len(list))
	}

	tracker.Set(&FactoryRunResponse{
		RunID:  "run-1",
		Status: "completed",
	})
	tracker.Set(&FactoryRunResponse{
		RunID:  "run-2",
		Status: "running",
	})

	list := tracker.List()
	if len(list) != 2 {
		t.Errorf("expected 2 runs in list, got %d", len(list))
	}
}

func TestFactoryEndpoints_PayloadMapHelpers(t *testing.T) {
	// 1. Nil payload
	m1 := toPayloadMap(nil)
	if len(m1) != 0 {
		t.Errorf("expected empty map for nil payload, got %v", m1)
	}

	// 2. Already map[string]any
	m2 := toPayloadMap(map[string]any{"key": "value"})
	if m2["key"] != "value" {
		t.Errorf("expected key=value, got %v", m2)
	}

	// 3. Struct payload
	type sample struct {
		Name string `json:"name"`
	}
	m3 := toPayloadMap(sample{Name: "test"})
	if m3["name"] != "test" {
		t.Errorf("expected name=test, got %v", m3)
	}

	// 4. Channel payload (unmarshalable)
	ch := make(chan int)
	m4 := toPayloadMap(ch)
	if _, ok := m4["raw"]; !ok {
		t.Errorf("expected raw field for unmarshalable payload, got %v", m4)
	}

	// 5. JSON array (unmarshals to slice, not map)
	m5 := toPayloadMap([]string{"a", "b"})
	if _, ok := m5["data"]; !ok {
		t.Errorf("expected data field for slice payload, got %v", m5)
	}

	// 6. Broadcast with nil gateway
	broadcastStageEvent(nil, "run-1", "stage", nil)
}

func TestFactoryEndpoints_DefaultRunnerAndEdgeCases(t *testing.T) {
	gw := gateway.NewGateway()

	// 1. Default runner validation error
	defRunner := &defaultFactoryRunner{brainSvc: nil, gw: gw}
	_, err := defRunner.RunPipeline(context.Background(), agent.TaskContext{
		TaskID: "", // invalid empty ID
	})
	if err == nil {
		t.Error("expected error for empty TaskID in defaultFactoryRunner")
	}

	// 2. RegisterFactoryEndpoints with nil mux, runner, tracker
	registerFactoryEndpointsWithTracker(nil, gw, nil, nil)

	mux := http.NewServeMux()
	registerFactoryEndpointsWithTracker(mux, nil, nil, nil)

	// 3. Sync run with nil outcome and error (500 Internal Server Error)
	failingRunner := &mockFactoryRunner{
		runFn: func(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error) {
			return nil, errors.New("unrecoverable crash")
		},
	}
	muxCrash := http.NewServeMux()
	RegisterFactoryEndpoints(muxCrash, gw, failingRunner)

	payload := `{"task_id": "task-crash", "title": "Crash Task"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/factory/run?async=false", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	muxCrash.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected HTTP 500 for unrecoverable crash, got %d. Body: %s", w.Code, w.Body.String())
	}

	// 4. Async run error handling
	asyncErrRunner := &mockFactoryRunner{
		runFn: func(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error) {
			return nil, errors.New("async background error")
		},
	}
	muxAsyncErr := http.NewServeMux()
	tracker := RegisterFactoryEndpoints(muxAsyncErr, gw, asyncErrRunner)

	payload2 := `{"task_id": "task-async-err", "title": "Async Error Task"}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/factory/run", bytes.NewBufferString(payload2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	muxAsyncErr.ServeHTTP(w2, req2)

	if w2.Code != http.StatusAccepted {
		t.Fatalf("expected HTTP 202 Accepted, got %d", w2.Code)
	}

	time.Sleep(50 * time.Millisecond)
	rec, found := tracker.Get("task-async-err")
	if !found {
		t.Fatal("expected run in tracker")
	}
	if rec.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", rec.Status)
	}
}

type testSpecGen struct {
	spec *agent.SpecDocument
	err  error
}

func (m *testSpecGen) GenerateSpec(ctx context.Context, task agent.TaskContext) (*agent.SpecDocument, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.spec != nil {
		return m.spec, nil
	}
	return &agent.SpecDocument{
		Title: task.Title,
		AtomicTasks: []agent.AtomicTask{
			{ID: "task-atomic-1", Title: "Create math engine"},
		},
	}, nil
}

type testFileWritingBuilder struct {
	baseDir string
	files   map[string]string
}

func (b *testFileWritingBuilder) BuildSpec(ctx context.Context, spec *agent.SpecDocument) ([]*agent.BuildResult, error) {
	for name, content := range b.files {
		target := filepath.Join(b.baseDir, name)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, []byte(content), 0644); err != nil {
			return nil, err
		}
	}
	return []*agent.BuildResult{
		{
			TaskID:  "task-atomic-1",
			Success: true,
		},
	}, nil
}

type testApprover struct {
	decision *agent.ApprovalDecision
	lastReq  agent.ApprovalRequest
}

func (m *testApprover) Decide(ctx context.Context, req agent.ApprovalRequest) (*agent.ApprovalDecision, error) {
	m.lastReq = req
	if m.decision != nil {
		return m.decision, nil
	}
	return &agent.ApprovalDecision{
		DecisionType: agent.DecisionApproved,
	}, nil
}

func TestFactoryEndpoints_InitGitSandbox(t *testing.T) {
	tempDir := t.TempDir()
	taskID := "task-init-git-101"
	sandboxDir := filepath.Join(tempDir, taskID)

	ctx := context.Background()
	err := initGitSandbox(ctx, sandboxDir, taskID, "main")
	if err != nil {
		t.Fatalf("unexpected error in initGitSandbox: %v", err)
	}

	// Verify .git exists
	gitDir := filepath.Join(sandboxDir, ".git")
	if fi, err := os.Stat(gitDir); err != nil || !fi.IsDir() {
		t.Fatalf("expected .git directory to exist in %s", sandboxDir)
	}

	// Verify git author config
	name, err := runGitCmd(ctx, sandboxDir, "config", "user.name")
	if err != nil || name != "GV Factory Bot" {
		t.Errorf("expected user.name 'GV Factory Bot', got %q (err: %v)", name, err)
	}
	email, err := runGitCmd(ctx, sandboxDir, "config", "user.email")
	if err != nil || email != "bot@gv-factory.local" {
		t.Errorf("expected user.email 'bot@gv-factory.local', got %q (err: %v)", email, err)
	}

	// Verify current branch is ov-run/<task_id>
	branch, err := runGitCmd(ctx, sandboxDir, "branch", "--show-current")
	if err != nil || branch != "ov-run/"+taskID {
		t.Errorf("expected branch 'ov-run/%s', got %q (err: %v)", taskID, branch, err)
	}

	// Verify go.mod exists
	goModBytes, err := os.ReadFile(filepath.Join(sandboxDir, "go.mod"))
	if err != nil {
		t.Fatalf("expected go.mod to exist: %v", err)
	}
	if !strings.Contains(string(goModBytes), "module task_init_git_101") && !strings.Contains(string(goModBytes), "module task-init-git-101") {
		t.Errorf("expected go.mod to contain task identifier, got %s", string(goModBytes))
	}

	// Verify initial commit on main branch
	logMsg, err := runGitCmd(ctx, sandboxDir, "log", "-1", "--format=%s", "main")
	if err != nil || logMsg != "chore: initial commit with go.mod" {
		t.Errorf("expected commit message 'chore: initial commit with go.mod', got %q (err: %v)", logMsg, err)
	}

	// Verify idempotency on second call
	if err := initGitSandbox(ctx, sandboxDir, taskID, "main"); err != nil {
		t.Fatalf("expected second call to initGitSandbox to succeed, got %v", err)
	}
	branch2, _ := runGitCmd(ctx, sandboxDir, "branch", "--show-current")
	if branch2 != "ov-run/"+taskID {
		t.Errorf("expected branch to remain 'ov-run/%s', got %q", taskID, branch2)
	}
}

func TestFactoryEndpoints_CommitAndExtractDiff(t *testing.T) {
	tempDir := t.TempDir()
	taskID := "task-diff-202"
	sandboxDir := filepath.Join(tempDir, taskID)

	ctx := context.Background()
	if err := initGitSandbox(ctx, sandboxDir, taskID, "main"); err != nil {
		t.Fatalf("failed initGitSandbox: %v", err)
	}

	// Write a new file
	// REUSE-IgnoreStart
	serviceFile := filepath.Join(sandboxDir, "service.go")
	serviceContent := "// SPDX-License-Identifier: Apache-2.0\npackage main\n\nfunc Run() string { return \"ok\" }\n"
	if err := os.WriteFile(serviceFile, []byte(serviceContent), 0644); err != nil {
		t.Fatalf("failed to write service file: %v", err)
	}
	// REUSE-IgnoreEnd

	diff := commitAndExtractDiff(ctx, sandboxDir, taskID, "implement service runner", "main")
	if diff == "" {
		t.Fatal("expected non-empty diff after committing modified files")
	}
	if !strings.Contains(diff, "diff --git a/service.go b/service.go") {
		t.Errorf("expected diff to reference service.go, got: %s", diff)
	}
	if !strings.Contains(diff, "+func Run() string") {
		t.Errorf("expected diff to contain added function, got: %s", diff)
	}

	// Verify commit message
	commitMsg, err := runGitCmd(ctx, sandboxDir, "log", "-1", "--format=%s")
	if err != nil || !strings.Contains(commitMsg, "feat(task-diff-202): implement implement service runner") {
		t.Errorf("unexpected commit message: %q (err: %v)", commitMsg, err)
	}

	// Calling commitAndExtractDiff when clean still extracts diff against HEAD~1 or baseBranch
	cleanDiff := commitAndExtractDiff(ctx, sandboxDir, taskID, "implement service runner", "main")
	if cleanDiff == "" {
		t.Fatal("expected diff against HEAD~1 to remain accessible")
	}
}

func TestFactoryEndpoints_DefaultRunner_RealGitSandbox(t *testing.T) {
	tempBase := t.TempDir()
	taskID := "task-runner-git-303"
	sandboxDir := filepath.Join(tempBase, taskID)

	gw := gateway.NewGateway()
	specGen := &testSpecGen{}
	// REUSE-IgnoreStart
	builder := &testFileWritingBuilder{
		baseDir: sandboxDir,
		files: map[string]string{
			"math_engine.go": "// SPDX-License-Identifier: Apache-2.0\npackage main\n\nfunc Multiply(a, b int) int {\n\treturn a * b\n}\n",
		},
	}
	// REUSE-IgnoreEnd
	approver := &testApprover{}

	runner := &defaultFactoryRunner{
		gw:             gw,
		sandboxBaseDir: tempBase,
		specWriter:     specGen,
		builder:        builder,
		approver:       approver,
	}

	ctx := context.Background()
	task := agent.TaskContext{
		TaskID: taskID,
		Title:  "Implement high-performance math engine",
		Metadata: map[string]any{
			"target_branch": "main",
		},
	}

	outcome, err := runner.RunPipeline(ctx, task)
	if err != nil {
		t.Fatalf("unexpected error in RunPipeline: %v", err)
	}
	if outcome == nil {
		t.Fatal("expected non-nil outcome")
	}
	if !outcome.Success {
		t.Errorf("expected outcome.Success to be true")
	}
	if outcome.BuildResult == nil {
		t.Fatal("expected outcome.BuildResult to be non-nil")
	}

	// Assert real GitDiff is populated in BuildResult
	if outcome.BuildResult.GitDiff == "" {
		t.Fatal("expected outcome.BuildResult.GitDiff to be non-empty real diff")
	}
	if !strings.Contains(outcome.BuildResult.GitDiff, "diff --git a/math_engine.go b/math_engine.go") {
		t.Errorf("expected GitDiff to contain math_engine.go diff, got:\n%s", outcome.BuildResult.GitDiff)
	}

	// Assert Approver received the real diff
	if approver.lastReq.Diff == "" {
		t.Fatal("expected Approver to receive real diff in ApprovalRequest")
	}
	if !strings.Contains(approver.lastReq.Diff, "math_engine.go") {
		t.Errorf("expected Approver diff to contain math_engine.go, got:\n%s", approver.lastReq.Diff)
	}

	// Verify sandbox git branch and commit author
	branch, err := runGitCmd(ctx, sandboxDir, "branch", "--show-current")
	if err != nil || branch != "ov-run/"+taskID {
		t.Errorf("expected active branch 'ov-run/%s', got %q (err: %v)", taskID, branch, err)
	}

	authorName, _ := runGitCmd(ctx, sandboxDir, "log", "-1", "--format=%an")
	if authorName != "GV Factory Bot" {
		t.Errorf("expected author name 'GV Factory Bot', got %q", authorName)
	}
	authorEmail, _ := runGitCmd(ctx, sandboxDir, "log", "-1", "--format=%ae")
	if authorEmail != "bot@gv-factory.local" {
		t.Errorf("expected author email 'bot@gv-factory.local', got %q", authorEmail)
	}
}

func TestFactoryEndpoints_DefaultRunner_NoFilesModified(t *testing.T) {
	tempBase := t.TempDir()
	taskID := "task-no-files-404"
	sandboxDir := filepath.Join(tempBase, taskID)

	gw := gateway.NewGateway()
	specGen := &testSpecGen{}
	builder := &testFileWritingBuilder{
		baseDir: sandboxDir,
		files:   map[string]string{}, // no files written
	}
	approver := &testApprover{}

	runner := &defaultFactoryRunner{
		gw:             gw,
		sandboxBaseDir: tempBase,
		specWriter:     specGen,
		builder:        builder,
		approver:       approver,
	}

	ctx := context.Background()
	task := agent.TaskContext{
		TaskID: taskID,
		Title:  "Task with no file modifications",
	}

	outcome, err := runner.RunPipeline(ctx, task)
	if err != nil {
		t.Fatalf("unexpected error in RunPipeline: %v", err)
	}
	if outcome == nil {
		t.Fatal("expected non-nil outcome")
	}
	if outcome.BuildResult == nil {
		t.Fatal("expected non-nil outcome.BuildResult")
	}
}

func TestFactoryEndpoints_EdgeCases_GitCoverage(t *testing.T) {
	// 1. Sanitize identifier edge cases
	if res := sanitizeIdentifier(""); res != "task" {
		t.Errorf("expected 'task' for empty string, got %q", res)
	}
	if res := sanitizeIdentifier("a!b@c#d$"); res != "a_b_c_d_" {
		t.Errorf("expected 'a_b_c_d_', got %q", res)
	}

	// 2. commitAndExtractDiff with empty taskTitle
	tempDir := t.TempDir()
	taskID := "task-notitle-99"
	_ = initGitSandbox(context.Background(), tempDir, taskID, "main")
	_ = os.WriteFile(filepath.Join(tempDir, "sample.txt"), []byte("hello"), 0644)
	diff := commitAndExtractDiff(context.Background(), tempDir, taskID, "", "main")
	if !strings.Contains(diff, "sample.txt") {
		t.Errorf("expected diff to contain sample.txt, got %s", diff)
	}
	commitMsg, _ := runGitCmd(context.Background(), tempDir, "log", "-1", "--format=%s")
	if !strings.Contains(commitMsg, "implement atomic tasks") {
		t.Errorf("expected 'implement atomic tasks' in commit message, got %q", commitMsg)
	}

	// 3. gitSandboxBuilder with error from inner builder
	failingInner := &mockTaskBuilder{
		err: errors.New("builder failure"),
	}
	wrapped := &gitSandboxBuilder{
		inner:      failingInner,
		dir:        tempDir,
		taskID:     taskID,
		taskTitle:  "failing task",
		baseBranch: "main",
	}
	_, err := wrapped.BuildSpec(context.Background(), &agent.SpecDocument{})
	if err == nil || !strings.Contains(err.Error(), "builder failure") {
		t.Errorf("expected builder failure, got %v", err)
	}

	// 4. defaultFactoryRunner with base_branch metadata and env MODEL
	t.Setenv("MODEL", "gpt-4o-custom")
	tempBase := t.TempDir()
	runner := &defaultFactoryRunner{
		sandboxBaseDir: tempBase,
		builder: &testFileWritingBuilder{
			baseDir: filepath.Join(tempBase, "task-base-branch"),
			files:   map[string]string{"dummy.txt": "dummy"},
		},
		approver: &testApprover{},
	}
	out, err := runner.RunPipeline(context.Background(), agent.TaskContext{
		TaskID: "task-base-branch",
		Title:  "Task with base branch metadata",
		Metadata: map[string]any{
			"base_branch": "main",
		},
	})
	if err != nil || out == nil {
		t.Fatalf("unexpected failure with base_branch metadata: %v", err)
	}
}

type mockTaskBuilder struct {
	err error
}

func (m *mockTaskBuilder) BuildSpec(ctx context.Context, spec *agent.SpecDocument) ([]*agent.BuildResult, error) {
	return nil, m.err
}

type mockMergeQueue struct {
	mu       sync.Mutex
	enqueued []merger.MergeRequest
}

func (m *mockMergeQueue) Enqueue(ctx context.Context, req merger.MergeRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enqueued = append(m.enqueued, req)
	return nil
}

func TestFactoryEndpoints_HumanGate_LifecycleAndSSE(t *testing.T) {
	gw := gateway.NewGateway()
	defer gw.Close()

	authSvc := auth.NewService("test-secret-key-32-bytes-long!")
	st := store.NewMemoryStore()
	pendingTracker := NewPendingGateTracker(context.Background(), gw, st)

	gateWaitingEvents := make(chan gateway.Event, 10)
	_, _ = gw.SubscribeTaskEvents(context.Background(), "gate_waiting", gateWaitingEvents)

	taskID := "task-gate-100"
	expectedDiff := "diff --git a/auth.go b/auth.go\n+ func NewAuth() {}\n"
	expectedReason := "High risk security operation requires human approval"

	gateRunner := &mockFactoryRunner{
		runFn: func(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error) {
			return &agent.PipelineOutcome{
				TaskID:   task.TaskID,
				Success:  false,
				Decision: agent.DecisionHumanGate,
				BuildResult: &agent.BuildResult{
					TaskID:  task.TaskID,
					Success: true,
					GitDiff: expectedDiff,
				},
				Approval: &agent.ApprovalDecision{
					DecisionType:      agent.DecisionHumanGate,
					GateReason:        expectedReason,
					RequiresHumanSign: true,
				},
				Duration: 100 * time.Millisecond,
			}, nil
		},
	}

	mux := http.NewServeMux()
	tracker := RegisterFactoryEndpointsWithOptions(mux, gw, gateRunner, FactoryEndpointsOptions{
		AuthSvc:        authSvc,
		Store:          st,
		PendingTracker: pendingTracker,
	})

	reqBody := `{"task_id":"task-gate-100","title":"Implement OAuth","description":"Sensitive OAuth implementation"}`
	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/factory/run?async=false", strings.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, httpReq)
	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", w.Code, w.Body.String())
	}

	// 1. Verify response record in tracker
	rec, found := tracker.Get(taskID)
	if !found {
		t.Fatalf("expected run %s to be tracked", taskID)
	}
	if rec.Status != "gate_waiting" {
		t.Errorf("expected status 'gate_waiting', got %q", rec.Status)
	}
	if rec.Token == "" {
		t.Errorf("expected non-empty HMAC token in response record")
	}

	// 2. Verify token is cryptographically valid
	validatedRunID, valid := authSvc.ValidateApprovalToken(rec.Token)
	if !valid {
		t.Errorf("token generated was not valid HMAC token")
	}
	if validatedRunID != taskID {
		t.Errorf("expected token runID %s, got %s", taskID, validatedRunID)
	}

	// 3. Verify event emitted over SSE / Gateway
	select {
	case ev := <-gateWaitingEvents:
		if ev.RunID != taskID {
			t.Errorf("expected event runID %s, got %s", taskID, ev.RunID)
		}
		if ev.Payload["token"] != rec.Token {
			t.Errorf("expected event token %s, got %v", rec.Token, ev.Payload["token"])
		}
		if ev.Payload["diff"] != expectedDiff {
			t.Errorf("expected event diff %s, got %v", expectedDiff, ev.Payload["diff"])
		}
		if ev.Payload["reason"] != expectedReason {
			t.Errorf("expected event reason %s, got %v", expectedReason, ev.Payload["reason"])
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for gate_waiting SSE event")
	}

	// 4. Verify pending gate stored in store.PendingGate
	storedGates, err := st.ListPendingGates(context.Background())
	if err != nil {
		t.Fatalf("failed to list pending gates from store: %v", err)
	}
	if len(storedGates) != 1 {
		t.Fatalf("expected 1 stored pending gate, got %d", len(storedGates))
	}
	if storedGates[0].RunID != taskID {
		t.Errorf("expected stored gate runID %s, got %s", taskID, storedGates[0].RunID)
	}
	if storedGates[0].Arguments["diff"] != expectedDiff {
		t.Errorf("expected diff in arguments, got %v", storedGates[0].Arguments["diff"])
	}
	if storedGates[0].Arguments["reason"] != expectedReason {
		t.Errorf("expected reason in arguments, got %v", storedGates[0].Arguments["reason"])
	}
}

func TestServer_GateResolve_FactoryRun_EndToEnd(t *testing.T) {
	gw := gateway.NewGateway()
	defer gw.Close()

	authSvc := auth.NewService("test-secret-key-32-bytes-long!")
	st := store.NewMemoryStore()
	mq := &mockMergeQueue{}

	runID := "task-resolve-200"
	gateRunner := &mockFactoryRunner{
		runFn: func(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error) {
			return &agent.PipelineOutcome{
				TaskID:   task.TaskID,
				Success:  false,
				Decision: agent.DecisionHumanGate,
				BuildResult: &agent.BuildResult{
					TaskID:  task.TaskID,
					Success: true,
					GitDiff: "diff --git a/main.go b/main.go",
				},
				Approval: &agent.ApprovalDecision{
					DecisionType: agent.DecisionHumanGate,
					GateReason:   "Needs human review",
				},
			}, nil
		},
	}

	handler := NewServerMuxWithRunner(nil, gw, nil, nil, nil, authSvc, st, nil, gateRunner)

	// Step 1: Trigger run to produce gate_waiting
	runBody := `{"task_id":"task-resolve-200","title":"Task to Resolve"}`
	postReq := httptest.NewRequest(http.MethodPost, "/api/v1/factory/run?async=false", strings.NewReader(runBody))
	postReq.Header.Set("Content-Type", "application/json")
	postW := httptest.NewRecorder()
	handler.ServeHTTP(postW, postReq)

	if postW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", postW.Code, postW.Body.String())
	}

	// Step 2: Check pending gates
	pendingReq := httptest.NewRequest(http.MethodGet, "/api/v1/gates/pending", nil)
	pendingW := httptest.NewRecorder()
	handler.ServeHTTP(pendingW, pendingReq)

	var pendingResp struct {
		Gates []PendingGateInfo `json:"gates"`
		Total int               `json:"total"`
	}
	_ = json.NewDecoder(pendingW.Body).Decode(&pendingResp)
	if pendingResp.Total != 1 || len(pendingResp.Gates) != 1 {
		t.Fatalf("expected 1 pending gate, got %d", pendingResp.Total)
	}

	token := pendingResp.Gates[0].Token
	if token == "" {
		t.Fatalf("pending gate token is empty")
	}

	// Step 3: Resolve gate with 'approved'
	resolveBody := fmt.Sprintf(`{"run_id":"%s","token":"%s","decision":"approved"}`, runID, token)
	resolveReq := httptest.NewRequest(http.MethodPost, "/api/v1/gates/resolve", strings.NewReader(resolveBody))
	resolveReq.Header.Set("Content-Type", "application/json")
	resolveW := httptest.NewRecorder()
	handler.ServeHTTP(resolveW, resolveReq)

	if resolveW.Code != http.StatusOK {
		t.Fatalf("resolve failed with code %d: %s", resolveW.Code, resolveW.Body.String())
	}

	// Step 4: Verify factory run status transitioned to 'completed'
	statusReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/factory/runs/%s", runID), nil)
	statusW := httptest.NewRecorder()
	handler.ServeHTTP(statusW, statusReq)

	var runResp FactoryRunResponse
	_ = json.NewDecoder(statusW.Body).Decode(&runResp)
	if runResp.Status != "completed" {
		t.Errorf("expected factory run status 'completed', got %q", runResp.Status)
	}
	if runResp.Outcome == nil || runResp.Outcome.Decision != agent.DecisionApproved {
		t.Errorf("expected outcome decision APPROVED, got %+v", runResp.Outcome)
	}
	if runResp.Outcome.MergeStatus != "enqueued" {
		t.Errorf("expected merge_status 'enqueued', got %q", runResp.Outcome.MergeStatus)
	}

	// Step 5: Test rejection resolution transitions to 'failed'
	rejectRunID := "task-reject-300"
	rejectBody := fmt.Sprintf(`{"task_id":"%s","title":"Task to Reject"}`, rejectRunID)
	rReq := httptest.NewRequest(http.MethodPost, "/api/v1/factory/run?async=false", strings.NewReader(rejectBody))
	rReq.Header.Set("Content-Type", "application/json")
	rW := httptest.NewRecorder()
	handler.ServeHTTP(rW, rReq)

	rPendingReq := httptest.NewRequest(http.MethodGet, "/api/v1/gates/pending", nil)
	rPendingW := httptest.NewRecorder()
	handler.ServeHTTP(rPendingW, rPendingReq)
	_ = json.NewDecoder(rPendingW.Body).Decode(&pendingResp)

	var rejectToken string
	for _, g := range pendingResp.Gates {
		if g.RunID == rejectRunID {
			rejectToken = g.Token
			break
		}
	}
	if rejectToken == "" {
		t.Fatalf("could not find pending gate token for %s", rejectRunID)
	}

	resolveRejectBody := fmt.Sprintf(`{"run_id":"%s","token":"%s","decision":"rejected"}`, rejectRunID, rejectToken)
	resRejReq := httptest.NewRequest(http.MethodPost, "/api/v1/gates/resolve", strings.NewReader(resolveRejectBody))
	resRejReq.Header.Set("Content-Type", "application/json")
	resRejW := httptest.NewRecorder()
	handler.ServeHTTP(resRejW, resRejReq)

	if resRejW.Code != http.StatusOK {
		t.Fatalf("resolve reject failed with code %d: %s", resRejW.Code, resRejW.Body.String())
	}

	rStatusReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/factory/runs/%s", rejectRunID), nil)
	rStatusW := httptest.NewRecorder()
	handler.ServeHTTP(rStatusW, rStatusReq)

	var rejectRunResp FactoryRunResponse
	_ = json.NewDecoder(rStatusW.Body).Decode(&rejectRunResp)
	if rejectRunResp.Status != "failed" {
		t.Errorf("expected factory run status 'failed', got %q", rejectRunResp.Status)
	}
	_ = mq
}

func TestFactoryEndpoints_PostRun_ModelInjection(t *testing.T) {
	gw := gateway.NewGateway()
	defer gw.Close()

	var capturedTask agent.TaskContext
	var capturedMu sync.Mutex

	runner := &mockFactoryRunner{
		runFn: func(ctx context.Context, task agent.TaskContext) (*agent.PipelineOutcome, error) {
			capturedMu.Lock()
			capturedTask = task
			capturedMu.Unlock()
			return &agent.PipelineOutcome{
				TaskID:   task.TaskID,
				Success:  true,
				Decision: agent.DecisionApproved,
			}, nil
		},
	}

	mux := http.NewServeMux()
	RegisterFactoryEndpoints(mux, gw, runner)

	t.Run("injects model field into task metadata", func(t *testing.T) {
		payload := `{
			"task_id": "task-model-1",
			"title": "Build Model Pipeline",
			"model": "deepseek-chat"
		}`

		req := httptest.NewRequest(http.MethodPost, "/api/v1/factory/run?async=false", bytes.NewBufferString(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		capturedMu.Lock()
		task := capturedTask
		capturedMu.Unlock()

		if task.TaskID != "task-model-1" {
			t.Errorf("expected task_id 'task-model-1', got %q", task.TaskID)
		}
		if model, ok := task.Metadata["model"].(string); !ok || model != "deepseek-chat" {
			t.Errorf("expected task.Metadata['model'] to be 'deepseek-chat', got %v", task.Metadata["model"])
		}
	})

	t.Run("model in metadata is preserved when req.Model is empty", func(t *testing.T) {
		payload := `{
			"task_id": "task-model-2",
			"title": "Build Model Pipeline Metadata",
			"metadata": {
				"model": "gemini-3.8-flash-high"
			}
		}`

		req := httptest.NewRequest(http.MethodPost, "/api/v1/factory/run?async=false", bytes.NewBufferString(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		capturedMu.Lock()
		task := capturedTask
		capturedMu.Unlock()

		if model, ok := task.Metadata["model"].(string); !ok || model != "gemini-3.8-flash-high" {
			t.Errorf("expected task.Metadata['model'] to be 'gemini-3.8-flash-high', got %v", task.Metadata["model"])
		}
	})

	t.Run("req.Model overrides metadata.model when both provided", func(t *testing.T) {
		payload := `{
			"task_id": "task-model-3",
			"title": "Build Model Pipeline Override",
			"model": "claude-3-5-sonnet",
			"metadata": {
				"model": "old-model"
			}
		}`

		req := httptest.NewRequest(http.MethodPost, "/api/v1/factory/run?async=false", bytes.NewBufferString(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		capturedMu.Lock()
		task := capturedTask
		capturedMu.Unlock()

		if model, ok := task.Metadata["model"].(string); !ok || model != "claude-3-5-sonnet" {
			t.Errorf("expected task.Metadata['model'] to be 'claude-3-5-sonnet', got %v", task.Metadata["model"])
		}
	})
}

func TestFactoryEndpoints_RunPipeline_MetadataModelConfiguration(t *testing.T) {
	t.Setenv("MODEL", "default-env-model")
	tempBase := t.TempDir()

	runner := &defaultFactoryRunner{
		sandboxBaseDir: tempBase,
		builder: &testFileWritingBuilder{
			baseDir: filepath.Join(tempBase, "task-model-runner"),
			files:   map[string]string{"result.txt": "ok"},
		},
		approver: &testApprover{},
	}

	out, err := runner.RunPipeline(context.Background(), agent.TaskContext{
		TaskID: "task-model-runner",
		Title:  "Task with custom model in metadata",
		Metadata: map[string]any{
			"model": "ollama/llama3.3",
		},
	})
	if err != nil || out == nil {
		t.Fatalf("unexpected failure running pipeline with custom model: %v", err)
	}
}

func TestEvaluateCoverageWithStack(t *testing.T) {
	t.Run("nil or failed build returns 0", func(t *testing.T) {
		stack := sandbox.DefaultGoStack()
		if cov := EvaluateCoverageWithStack(nil, stack); cov != 0.0 {
			t.Fatalf("expected 0 for nil build, got %f", cov)
		}
		failedBuild := &agent.BuildResult{Success: false}
		if cov := EvaluateCoverageWithStack(failedBuild, stack); cov != 0.0 {
			t.Fatalf("expected 0 for failed build, got %f", cov)
		}
	})

	t.Run("go stack coverage", func(t *testing.T) {
		stack := sandbox.DefaultGoStack()
		build := &agent.BuildResult{
			Success: true,
			Phases: []agent.PhaseOutcome{
				{Phase: "Refactor", Stdout: "coverage: 91.5% of statements\nPASS"},
			},
		}
		cov := EvaluateCoverageWithStack(build, stack)
		if cov < 0.91 || cov > 0.92 {
			t.Fatalf("expected ~0.915, got %f", cov)
		}
	})

	t.Run("typescript stack coverage", func(t *testing.T) {
		stack := sandbox.StackInfo{Language: "typescript"}
		build := &agent.BuildResult{
			Success: true,
			Phases: []agent.PhaseOutcome{
				{Phase: "Refactor", Stdout: "All files | 89.5 | 85 | 100 | 89.5 |"},
			},
		}
		cov := EvaluateCoverageWithStack(build, stack)
		if cov < 0.89 || cov > 0.90 {
			t.Fatalf("expected ~0.895, got %f", cov)
		}
	})

	t.Run("python stack coverage", func(t *testing.T) {
		stack := sandbox.StackInfo{Language: "python"}
		build := &agent.BuildResult{
			Success: true,
			Phases: []agent.PhaseOutcome{
				{Phase: "Refactor", Stdout: "TOTAL    50    5    90%"},
			},
		}
		cov := EvaluateCoverageWithStack(build, stack)
		if cov < 0.89 || cov > 0.91 {
			t.Fatalf("expected ~0.90, got %f", cov)
		}
	})

	t.Run("rust stack coverage", func(t *testing.T) {
		stack := sandbox.StackInfo{Language: "rust"}
		build := &agent.BuildResult{
			Success: true,
			Phases: []agent.PhaseOutcome{
				{Phase: "Refactor", Stdout: "88.0% coverage, 176/200 lines"},
			},
		}
		cov := EvaluateCoverageWithStack(build, stack)
		if cov < 0.87 || cov > 0.89 {
			t.Fatalf("expected ~0.88, got %f", cov)
		}
	})

	t.Run("fallback 1.0 when successful without coverage text", func(t *testing.T) {
		stack := sandbox.StackInfo{Language: "go"}
		build := &agent.BuildResult{
			Success: true,
			Phases: []agent.PhaseOutcome{
				{Phase: "Refactor", Stdout: "clean build output"},
			},
		}
		cov := EvaluateCoverageWithStack(build, stack)
		if cov != 1.0 {
			t.Fatalf("expected 1.0 for success with no coverage string, got %f", cov)
		}
	})
}




