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

	"github.com/gv-factory/gv-factory/internal/gateway"
)

func TestServer_GatePendingAndAlias(t *testing.T) {
	mux, gw, authSvc := setupTestMux(t)
	defer gw.Close()

	t.Run("Alias /api/v1/gates/resolve behaves identically to /api/gate/resolve", func(t *testing.T) {
		runID := "run-alias-123"
		validToken := authSvc.GenerateApprovalToken(runID, time.Now().Add(1*time.Hour))
		payload := map[string]string{
			"run_id":   runID,
			"token":    validToken,
			"decision": "approved",
		}
		data, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/gates/resolve", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200 on /api/v1/gates/resolve, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET /api/v1/gates/pending lifecycle", func(t *testing.T) {
		// 1. Initial pending list should be empty
		req := httptest.NewRequest(http.MethodGet, "/api/v1/gates/pending", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200, got %d", rec.Code)
		}

		var resp struct {
			Gates []PendingGateInfo `json:"gates"`
			Total int               `json:"total"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if resp.Total != 0 || len(resp.Gates) != 0 {
			t.Fatalf("expected 0 pending gates, got %d", resp.Total)
		}

		// 2. Publish gate.approval_required
		runID := "run-pending-999"
		token := authSvc.GenerateApprovalToken(runID, time.Now().Add(10*time.Minute))
		err := gw.Publish(context.Background(), gateway.TopicGateApprovalRequired, gateway.Event{
			Type:      gateway.TopicGateApprovalRequired,
			RunID:     runID,
			Timestamp: time.Now().UnixMilli(),
			Payload: map[string]any{
				"run_id":     runID,
				"tool_name":  "fs_delete",
				"arguments":  map[string]any{"path": "/tmp/secret.txt"},
				"token":      token,
				"timeout_ms": 600000,
				"expires_at": time.Now().Add(10 * time.Minute).Unix(),
				"caller_id":  "agent-tester",
			},
		})
		if err != nil {
			t.Fatalf("publish failed: %v", err)
		}

		// Small delay to ensure event processing
		time.Sleep(50 * time.Millisecond)

		// 3. Query GET /api/v1/gates/pending
		req = httptest.NewRequest(http.MethodGet, "/api/v1/gates/pending", nil)
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200, got %d", rec.Code)
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if resp.Total != 1 || len(resp.Gates) != 1 {
			t.Fatalf("expected 1 pending gate, got %d", resp.Total)
		}
		gate := resp.Gates[0]
		if gate.RunID != runID || gate.ToolName != "fs_delete" {
			t.Fatalf("unexpected gate info: %+v", gate)
		}
		if gate.TokenHMACPrefix == "" {
			t.Errorf("expected non-empty TokenHMACPrefix")
		}

		// 4. Resolve the gate via /api/gate/resolve
		resolvePayload := map[string]string{
			"run_id":   runID,
			"token":    token,
			"decision": "approved",
		}
		resolveData, _ := json.Marshal(resolvePayload)
		resReq := httptest.NewRequest(http.MethodPost, "/api/gate/resolve", bytes.NewReader(resolveData))
		resReq.Header.Set("Content-Type", "application/json")
		resRec := httptest.NewRecorder()
		mux.ServeHTTP(resRec, resReq)

		if resRec.Code != http.StatusOK {
			t.Fatalf("resolve failed: %d, body: %s", resRec.Code, resRec.Body.String())
		}

		time.Sleep(50 * time.Millisecond)

		// 5. Query GET /api/v1/gates/pending again -> should be empty now
		req = httptest.NewRequest(http.MethodGet, "/api/v1/gates/pending", nil)
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if resp.Total != 0 || len(resp.Gates) != 0 {
			t.Fatalf("expected 0 pending gates after resolution, got %d", resp.Total)
		}
	})
}
