// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package telemetry_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/telemetry"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestStartStepSpanSuccess(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("dag-test")
	metrics := telemetry.NewMetrics()

	opts := telemetry.StepSpanOpts{
		FactoryID:  "bugfix-express",
		RunID:      "run-42",
		NodeID:     "node-tdd-1",
		NodeType:   "tdd",
		GitCommit:  "abcdef123456",
		AgentModel: "claude-3-5-sonnet",
	}

	ctx, span, done := telemetry.StartStepSpan(context.Background(), tracer, metrics, opts)
	if ctx == nil || span == nil || done == nil {
		t.Fatal("expected non-nil ctx, span, and done func")
	}

	time.Sleep(10 * time.Millisecond)
	done("COMPLETED", nil)

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 ended span, got %d", len(spans))
	}

	s := spans[0]
	expectedName := "dag.step.tdd"
	if s.Name() != expectedName {
		t.Errorf("expected span name %q, got %q", expectedName, s.Name())
	}

	attrMap := make(map[string]string)
	for _, a := range s.Attributes() {
		attrMap[string(a.Key)] = a.Value.AsString()
	}

	expectedAttrs := map[string]string{
		"factory.id":  "bugfix-express",
		"run.id":      "run-42",
		"node.id":     "node-tdd-1",
		"node.type":   "tdd",
		"git.commit":  "abcdef123456",
		"agent.model": "claude-3-5-sonnet",
		"step.status": "COMPLETED",
	}

	for k, v := range expectedAttrs {
		if attrMap[k] != v {
			t.Errorf("expected attribute %s=%q, got %q", k, v, attrMap[k])
		}
	}
}

func TestStartStepSpanFailure(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("dag-test")
	metrics := telemetry.NewMetrics()

	opts := telemetry.StepSpanOpts{
		FactoryID:  "feature-complete",
		RunID:      "run-99",
		NodeID:     "node-review-sec",
		NodeType:   "reviewer",
		GitCommit:  "123456abcdef",
		AgentModel: "claude-3-5-sonnet",
	}

	_, _, done := telemetry.StartStepSpan(context.Background(), tracer, metrics, opts)
	errSample := errors.New("security check failed")
	done("FAILED", errSample)

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 ended span, got %d", len(spans))
	}

	s := spans[0]
	if s.Name() != "dag.step.reviewer" {
		t.Errorf("expected 'dag.step.reviewer', got %q", s.Name())
	}

	foundErrorStatus := false
	for _, a := range s.Attributes() {
		if string(a.Key) == "step.status" && a.Value.AsString() == "FAILED" {
			foundErrorStatus = true
		}
	}
	if !foundErrorStatus {
		t.Error("expected step.status=FAILED in span attributes")
	}

	// Verify events recorded
	events := s.Events()
	if len(events) == 0 {
		t.Error("expected recorded exception event on error")
	} else if !strings.Contains(events[0].Name, "exception") {
		t.Errorf("expected exception event, got %q", events[0].Name)
	}
}

func TestStartStepSpanConcurrency(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("dag-concurrent-test")
	metrics := telemetry.NewMetrics()

	const concurrentNodes = 20
	var wg sync.WaitGroup

	for i := 0; i < concurrentNodes; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			opts := telemetry.StepSpanOpts{
				FactoryID: "multi-run",
				RunID:     "run-conc",
				NodeID:    "node-step",
				NodeType:  "tdd",
			}
			_, _, done := telemetry.StartStepSpan(context.Background(), tracer, metrics, opts)
			time.Sleep(2 * time.Millisecond)
			done("COMPLETED", nil)
		}(i)
	}

	wg.Wait()

	if len(sr.Ended()) != concurrentNodes {
		t.Errorf("expected %d spans, got %d", concurrentNodes, len(sr.Ended()))
	}
}
