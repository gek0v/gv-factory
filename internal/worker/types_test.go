// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestTaskMessage_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     TaskMessage
		wantErr bool
	}{
		{
			name: "valid wasm_plugin task",
			msg: TaskMessage{
				TaskID:   "task-1",
				RunID:    "run-1",
				StepID:   "step-1",
				TaskType: TaskTypeWasmPlugin,
				Payload:  json.RawMessage(`{"plugin":"extism://sample.wasm"}`),
			},
			wantErr: false,
		},
		{
			name: "valid agent_step task",
			msg: TaskMessage{
				TaskID:   "task-2",
				RunID:    "run-1",
				StepID:   "step-2",
				TaskType: TaskTypeAgentStep,
			},
			wantErr: false,
		},
		{
			name: "valid git_sandbox task",
			msg: TaskMessage{
				TaskID:   "task-3",
				RunID:    "run-1",
				StepID:   "step-3",
				TaskType: TaskTypeGitSandbox,
			},
			wantErr: false,
		},
		{
			name: "missing task_id",
			msg: TaskMessage{
				TaskID:   "",
				RunID:    "run-1",
				StepID:   "step-1",
				TaskType: TaskTypeWasmPlugin,
			},
			wantErr: true,
		},
		{
			name: "missing run_id",
			msg: TaskMessage{
				TaskID:   "task-1",
				RunID:    "",
				StepID:   "step-1",
				TaskType: TaskTypeWasmPlugin,
			},
			wantErr: true,
		},
		{
			name: "missing step_id",
			msg: TaskMessage{
				TaskID:   "task-1",
				RunID:    "run-1",
				StepID:   "",
				TaskType: TaskTypeWasmPlugin,
			},
			wantErr: true,
		},
		{
			name: "unsupported task_type",
			msg: TaskMessage{
				TaskID:   "task-1",
				RunID:    "run-1",
				StepID:   "step-1",
				TaskType: "invalid_type",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestHeartbeatMessage_Serialization(t *testing.T) {
	hb := HeartbeatMessage{
		WorkerID:       "worker-node-1",
		ActiveTasks:    3,
		MaxConcurrency: 8,
		UptimeSeconds:  120,
		Timestamp:      1700000000000,
	}

	data, err := json.Marshal(hb)
	if err != nil {
		t.Fatalf("failed to marshal HeartbeatMessage: %v", err)
	}

	var parsed HeartbeatMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal HeartbeatMessage: %v", err)
	}

	if parsed != hb {
		t.Fatalf("expected %+v, got %+v", hb, parsed)
	}
}

func TestRetryableError(t *testing.T) {
	baseErr := errors.New("transient network timeout")
	delay := 500 * time.Millisecond
	retryErr := NewRetryableError(baseErr, delay)

	if retryErr.Error() != baseErr.Error() {
		t.Fatalf("expected error string %q, got %q", baseErr.Error(), retryErr.Error())
	}
	if !errors.Is(retryErr, baseErr) {
		t.Fatalf("expected errors.Is to match baseErr")
	}
	if !IsRetryable(retryErr) {
		t.Fatalf("expected IsRetryable to return true")
	}
	if IsRetryable(baseErr) {
		t.Fatalf("expected standard error to not be retryable by default")
	}
	if retryErr.Delay != delay {
		t.Fatalf("expected delay %v, got %v", delay, retryErr.Delay)
	}
}
