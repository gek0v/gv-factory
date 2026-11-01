// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/worker"
)

func TestWorker_DistributedConsumer_Lifecycle(t *testing.T) {
	w := NewWorker(WorkerConfig{
		DisableNATS:       true,
		WorkerID:          "test-worker-e2e",
		MaxConcurrent:     2,
		HeartbeatInterval: 20 * time.Millisecond,
		DrainTimeout:      1 * time.Second,
	})
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	// Capture heartbeats from Gateway
	hbCh := make(chan gateway.Event, 10)
	unsub, err := w.Gateway.SubscribeTaskEvents(ctx, worker.TopicWorkerHeartbeat, hbCh)
	if err != nil {
		t.Fatalf("failed to subscribe to heartbeats: %v", err)
	}
	defer unsub()

	// Wait for at least one heartbeat
	select {
	case hbEv := <-hbCh:
		if hbEv.Type != worker.TopicWorkerHeartbeat {
			t.Fatalf("expected event type %s, got %s", worker.TopicWorkerHeartbeat, hbEv.Type)
		}
		if hbEv.Payload["worker_id"] != "test-worker-e2e" {
			t.Fatalf("expected worker_id test-worker-e2e, got %v", hbEv.Payload["worker_id"])
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timeout waiting for heartbeat event")
	}

	// Enqueue a distributed task message
	taskData, err := json.Marshal(worker.TaskMessage{
		TaskID:   "task-dist-1",
		RunID:    "run-dist-1",
		StepID:   "step-wasm",
		TaskType: worker.TaskTypeWasmPlugin,
	})
	if err != nil {
		t.Fatalf("failed to marshal task: %v", err)
	}

	taskEvCh := make(chan gateway.Event, 10)
	unsubTasks, err := w.Gateway.SubscribeTaskEvents(ctx, gateway.TopicAllTasks, taskEvCh)
	if err != nil {
		t.Fatalf("failed to subscribe to task events: %v", err)
	}
	defer unsubTasks()

	if err := w.QueueBackend.Publish(worker.TopicWorkTasks, taskData); err != nil {
		t.Fatalf("failed to publish task: %v", err)
	}

	var sawStarted, sawCompleted bool
	timeout := time.After(2 * time.Second)
	for !sawStarted || !sawCompleted {
		select {
		case ev := <-taskEvCh:
			if ev.Type == gateway.TopicTaskStarted && ev.TaskID == "task-dist-1" {
				sawStarted = true
			}
			if ev.Type == gateway.TopicTaskCompleted && ev.TaskID == "task-dist-1" {
				sawCompleted = true
			}
		case <-timeout:
			t.Fatalf("timeout waiting for task completion events (started=%v, completed=%v)", sawStarted, sawCompleted)
		}
	}
}

func TestWorker_DistributedConsumer_AgentStepAndSandbox(t *testing.T) {
	w := NewWorker(WorkerConfig{
		DisableNATS:   true,
		WorkerID:      "test-worker-steps",
		MaxConcurrent: 2,
	})
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}

	taskEvCh := make(chan gateway.Event, 20)
	unsubTasks, err := w.Gateway.SubscribeTaskEvents(ctx, gateway.TopicAllTasks, taskEvCh)
	if err != nil {
		t.Fatalf("failed to subscribe to task events: %v", err)
	}
	defer unsubTasks()

	// 1. Agent step task
	agentData, _ := json.Marshal(worker.TaskMessage{
		TaskID:   "task-agent-1",
		RunID:    "run-agent-1",
		StepID:   "step-agent",
		TaskType: worker.TaskTypeAgentStep,
	})
	_ = w.QueueBackend.Publish(worker.TopicWorkTasks, agentData)

	// 2. Git sandbox task
	sandboxData, _ := json.Marshal(worker.TaskMessage{
		TaskID:   "task-sandbox-1",
		RunID:    "run-sandbox-1",
		StepID:   "step-sandbox",
		TaskType: worker.TaskTypeGitSandbox,
	})
	_ = w.QueueBackend.Publish(worker.TopicWorkTasks, sandboxData)

	var agentCompleted, sandboxCompleted bool
	timeout := time.After(2 * time.Second)
	for !agentCompleted || !sandboxCompleted {
		select {
		case ev := <-taskEvCh:
			if ev.Type == gateway.TopicTaskCompleted {
				if ev.TaskID == "task-agent-1" {
					agentCompleted = true
				}
				if ev.TaskID == "task-sandbox-1" {
					sandboxCompleted = true
				}
			}
		case <-timeout:
			t.Fatalf("timeout waiting for agent/sandbox tasks (agent=%v, sandbox=%v)", agentCompleted, sandboxCompleted)
		}
	}
}
