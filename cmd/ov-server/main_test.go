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

	"github.com/gv-factory/gv-factory/internal/connectors/chat"
	"github.com/gv-factory/gv-factory/internal/connectors/inbound"
	"github.com/gv-factory/gv-factory/internal/gateway"
)

func TestServer_Smoke(t *testing.T) {
	// Smoke test ensuring ov-server package compiles and executes test suite
}

func setupTestMux(t *testing.T) (http.Handler, *gateway.Gateway) {
	t.Helper()
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})

	webhookIngestor := inbound.NewIngestor(inbound.Config{
		GitHubSecret: "test-secret",
	}, inbound.NewGatewayDispatcher(gw))

	chatHandler := chat.NewChatHandler(chat.HandlerConfig{
		SlackSigningSecret: "test-slack-secret",
	})

	mux := NewServerMux(nil, gw, webhookIngestor, chatHandler)
	return mux, gw
}

func TestServer_HealthCheck(t *testing.T) {
	mux, gw := setupTestMux(t)
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
	mux, gw := setupTestMux(t)
	defer gw.Close()

	subCh := make(chan gateway.Event, 10)
	unsub, err := gw.SubscribeTaskEvents(context.Background(), "gate.resolved", subCh)
	if err != nil {
		t.Fatalf("failed to subscribe to gate.resolved: %v", err)
	}
	defer unsub()

	t.Run("Valid resolution emits event", func(t *testing.T) {
		payload := map[string]string{
			"run_id":   "run-123",
			"token":    "tok-abc",
			"decision": "APPROVE",
		}
		data, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/gate/resolve", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200, got %d", rec.Code)
		}

		select {
		case ev := <-subCh:
			m := ev.Payload
			if m["run_id"] != "run-123" || m["decision"] != "APPROVE" {
				t.Errorf("unexpected event payload: %v", m)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for gate.resolved event")
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
	mux, gw := setupTestMux(t)
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

