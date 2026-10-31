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
}

// Worker orchestrates Hatchet workflow steps and dispatches tasks to WASM plugins and Sandboxes.
type Worker struct {
	Sandbox    *sandbox.Manager
	Gateway    *gateway.Gateway
	DAG        *dag.Engine
	PluginHost *plugins.Host
	Dispatcher *dag.Dispatcher
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

	dispatcher := dag.NewDispatcher(dagEngine, pluginHost, gw, dag.DispatcherConfig{
		DefaultStepPolicy: dag.StepPolicy{
			MaxRetries:     retries,
			InitialBackoff: 25 * time.Millisecond,
			Timeout:        timeout,
		},
	})

	return &Worker{
		Sandbox:    sb,
		Gateway:    gw,
		DAG:        dagEngine,
		PluginHost: pluginHost,
		Dispatcher: dispatcher,
	}
}

// ExecutePipeline executes a factory template workflow through the dispatcher.
func (w *Worker) ExecutePipeline(ctx context.Context, runID string, tpl schema.FactoryTemplate) (*dag.WorkflowReport, error) {
	if w.Dispatcher == nil {
		return nil, fmt.Errorf("dispatcher not initialized")
	}
	return w.Dispatcher.ExecuteWorkflow(ctx, runID, tpl)
}

// Close gracefully stops the worker and cleans up resources.
func (w *Worker) Close() error {
	if w.Gateway != nil {
		_ = w.Gateway.Close()
	}
	if w.PluginHost != nil {
		_ = w.PluginHost.Close()
	}
	return nil
}
