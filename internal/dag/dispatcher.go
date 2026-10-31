// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	pdk "github.com/gv-factory/gv-factory/plugins-sdk/go"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

// Dispatcher coordinates Hatchet/WASM task execution across DAG topologies.
type Dispatcher struct {
	engine    *Engine
	caller    PluginCaller
	publisher EventPublisher
	cfg       DispatcherConfig
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

	return &Dispatcher{
		engine:    engine,
		caller:    caller,
		publisher: pub,
		cfg:       cfg,
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
				res, execErr := d.executeNodeWithPolicy(ctx, runID, graphID, n)
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
								"error": errMsg,
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
								"output": output,
								"data":   data,
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

func (d *Dispatcher) executeNodeWithPolicy(ctx context.Context, runID, graphID string, node *Node) (*pdk.Result, error) {
	_ = d.engine.SetNodeStatus(graphID, node.ID, StatusRunning)

	if d.publisher != nil {
		_ = d.publisher.PublishTaskEvent(ctx, gateway.Event{
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

	for attempt := 0; attempt < policy.MaxRetries; attempt++ {
		stepCtx, cancel := context.WithTimeout(ctx, policy.Timeout)
		res, err := d.caller.CallPDK(stepCtx, node.PluginURI, functionName, pdkCtx)
		cancel()

		if err == nil && res != nil && res.Success {
			return res, nil
		}

		lastErr = err
		lastRes = res

		if attempt < policy.MaxRetries-1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
		}
	}

	if lastErr != nil {
		return lastRes, lastErr
	}
	return lastRes, nil
}
