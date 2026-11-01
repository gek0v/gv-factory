// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TaskMessage represents a work unit distributed via the task queue.
type TaskMessage struct {
	TaskID     string          `json:"task_id"`
	RunID      string          `json:"run_id"`
	StepID     string          `json:"step_id"`
	TaskType   string          `json:"task_type"` // "wasm_plugin", "agent_step", "git_sandbox"
	Payload    json.RawMessage `json:"payload"`
	TimeoutMS  int64           `json:"timeout_ms,omitempty"`
	MaxRetries int             `json:"max_retries,omitempty"`
}

// Validate checks contract invariants on the TaskMessage.
func (t *TaskMessage) Validate() error {
	if strings.TrimSpace(t.TaskID) == "" {
		return errors.New("task_id is required")
	}
	if strings.TrimSpace(t.RunID) == "" {
		return errors.New("run_id is required")
	}
	if strings.TrimSpace(t.StepID) == "" {
		return errors.New("step_id is required")
	}
	switch t.TaskType {
	case TaskTypeWasmPlugin, TaskTypeAgentStep, TaskTypeGitSandbox:
		return nil
	default:
		return fmt.Errorf("unsupported task_type: %q", t.TaskType)
	}
}

// HeartbeatMessage carries periodic worker health and telemetry.
type HeartbeatMessage struct {
	WorkerID       string `json:"worker_id"`
	ActiveTasks    int    `json:"active_tasks"`
	MaxConcurrency int    `json:"max_concurrency"`
	UptimeSeconds  int64  `json:"uptime_seconds"`
	Timestamp      int64  `json:"timestamp"`
}

// TaskResult holds the execution result of an individual task.
type TaskResult struct {
	Success   bool           `json:"success"`
	Output    string         `json:"output,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Retryable bool           `json:"retryable,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// RetryableError wraps an error indicating that the task failure is transient and can be retried.
type RetryableError struct {
	Err   error
	Delay time.Duration
}

// Error returns the underlying error message.
func (e *RetryableError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return "retryable error"
}

// Unwrap returns the underlying error.
func (e *RetryableError) Unwrap() error {
	return e.Err
}

// NewRetryableError creates a new RetryableError with the given backoff delay.
func NewRetryableError(err error, delay time.Duration) *RetryableError {
	return &RetryableError{
		Err:   err,
		Delay: delay,
	}
}

// IsRetryable determines whether an error is transient / retryable.
func IsRetryable(err error) bool {
	var re *RetryableError
	return errors.As(err, &re)
}
