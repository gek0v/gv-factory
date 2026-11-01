// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresStore_ErrorMappingAndUnits(t *testing.T) {
	if mapPgError(nil) != nil {
		t.Fatal("expected nil error")
	}
	if !errors.Is(mapPgError(pgx.ErrNoRows), ErrNotFound) {
		t.Fatal("expected ErrNotFound")
	}

	pgConflict := &pgconn.PgError{Code: "23505"}
	if !errors.Is(mapPgError(pgConflict), ErrConflict) {
		t.Fatal("expected ErrConflict")
	}

	genericErr := errors.New("other")
	if !errors.Is(mapPgError(genericErr), genericErr) {
		t.Fatal("expected genericErr")
	}

	var p *PostgresStore
	if err := p.Ping(context.Background()); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed, got %v", err)
	}
	if p.Pool() != nil {
		t.Fatal("expected nil pool")
	}
	// WithTx delegation and error mapping
	ctx := context.Background()
	if err := p.WithTx(ctx, func(tx Store) error { return errors.New("tx error") }); err == nil {
		t.Fatal("expected tx error")
	}

	// Methods delegating on closed pool
	if err := p.CreateRun(ctx, &Run{ID: "r"}); err == nil {
		// on nil pool it shouldn't panic
	}
	_, _ = p.GetRun(ctx, "r")
	_, _ = p.ListRuns(ctx, RunFilter{})
	_ = p.UpdateRunStatus(ctx, "r", RunStatusRunning, nil)
	_ = p.RecordStep(ctx, &Step{ID: "s"})
	_, _ = p.ListStepsByRun(ctx, "r")
	_ = p.RecordPendingGate(ctx, &PendingGate{RunID: "r"})
	_ = p.RecordGateAudit(ctx, &GateAudit{RunID: "r"})
	_, _ = p.ListPendingGates(ctx)
	_, _ = p.ResolveGate(ctx, "tok", GateResolution{})
}

// TestPostgresStore_Integration runs full lifecycle integration tests if DATABASE_URL is set.
func TestPostgresStore_Integration(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("skipping postgres integration test: DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := DefaultConfig()
	cfg.DatabaseURL = dbURL

	store, err := NewPostgresStore(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to connect to PostgreSQL: %v", err)
	}
	defer store.Close()

	if err := store.Ping(ctx); err != nil {
		t.Fatalf("failed to ping PostgreSQL: %v", err)
	}

	runID := "pg-run-integration-" + time.Now().Format("20060102150405")
	run := &Run{
		ID:           runID,
		TemplateName: "integration-template",
		Status:       RunStatusQueued,
		InitiatedBy:  "tester",
		Metadata: map[string]any{
			"env": "ci",
		},
	}

	// 1. CreateRun
	if err := store.CreateRun(ctx, run); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// 2. GetRun
	got, err := store.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}
	if got.TemplateName != "integration-template" || got.Metadata["env"] != "ci" {
		t.Fatalf("unexpected retrieved run: %+v", got)
	}

	// 3. UpdateRunStatus
	if err := store.UpdateRunStatus(ctx, runID, RunStatusRunning, nil); err != nil {
		t.Fatalf("failed to update status: %v", err)
	}

	// 4. RecordStep
	step := &Step{
		ID:         "step-pg-1",
		RunID:      runID,
		StepName:   "compile",
		Status:     StepStatusCompleted,
		DurationMS: 120,
		Output: map[string]any{
			"artifacts": 3,
		},
	}
	if err := store.RecordStep(ctx, step); err != nil {
		t.Fatalf("failed to record step: %v", err)
	}

	steps, err := store.ListStepsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(steps) != 1 || steps[0].StepName != "compile" {
		t.Fatalf("unexpected steps: %+v", steps)
	}

	// 5. Gate lifecycle
	token := "pg-test-token-" + time.Now().Format("150405")
	gate := &PendingGate{
		ID:        "gate-pg-1",
		RunID:     runID,
		Token:     token,
		GateType:  "APPROVAL",
		ToolName:  "push_code",
		Status:    GateStatusPending,
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
	}
	if err := store.RecordPendingGate(ctx, gate); err != nil {
		t.Fatalf("failed to record pending gate: %v", err)
	}

	pending, err := store.ListPendingGates(ctx)
	if err != nil {
		t.Fatalf("failed to list pending gates: %v", err)
	}
	found := false
	for _, p := range pending {
		if p.RunID == runID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected gate for %s to be listed in pending", runID)
	}

	audit, err := store.ResolveGate(ctx, token, GateResolution{
		Decision: "approved",
		Actor:    "tester",
		Reason:   "verified",
	})
	if err != nil {
		t.Fatalf("failed to resolve gate: %v", err)
	}
	if audit.Action != "approved" {
		t.Fatalf("unexpected audit entry: %+v", audit)
	}
}
