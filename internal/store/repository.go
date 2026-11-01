// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package store

import (
	"context"
)

// RunRepository handles persistence operations for factory runs.
type RunRepository interface {
	CreateRun(ctx context.Context, run *Run) error
	GetRun(ctx context.Context, id string) (*Run, error)
	ListRuns(ctx context.Context, filter RunFilter) ([]*Run, error)
	UpdateRunStatus(ctx context.Context, id string, status RunStatus, runErr error) error
}

// StepRepository handles persistence operations for pipeline steps.
type StepRepository interface {
	RecordStep(ctx context.Context, step *Step) error
	ListStepsByRun(ctx context.Context, runID string) ([]*Step, error)
}

// GateAuditRepository manages active decision gates and forensic audit records.
type GateAuditRepository interface {
	RecordPendingGate(ctx context.Context, gate *PendingGate) error
	RecordGateAudit(ctx context.Context, entry *GateAudit) error
	ListPendingGates(ctx context.Context) ([]*PendingGate, error)
	ResolveGate(ctx context.Context, token string, resolution GateResolution) (*GateAudit, error)
}

// Store aggregates repositories and exposes transactional boundaries.
type Store interface {
	RunRepository
	StepRepository
	GateAuditRepository

	// WithTx runs fn within an atomic transactional boundary.
	// In PostgreSQL this corresponds to a database transaction; in memory, to atomic rollback semantics.
	WithTx(ctx context.Context, fn func(tx Store) error) error

	// Close terminates connections or cleans up memory resources.
	Close() error
}
