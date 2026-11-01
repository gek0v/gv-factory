// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package store

import (
	"time"
)

// RunStatus defines the lifecycle status of a factory run.
type RunStatus string

const (
	RunStatusQueued    RunStatus = "QUEUED"
	RunStatusRunning   RunStatus = "RUNNING"
	RunStatusCompleted RunStatus = "COMPLETED"
	RunStatusFailed    RunStatus = "FAILED"
	RunStatusCancelled RunStatus = "CANCELLED"
)

// Run represents a persisted factory run entity.
type Run struct {
	ID           string         `json:"id"`
	TemplateName string         `json:"template_name"`
	Status       RunStatus      `json:"status"`
	InitiatedBy  string         `json:"initiated_by,omitempty"`
	StartedAt    *time.Time     `json:"started_at,omitempty"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// StepStatus defines the execution state of an individual step.
type StepStatus string

const (
	StepStatusPending   StepStatus = "PENDING"
	StepStatusRunning   StepStatus = "RUNNING"
	StepStatusCompleted StepStatus = "COMPLETED"
	StepStatusFailed    StepStatus = "FAILED"
	StepStatusSkipped   StepStatus = "SKIPPED"
)

// Step represents a persisted pipeline step execution.
type Step struct {
	ID           string         `json:"id"`
	RunID        string         `json:"run_id"`
	StepName     string         `json:"step_name"`
	Status       StepStatus     `json:"status"`
	DurationMS   int64          `json:"duration_ms"`
	ErrorMessage string         `json:"error_message,omitempty"`
	Output       map[string]any `json:"output,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

// GateStatus defines the pending or resolved status of a human approval gate.
type GateStatus string

const (
	GateStatusPending  GateStatus = "PENDING"
	GateStatusResolved GateStatus = "RESOLVED"
	GateStatusExpired  GateStatus = "EXPIRED"
)

// PendingGate represents an active decision gate awaiting human approval.
type PendingGate struct {
	ID        string         `json:"id"`
	RunID     string         `json:"run_id"`
	Token     string         `json:"token,omitempty"`
	TokenHash string         `json:"token_hash"`
	GateType  string         `json:"gate_type"`
	ToolName  string         `json:"tool_name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Status    GateStatus     `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	ExpiresAt time.Time      `json:"expires_at"`
}

// GateAudit captures an immutable audit record of a gate resolution.
type GateAudit struct {
	ID        int64          `json:"id"`
	RunID     string         `json:"run_id"`
	TokenHash string         `json:"token_hash"`
	GateType  string         `json:"gate_type"`
	Action    string         `json:"action"` // e.g. "approved", "rejected", "expired"
	Actor     string         `json:"actor"`  // User ID, IP address, or agent identifier
	Reason    string         `json:"reason,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// RunFilter specifies criteria for querying runs.
type RunFilter struct {
	TemplateName string    `json:"template_name,omitempty"`
	Status       RunStatus `json:"status,omitempty"`
	InitiatedBy  string    `json:"initiated_by,omitempty"`
	Limit        int       `json:"limit,omitempty"`
	Offset       int       `json:"offset,omitempty"`
}

// GateResolution contains user or agent decision data when resolving a gate.
type GateResolution struct {
	Decision string         `json:"decision"` // "approved" or "rejected"
	Actor    string         `json:"actor"`
	Reason   string         `json:"reason,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}
