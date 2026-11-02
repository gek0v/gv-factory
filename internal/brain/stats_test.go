// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestGetStats_Success(t *testing.T) {
	ctx := context.Background()
	mock := &mockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				values: []any{12, 5, 8},
			}
		},
	}

	svc := NewServiceWithDB(DefaultConfig(), mock)
	stats, err := svc.GetStats(ctx)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	if stats.IndexedKnowledge != 12 {
		t.Errorf("expected 12 indexed knowledge, got %d", stats.IndexedKnowledge)
	}
	if stats.ActiveRules != 5 {
		t.Errorf("expected 5 active rules, got %d", stats.ActiveRules)
	}
	if stats.TaskEpisodes != 8 {
		t.Errorf("expected 8 task episodes, got %d", stats.TaskEpisodes)
	}
}

func TestGetStats_NilDB(t *testing.T) {
	ctx := context.Background()
	svc := &Service{cfg: DefaultConfig()}
	_, err := svc.GetStats(ctx)
	if err == nil {
		t.Fatal("expected error with nil db, got nil")
	}
}

func TestGetStats_QueryError(t *testing.T) {
	ctx := context.Background()
	mock := &mockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				err: errors.New("db scan error"),
			}
		},
	}

	svc := NewServiceWithDB(DefaultConfig(), mock)
	_, err := svc.GetStats(ctx)
	if err == nil {
		t.Fatal("expected error on query failure, got nil")
	}
}
