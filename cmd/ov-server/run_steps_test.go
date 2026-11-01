// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/store"
)

func TestStepLifecycleTracker_TaskLifecyclePersistence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gw := gateway.NewGateway()
	st := store.NewMemoryStore()

	tracker := NewStepLifecycleTracker(ctx, gw, st)
	defer tracker.Close()

	runID := "run-lifecycle-1"

	// Create Run entity first
	err := st.CreateRun(ctx, &store.Run{
		ID:           runID,
		TemplateName: "feature-pipeline",
		Status:       store.RunStatusQueued,
		CreatedAt:    time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// 1. Task Started
	_ = gw.Publish(ctx, gateway.TopicTaskStarted, gateway.Event{
		Type:      gateway.TopicTaskStarted,
		RunID:     runID,
		TaskID:    "step-lint",
		Timestamp: time.Now().UnixMilli(),
		Payload: map[string]any{
			"step_name": "lint",
		},
	})

	time.Sleep(50 * time.Millisecond)

	steps, err := st.ListStepsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step recorded, got %d", len(steps))
	}
	if steps[0].StepName != "lint" {
		t.Errorf("expected step_name 'lint', got %q", steps[0].StepName)
	}
	if steps[0].Status != store.StepStatusRunning {
		t.Errorf("expected step status RUNNING, got %v", steps[0].Status)
	}

	// 2. Task Completed
	_ = gw.Publish(ctx, gateway.TopicTaskCompleted, gateway.Event{
		Type:      gateway.TopicTaskCompleted,
		RunID:     runID,
		TaskID:    "step-lint",
		Timestamp: time.Now().UnixMilli(),
		Payload: map[string]any{
			"step_name":   "lint",
			"duration_ms": 350,
			"output":      "0 errors found",
			"data":        map[string]any{"clean": true},
		},
	})

	time.Sleep(50 * time.Millisecond)

	steps, err = st.ListStepsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps recorded (started and completed), got %d", len(steps))
	}
	completedStep := steps[1]
	if completedStep.Status != store.StepStatusCompleted {
		t.Errorf("expected step status COMPLETED, got %v", completedStep.Status)
	}
	if completedStep.DurationMS != 350 {
		t.Errorf("expected duration_ms 350, got %d", completedStep.DurationMS)
	}
	if completedStep.Output["output"] != "0 errors found" {
		t.Errorf("expected output payload, got %v", completedStep.Output)
	}

	// 3. Task Failed
	_ = gw.Publish(ctx, gateway.TopicTaskFailed, gateway.Event{
		Type:      gateway.TopicTaskFailed,
		RunID:     runID,
		TaskID:    "step-test",
		Timestamp: time.Now().UnixMilli(),
		Payload: map[string]any{
			"step_name":   "unit-test",
			"duration_ms": 1200,
			"error":       "assertion failed in test_calc.go",
		},
	})

	time.Sleep(50 * time.Millisecond)

	steps, err = st.ListStepsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps recorded, got %d", len(steps))
	}
	failedStep := steps[2]
	if failedStep.Status != store.StepStatusFailed {
		t.Errorf("expected step status FAILED, got %v", failedStep.Status)
	}
	if failedStep.DurationMS != 1200 {
		t.Errorf("expected duration_ms 1200, got %d", failedStep.DurationMS)
	}
	if failedStep.ErrorMessage != "assertion failed in test_calc.go" {
		t.Errorf("expected error message recorded, got %q", failedStep.ErrorMessage)
	}
}

func TestStepLifecycleTracker_RunStatusPersistence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gw := gateway.NewGateway()
	st := store.NewMemoryStore()

	tracker := NewStepLifecycleTracker(ctx, gw, st)
	defer tracker.Close()

	runID := "run-status-1"

	err := st.CreateRun(ctx, &store.Run{
		ID:           runID,
		TemplateName: "pipeline",
		Status:       store.RunStatusQueued,
		CreatedAt:    time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// TopicRunStarted
	_ = gw.Publish(ctx, gateway.TopicRunStarted, gateway.Event{
		Type:      gateway.TopicRunStarted,
		RunID:     runID,
		Timestamp: time.Now().UnixMilli(),
	})
	time.Sleep(50 * time.Millisecond)

	r, _ := st.GetRun(ctx, runID)
	if r.Status != store.RunStatusRunning {
		t.Errorf("expected RUNNING status, got %v", r.Status)
	}

	// TopicRunCompleted
	_ = gw.Publish(ctx, gateway.TopicRunCompleted, gateway.Event{
		Type:      gateway.TopicRunCompleted,
		RunID:     runID,
		Timestamp: time.Now().UnixMilli(),
	})
	time.Sleep(50 * time.Millisecond)

	r, _ = st.GetRun(ctx, runID)
	if r.Status != store.RunStatusCompleted {
		t.Errorf("expected COMPLETED status, got %v", r.Status)
	}

	// TopicRunFailed for another run
	runID2 := "run-status-2"
	_ = st.CreateRun(ctx, &store.Run{
		ID:           runID2,
		TemplateName: "pipeline",
		Status:       store.RunStatusRunning,
		CreatedAt:    time.Now().UTC(),
	})

	_ = gw.Publish(ctx, gateway.TopicRunFailed, gateway.Event{
		Type:      gateway.TopicRunFailed,
		RunID:     runID2,
		Timestamp: time.Now().UnixMilli(),
		Payload: map[string]any{
			"error": "out of memory",
		},
	})
	time.Sleep(50 * time.Millisecond)

	r2, _ := st.GetRun(ctx, runID2)
	if r2.Status != store.RunStatusFailed {
		t.Errorf("expected FAILED status, got %v", r2.Status)
	}
	if r2.ErrorMessage != "out of memory" {
		t.Errorf("expected error message 'out of memory', got %q", r2.ErrorMessage)
	}
}

func TestServer_GetRunSteps(t *testing.T) {
	ctx := context.Background()
	gw := gateway.NewGateway()
	st := store.NewMemoryStore()

	server := NewServerMux(nil, gw, nil, nil, nil, nil, st, nil)

	runID := "run-steps-test"
	_ = st.CreateRun(ctx, &store.Run{
		ID:           runID,
		TemplateName: "test-tpl",
		Status:       store.RunStatusRunning,
		CreatedAt:    time.Now().UTC(),
	})

	_ = st.RecordStep(ctx, &store.Step{
		ID:         "step-1",
		RunID:      runID,
		StepName:   "compile",
		Status:     store.StepStatusCompleted,
		DurationMS: 240,
		Output:     map[string]any{"binary": "app"},
		CreatedAt:  time.Now().UTC(),
	})
	_ = st.RecordStep(ctx, &store.Step{
		ID:         "step-2",
		RunID:      runID,
		StepName:   "test",
		Status:     store.StepStatusRunning,
		DurationMS: 0,
		CreatedAt:  time.Now().UTC(),
	})

	// 1. Success GET /api/v1/runs/{id}/steps
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID+"/steps", nil)
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var steps []*store.Step
	if err := json.NewDecoder(rr.Body).Decode(&steps); err != nil {
		t.Fatalf("failed to decode steps JSON: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps returned, got %d", len(steps))
	}
	if steps[0].StepName != "compile" || steps[1].StepName != "test" {
		t.Errorf("unexpected steps ordering or content: %+v", steps)
	}

	// 2. Not Found GET /api/v1/runs/non-existent/steps
	reqNotFound := httptest.NewRequest(http.MethodGet, "/api/v1/runs/non-existent/steps", nil)
	rrNotFound := httptest.NewRecorder()
	server.ServeHTTP(rrNotFound, reqNotFound)

	if rrNotFound.Code != http.StatusNotFound {
		t.Fatalf("expected HTTP 404, got %d", rrNotFound.Code)
	}

	// 3. Method Not Allowed POST /api/v1/runs/{id}/steps
	reqMethod := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+runID+"/steps", nil)
	rrMethod := httptest.NewRecorder()
	server.ServeHTTP(rrMethod, reqMethod)

	if rrMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected HTTP 405 Method Not Allowed, got %d", rrMethod.Code)
	}
}

func TestServer_CoverageExtras(t *testing.T) {
	// 1. NewStepLifecycleTracker with nil params
	t1 := NewStepLifecycleTracker(context.Background(), nil, nil)
	t1.Close()

	// 2. processEvent with empty RunID or unknown event
	gw := gateway.NewGateway()
	st := store.NewMemoryStore()
	t2 := NewStepLifecycleTracker(context.Background(), gw, st)
	defer t2.Close()

	t2.processEvent(gateway.Event{RunID: ""})
	t2.processEvent(gateway.Event{RunID: "r-x", Type: "unknown.type"})
	t2.processEvent(gateway.Event{RunID: "r-x", Type: gateway.TopicRunCancelled})

	// 3. getClientIP with headers
	reqXFF := httptest.NewRequest(http.MethodGet, "/", nil)
	reqXFF.Header.Set("X-Forwarded-For", "203.0.113.195, 70.41.3.18")
	if ip := getClientIP(reqXFF); ip != "203.0.113.195" {
		t.Errorf("expected 203.0.113.195, got %q", ip)
	}

	reqXRI := httptest.NewRequest(http.MethodGet, "/", nil)
	reqXRI.Header.Set("X-Real-IP", "198.51.100.1")
	if ip := getClientIP(reqXRI); ip != "198.51.100.1" {
		t.Errorf("expected 198.51.100.1, got %q", ip)
	}
}
