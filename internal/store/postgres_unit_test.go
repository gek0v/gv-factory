// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// mockQuerier implements dbQuerier for unit testing without an external database.
type mockQuerier struct {
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
}

func (m *mockQuerier) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.execFn != nil {
		return m.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (m *mockQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, sql, args...)
	}
	return nil, nil
}

func (m *mockQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, sql, args...)
	}
	return &mockRow{}
}

type mockRow struct {
	scanFn func(dest ...any) error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.scanFn != nil {
		return r.scanFn(dest...)
	}
	return nil
}

func TestPostgresFunctions_Unit(t *testing.T) {
	ctx := context.Background()

	t.Run("createRun validation and execution", func(t *testing.T) {
		if err := createRun(ctx, &mockQuerier{}, nil); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}
		if err := createRun(ctx, &mockQuerier{}, &Run{ID: ""}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}

		run := &Run{
			ID:           "test-run",
			TemplateName: "tpl",
			Metadata:     map[string]any{"k": "v"},
		}
		var recordedSQL string
		mock := &mockQuerier{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				recordedSQL = sql
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			},
		}

		if err := createRun(ctx, mock, run); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if recordedSQL == "" {
			t.Fatal("expected SQL to be executed")
		}
	})

	t.Run("getRun scan and validation", func(t *testing.T) {
		if _, err := getRun(ctx, &mockQuerier{}, ""); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}

		now := time.Now().UTC()
		metaJSON, _ := json.Marshal(map[string]any{"region": "eu"})

		mock := &mockQuerier{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					scanFn: func(dest ...any) error {
						*dest[0].(*string) = "run-123"
						*dest[1].(*string) = "test-tpl"
						*dest[2].(*string) = string(RunStatusRunning)
						*dest[3].(*string) = "initiator"
						*dest[4].(**time.Time) = &now
						*dest[5].(**time.Time) = nil
						*dest[6].(*string) = ""
						*dest[7].(*[]byte) = metaJSON
						*dest[8].(*time.Time) = now
						*dest[9].(*time.Time) = now
						return nil
					},
				}
			},
		}

		r, err := getRun(ctx, mock, "run-123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.ID != "run-123" || r.Status != RunStatusRunning || r.Metadata["region"] != "eu" {
			t.Fatalf("unexpected scanned run: %+v", r)
		}
	})

	t.Run("updateRunStatus execution and error mapping", func(t *testing.T) {
		if err := updateRunStatus(ctx, &mockQuerier{}, "", RunStatusRunning, nil); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}

		// When 0 rows affected, return ErrNotFound
		mockZero := &mockQuerier{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
		}
		if err := updateRunStatus(ctx, mockZero, "run-404", RunStatusCompleted, nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}

		// Success
		mockOk := &mockQuerier{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		if err := updateRunStatus(ctx, mockOk, "run-200", RunStatusFailed, errors.New("failed step")); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("recordStep serialization and execution", func(t *testing.T) {
		if err := recordStep(ctx, &mockQuerier{}, nil); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}
		if err := recordStep(ctx, &mockQuerier{}, &Step{ID: ""}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}
		if err := recordStep(ctx, &mockQuerier{}, &Step{ID: "s1", RunID: ""}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}

		step := &Step{
			ID:       "step-1",
			RunID:    "run-1",
			StepName: "test-step",
			Output:   map[string]any{"exit_code": 0},
		}
		mock := &mockQuerier{}
		if err := recordStep(ctx, mock, step); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		stepEmpty := &Step{
			ID:       "step-2",
			RunID:    "run-1",
			StepName: "empty",
		}
		if err := recordStep(ctx, mock, stepEmpty); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("recordPendingGate validation and execution", func(t *testing.T) {
		if err := recordPendingGate(ctx, &mockQuerier{}, nil); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}
		if err := recordPendingGate(ctx, &mockQuerier{}, &PendingGate{RunID: ""}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}

		gate := &PendingGate{
			RunID:     "run-gate",
			Token:     "raw-token",
			Arguments: map[string]any{"cmd": "rm"},
		}
		mock := &mockQuerier{}
		if err := recordPendingGate(ctx, mock, gate); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		gateEmpty := &PendingGate{
			RunID: "run-gate-2",
		}
		if err := recordPendingGate(ctx, mock, gateEmpty); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("recordGateAudit validation and execution", func(t *testing.T) {
		if err := recordGateAudit(ctx, &mockQuerier{}, nil); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}
		if err := recordGateAudit(ctx, &mockQuerier{}, &GateAudit{RunID: ""}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}

		mock := &mockQuerier{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					scanFn: func(dest ...any) error {
						*dest[0].(*int64) = 42
						return nil
					},
				}
			},
		}
		audit := &GateAudit{
			RunID:     "run-audit",
			TokenHash: "hash-123",
			Action:    "approved",
			Metadata:  map[string]any{"actor": "bob"},
		}
		if err := recordGateAudit(ctx, mock, audit); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if audit.ID != 42 {
			t.Fatalf("expected ID 42, got %d", audit.ID)
		}

		auditEmpty := &GateAudit{
			RunID: "run-audit-empty",
		}
		if err := recordGateAudit(ctx, mock, auditEmpty); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("listStepsByRun and validation", func(t *testing.T) {
		if _, err := listStepsByRun(ctx, &mockQuerier{}, ""); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}
	})
}
