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

	"github.com/gv-factory/gv-factory/internal/dag"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/sandbox"
	pdk "github.com/gv-factory/gv-factory/plugins-sdk/go"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

type workerMockCaller struct{}

func (m *workerMockCaller) CallPDK(ctx context.Context, pluginRef string, functionName string, req pdk.Context) (*pdk.Result, error) {
	return &pdk.Result{
		Success: true,
		Output:  "completed task " + req.TaskID,
		Data: map[string]any{
			"status": "ok",
		},
	}, nil
}

func TestWorker_PipelineExecution(t *testing.T) {
	worker := NewWorker(WorkerConfig{
		DisableNATS:    true,
		DefaultRetries: 2,
		DefaultTimeout: 2 * time.Second,
		SandboxConfig: sandbox.ContainerSandboxConfig{
			Runtime: sandbox.RuntimeHost,
		},
	})
	defer worker.Close()

	// Inject test caller for deterministic execution in worker tests
	worker.Dispatcher = dag.NewDispatcher(worker.DAG, &workerMockCaller{}, worker.Gateway, dag.DispatcherConfig{
		DefaultStepPolicy: dag.StepPolicy{
			MaxRetries:     2,
			InitialBackoff: 5 * time.Millisecond,
			Timeout:        1 * time.Second,
		},
	})

	eventsCh := make(chan gateway.Event, 20)
	unsub, err := worker.Gateway.SubscribeTaskEvents(context.Background(), gateway.TopicAllEvents, eventsCh)
	if err != nil {
		t.Fatalf("failed to subscribe to events: %v", err)
	}
	defer unsub()

	tpl := schema.FactoryTemplate{
		Metadata: schema.Metadata{
			Name: "worker-e2e",
		},
		Spec: schema.FactorySpec{
			Steps: []schema.StepConfig{
				{
					ID:     "ingestion",
					Name:   "Ingest Repository",
					Plugin: "extism://plugins/ingestion.wasm",
				},
				{
					ID:        "spec-writer",
					Name:      "Generate Spec",
					Plugin:    "extism://plugins/spec-writer.wasm",
					DependsOn: []string{"ingestion"},
				},
				{
					ID:        "builder-tdd",
					Name:      "Build TDD Code",
					Plugin:    "extism://plugins/builder-tdd.wasm",
					DependsOn: []string{"spec-writer"},
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	report, err := worker.ExecutePipeline(ctx, "run-worker-test", tpl)
	if err != nil {
		t.Fatalf("pipeline execution error: %v", err)
	}

	if report.Status != dag.StatusCompleted {
		t.Fatalf("expected StatusCompleted, got %s", report.Status)
	}
	if len(report.CompletedSteps) != 3 {
		t.Fatalf("expected 3 completed steps, got %d", len(report.CompletedSteps))
	}

	close(eventsCh)
	var gotTaskEvents int
	for ev := range eventsCh {
		if ev.RunID == "run-worker-test" {
			gotTaskEvents++
		}
	}
	if gotTaskEvents == 0 {
		t.Errorf("expected lifecycle events to be published during execution")
	}
}

func TestWorker_MultiFactoryPipelineSubmission(t *testing.T) {
	worker := NewWorker(WorkerConfig{
		DisableNATS:    true,
		DefaultRetries: 1,
		DefaultTimeout: 5 * time.Second,
		MaxConcurrent:  2,
		SandboxConfig: sandbox.ContainerSandboxConfig{
			Runtime: sandbox.RuntimeHost,
		},
	})
	defer worker.Close()

	if worker.Orchestrator == nil {
		t.Fatalf("expected orchestrator to be initialized on worker")
	}

	tpl := schema.FactoryTemplate{
		Metadata: schema.Metadata{
			Name: "worker-submit-test",
		},
		Spec: schema.FactorySpec{
			Steps: []schema.StepConfig{
				{
					ID:     "step-1",
					Name:   "Init",
					Plugin: "extism://plugins/init.wasm",
				},
			},
		},
	}

	run, err := worker.SubmitPipeline(context.Background(), dag.RunRequest{
		RunID:    "worker-sub-1",
		Template: tpl,
		Branch:   "ov-run/worker-branch",
	})
	if err != nil {
		t.Fatalf("unexpected error submitting pipeline: %v", err)
	}

	if run == nil || run.ID != "worker-sub-1" {
		t.Fatalf("expected run with ID worker-sub-1, got %v", run)
	}
}

func TestWorker_HealthMux(t *testing.T) {
	mux := NewHealthMux()

	for _, ep := range []string{"/healthz", "/health"} {
		t.Run(ep, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ep, nil)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected HTTP 200, got %d", rec.Code)
			}

			var resp map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode JSON response: %v", err)
			}

			if resp["status"] != "ok" {
				t.Errorf("expected status 'ok', got %v", resp["status"])
			}
			if resp["service"] != "ov-worker" {
				t.Errorf("expected service 'ov-worker', got %v", resp["service"])
			}
		})
	}
}

