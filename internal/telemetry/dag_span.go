// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package telemetry

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// StepSpanOpts holds metadata attributes for structured DAG task execution spans.
type StepSpanOpts struct {
	FactoryID  string `json:"factory_id"`
	RunID      string `json:"run_id"`
	NodeID     string `json:"node_id"`
	NodeType   string `json:"node_type"`
	GitCommit  string `json:"git_commit"`
	AgentModel string `json:"agent_model"`
}

// StartStepSpan starts an OpenTelemetry span formatted as "dag.step.<node_type>"
// and returns the span, enriched context, and a completion function.
func StartStepSpan(
	ctx context.Context,
	tracer trace.Tracer,
	metrics *Metrics,
	opts StepSpanOpts,
) (context.Context, trace.Span, func(status string, err error)) {
	if ctx == nil {
		ctx = context.Background()
	}
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("dag.step")
	}

	nodeType := strings.ToLower(strings.TrimSpace(opts.NodeType))
	if nodeType == "" {
		nodeType = "unknown"
	}

	spanName := fmt.Sprintf("dag.step.%s", nodeType)
	startTime := time.Now()

	attrs := []attribute.KeyValue{
		attribute.String("factory.id", opts.FactoryID),
		attribute.String("run.id", opts.RunID),
		attribute.String("node.id", opts.NodeID),
		attribute.String("node.type", nodeType),
		attribute.String("git.commit", opts.GitCommit),
		attribute.String("agent.model", opts.AgentModel),
		attribute.String("step.status", "RUNNING"),
	}

	ctx, span := tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))

	var once sync.Once
	done := func(status string, err error) {
		once.Do(func() {
			duration := time.Since(startTime)
			statusNorm := strings.ToUpper(strings.TrimSpace(status))
			if statusNorm == "" {
				if err != nil {
					statusNorm = "FAILED"
				} else {
					statusNorm = "COMPLETED"
				}
			}

			span.SetAttributes(attribute.String("step.status", statusNorm))

			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			} else {
				span.SetStatus(codes.Ok, statusNorm)
			}

			if metrics != nil {
				metrics.RecordDAGStep(nodeType, statusNorm, duration)
			}

			span.End()
		})
	}

	return ctx, span, done
}
