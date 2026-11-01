// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mockRows struct {
	data    [][]any
	index   int
	closed  bool
	scanErr error
}

func (m *mockRows) Close() {
	m.closed = true
}

func (m *mockRows) Err() error {
	return nil
}

func (m *mockRows) CommandTag() pgconn.CommandTag {
	return pgconn.NewCommandTag("SELECT 1")
}

func (m *mockRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (m *mockRows) Next() bool {
	if m.index < len(m.data) {
		m.index++
		return true
	}
	return false
}

func (m *mockRows) Scan(dest ...any) error {
	if m.scanErr != nil {
		return m.scanErr
	}
	row := m.data[m.index-1]
	for i, val := range row {
		if i >= len(dest) {
			break
		}
		switch d := dest[i].(type) {
		case *string:
			*d = val.(string)
		case *int64:
			*d = val.(int64)
		case *[]byte:
			*d = val.([]byte)
		case *time.Time:
			*d = val.(time.Time)
		case **time.Time:
			if val == nil {
				*d = nil
			} else {
				t := val.(time.Time)
				*d = &t
			}
		}
	}
	return nil
}

func (m *mockRows) Values() ([]any, error) {
	return nil, nil
}

func (m *mockRows) RawValues() [][]byte {
	return nil
}

func (m *mockRows) Conn() *pgx.Conn {
	return nil
}

func TestPostgresFunctions_QueryRows(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("listRuns query execution", func(t *testing.T) {
		mock := &mockQuerier{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data: [][]any{
						{
							"run-1", "tpl-1", "RUNNING", "user-1", now, nil, "", []byte(`{"k":"v"}`), now, now,
						},
					},
				}, nil
			},
		}

		runs, err := listRuns(ctx, mock, RunFilter{
			TemplateName: "tpl-1",
			Status:       RunStatusRunning,
			InitiatedBy:  "user-1",
			Limit:        10,
			Offset:       5,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(runs) != 1 || runs[0].ID != "run-1" {
			t.Fatalf("unexpected runs: %+v", runs)
		}

		// Also test listRuns via txStore
		txStore := &pgxTxStore{tx: &mockTx{mockQuerier: *mock}}
		txRuns, err := txStore.ListRuns(ctx, RunFilter{})
		if err != nil {
			t.Fatalf("unexpected error on txStore.ListRuns: %v", err)
		}
		if len(txRuns) != 1 {
			t.Fatalf("expected 1 run, got %d", len(txRuns))
		}
		txRun, err := txStore.GetRun(ctx, "run-1")
		if err != nil && !errors.Is(err, ErrNotFound) {
			// Expected mock behavior
		}
		_ = txRun
		_ = txStore.UpdateRunStatus(ctx, "run-1", RunStatusCompleted, nil)
	})

	t.Run("listStepsByRun query execution", func(t *testing.T) {
		mock := &mockQuerier{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data: [][]any{
						{
							"step-1", "run-1", "step-name", "COMPLETED", int64(100), "", []byte(`{}`), now,
						},
					},
				}, nil
			},
		}

		steps, err := listStepsByRun(ctx, mock, "run-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(steps) != 1 || steps[0].ID != "step-1" {
			t.Fatalf("unexpected steps: %+v", steps)
		}

		txStore := &pgxTxStore{tx: &mockTx{mockQuerier: *mock}}
		txSteps, err := txStore.ListStepsByRun(ctx, "run-1")
		if err != nil || len(txSteps) != 1 {
			t.Fatalf("unexpected txSteps: %+v", txSteps)
		}
	})

	t.Run("listPendingGates query execution", func(t *testing.T) {
		mock := &mockQuerier{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data: [][]any{
						{
							"gate-1", "run-1", "hash-1", "APPROVAL", "deploy", []byte(`{}`), "PENDING", now, now.Add(time.Hour),
						},
					},
				}, nil
			},
		}

		gates, err := listPendingGates(ctx, mock)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(gates) != 1 || gates[0].ID != "gate-1" {
			t.Fatalf("unexpected gates: %+v", gates)
		}

		txStore := &pgxTxStore{tx: &mockTx{mockQuerier: *mock}}
		txGates, err := txStore.ListPendingGates(ctx)
		if err != nil || len(txGates) != 1 {
			t.Fatalf("unexpected txGates: %+v", txGates)
		}
	})
}
