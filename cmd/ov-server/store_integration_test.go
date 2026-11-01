// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/auth"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/store"
)

func TestServer_StoreIntegration(t *testing.T) {
	memStore := store.NewMemoryStore()
	defer memStore.Close()

	orch := newMockOrchestrator()
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	authSvc := auth.NewService("store-test-secret")
	mux := NewServerMux(nil, gw, nil, nil, orch, authSvc, memStore)

	t.Run("POST /api/v1/runs creates record in persistent store", func(t *testing.T) {
		reqBody := map[string]any{
			"run_id":   "run-store-test-01",
			"template": validTestTemplate(),
			"branch":   "feature/test",
		}
		data, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		// Verify presence in store
		storedRun, err := memStore.GetRun(context.Background(), "run-store-test-01")
		if err != nil {
			t.Fatalf("run not found in store: %v", err)
		}
		if storedRun.ID != "run-store-test-01" || storedRun.TemplateName != "test-pipeline" {
			t.Fatalf("unexpected stored run: %+v", storedRun)
		}
	})

	t.Run("GET /api/v1/runs/{id} fallback to store when orchestrator does not have run", func(t *testing.T) {
		// Manually insert run into store
		err := memStore.CreateRun(context.Background(), &store.Run{
			ID:           "run-historical-42",
			TemplateName: "archived-template",
			Status:       store.RunStatusCompleted,
		})
		if err != nil {
			t.Fatalf("failed to insert historical run: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/run-historical-42", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK from persistent store, got %d", rec.Code)
		}

		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["id"] != "run-historical-42" {
			t.Fatalf("expected id run-historical-42, got %v", resp["id"])
		}
	})

	t.Run("POST /api/v1/gates/resolve updates audit in store", func(t *testing.T) {
		runID := "run-store-gate-1"
		token := authSvc.GenerateApprovalToken(runID, time.Now().Add(1*time.Hour))

		// Pre-register pending gate in store
		_ = memStore.RecordPendingGate(context.Background(), &store.PendingGate{
			ID:        "gate-store-1",
			RunID:     runID,
			Token:     token,
			GateType:  "HUMAN_APPROVAL",
			Status:    store.GateStatusPending,
			ExpiresAt: time.Now().Add(1 * time.Hour),
		})

		payload := map[string]string{
			"run_id":   runID,
			"token":    token,
			"decision": "approved",
		}
		data, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/gates/resolve", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		// Verify gate was resolved in store
		pending, err := memStore.ListPendingGates(context.Background())
		if err != nil {
			t.Fatalf("failed to list pending gates: %v", err)
		}
		for _, p := range pending {
			if p.RunID == runID {
				t.Fatalf("gate %s should no longer be pending in store", runID)
			}
		}
	})

	t.Run("GET /api/v1/runs/{id}/steps integration trace", func(t *testing.T) {
		runID := "run-trace-steps"
		_ = memStore.CreateRun(context.Background(), &store.Run{
			ID:           runID,
			TemplateName: "feature-pipeline",
			Status:       store.RunStatusRunning,
			CreatedAt:    time.Now().UTC(),
		})

		// Emit step events through the gateway
		_ = gw.Publish(context.Background(), gateway.TopicTaskStarted, gateway.Event{
			Type:      gateway.TopicTaskStarted,
			RunID:     runID,
			TaskID:    "step-lint",
			Timestamp: time.Now().UnixMilli(),
			Payload: map[string]any{
				"step_name": "lint",
			},
		})

		_ = gw.Publish(context.Background(), gateway.TopicTaskCompleted, gateway.Event{
			Type:      gateway.TopicTaskCompleted,
			RunID:     runID,
			TaskID:    "step-lint",
			Timestamp: time.Now().UnixMilli(),
			Payload: map[string]any{
				"step_name":   "lint",
				"duration_ms": 150,
				"output":      "ok",
			},
		})

		time.Sleep(50 * time.Millisecond)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID+"/steps", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var steps []*store.Step
		if err := json.NewDecoder(rec.Body).Decode(&steps); err != nil {
			t.Fatalf("failed to decode steps JSON: %v", err)
		}
		if len(steps) != 2 {
			t.Fatalf("expected 2 steps returned, got %d", len(steps))
		}
	})
}
