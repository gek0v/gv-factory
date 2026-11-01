// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/telemetry"
	"github.com/gv-factory/gv-factory/pkg/schema"
	pdk "github.com/gv-factory/gv-factory/plugins-sdk/go"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Dispatcher coordinates Hatchet/WASM task execution across DAG topologies.
type Dispatcher struct {
	engine    *Engine
	caller    PluginCaller
	publisher EventPublisher
	cfg       DispatcherConfig
	tracer    trace.Tracer
	metrics   *telemetry.Metrics
}

// NewDispatcher creates an initialized task dispatcher.
func NewDispatcher(engine *Engine, caller PluginCaller, pub EventPublisher, cfg DispatcherConfig) *Dispatcher {
	if cfg.DefaultStepPolicy.MaxRetries <= 0 {
		cfg.DefaultStepPolicy.MaxRetries = 1
	}
	if cfg.DefaultStepPolicy.Timeout <= 0 {
		cfg.DefaultStepPolicy.Timeout = 30 * time.Second
	}
	if cfg.DefaultStepPolicy.InitialBackoff <= 0 {
		cfg.DefaultStepPolicy.InitialBackoff = 20 * time.Millisecond
	}
	if cfg.StepPolicies == nil {
		cfg.StepPolicies = make(map[string]StepPolicy)
	}

	tracer := cfg.Tracer
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("dag.dispatcher")
	}

	return &Dispatcher{
		engine:    engine,
		caller:    caller,
		publisher: pub,
		cfg:       cfg,
		tracer:    tracer,
		metrics:   cfg.Metrics,
	}
}

// ExecuteWorkflow executes an entire pipeline template to completion.
func (d *Dispatcher) ExecuteWorkflow(ctx context.Context, runID string, tpl schema.FactoryTemplate) (*WorkflowReport, error) {
	graphID := fmt.Sprintf("%s-%s", tpl.Metadata.Name, runID)
	g, err := MapTemplateToGraph(graphID, tpl)
	if err != nil {
		return nil, err
	}

	d.engine.RegisterGraph(g)

	if d.publisher != nil {
		for _, node := range g.Nodes {
			_ = d.publisher.PublishTaskEvent(ctx, gateway.Event{
				Type:   gateway.TopicTaskPending,
				RunID:  runID,
				TaskID: node.ID,
			})
		}
	}

	stepResults := make(map[string]*pdk.Result)
	var mu sync.Mutex

	for {
		select {
		case <-ctx.Done():
			if d.metrics != nil {
				d.metrics.RecordFactoryRun("CANCELLED")
			}
			return nil, ctx.Err()
		default:
		}

		readyNodes, err := d.engine.GetReadyNodes(graphID)
		if err != nil {
			return nil, err
		}

		if len(readyNodes) == 0 {
			currentGraph, err := d.engine.GetGraph(graphID)
			if err != nil {
				return nil, err
			}

			allCompleted := true
			var failed, completed []string
			for _, n := range currentGraph.Nodes {
				if n.Status == StatusCompleted {
					completed = append(completed, n.ID)
				} else {
					allCompleted = false
					if n.Status == StatusFailed || n.Status == StatusBlocked {
						failed = append(failed, n.ID)
					}
				}
			}

			if allCompleted {
				if d.metrics != nil {
					d.metrics.RecordFactoryRun("COMPLETED")
				}
				if d.publisher != nil {
					_ = d.publisher.PublishTaskEvent(ctx, gateway.Event{
						Type:  gateway.TopicRunCompleted,
						RunID: runID,
					})
				}
				return &WorkflowReport{
					RunID:          runID,
					GraphID:        graphID,
					Status:         StatusCompleted,
					CompletedSteps: completed,
					StepResults:    stepResults,
				}, nil
			}

			if d.metrics != nil {
				d.metrics.RecordFactoryRun("FAILED")
			}
			return &WorkflowReport{
				RunID:          runID,
				GraphID:        graphID,
				Status:         StatusFailed,
				CompletedSteps: completed,
				FailedSteps:    failed,
				StepResults:    stepResults,
			}, nil
		}

		var wg sync.WaitGroup
		for _, node := range readyNodes {
			wg.Add(1)
			go func(n *Node) {
				defer wg.Done()
				res, durationMS, execErr := d.executeNodeWithPolicy(ctx, runID, graphID, n)
				mu.Lock()
				if res != nil {
					stepResults[n.ID] = res
				}
				mu.Unlock()

				if execErr != nil || (res != nil && !res.Success) {
					_ = d.engine.SetNodeStatus(graphID, n.ID, StatusFailed)
					if d.publisher != nil {
						errMsg := "unknown execution error"
						if execErr != nil {
							errMsg = execErr.Error()
						} else if res != nil {
							errMsg = res.Output
						}
						_ = d.publisher.PublishTaskEvent(ctx, gateway.Event{
							Type:   gateway.TopicTaskFailed,
							RunID:  runID,
							TaskID: n.ID,
							Payload: map[string]any{
								"error":       errMsg,
								"duration_ms": durationMS,
							},
						})
					}
				} else {
					_ = d.engine.SetNodeStatus(graphID, n.ID, StatusCompleted)
					if d.publisher != nil {
						var data map[string]any
						var output string
						if res != nil {
							data = res.Data
							output = res.Output
						}
						_ = d.publisher.PublishTaskEvent(ctx, gateway.Event{
							Type:   gateway.TopicTaskCompleted,
							RunID:  runID,
							TaskID: n.ID,
							Payload: map[string]any{
								"output":      output,
								"data":        data,
								"duration_ms": durationMS,
							},
						})
					}
				}
			}(node)
		}
		wg.Wait()

		if err := d.engine.Step(ctx, graphID); err != nil {
			return nil, err
		}
	}
}

func (d *Dispatcher) executeNodeWithPolicy(ctx context.Context, runID, graphID string, node *Node) (*pdk.Result, int64, error) {
	startTime := time.Now()
	nodeType := "step"
	if nt, ok := node.Inputs["node_type"].(string); ok && nt != "" {
		nodeType = nt
	} else if strings.Contains(strings.ToLower(node.PluginURI+node.ID), "tdd") {
		nodeType = "tdd"
	} else if strings.Contains(strings.ToLower(node.PluginURI+node.ID), "review") {
		nodeType = "review"
	} else if strings.Contains(strings.ToLower(node.PluginURI+node.ID), "gate") || strings.Contains(strings.ToLower(node.PluginURI+node.ID), "approv") {
		nodeType = "gate"
	} else if strings.Contains(strings.ToLower(node.PluginURI+node.ID), "spec") {
		nodeType = "spec_writer"
	}

	gitCommit := ""
	if gc, ok := node.Inputs["git_commit"].(string); ok {
		gitCommit = gc
	}
	agentModel := "claude-3-5-sonnet"
	if am, ok := node.Inputs["agent_model"].(string); ok && am != "" {
		agentModel = am
	} else if m, ok := node.Inputs["model"].(string); ok && m != "" {
		agentModel = m
	}

	factoryID := graphID
	if parts := strings.Split(graphID, "-"); len(parts) > 1 {
		factoryID = parts[0]
	}

	stepCtx, span, done := telemetry.StartStepSpan(ctx, d.tracer, d.metrics, telemetry.StepSpanOpts{
		FactoryID:  factoryID,
		RunID:      runID,
		NodeID:     node.ID,
		NodeType:   nodeType,
		GitCommit:  gitCommit,
		AgentModel: agentModel,
	})

	traceID := ""
	spanID := ""
	if span != nil && span.SpanContext().IsValid() {
		traceID = span.SpanContext().TraceID().String()
		spanID = span.SpanContext().SpanID().String()
	}

	_ = d.engine.SetNodeStatus(graphID, node.ID, StatusRunning)

	if d.publisher != nil {
		_ = d.publisher.PublishTaskEvent(stepCtx, gateway.Event{
			Type:   gateway.TopicTaskStarted,
			RunID:  runID,
			TaskID: node.ID,
		})
	}

	policy := d.cfg.DefaultStepPolicy
	if custom, ok := d.cfg.StepPolicies[node.ID]; ok {
		policy = custom
	}

	configs := make(map[string]string, len(node.Inputs))
	for k, v := range node.Inputs {
		configs[k] = fmt.Sprint(v)
	}

	pdkCtx := pdk.Context{
		RunID:   runID,
		TaskID:  node.ID,
		Configs: configs,
	}

	var lastErr error
	var lastRes *pdk.Result
	backoff := policy.InitialBackoff

	functionName := "execute"
	if fn, ok := node.Inputs["entrypoint"].(string); ok && fn != "" {
		functionName = fn
	} else if fn, ok := node.Inputs["function"].(string); ok && fn != "" {
		functionName = fn
	}

	var completedRes *pdk.Result
	var completedErr error

	for attempt := 0; attempt < policy.MaxRetries; attempt++ {
		attemptCtx, cancel := context.WithTimeout(stepCtx, policy.Timeout)
		res, err := d.caller.CallPDK(attemptCtx, node.PluginURI, functionName, pdkCtx)
		cancel()

		if err == nil && res != nil && res.Success {
			completedRes = res
			break
		}

		lastErr = err
		lastRes = res

		if attempt < policy.MaxRetries-1 {
			select {
			case <-stepCtx.Done():
				completedErr = stepCtx.Err()
				break
			case <-time.After(backoff):
				backoff *= 2
			}
		}
	}

	if completedRes == nil && completedErr == nil {
		if lastErr != nil {
			completedErr = lastErr
			completedRes = lastRes
		} else if lastRes != nil && !lastRes.Success {
			completedErr = fmt.Errorf("step execution failed: %s", lastRes.Output)
			completedRes = lastRes
		} else {
			completedRes = lastRes
		}
	}

	durationMS := time.Since(startTime).Milliseconds()
	status := "COMPLETED"
	if completedErr != nil || (completedRes != nil && !completedRes.Success) {
		status = "FAILED"
		done("FAILED", completedErr)
	} else {
		done("COMPLETED", nil)
	}

	// Extract tokens if present in result data
	var tokens int64
	if completedRes != nil && completedRes.Data != nil {
		if t, ok := completedRes.Data["tokens"].(int); ok {
			tokens = int64(t)
		} else if t, ok := completedRes.Data["tokens"].(float64); ok {
			tokens = int64(t)
		} else if t, ok := completedRes.Data["tokens"].(int64); ok {
			tokens = t
		} else if t, ok := completedRes.Data["token_count"].(int); ok {
			tokens = int64(t)
		} else if t, ok := completedRes.Data["token_count"].(float64); ok {
			tokens = int64(t)
		} else if t, ok := completedRes.Data["token_count"].(int64); ok {
			tokens = t
		}
	}

	// Emit stdout logs
	if d.publisher != nil && completedRes != nil && completedRes.Output != "" {
		normalizedOutput := strings.ReplaceAll(completedRes.Output, "\r\n", "\n")
		lines := strings.Split(normalizedOutput, "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			_ = d.publisher.PublishTaskEvent(stepCtx, gateway.Event{
				Type:   gateway.TopicTaskLog,
				RunID:  runID,
				TaskID: node.ID,
				Payload: map[string]any{
					"stream":    "stdout",
					"message":   line,
					"timestamp": time.Now().UnixMilli(),
				},
			})
		}
	}

	// Emit stderr logs if there's an error
	if d.publisher != nil && completedErr != nil {
		_ = d.publisher.PublishTaskEvent(stepCtx, gateway.Event{
			Type:   gateway.TopicTaskLog,
			RunID:  runID,
			TaskID: node.ID,
			Payload: map[string]any{
				"stream":    "stderr",
				"message":   completedErr.Error(),
				"timestamp": time.Now().UnixMilli(),
			},
		})
	}

	// Emit TopicTaskSpan
	if d.publisher != nil {
		_ = d.publisher.PublishTaskEvent(stepCtx, gateway.Event{
			Type:   gateway.TopicTaskSpan,
			RunID:  runID,
			TaskID: node.ID,
			Payload: map[string]any{
				"duration_ms": durationMS,
				"trace_id":    traceID,
				"span_id":     spanID,
				"node_type":   nodeType,
				"status":      status,
				"tokens":      tokens,
			},
		})
	}

	return completedRes, durationMS, completedErr
}
