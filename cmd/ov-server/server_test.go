// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/dag"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

type mockOrchestrator struct {
	mu        sync.RWMutex
	runs      map[string]*dag.FactoryRun
	submitErr error
	cancelErr error
}

func newMockOrchestrator() *mockOrchestrator {
	return &mockOrchestrator{
		runs: make(map[string]*dag.FactoryRun),
	}
}

func (m *mockOrchestrator) SubmitRun(ctx context.Context, req dag.RunRequest) (*dag.FactoryRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.submitErr != nil {
		return nil, m.submitErr
	}

	if _, exists := m.runs[req.RunID]; exists {
		return nil, fmt.Errorf("run %q already exists", req.RunID)
	}

	run := &dag.FactoryRun{
		ID:           req.RunID,
		TemplateName: req.Template.Metadata.Name,
		Branch:       req.Branch,
		State:        dag.FactoryRunStateQueued,
		TotalSteps:   len(req.Template.Spec.Steps),
		QueuedAt:     time.Now(),
		RepoPath:     req.RepoPath,
		Labels:       req.Labels,
	}

	m.runs[req.RunID] = run
	return run, nil
}

func (m *mockOrchestrator) ListRuns() []*dag.FactoryRun {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runs := make([]*dag.FactoryRun, 0, len(m.runs))
	for _, r := range m.runs {
		copied := *r
		runs = append(runs, &copied)
	}
	return runs
}

func (m *mockOrchestrator) GetRun(runID string) (*dag.FactoryRun, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	r, exists := m.runs[runID]
	if !exists {
		return nil, dag.ErrRunNotFound
	}

	copied := *r
	return &copied, nil
}

func (m *mockOrchestrator) CancelRun(runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancelErr != nil {
		return m.cancelErr
	}

	r, exists := m.runs[runID]
	if !exists {
		return dag.ErrRunNotFound
	}

	r.State = dag.FactoryRunStateCancelled
	return nil
}

func validTestTemplate() *schema.FactoryTemplate {
	return &schema.FactoryTemplate{
		APIVersion: "factory.ov.io/v1alpha1",
		Kind:       "FactoryTemplate",
		Metadata: schema.Metadata{
			Name: "test-pipeline",
		},
		Spec: schema.FactorySpec{
			Entrypoint: "step-1",
			Steps: []schema.StepConfig{
				{
					ID:     "step-1",
					Name:   "Trigger",
					Plugin: "extism://plugins/ingest.wasm",
				},
				{
					ID:        "step-2",
					Name:      "Build",
					Plugin:    "extism://plugins/builder.wasm",
					DependsOn: []string{"step-1"},
				},
			},
		},
	}
}

func cyclicTestTemplate() *schema.FactoryTemplate {
	return &schema.FactoryTemplate{
		APIVersion: "factory.ov.io/v1alpha1",
		Kind:       "FactoryTemplate",
		Metadata: schema.Metadata{
			Name: "cyclic-pipeline",
		},
		Spec: schema.FactorySpec{
			Entrypoint: "step-1",
			Steps: []schema.StepConfig{
				{
					ID:        "step-1",
					Name:      "Step 1",
					Plugin:    "extism://plugins/step1.wasm",
					DependsOn: []string{"step-2"},
				},
				{
					ID:        "step-2",
					Name:      "Step 2",
					Plugin:    "extism://plugins/step2.wasm",
					DependsOn: []string{"step-1"},
				},
			},
		},
	}
}

func setupAPITestServer(t *testing.T, orch PipelineOrchestrator) (http.Handler, *gateway.Gateway) {
	t.Helper()
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	mux := NewServerMux(nil, gw, nil, nil, orch, nil, nil, nil)
	return mux, gw
}

func TestServer_PipelineValidate(t *testing.T) {
	orch := newMockOrchestrator()
	mux, gw := setupAPITestServer(t, orch)
	defer gw.Close()

	t.Run("Valid FactoryTemplate returns valid:true", func(t *testing.T) {
		reqBody := map[string]any{
			"template": validTestTemplate(),
		}
		data, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/validate", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		if resp["valid"] != true {
			t.Errorf("expected valid=true, got %v", resp["valid"])
		}
	})

	t.Run("Cyclic FactoryTemplate returns valid:false with errors", func(t *testing.T) {
		reqBody := map[string]any{
			"template": cyclicTestTemplate(),
		}
		data, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/validate", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		if resp["valid"] != false {
			t.Errorf("expected valid=false, got %v", resp["valid"])
		}
		errorsArr, ok := resp["errors"].([]any)
		if !ok || len(errorsArr) == 0 {
			t.Errorf("expected errors array, got %v", resp["errors"])
		}
	})

	t.Run("Valid CanvasGraph returns valid:true", func(t *testing.T) {
		cg, err := dag.TemplateToCanvas(validTestTemplate())
		if err != nil {
			t.Fatalf("failed to convert template to canvas: %v", err)
		}

		reqBody := map[string]any{
			"canvas_graph": cg,
		}
		data, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/validate", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		if resp["valid"] != true {
			t.Errorf("expected valid=true, got %v", resp["valid"])
		}
	})

	t.Run("Empty payload returns 400 Bad Request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/validate", bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("Method Not Allowed on GET", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/validate", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 Method Not Allowed, got %d", rec.Code)
		}
	})
}

func TestServer_RunsLifecycle(t *testing.T) {
	orch := newMockOrchestrator()
	mux, gw := setupAPITestServer(t, orch)
	defer gw.Close()

	var createdRunID string

	t.Run("POST /api/v1/runs submits valid pipeline returning 201 Created", func(t *testing.T) {
		reqBody := map[string]any{
			"run_id":    "test-run-001",
			"template":  validTestTemplate(),
			"repo_path": "/tmp/repo",
			"branch":    "feat/test-001",
		}
		data, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d (body: %s)", rec.Code, rec.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		if resp["run_id"] != "test-run-001" {
			t.Errorf("expected run_id test-run-001, got %v", resp["run_id"])
		}
		if resp["state"] != "QUEUED" {
			t.Errorf("expected state QUEUED, got %v", resp["state"])
		}

		createdRunID = "test-run-001"
	})

	t.Run("POST /api/v1/runs with canvas_graph generates run", func(t *testing.T) {
		cg, err := dag.TemplateToCanvas(validTestTemplate())
		if err != nil {
			t.Fatalf("failed to convert template to canvas: %v", err)
		}

		reqBody := map[string]any{
			"canvas_graph": cg,
		}
		data, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d (body: %s)", rec.Code, rec.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		runID, ok := resp["run_id"].(string)
		if !ok || runID == "" {
			t.Errorf("expected generated run_id, got %v", resp["run_id"])
		}
	})

	t.Run("POST /api/v1/runs with cyclic DAG returns 400 Bad Request", func(t *testing.T) {
		reqBody := map[string]any{
			"template": cyclicTestTemplate(),
		}
		data, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d (body: %s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/runs with malformed JSON returns 400 Bad Request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader([]byte("{invalid-json")))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("GET /api/v1/runs lists active/recent runs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/runs", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var runs []*dag.FactoryRun
		if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil {
			t.Fatalf("failed to decode runs array: %v", err)
		}

		if len(runs) < 2 {
			t.Errorf("expected at least 2 runs, got %d", len(runs))
		}
	})

	t.Run("GET /api/v1/runs/{id} returns run details", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+createdRunID, nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var run dag.FactoryRun
		if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
			t.Fatalf("failed to decode run JSON: %v", err)
		}

		if run.ID != createdRunID {
			t.Errorf("expected run ID %q, got %q", createdRunID, run.ID)
		}
	})

	t.Run("GET /api/v1/runs/{id} returns 404 for non-existent run", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/non-existent-id", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/runs/{id}/cancel cancels active run returning 200 OK", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+createdRunID+"/cancel", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d (body: %s)", rec.Code, rec.Body.String())
		}

		// Verify state cancelled
		r, err := orch.GetRun(createdRunID)
		if err != nil {
			t.Fatalf("failed to get run: %v", err)
		}
		if r.State != dag.FactoryRunStateCancelled {
			t.Errorf("expected run state CANCELLED, got %s", r.State)
		}
	})

	t.Run("POST /api/v1/runs/{id}/cancel returns 404 for non-existent run", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/non-existent-id/cancel", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})

	t.Run("GET /api/v1/runs/{id}/events returns 404 for non-existent run", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/non-existent-id/events", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/runs/{id}/events returns 405 Method Not Allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+createdRunID+"/events", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 Method Not Allowed, got %d", rec.Code)
		}
	})

	t.Run("GET /api/v1/runs/{id}/events streams SSE filtered for run", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/runs/"+createdRunID+"/events", nil)
		rec := httptest.NewRecorder()

		done := make(chan struct{})
		go func() {
			mux.ServeHTTP(rec, req)
			close(done)
		}()

		// Wait for registration
		time.Sleep(30 * time.Millisecond)

		// Broadcast event for another run
		_ = gw.PublishTaskEvent(context.Background(), gateway.Event{
			Type:   gateway.TopicTaskLog,
			RunID:  "other-run-id",
			TaskID: "step-1",
			Payload: map[string]any{
				"stream":  "stdout",
				"message": "other log",
			},
		})

		// Broadcast event for this run
		_ = gw.PublishTaskEvent(context.Background(), gateway.Event{
			Type:   gateway.TopicTaskLog,
			RunID:  createdRunID,
			TaskID: "step-2",
			Payload: map[string]any{
				"stream":  "stdout",
				"message": "my run log",
			},
		})

		time.Sleep(50 * time.Millisecond)
		cancel()

		select {
		case <-done:
		case <-time.After(1 * time.Second):
			t.Fatal("ServeHTTP did not terminate after client context cancellation")
		}

		if rec.Header().Get("Content-Type") != "text/event-stream" {
			t.Errorf("expected Content-Type text/event-stream, got %s", rec.Header().Get("Content-Type"))
		}

		body := rec.Body.String()
		if !strings.Contains(body, "my run log") {
			t.Errorf("expected body to contain 'my run log', got: %s", body)
		}
		if strings.Contains(body, "other log") {
			t.Errorf("expected body NOT to contain 'other log', got: %s", body)
		}
	})
}

func TestServer_CORS(t *testing.T) {
	orch := newMockOrchestrator()
	mux, gw := setupAPITestServer(t, orch)
	defer gw.Close()

	t.Run("OPTIONS preflight returns 204 with CORS headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/runs", nil)
		req.Header.Set("Origin", "http://localhost:5173")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content, got %d", rec.Code)
		}

		origin := rec.Header().Get("Access-Control-Allow-Origin")
		if origin != "http://localhost:5173" {
			t.Errorf("expected Access-Control-Allow-Origin http://localhost:5173, got %q", origin)
		}

		methods := rec.Header().Get("Access-Control-Allow-Methods")
		if !bytes.Contains([]byte(methods), []byte("POST")) {
			t.Errorf("expected POST in Allow-Methods, got %q", methods)
		}
	})

	t.Run("Wildcard origin allowed for other origins", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		origin := rec.Header().Get("Access-Control-Allow-Origin")
		if origin != "*" {
			t.Errorf("expected Access-Control-Allow-Origin *, got %q", origin)
		}
	})
}

func TestServer_WorkerPoolEndpoints(t *testing.T) {
	orch := newMockOrchestrator()
	mux, gw := setupAPITestServer(t, orch)
	defer gw.Close()

	t.Run("GET /api/v1/workers returns empty list initially", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workers", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp WorkersResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON response: %v", err)
		}

		if resp.TotalWorkers != 0 || resp.ActiveWorkers != 0 || resp.TotalCapacity != 0 || resp.TotalActiveTasks != 0 {
			t.Errorf("expected all zeros, got %+v", resp)
		}
		if resp.Workers == nil || len(resp.Workers) != 0 {
			t.Errorf("expected empty non-nil workers slice, got %v", resp.Workers)
		}
	})

	t.Run("GET /api/v1/workers and GET /api/v1/workers/{id} populated via heartbeat", func(t *testing.T) {
		// Emit heartbeat via Gateway
		hbPayload := map[string]any{
			"worker_id":       "worker-alpha",
			"active_tasks":    1,
			"max_concurrency": 4,
			"uptime_seconds":  120,
			"timestamp":       time.Now().Unix(),
		}

		err := gw.PublishTaskEvent(context.Background(), gateway.Event{
			Type:      gateway.TopicWorkerHeartbeat,
			RunID:     "worker-alpha",
			Payload:   hbPayload,
			Timestamp: time.Now().UnixMilli(),
		})
		if err != nil {
			t.Fatalf("failed to publish heartbeat: %v", err)
		}

		// Wait briefly for asynchronous registry processing
		time.Sleep(50 * time.Millisecond)

		// 1. GET /api/v1/workers
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workers", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp WorkersResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.TotalWorkers != 1 || resp.ActiveWorkers != 1 {
			t.Errorf("expected 1 total and active worker, got total=%d active=%d", resp.TotalWorkers, resp.ActiveWorkers)
		}
		if resp.TotalCapacity != 4 || resp.TotalActiveTasks != 1 {
			t.Errorf("expected capacity=4 tasks=1, got cap=%d tasks=%d", resp.TotalCapacity, resp.TotalActiveTasks)
		}
		if len(resp.Workers) != 1 || resp.Workers[0].WorkerID != "worker-alpha" {
			t.Fatalf("expected worker-alpha in list, got %+v", resp.Workers)
		}
		if resp.Workers[0].Status != WorkerStatusReady {
			t.Errorf("expected status %q, got %q", WorkerStatusReady, resp.Workers[0].Status)
		}

		// 2. GET /api/v1/workers/worker-alpha
		reqSingle := httptest.NewRequest(http.MethodGet, "/api/v1/workers/worker-alpha", nil)
		recSingle := httptest.NewRecorder()
		mux.ServeHTTP(recSingle, reqSingle)

		if recSingle.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for worker-alpha, got %d: %s", recSingle.Code, recSingle.Body.String())
		}

		var single WorkerInfo
		if err := json.NewDecoder(recSingle.Body).Decode(&single); err != nil {
			t.Fatalf("failed to decode single worker: %v", err)
		}
		if single.WorkerID != "worker-alpha" || single.Status != WorkerStatusReady || single.MaxConcurrency != 4 {
			t.Errorf("unexpected single worker info: %+v", single)
		}

		// 3. GET /api/v1/workers/unknown-worker -> 404
		reqNotFound := httptest.NewRequest(http.MethodGet, "/api/v1/workers/unknown-worker", nil)
		recNotFound := httptest.NewRecorder()
		mux.ServeHTTP(recNotFound, reqNotFound)

		if recNotFound.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", recNotFound.Code)
		}
	})

	t.Run("Method Not Allowed on workers endpoints", func(t *testing.T) {
		reqPost := httptest.NewRequest(http.MethodPost, "/api/v1/workers", nil)
		recPost := httptest.NewRecorder()
		mux.ServeHTTP(recPost, reqPost)
		if recPost.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 for POST /api/v1/workers, got %d", recPost.Code)
		}

		reqPostSingle := httptest.NewRequest(http.MethodPost, "/api/v1/workers/worker-alpha", nil)
		recPostSingle := httptest.NewRecorder()
		mux.ServeHTTP(recPostSingle, reqPostSingle)
		if recPostSingle.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 for POST /api/v1/workers/{id}, got %d", recPostSingle.Code)
		}
	})

	t.Run("Heartbeat broadcast received over SSE (/api/v1/events)", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
		rec := httptest.NewRecorder()

		done := make(chan struct{})
		go func() {
			defer close(done)
			mux.ServeHTTP(rec, req)
		}()

		// Wait for client registration
		time.Sleep(30 * time.Millisecond)

		// Broadcast a worker heartbeat
		hbEvent := gateway.Event{
			Type:  gateway.TopicWorkerHeartbeat,
			RunID: "worker-sse",
			Payload: map[string]any{
				"worker_id":       "worker-sse",
				"active_tasks":    0,
				"max_concurrency": 2,
				"uptime_seconds":  50,
			},
			Timestamp: time.Now().UnixMilli(),
		}
		_ = gw.PublishTaskEvent(context.Background(), hbEvent)

		time.Sleep(50 * time.Millisecond)
		cancel()

		select {
		case <-done:
		case <-time.After(1 * time.Second):
			t.Fatal("SSE handler did not close after context cancel")
		}

		body := rec.Body.String()
		if !strings.Contains(body, "worker-sse") {
			t.Errorf("expected SSE body to contain worker-sse, got: %s", body)
		}
	})
}
