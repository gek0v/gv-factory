// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/agent"
	"github.com/gv-factory/gv-factory/internal/gateway"
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
