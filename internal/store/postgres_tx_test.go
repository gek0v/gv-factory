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

type mockTx struct {
	mockQuerier
	commitErr   error
	rollbackErr error
}

func (m *mockTx) Begin(ctx context.Context) (pgx.Tx, error) {
	return m, nil
}

func (m *mockTx) Commit(ctx context.Context) error {
	return m.commitErr
}

func (m *mockTx) Rollback(ctx context.Context) error {
	return m.rollbackErr
}

func (m *mockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}

func (m *mockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	return nil
}

func (m *mockTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (m *mockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (m *mockTx) Conn() *pgx.Conn {
	return nil
}

func TestPgxTxStore_Coverage(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	txMock := &mockTx{}
	txStore := &pgxTxStore{tx: txMock}

	if err := txStore.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// WithTx nested
	nestedRan := false
	err := txStore.WithTx(ctx, func(nested Store) error {
		nestedRan = true
		return nil
	})
	if err != nil || !nestedRan {
		t.Fatalf("expected nested tx to run without error")
	}

	// CreateRun delegation
	run := &Run{ID: "tx-run", TemplateName: "tx-tpl"}
	if err := txStore.CreateRun(ctx, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// RecordStep delegation
	step := &Step{ID: "tx-s", RunID: "tx-run", StepName: "init"}
	if err := txStore.RecordStep(ctx, step); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// RecordPendingGate delegation
	gate := &PendingGate{RunID: "tx-run", Token: "tx-tok"}
	if err := txStore.RecordPendingGate(ctx, gate); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// RecordGateAudit delegation
	txMock.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{
			scanFn: func(dest ...any) error {
				*dest[0].(*int64) = 99
				return nil
			},
		}
	}
	audit := &GateAudit{RunID: "tx-run", TokenHash: "hash-99"}
	if err := txStore.RecordGateAudit(ctx, audit); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ResolveGate: invalid input
	if _, err := txStore.ResolveGate(ctx, "", GateResolution{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}

	// ResolveGate: not found
	txMock.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{
			scanFn: func(dest ...any) error {
				return pgx.ErrNoRows
			},
		}
	}
	if _, err := txStore.ResolveGate(ctx, "nonexistent-token", GateResolution{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// ResolveGate: expired
	txMock.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{
			scanFn: func(dest ...any) error {
				*dest[0].(*string) = "gate-1"
				*dest[1].(*string) = "run-1"
				*dest[2].(*string) = "hash-1"
				*dest[3].(*string) = "APPROVAL"
				*dest[4].(*string) = string(GateStatusPending)
				*dest[5].(*time.Time) = now.Add(-time.Hour)
				return nil
			},
		}
	}
	if _, err := txStore.ResolveGate(ctx, "expired-token", GateResolution{}); !errors.Is(err, ErrGateExpired) {
		t.Fatalf("expected ErrGateExpired, got %v", err)
	}

	// ResolveGate: already resolved
	txMock.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &mockRow{
			scanFn: func(dest ...any) error {
				*dest[0].(*string) = "gate-1"
				*dest[1].(*string) = "run-1"
				*dest[2].(*string) = "hash-1"
				*dest[3].(*string) = "APPROVAL"
				*dest[4].(*string) = string(GateStatusResolved)
				*dest[5].(*time.Time) = now.Add(time.Hour)
				return nil
			},
		}
	}
	if _, err := txStore.ResolveGate(ctx, "resolved-token", GateResolution{}); !errors.Is(err, ErrGateAlreadyResolved) {
		t.Fatalf("expected ErrGateAlreadyResolved, got %v", err)
	}

	// ResolveGate: success
	callCount := 0
	txMock.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		callCount++
		if callCount == 1 {
			// select query
			return &mockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*string) = "gate-ok"
					*dest[1].(*string) = "run-ok"
					*dest[2].(*string) = "hash-ok"
					*dest[3].(*string) = "APPROVAL"
					*dest[4].(*string) = string(GateStatusPending)
					*dest[5].(*time.Time) = now.Add(time.Hour)
					return nil
				},
			}
		}
		// insert audit returning id
		return &mockRow{
			scanFn: func(dest ...any) error {
				*dest[0].(*int64) = 100
				return nil
			},
		}
	}

	resAudit, err := txStore.ResolveGate(ctx, "valid-token", GateResolution{
		Decision: "approved",
		Actor:    "admin",
	})
	if err != nil {
		t.Fatalf("unexpected error resolving gate: %v", err)
	}
	if resAudit.ID != 100 || resAudit.Action != "approved" {
		t.Fatalf("unexpected audit entry: %+v", resAudit)
	}
}

func TestMigrate_ExecutionMock(t *testing.T) {
	ctx := context.Background()

	txMock := &mockTx{
		mockQuerier: mockQuerier{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{
					data: [][]any{
						{"000_fake_already_applied.sql"},
					},
				}, nil
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			},
		},
	}

	if err := Migrate(ctx, txMock); err != nil {
		t.Fatalf("expected successful migration run: %v", err)
	}
}
