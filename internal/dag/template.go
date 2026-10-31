// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
	"fmt"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	pdk "github.com/gv-factory/gv-factory/plugins-sdk/go"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

// PluginCaller abstracts execution of WASM plugins conforming to the PDK context contract.
type PluginCaller interface {
	CallPDK(ctx context.Context, pluginRef string, functionName string, req pdk.Context) (*pdk.Result, error)
}

// EventPublisher abstracts broadcasting of task lifecycle events.
type EventPublisher interface {
	PublishTaskEvent(ctx context.Context, ev gateway.Event) error
}

// StepPolicy defines retry behavior and timeout for task step executions.
type StepPolicy struct {
	MaxRetries     int
	InitialBackoff time.Duration
	Timeout        time.Duration
}

// DispatcherConfig configures workflow dispatching policies.
type DispatcherConfig struct {
	DefaultStepPolicy StepPolicy
	StepPolicies      map[string]StepPolicy
}

// WorkflowReport summarizes the outcome of a dispatched template workflow execution.
type WorkflowReport struct {
	RunID          string                 `json:"run_id"`
	GraphID        string                 `json:"graph_id"`
	Status         NodeStatus             `json:"status"`
	CompletedSteps []string               `json:"completed_steps"`
	FailedSteps    []string               `json:"failed_steps"`
	StepResults    map[string]*pdk.Result `json:"step_results"`
}

// MapTemplateToGraph converts a declarative FactoryTemplate into an executable DAG graph.
func MapTemplateToGraph(graphID string, tpl schema.FactoryTemplate) (*Graph, error) {
	if graphID == "" {
		graphID = tpl.Metadata.Name
	}
	if graphID == "" {
		return nil, fmt.Errorf("graph ID and template name cannot both be empty")
	}

	nodes := make(map[string]*Node, len(tpl.Spec.Steps))
	for _, step := range tpl.Spec.Steps {
		if step.ID == "" {
			return nil, fmt.Errorf("step ID cannot be empty in template %q", tpl.Metadata.Name)
		}
		deps := make([]string, len(step.DependsOn))
		copy(deps, step.DependsOn)

		nodes[step.ID] = &Node{
			ID:        step.ID,
			PluginURI: step.Plugin,
			Inputs:    step.Config,
			DependsOn: deps,
			Status:    StatusPending,
		}
	}

	g := &Graph{
		ID:    graphID,
		Nodes: nodes,
	}

	if err := g.Validate(); err != nil {
		return nil, fmt.Errorf("invalid template graph: %w", err)
	}

	return g, nil
}
