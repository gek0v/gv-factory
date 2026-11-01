// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/telemetry"
	pdk "github.com/gv-factory/gv-factory/plugins-sdk/go"
	"github.com/gv-factory/gv-factory/pkg/schema"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type mockPluginCaller struct {
	mu        sync.Mutex
	callCount map[string]int
	failUntil map[string]int
	failAll   map[string]bool
	outputs   map[string]string
}

func newMockPluginCaller() *mockPluginCaller {
	return &mockPluginCaller{
		callCount: make(map[string]int),
		failUntil: make(map[string]int),
		failAll:   make(map[string]bool),
		outputs:   make(map[string]string),
	}
}

func (m *mockPluginCaller) CallPDK(ctx context.Context, pluginRef string, functionName string, req pdk.Context) (*pdk.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	taskID := req.TaskID
	m.callCount[taskID]++

	if m.failAll[taskID] {
		return &pdk.Result{Success: false, Output: "failed"}, fmt.Errorf("plugin error")
	}

	if until, ok := m.failUntil[taskID]; ok && m.callCount[taskID] < until {
		return &pdk.Result{Success: false, Output: "transient"}, fmt.Errorf("transient error")
	}

	out := m.outputs[taskID]
	if out == "" {
		out = "ok:" + taskID
	}

	return &pdk.Result{
		Success: true,
		Output:  out,
		Data:    map[string]any{"task": taskID},
	}, nil
}

func TestDispatcher_ThreeStepPipelineSuccess(t *testing.T) {
	engine := NewEngine()
	caller := newMockPluginCaller()
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	eventsCh := make(chan gateway.Event, 50)
	unsub, _ := gw.SubscribeTaskEvents(context.Background(), gateway.TopicAllEvents, eventsCh)
	defer unsub()

	dispatcher := NewDispatcher(engine, caller, gw, DispatcherConfig{
		DefaultStepPolicy: StepPolicy{
			MaxRetries:     2,
			InitialBackoff: 10 * time.Millisecond,
			Timeout:        2 * time.Second,
		},
	})

	// 3-step pipeline: ingestion -> spec-writer -> builder-tdd
	tpl := schema.FactoryTemplate{
		Metadata: schema.Metadata{Name: "code-factory"},
		Spec: schema.FactorySpec{
			Steps: []schema.StepConfig{
				{ID: "ingestion", Plugin: "extism://plugins/ingestion.wasm"},
				{ID: "spec-writer", Plugin: "extism://plugins/spec-writer.wasm", DependsOn: []string{"ingestion"}},
				{ID: "builder-tdd", Plugin: "extism://plugins/builder-tdd.wasm", DependsOn: []string{"spec-writer"}},
			},
		},
	}

	report, err := dispatcher.ExecuteWorkflow(context.Background(), "run-42", tpl)
	if err != nil {
		t.Fatalf("workflow failed: %v", err)
	}

	if report.Status != StatusCompleted || len(report.CompletedSteps) != 3 {
		t.Fatalf("unexpected report: %+v", report)
	}

	if caller.callCount["ingestion"] != 1 || caller.callCount["spec-writer"] != 1 || caller.callCount["builder-tdd"] != 1 {
		t.Errorf("unexpected call counts: %+v", caller.callCount)
	}

	close(eventsCh)
	hasCompletedRun := false
	for ev := range eventsCh {
		if ev.Type == gateway.TopicRunCompleted {
			hasCompletedRun = true
			break
		}
	}
	if !hasCompletedRun {
		t.Errorf("expected ov.run.completed event")
	}
}

func TestDispatcher_RetryOnTransientFailure(t *testing.T) {
	engine := NewEngine()
	caller := newMockPluginCaller()
	caller.failUntil["builder-tdd"] = 2

	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	dispatcher := NewDispatcher(engine, caller, gw, DispatcherConfig{
		DefaultStepPolicy: StepPolicy{
			MaxRetries:     3,
			InitialBackoff: 5 * time.Millisecond,
			Timeout:        1 * time.Second,
		},
	})

	tpl := schema.FactoryTemplate{
		Metadata: schema.Metadata{Name: "retry-factory"},
		Spec: schema.FactorySpec{
			Steps: []schema.StepConfig{
				{ID: "builder-tdd", Plugin: "extism://plugins/builder-tdd.wasm"},
			},
		},
	}

	report, err := dispatcher.ExecuteWorkflow(context.Background(), "run-retry", tpl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Status != StatusCompleted {
		t.Fatalf("expected StatusCompleted, got %s", report.Status)
	}
	if caller.callCount["builder-tdd"] != 2 {
		t.Fatalf("expected 2 attempts, got %d", caller.callCount["builder-tdd"])
	}
}

func TestDispatcher_FailureAndBlockCascading(t *testing.T) {
	engine := NewEngine()
	caller := newMockPluginCaller()
	caller.failAll["step-1"] = true

	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	dispatcher := NewDispatcher(engine, caller, gw, DispatcherConfig{
		DefaultStepPolicy: StepPolicy{
			MaxRetries:     2,
			InitialBackoff: 5 * time.Millisecond,
			Timeout:        500 * time.Millisecond,
		},
	})

	tpl := schema.FactoryTemplate{
		Metadata: schema.Metadata{Name: "fail-cascade"},
		Spec: schema.FactorySpec{
			Steps: []schema.StepConfig{
				{ID: "step-1", Plugin: "p1"},
				{ID: "step-2", Plugin: "p2", DependsOn: []string{"step-1"}},
			},
		},
	}

	report, err := dispatcher.ExecuteWorkflow(context.Background(), "run-fail", tpl)
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}

	if report.Status != StatusFailed {
		t.Fatalf("expected StatusFailed, got %s", report.Status)
	}
	if caller.callCount["step-2"] != 0 {
		t.Fatalf("expected step-2 not to run, got %d", caller.callCount["step-2"])
	}
}

func TestDispatcher_TelemetrySpansAndMetrics(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("dag-telemetry-test")
	metrics := telemetry.NewMetrics()

	engine := NewEngine()
	caller := newMockPluginCaller()
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	dispatcher := NewDispatcher(engine, caller, gw, DispatcherConfig{
		DefaultStepPolicy: StepPolicy{
			MaxRetries: 1,
			Timeout:    500 * time.Millisecond,
		},
		Tracer:  tracer,
		Metrics: metrics,
	})

	tpl := schema.FactoryTemplate{
		Metadata: schema.Metadata{Name: "telemetry-pipeline"},
		Spec: schema.FactorySpec{
			Steps: []schema.StepConfig{
				{
					ID:     "step-tdd",
					Plugin: "plugin-tdd",
					Config: map[string]any{"node_type": "tdd", "git_commit": "c0ffee"},
				},
				{
					ID:        "step-review",
					Plugin:    "plugin-reviewer",
					DependsOn: []string{"step-tdd"},
					Config:    map[string]any{"node_type": "review"},
				},
			},
		},
	}

	report, err := dispatcher.ExecuteWorkflow(context.Background(), "run-tel-43", tpl)
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}
	if report.Status != StatusCompleted {
		t.Fatalf("expected StatusCompleted, got %s", report.Status)
	}

	spans := sr.Ended()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}

	spanNames := make(map[string]bool)
	for _, s := range spans {
		spanNames[s.Name()] = true
	}

	if !spanNames["dag.step.tdd"] {
		t.Errorf("missing expected span 'dag.step.tdd'")
	}
	if !spanNames["dag.step.review"] {
		t.Errorf("missing expected span 'dag.step.review'")
	}
}
