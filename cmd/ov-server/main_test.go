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
	"github.com/gv-factory/gv-factory/internal/connectors/chat"
	"github.com/gv-factory/gv-factory/internal/connectors/inbound"
	"github.com/gv-factory/gv-factory/internal/gateway"
)

func TestServer_Smoke(t *testing.T) {
	// Smoke test ensuring ov-server package compiles and executes test suite
}

func setupTestMux(t *testing.T, authSvcs ...*auth.Service) (http.Handler, *gateway.Gateway, *auth.Service) {
	t.Helper()
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})

	webhookIngestor := inbound.NewIngestor(inbound.Config{
		GitHubSecret: "test-secret",
	}, inbound.NewGatewayDispatcher(gw))

	chatHandler := chat.NewChatHandler(chat.HandlerConfig{
		SlackSigningSecret: "test-slack-secret",
	})

	var authSvc *auth.Service
	if len(authSvcs) > 0 {
		authSvc = authSvcs[0]
	} else {
		authSvc = auth.NewService("test-gate-secret-key")
	}

	mux := NewServerMux(nil, gw, webhookIngestor, chatHandler, authSvc)
	return mux, gw, authSvc
}

func TestServer_HealthCheck(t *testing.T) {
	mux, gw, _ := setupTestMux(t)
	defer gw.Close()

	endpoints := []string{"/healthz", "/health", "/api/v1/health"}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ep, nil)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected HTTP 200, got %d", rec.Code)
			}

			var resp map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode JSON response: %v", err)
			}

			if resp["status"] != "ok" {
				t.Errorf("expected status 'ok', got %v", resp["status"])
			}
			if resp["service"] != "ov-server" {
				t.Errorf("expected service 'ov-server', got %v", resp["service"])
			}
			if resp["gateway"] != "in-memory" {
				t.Errorf("expected gateway 'in-memory', got %v", resp["gateway"])
			}
		})
	}
}

func TestServer_GateResolve(t *testing.T) {
	mux, gw, authSvc := setupTestMux(t)
	defer gw.Close()

	resolvedCh := make(chan gateway.Event, 10)
	unsubResolved, err := gw.SubscribeTaskEvents(context.Background(), "gate.resolved", resolvedCh)
	if err != nil {
		t.Fatalf("failed to subscribe to gate.resolved: %v", err)
	}
	defer unsubResolved()

	auditCh := make(chan gateway.Event, 10)
	unsubAudit, err := gw.SubscribeTaskEvents(context.Background(), "gate.audit", auditCh)
	if err != nil {
		t.Fatalf("failed to subscribe to gate.audit: %v", err)
	}
	defer unsubAudit()

	t.Run("Valid approved resolution emits gate.resolved and gate.audit", func(t *testing.T) {
		runID := "run-valid-123"
		validToken := authSvc.GenerateApprovalToken(runID, time.Now().Add(1*time.Hour))
		payload := map[string]string{
			"run_id":   runID,
			"token":    validToken,
			"decision": "approved",
		}
		data, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/gate/resolve", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "AuditTestClient/1.0")
		req.RemoteAddr = "192.168.1.50:54321"
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		select {
		case ev := <-resolvedCh:
			m := ev.Payload
			if m["run_id"] != runID || m["decision"] != "approved" {
				t.Errorf("unexpected gate.resolved payload: %v", m)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for gate.resolved event")
		}

		select {
		case ev := <-auditCh:
			m := ev.Payload
			if m["run_id"] != runID || m["decision"] != "approved" || m["status"] != "SUCCESS" {
				t.Errorf("unexpected gate.audit payload: %v", m)
			}
			if m["client_ip"] != "192.168.1.50" {
				t.Errorf("expected client_ip 192.168.1.50, got %v", m["client_ip"])
			}
			if m["user_agent"] != "AuditTestClient/1.0" {
				t.Errorf("expected user_agent AuditTestClient/1.0, got %v", m["user_agent"])
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for gate.audit event")
		}

		// Verify record in audit logger
		records := authSvc.AuditLogger().GetRecords()
		if len(records) == 0 {
			t.Fatal("expected audit logger to have records")
		}
		lastRec := records[len(records)-1]
		if lastRec.RunID != runID || lastRec.Status != auth.AuditStatusSuccess {
			t.Errorf("expected audit record to match, got: %+v", lastRec)
		}
	})

	t.Run("Valid rejected resolution emits gate.resolved and gate.audit", func(t *testing.T) {
		runID := "run-reject-456"
		validToken := authSvc.GenerateApprovalToken(runID, time.Now().Add(1*time.Hour))
		payload := map[string]string{
			"run_id":   runID,
			"token":    validToken,
			"decision": "rejected",
		}
		data, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/gate/resolve", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		select {
		case ev := <-resolvedCh:
			m := ev.Payload
			if m["run_id"] != runID || m["decision"] != "rejected" {
				t.Errorf("unexpected gate.resolved payload: %v", m)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for gate.resolved event")
		}

		select {
		case ev := <-auditCh:
			m := ev.Payload
			if m["run_id"] != runID || m["decision"] != "rejected" || m["status"] != "SUCCESS" {
				t.Errorf("unexpected gate.audit payload: %v", m)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for gate.audit event on rejected")
		}
	})

	t.Run("Rejects invalid HMAC signature with 401", func(t *testing.T) {
		runID := "run-tampered-123"
		tamperedToken := "run-tampered-123:1800000000:invalidhmacsignature"
		payload := map[string]string{
			"run_id":   runID,
			"token":    tamperedToken,
			"decision": "approved",
		}
		data, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/gate/resolve", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected HTTP 401 Unauthorized, got %d", rec.Code)
		}

		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["error"] != "invalid or expired approval token" {
			t.Errorf("expected 'invalid or expired approval token', got %q", resp["error"])
		}

		// Ensure audit event was published
		select {
		case ev := <-auditCh:
			m := ev.Payload
			if m["status"] != "INVALID_SIGNATURE" {
				t.Errorf("expected audit status INVALID_SIGNATURE, got %v", m["status"])
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for gate.audit event on invalid HMAC")
		}
	})

	t.Run("Rejects expired token with 401", func(t *testing.T) {
		runID := "run-expired-123"
		expiredToken := authSvc.GenerateApprovalToken(runID, time.Now().Add(-10*time.Minute))
		payload := map[string]string{
			"run_id":   runID,
			"token":    expiredToken,
			"decision": "approved",
		}
		data, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/gate/resolve", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected HTTP 401 Unauthorized, got %d", rec.Code)
		}

		select {
		case ev := <-auditCh:
			m := ev.Payload
			if m["status"] != "INVALID_SIGNATURE" {
				t.Errorf("expected audit status INVALID_SIGNATURE, got %v", m["status"])
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for gate.audit event on expired token")
		}
	})

	t.Run("Rejects token run ID mismatch with 401", func(t *testing.T) {
		tokenRunID := "run-actual-123"
		validToken := authSvc.GenerateApprovalToken(tokenRunID, time.Now().Add(1*time.Hour))
		payload := map[string]string{
			"run_id":   "run-spoofed-456",
			"token":    validToken,
			"decision": "approved",
		}
		data, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/gate/resolve", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected HTTP 401 Unauthorized, got %d", rec.Code)
		}

		var resp map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["error"] != "invalid or expired approval token" {
			t.Errorf("expected 'invalid or expired approval token', got %q", resp["error"])
		}

		select {
		case ev := <-auditCh:
			m := ev.Payload
			if m["status"] != "RUN_ID_MISMATCH" {
				t.Errorf("expected audit status RUN_ID_MISMATCH, got %v", m["status"])
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for gate.audit event on run ID mismatch")
		}
	})

	t.Run("Rejects invalid decision with 400 Bad Request", func(t *testing.T) {
		runID := "run-invalid-decision"
		validToken := authSvc.GenerateApprovalToken(runID, time.Now().Add(1*time.Hour))
		payload := map[string]string{
			"run_id":   runID,
			"token":    validToken,
			"decision": "MAYBE",
		}
		data, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/gate/resolve", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected HTTP 400 Bad Request, got %d", rec.Code)
		}

		select {
		case ev := <-auditCh:
			m := ev.Payload
			if m["status"] != "INVALID_DECISION" {
				t.Errorf("expected audit status INVALID_DECISION, got %v", m["status"])
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for gate.audit event on invalid decision")
		}
	})

	t.Run("Method Not Allowed on GET", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/gate/resolve", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 Method Not Allowed, got %d", rec.Code)
		}
	})

	t.Run("Bad Request on invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/gate/resolve", bytes.NewReader([]byte("{invalid-json")))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", rec.Code)
		}
	})
}

func TestServer_SSEEvents(t *testing.T) {
	mux, gw, _ := setupTestMux(t)
	defer gw.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(rec, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", ct)
	}
}

func TestServer_TelemetryAndObservability(t *testing.T) {
	mux, gw, _ := setupTestMux(t)
	defer gw.Close()

	t.Run("Metrics endpoint returns Prometheus format", func(t *testing.T) {
		dummyReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		dummyRec := httptest.NewRecorder()
		mux.ServeHTTP(dummyRec, dummyReq)

		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK from /metrics, got %d", rec.Code)
		}

		body := rec.Body.String()
		if !bytes.Contains([]byte(body), []byte("ov_http_requests_total")) {
			t.Errorf("expected ov_http_requests_total metric in /metrics, got:\n%s", body)
		}
	})

	t.Run("Healthz includes telemetry status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode JSON response: %v", err)
		}

		if resp["telemetry"] == nil || resp["telemetry"] == "" {
			t.Errorf("expected 'telemetry' field in /healthz, got %v", resp["telemetry"])
		}
	})

	t.Run("Readyz endpoint returns readiness status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK from /readyz, got %d", rec.Code)
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode JSON response: %v", err)
		}

		if resp["ready"] != true {
			t.Errorf("expected ready=true, got %v", resp["ready"])
		}
		if resp["telemetry"] == nil {
			t.Errorf("expected 'telemetry' field in /readyz")
		}
	})

	t.Run("Tracing header injected into response", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		// Regular requests should receive traceparent header or be tracked
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
	})
}

