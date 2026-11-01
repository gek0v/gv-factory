// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryStore_RunLifecycle(t *testing.T) {
	st := NewMemoryStore()
	defer st.Close()

	ctx := context.Background()

	// Test ErrInvalidInput on nil or empty run
	if err := st.CreateRun(ctx, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for nil run, got %v", err)
	}
	if err := st.CreateRun(ctx, &Run{ID: ""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty ID, got %v", err)
	}
	if _, err := st.GetRun(ctx, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty ID, got %v", err)
	}
	if err := st.UpdateRunStatus(ctx, "", RunStatusRunning, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty ID, got %v", err)
	}
	if _, err := st.GetRun(ctx, "non-existent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := st.UpdateRunStatus(ctx, "non-existent", RunStatusRunning, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	run1 := &Run{
		ID:           "run-101",
		TemplateName: "microservice-pipeline",
		InitiatedBy:  "alice",
		Metadata: map[string]any{
			"env": "production",
		},
	}

	// Create run
	if err := st.CreateRun(ctx, run1); err != nil {
		t.Fatalf("failed to create run: %v", err)
	}

	// Conflict on duplicate ID
	if err := st.CreateRun(ctx, run1); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate run, got %v", err)
	}

	// Get run
	got, err := st.GetRun(ctx, "run-101")
	if err != nil {
		t.Fatalf("failed to get run: %v", err)
	}
	if got.ID != "run-101" || got.Status != RunStatusQueued || got.InitiatedBy != "alice" {
		t.Fatalf("unexpected run fields: %+v", got)
	}
	if got.Metadata["env"] != "production" {
		t.Fatalf("expected metadata env=production, got %v", got.Metadata["env"])
	}

	// Update status to RUNNING
	if err := st.UpdateRunStatus(ctx, "run-101", RunStatusRunning, nil); err != nil {
		t.Fatalf("failed to update status to RUNNING: %v", err)
	}
	runningRun, _ := st.GetRun(ctx, "run-101")
	if runningRun.Status != RunStatusRunning || runningRun.StartedAt == nil {
		t.Fatalf("expected status RUNNING and StartedAt set, got %+v", runningRun)
	}

	// Update status to FAILED with error
	expectedErr := errors.New("compilation failed")
	if err := st.UpdateRunStatus(ctx, "run-101", RunStatusFailed, expectedErr); err != nil {
		t.Fatalf("failed to update status to FAILED: %v", err)
	}
	failedRun, _ := st.GetRun(ctx, "run-101")
	if failedRun.Status != RunStatusFailed || failedRun.CompletedAt == nil || failedRun.ErrorMessage != expectedErr.Error() {
		t.Fatalf("expected status FAILED with error message, got %+v", failedRun)
	}

	// List runs with filter
	runs, err := st.ListRuns(ctx, RunFilter{Status: RunStatusFailed, TemplateName: "microservice-pipeline", InitiatedBy: "alice"})
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "run-101" {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}

	// Filter with offset beyond total
	runsEmpty, err := st.ListRuns(ctx, RunFilter{Offset: 10})
	if err != nil {
		t.Fatalf("failed list runs with offset: %v", err)
	}
	if len(runsEmpty) != 0 {
		t.Fatalf("expected 0 runs, got %d", len(runsEmpty))
	}

	// Close store and check ErrStoreClosed
	_ = st.Close()
	if err := st.CreateRun(ctx, &Run{ID: "run-closed"}); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed, got %v", err)
	}
	if _, err := st.GetRun(ctx, "run-101"); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed, got %v", err)
	}
	if _, err := st.ListRuns(ctx, RunFilter{}); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed, got %v", err)
	}
	if err := st.UpdateRunStatus(ctx, "run-101", RunStatusCompleted, nil); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed, got %v", err)
	}
	if err := st.RecordStep(ctx, &Step{ID: "s1", RunID: "r1"}); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed, got %v", err)
	}
	if _, err := st.ListStepsByRun(ctx, "r1"); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed, got %v", err)
	}
	if err := st.RecordPendingGate(ctx, &PendingGate{RunID: "r1"}); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed, got %v", err)
	}
	if err := st.RecordGateAudit(ctx, &GateAudit{RunID: "r1"}); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed, got %v", err)
	}
	if _, err := st.ListPendingGates(ctx); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed, got %v", err)
	}
	if _, err := st.ResolveGate(ctx, "t1", GateResolution{}); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed, got %v", err)
	}
	if err := st.WithTx(ctx, func(tx Store) error { return nil }); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed, got %v", err)
	}
}

func TestMemoryStore_StepLifecycle(t *testing.T) {
	st := NewMemoryStore()
	defer st.Close()

	ctx := context.Background()

	if err := st.RecordStep(ctx, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if err := st.RecordStep(ctx, &Step{ID: ""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if err := st.RecordStep(ctx, &Step{ID: "s1", RunID: ""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if _, err := st.ListStepsByRun(ctx, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}

	step1 := &Step{
		ID:         "step-1",
		RunID:      "run-200",
		StepName:   "lint",
		Status:     StepStatusCompleted,
		DurationMS: 1250,
		Output: map[string]any{
			"warnings": 0,
		},
	}
	step2 := &Step{
		ID:         "step-2",
		RunID:      "run-200",
		StepName:   "test",
		Status:     StepStatusRunning,
		DurationMS: 500,
	}

	if err := st.RecordStep(ctx, step1); err != nil {
		t.Fatalf("failed to record step 1: %v", err)
	}
	if err := st.RecordStep(ctx, step2); err != nil {
		t.Fatalf("failed to record step 2: %v", err)
	}

	steps, err := st.ListStepsByRun(ctx, "run-200")
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if steps[0].StepName != "lint" || steps[1].StepName != "test" {
		t.Fatalf("steps out of order: %+v", steps)
	}

	emptySteps, err := st.ListStepsByRun(ctx, "non-existent-run")
	if err != nil {
		t.Fatalf("failed to list steps for empty run: %v", err)
	}
	if len(emptySteps) != 0 {
		t.Fatalf("expected 0 steps, got %d", len(emptySteps))
	}
}

func TestMemoryStore_GateLifecycle(t *testing.T) {
	st := NewMemoryStore()
	defer st.Close()

	ctx := context.Background()

	if err := st.RecordPendingGate(ctx, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if err := st.RecordPendingGate(ctx, &PendingGate{RunID: ""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if err := st.RecordGateAudit(ctx, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if err := st.RecordGateAudit(ctx, &GateAudit{RunID: ""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if _, err := st.ResolveGate(ctx, "", GateResolution{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}

	token := "token-secret-xyz-12345"
	tokenHash := HashToken(token)

	gate := &PendingGate{
		ID:        "gate-1",
		RunID:     "run-300",
		Token:     token,
		GateType:  "HUMAN_APPROVAL",
		ToolName:  "deploy_prod",
		Status:    GateStatusPending,
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
	}

	if err := st.RecordPendingGate(ctx, gate); err != nil {
		t.Fatalf("failed to record pending gate: %v", err)
	}

	pending, err := st.ListPendingGates(ctx)
	if err != nil {
		t.Fatalf("failed to list pending gates: %v", err)
	}
	if len(pending) != 1 || pending[0].TokenHash != tokenHash {
		t.Fatalf("expected 1 pending gate with hash %s, got %+v", tokenHash, pending)
	}

	// Resolve Gate successfully
	audit, err := st.ResolveGate(ctx, token, GateResolution{
		Decision: "approved",
		Actor:    "admin@gv-factory.local",
		Reason:   "Verified release checklist",
	})
	if err != nil {
		t.Fatalf("failed to resolve gate: %v", err)
	}
	if audit.Action != "approved" || audit.Actor != "admin@gv-factory.local" {
		t.Fatalf("unexpected audit entry: %+v", audit)
	}

	// Replay resolution should return ErrGateAlreadyResolved
	if _, err := st.ResolveGate(ctx, token, GateResolution{Decision: "approved"}); !errors.Is(err, ErrGateAlreadyResolved) {
		t.Fatalf("expected ErrGateAlreadyResolved on duplicate resolve, got %v", err)
	}

	// Verify gate is no longer listed in pending
	pendingAfter, err := st.ListPendingGates(ctx)
	if err != nil {
		t.Fatalf("failed to list pending gates after resolve: %v", err)
	}
	if len(pendingAfter) != 0 {
		t.Fatalf("expected 0 pending gates after resolve, got %d", len(pendingAfter))
	}

	// Direct audit recording
	manualAudit := &GateAudit{
		RunID:     "run-manual",
		TokenHash: "hash-manual",
		GateType:  "SECURITY",
		Action:    "rejected",
		Actor:     "sec-bot",
	}
	if err := st.RecordGateAudit(ctx, manualAudit); err != nil {
		t.Fatalf("failed to record manual audit: %v", err)
	}
}

func TestMemoryStore_GateExpiration(t *testing.T) {
	st := NewMemoryStore()
	defer st.Close()

	ctx := context.Background()
	token := "expired-token-abc"

	gate := &PendingGate{
		ID:        "gate-expired",
		RunID:     "run-exp",
		Token:     token,
		Status:    GateStatusPending,
		ExpiresAt: time.Now().UTC().Add(-1 * time.Minute), // Already expired
	}

	if err := st.RecordPendingGate(ctx, gate); err != nil {
		t.Fatalf("failed to record gate: %v", err)
	}

	// Expired gates should not appear in ListPendingGates
	pending, err := st.ListPendingGates(ctx)
	if err != nil {
		t.Fatalf("failed to list pending gates: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected 0 pending gates, got %d", len(pending))
	}

	// Resolving expired gate returns ErrGateExpired
	if _, err := st.ResolveGate(ctx, token, GateResolution{Decision: "approved"}); !errors.Is(err, ErrGateExpired) {
		t.Fatalf("expected ErrGateExpired, got %v", err)
	}
}

func TestMemoryStore_WithTxRollback(t *testing.T) {
	st := NewMemoryStore()
	defer st.Close()

	ctx := context.Background()

	_ = st.CreateRun(ctx, &Run{ID: "run-pre-existing", TemplateName: "initial"})

	errFailing := errors.New("simulated transaction failure")

	txErr := st.WithTx(ctx, func(tx Store) error {
		if err := tx.CreateRun(ctx, &Run{ID: "run-in-tx", TemplateName: "tx-template"}); err != nil {
			return err
		}
		if err := tx.RecordStep(ctx, &Step{ID: "step-in-tx", RunID: "run-pre-existing", StepName: "tx-step"}); err != nil {
			return err
		}
		return errFailing
	})

	if !errors.Is(txErr, errFailing) {
		t.Fatalf("expected errFailing, got %v", txErr)
	}

	// Verify rollback took effect
	if _, err := st.GetRun(ctx, "run-in-tx"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected run-in-tx to be rolled back, but found it or got %v", err)
	}

	steps, err := st.ListStepsByRun(ctx, "run-pre-existing")
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(steps) != 0 {
		t.Fatalf("expected steps to be rolled back, got %d", len(steps))
	}

	// Verify commit on success
	commitErr := st.WithTx(ctx, func(tx Store) error {
		if err := tx.CreateRun(ctx, &Run{ID: "run-committed", TemplateName: "committed"}); err != nil {
			return err
		}
		if _, err := tx.GetRun(ctx, "run-committed"); err != nil {
			return err
		}
		if _, err := tx.ListRuns(ctx, RunFilter{TemplateName: "committed"}); err != nil {
			return err
		}
		if err := tx.UpdateRunStatus(ctx, "run-committed", RunStatusRunning, nil); err != nil {
			return err
		}
		if err := tx.RecordStep(ctx, &Step{ID: "step-tx-1", RunID: "run-committed", StepName: "init"}); err != nil {
			return err
		}
		if _, err := tx.ListStepsByRun(ctx, "run-committed"); err != nil {
			return err
		}
		if err := tx.RecordPendingGate(ctx, &PendingGate{ID: "g-tx", RunID: "run-committed", Token: "tx-token"}); err != nil {
			return err
		}
		if _, err := tx.ListPendingGates(ctx); err != nil {
			return err
		}
		if _, err := tx.ResolveGate(ctx, "tx-token", GateResolution{Decision: "approved"}); err != nil {
			return err
		}
		if err := tx.RecordGateAudit(ctx, &GateAudit{RunID: "run-committed", Action: "tx-audit"}); err != nil {
			return err
		}
		_ = tx.Close()
		return tx.WithTx(ctx, func(nested Store) error {
			return nil
		})
	})
	if commitErr != nil {
		t.Fatalf("expected success, got %v", commitErr)
	}

	committedRun, err := st.GetRun(ctx, "run-committed")
	if err != nil || committedRun == nil {
		t.Fatalf("expected run-committed to exist, err: %v", err)
	}
}
