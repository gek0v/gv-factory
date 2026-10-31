// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/pkg/schema"
)

func TestIngestor_ServeHTTP_Success(t *testing.T) {
	ch := make(chan schema.TriggerEvent, 10)
	dispatcher := NewChannelDispatcher(ch)

	cfg := Config{
		GitHubSecret:          testGitHubSecret,
		GitLabSecret:          testGitLabSecret,
		SentrySecret:          testSentrySecret,
		DisableFreshnessCheck: true,
	}
	ing := NewIngestor(cfg, dispatcher)

	body := []byte(fixtureGitHubIssueOpened)
	sig := computeHMAC([]byte(testGitHubSecret), body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "delivery-gh-99")

	rec := httptest.NewRecorder()
	ing.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case ev := <-ch:
		if ev.Source != schema.SourceGitHub {
			t.Errorf("expected source %s, got %s", schema.SourceGitHub, ev.Source)
		}
		if ev.EventType != schema.EventIssueOpened {
			t.Errorf("expected event_type %s, got %s", schema.EventIssueOpened, ev.EventType)
		}
		if ev.IssueNumber != 33 {
			t.Errorf("expected issue 33, got %d", ev.IssueNumber)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for dispatched trigger event")
	}
}

func TestIngestor_ServeHTTP_Ping(t *testing.T) {
	ing := NewIngestor(Config{GitHubSecret: testGitHubSecret}, nil)
	body := []byte(`{"zen":"Mindfulness"}`)
	sig := computeHMAC([]byte(testGitHubSecret), body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "ping")

	rec := httptest.NewRecorder()
	ing.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for ping, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "pong") {
		t.Errorf("expected pong in response body, got %s", rec.Body.String())
	}
}

func TestIngestor_ServeHTTP_Errors(t *testing.T) {
	ing := NewIngestor(Config{
		GitHubSecret: testGitHubSecret,
		MaxBodySize:  512, // 512 bytes limit
	}, nil)

	// 1. Method Not Allowed (GET)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/github", nil)
	recGet := httptest.NewRecorder()
	ing.ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", recGet.Code)
	}

	// 2. Unknown Provider (404)
	reqUnknown := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/unknown-vendor", strings.NewReader(`{}`))
	recUnknown := httptest.NewRecorder()
	ing.ServeHTTP(recUnknown, reqUnknown)
	if recUnknown.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d", recUnknown.Code)
	}

	// 3. Invalid Signature (401)
	reqBadSig := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(`{"action":"opened"}`))
	reqBadSig.Header.Set("X-Hub-Signature-256", "sha256=invalidhex")
	reqBadSig.Header.Set("X-GitHub-Event", "issues")
	recBadSig := httptest.NewRecorder()
	ing.ServeHTTP(recBadSig, reqBadSig)
	if recBadSig.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", recBadSig.Code)
	}

	// 4. Malformed JSON (400)
	badJSON := []byte(`{"invalid-json`)
	badSig := computeHMAC([]byte(testGitHubSecret), badJSON)
	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(badJSON))
	reqBadJSON.Header.Set("X-Hub-Signature-256", badSig)
	reqBadJSON.Header.Set("X-GitHub-Event", "issues")
	recBadJSON := httptest.NewRecorder()
	ing.ServeHTTP(recBadJSON, reqBadJSON)
	if recBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for malformed JSON, got %d", recBadJSON.Code)
	}

	// 5. Oversized Payload (413)
	largeBody := bytes.Repeat([]byte("A"), 1024)
	largeSig := computeHMAC([]byte(testGitHubSecret), largeBody)
	reqLarge := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(largeBody))
	reqLarge.Header.Set("X-Hub-Signature-256", largeSig)
	reqLarge.Header.Set("X-GitHub-Event", "issues")
	recLarge := httptest.NewRecorder()
	ing.ServeHTTP(recLarge, reqLarge)
	if recLarge.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 Request Entity Too Large, got %d", recLarge.Code)
	}
}
