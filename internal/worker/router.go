// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"fmt"
	"sync"
)

// TaskHandler processes a dispatched TaskMessage.
type TaskHandler interface {
	HandleTask(ctx context.Context, task *TaskMessage) (*TaskResult, error)
}

// TaskHandlerFunc allows ordinary functions to act as TaskHandler.
type TaskHandlerFunc func(ctx context.Context, task *TaskMessage) (*TaskResult, error)

// HandleTask executes the underlying function.
func (f TaskHandlerFunc) HandleTask(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
	return f(ctx, task)
}

// TaskRouter routes TaskMessages to registered TaskHandlers based on TaskType.
type TaskRouter struct {
	mu       sync.RWMutex
	handlers map[string]TaskHandler
}

// NewTaskRouter creates an initialized TaskRouter.
func NewTaskRouter() *TaskRouter {
	return &TaskRouter{
		handlers: make(map[string]TaskHandler),
	}
}

// RegisterHandler registers a TaskHandler for a specific task type.
func (r *TaskRouter) RegisterHandler(taskType string, handler TaskHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[taskType] = handler
}

// Dispatch executes the appropriate handler for the given task.
func (r *TaskRouter) Dispatch(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
	r.mu.RLock()
	handler, ok := r.handlers[task.TaskType]
	r.mu.RUnlock()

	if !ok || handler == nil {
		return nil, fmt.Errorf("no handler registered for task type: %s", task.TaskType)
	}

	return handler.HandleTask(ctx, task)
}
