// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTaskRouter_Dispatch(t *testing.T) {
	router := NewTaskRouter()

	wasmCalled := false
	router.RegisterHandler(TaskTypeWasmPlugin, TaskHandlerFunc(func(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
		wasmCalled = true
		return &TaskResult{
			Success: true,
			Output:  "wasm executed",
			Data:    map[string]any{"task_id": task.TaskID},
		}, nil
	}))

	task := &TaskMessage{
		TaskID:   "task-wasm-1",
		RunID:    "run-1",
		StepID:   "step-1",
		TaskType: TaskTypeWasmPlugin,
	}

	res, err := router.Dispatch(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected dispatch error: %v", err)
	}
	if !wasmCalled {
		t.Fatalf("expected wasm handler to be called")
	}
	if !res.Success || res.Output != "wasm executed" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestTaskRouter_UnregisteredHandler(t *testing.T) {
	router := NewTaskRouter()

	task := &TaskMessage{
		TaskID:   "task-unknown",
		RunID:    "run-1",
		StepID:   "step-1",
		TaskType: TaskTypeGitSandbox,
	}

	_, err := router.Dispatch(context.Background(), task)
	if err == nil {
		t.Fatalf("expected error for unregistered task type, got nil")
	}
}

func TestTaskRouter_ContextTimeout(t *testing.T) {
	router := NewTaskRouter()

	router.RegisterHandler(TaskTypeAgentStep, TaskHandlerFunc(func(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
		select {
		case <-time.After(500 * time.Millisecond):
			return &TaskResult{Success: true}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	task := &TaskMessage{
		TaskID:   "task-agent-timeout",
		RunID:    "run-1",
		StepID:   "step-1",
		TaskType: TaskTypeAgentStep,
	}

	_, err := router.Dispatch(ctx, task)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}
