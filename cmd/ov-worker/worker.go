// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/gv-factory/gv-factory/internal/dag"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/plugins"
	"github.com/gv-factory/gv-factory/internal/sandbox"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

// WorkerConfig holds configuration for the ov-worker agent.
type WorkerConfig struct {
	BaseWorktreeDir string
	NATSURL         string
	DisableNATS     bool
	DefaultRetries  int
	DefaultTimeout  time.Duration
	MaxConcurrent   int
}

// Worker orchestrates Hatchet workflow steps and dispatches tasks to WASM plugins and Sandboxes.
type Worker struct {
	Sandbox      *sandbox.Manager
	Gateway      *gateway.Gateway
	DAG          *dag.Engine
	PluginHost   *plugins.Host
	Dispatcher   *dag.Dispatcher
	Orchestrator *dag.MultiFactoryOrchestrator
}

// NewWorker initializes a worker instance with all execution sub-engines.
func NewWorker(cfg WorkerConfig) *Worker {
	sb := sandbox.NewManager(sandbox.Config{
		BaseWorktreeDir: cfg.BaseWorktreeDir,
	})

	gw := gateway.NewGatewayWithConfig(gateway.Config{
		NATSURL:     cfg.NATSURL,
		DisableNATS: cfg.DisableNATS,
	})

	dagEngine := dag.NewEngine()
	pluginHost := plugins.NewHost()

	retries := cfg.DefaultRetries
	if retries <= 0 {
		retries = 3
	}
	timeout := cfg.DefaultTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	dispatcherCfg := dag.DispatcherConfig{
		DefaultStepPolicy: dag.StepPolicy{
			MaxRetries:     retries,
			InitialBackoff: 25 * time.Millisecond,
			Timeout:        timeout,
		},
	}

	dispatcher := dag.NewDispatcher(dagEngine, pluginHost, gw, dispatcherCfg)

	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}

	orchestrator := dag.NewMultiFactoryOrchestrator(
		dag.OrchestratorConfig{
			MaxConcurrentRuns: maxConcurrent,
			DefaultTimeout:    timeout,
		},
		sb,
		dagEngine,
		pluginHost,
		gw,
		dispatcherCfg,
	)

	return &Worker{
		Sandbox:      sb,
		Gateway:      gw,
		DAG:          dagEngine,
		PluginHost:   pluginHost,
		Dispatcher:   dispatcher,
		Orchestrator: orchestrator,
	}
}

// ExecutePipeline executes a factory template workflow through the dispatcher.
func (w *Worker) ExecutePipeline(ctx context.Context, runID string, tpl schema.FactoryTemplate) (*dag.WorkflowReport, error) {
	if w.Dispatcher == nil {
		return nil, fmt.Errorf("dispatcher not initialized")
	}
	return w.Dispatcher.ExecuteWorkflow(ctx, runID, tpl)
}

// SubmitPipeline submits a run request to the concurrent multi-factory orchestrator.
func (w *Worker) SubmitPipeline(ctx context.Context, req dag.RunRequest) (*dag.FactoryRun, error) {
	if w.Orchestrator == nil {
		return nil, fmt.Errorf("orchestrator not initialized")
	}
	return w.Orchestrator.SubmitRun(ctx, req)
}

// Close gracefully stops the worker and cleans up resources.
func (w *Worker) Close() error {
	if w.Orchestrator != nil {
		w.Orchestrator.Stop()
	}
	if w.Gateway != nil {
		_ = w.Gateway.Close()
	}
	if w.PluginHost != nil {
		_ = w.PluginHost.Close()
	}
	return nil
}
