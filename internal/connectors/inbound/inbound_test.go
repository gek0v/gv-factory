// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gv-factory/gv-factory/pkg/schema"
)

func TestGitHubProvider_Verification(t *testing.T) {
	p := NewGitHubProvider(testGitHubSecret)
	body := []byte(fixtureGitHubIssueOpened)
	sig := computeHMAC([]byte(testGitHubSecret), body)

	// Valid HMAC
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(string(body)))
	req.Header.Set("X-Hub-Signature-256", sig)
	if err := p.Verify(req, body); err != nil {
		t.Fatalf("expected valid HMAC to verify, got: %v", err)
	}

	// Tampered payload
	tamperedBody := []byte(`{"action":"opened","tampered":true}`)
	if err := p.Verify(req, tamperedBody); err == nil {
		t.Fatal("expected tampered body to fail verification")
	}

	// Missing signature header
	reqNoSig, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(string(body)))
	if err := p.Verify(reqNoSig, body); err == nil {
		t.Fatal("expected missing signature header to fail")
	}

	// Invalid signature
	reqBadSig, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(string(body)))
	reqBadSig.Header.Set("X-Hub-Signature-256", "sha256=0000000000000000000000000000000000000000000000000000000000000000")
	if err := p.Verify(reqBadSig, body); err == nil {
		t.Fatal("expected invalid signature to fail")
	}
}

func TestGitHubProvider_Normalization(t *testing.T) {
	p := NewGitHubProvider(testGitHubSecret)
	p.SkipFreshnessCheck = true

	// Issue opened
	body := []byte(fixtureGitHubIssueOpened)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/github", nil)
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "deliv-1234")

	ev, err := p.Normalize(req, body)
	if err != nil {
		t.Fatalf("failed to normalize github issue: %v", err)
	}

	if ev.Source != schema.SourceGitHub {
		t.Errorf("expected source %q, got %q", schema.SourceGitHub, ev.Source)
	}
	if ev.EventType != schema.EventIssueOpened {
		t.Errorf("expected event_type %q, got %q", schema.EventIssueOpened, ev.EventType)
	}
	if ev.IssueNumber != 33 {
		t.Errorf("expected issue number 33, got %d", ev.IssueNumber)
	}
	if ev.Repository != "gek0v/gv-factory" {
		t.Errorf("expected repository gek0v/gv-factory, got %q", ev.Repository)
	}
	if ev.Sender != "gek0v" {
		t.Errorf("expected sender gek0v, got %q", ev.Sender)
	}

	// PR review requested
	prBody := []byte(fixtureGitHubPRReviewReq)
	prReq, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/github", nil)
	prReq.Header.Set("X-GitHub-Event", "pull_request")

	prEv, err := p.Normalize(prReq, prBody)
	if err != nil {
		t.Fatalf("failed to normalize github PR: %v", err)
	}
	if prEv.EventType != schema.EventPRReviewReq {
		t.Errorf("expected event_type %q, got %q", schema.EventPRReviewReq, prEv.EventType)
	}
	if prEv.PRNumber != 42 {
		t.Errorf("expected PR number 42, got %d", prEv.PRNumber)
	}
	if prEv.Reference != "feat/inbound-webhooks" {
		t.Errorf("expected reference feat/inbound-webhooks, got %q", prEv.Reference)
	}

	// PR merged vs closed
	mergedPRBody := []byte(`{"action":"closed","pull_request":{"number":10,"merged":true}}`)
	mEv, err := p.Normalize(prReq, mergedPRBody)
	if err != nil || mEv.EventType != schema.EventPRMerged {
		t.Errorf("expected pr.merged, got %v (err: %v)", mEv.EventType, err)
	}

	unmergedPRBody := []byte(`{"action":"closed","pull_request":{"number":10,"merged":false}}`)
	uEv, err := p.Normalize(prReq, unmergedPRBody)
	if err != nil || uEv.EventType != schema.EventPRClosed {
		t.Errorf("expected pr.closed, got %v (err: %v)", uEv.EventType, err)
	}

	// Push event
	pushBody := []byte(`{"ref":"refs/heads/main","after":"c0ffee","head_commit":{"timestamp":"2026-10-31T20:00:00Z"}}`)
	pushReq, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/github", nil)
	pushReq.Header.Set("X-GitHub-Event", "push")
	pushEv, err := p.Normalize(pushReq, pushBody)
	if err != nil || pushEv.EventType != schema.EventPush {
		t.Errorf("expected push, got %v (err: %v)", pushEv.EventType, err)
	}
	if pushEv.Reference != "refs/heads/main" {
		t.Errorf("expected refs/heads/main, got %s", pushEv.Reference)
	}

	// Ping event
	pingReq, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/github", nil)
	pingReq.Header.Set("X-GitHub-Event", "ping")
	pingEv, err := p.Normalize(pingReq, []byte(`{"zen":"Design for failure"}`))
	if err != nil || pingEv.EventType != "ping" {
		t.Errorf("expected ping event, got %v (err: %v)", pingEv.EventType, err)
	}
}

func TestGitLabProvider_Verification(t *testing.T) {
	p := NewGitLabProvider(testGitLabSecret)
	body := []byte(fixtureGitLabMergeRequest)

	// Valid token
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/gitlab", nil)
	req.Header.Set("X-Gitlab-Token", testGitLabSecret)
	if err := p.Verify(req, body); err != nil {
		t.Fatalf("expected valid token to verify, got: %v", err)
	}

	// Invalid token
	reqBad, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/gitlab", nil)
	reqBad.Header.Set("X-Gitlab-Token", "wrong-secret-token")
	if err := p.Verify(reqBad, body); err == nil {
		t.Fatal("expected invalid token to fail")
	}

	// Missing token
	reqNoToken, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/gitlab", nil)
	if err := p.Verify(reqNoToken, body); err == nil {
		t.Fatal("expected missing token to fail")
	}
}

func TestGitLabProvider_Normalization(t *testing.T) {
	p := NewGitLabProvider(testGitLabSecret)
	p.SkipFreshnessCheck = true

	// Merge Request
	body := []byte(fixtureGitLabMergeRequest)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/gitlab", nil)
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	req.Header.Set("X-Gitlab-Event-UUID", "uuid-gl-1")

	ev, err := p.Normalize(req, body)
	if err != nil {
		t.Fatalf("failed to normalize gitlab merge request: %v", err)
	}

	if ev.Source != schema.SourceGitLab {
		t.Errorf("expected source gitlab, got %s", ev.Source)
	}
	if ev.EventType != schema.EventPROpened {
		t.Errorf("expected event_type %s, got %s", schema.EventPROpened, ev.EventType)
	}
	if ev.PRNumber != 105 {
		t.Errorf("expected PR number 105, got %d", ev.PRNumber)
	}
	if ev.Repository != "gitlab-org/project-x" {
		t.Errorf("expected repository gitlab-org/project-x, got %s", ev.Repository)
	}
	if ev.Reference != "feature/ai-agent" {
		t.Errorf("expected reference feature/ai-agent, got %s", ev.Reference)
	}
	if ev.Sender != "gitlab-user" {
		t.Errorf("expected sender gitlab-user, got %s", ev.Sender)
	}

	// Pipeline Hook
	pipeBody := []byte(fixtureGitLabPipeline)
	pipeReq, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/gitlab", nil)
	pipeReq.Header.Set("X-Gitlab-Event", "Pipeline Hook")

	pipeEv, err := p.Normalize(pipeReq, pipeBody)
	if err != nil {
		t.Fatalf("failed to normalize pipeline event: %v", err)
	}
	if pipeEv.EventType != schema.EventPipelineSuccess {
		t.Errorf("expected event_type %s, got %s", schema.EventPipelineSuccess, pipeEv.EventType)
	}
	if pipeEv.Reference != "main" {
		t.Errorf("expected reference main, got %s", pipeEv.Reference)
	}
}

func TestSentryProvider_Verification(t *testing.T) {
	p := NewSentryProvider(testSentrySecret)
	p.SkipFreshnessCheck = true
	body := []byte(fixtureSentryAlert)
	sig := computeHMAC([]byte(testSentrySecret), body)

	// HMAC signature header
	reqSig, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/sentry", nil)
	reqSig.Header.Set("Sentry-Hook-Signature", sig)
	if err := p.Verify(reqSig, body); err != nil {
		t.Fatalf("expected valid sentry HMAC to verify: %v", err)
	}

	// Bearer token header
	reqBearer, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/sentry", nil)
	reqBearer.Header.Set("Authorization", "Bearer "+testSentrySecret)
	if err := p.Verify(reqBearer, body); err != nil {
		t.Fatalf("expected valid sentry bearer token to verify: %v", err)
	}

	// Query token
	reqQuery, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/sentry?token="+testSentrySecret, nil)
	if err := p.Verify(reqQuery, body); err != nil {
		t.Fatalf("expected valid query token to verify: %v", err)
	}

	// Invalid signature
	reqBadSig, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/sentry", nil)
	reqBadSig.Header.Set("Sentry-Hook-Signature", "invalid-sig")
	if err := p.Verify(reqBadSig, body); err == nil {
		t.Fatal("expected invalid signature to fail")
	}

	// Missing auth
	reqNone, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/sentry", nil)
	if err := p.Verify(reqNone, body); err == nil {
		t.Fatal("expected missing auth to fail")
	}
}

func TestSentryProvider_Normalization(t *testing.T) {
	p := NewSentryProvider(testSentrySecret)
	p.SkipFreshnessCheck = true
	body := []byte(fixtureSentryAlert)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/sentry", nil)
	req.Header.Set("Sentry-Hook-Resource", "event_alert")

	ev, err := p.Normalize(req, body)
	if err != nil {
		t.Fatalf("failed to normalize sentry alert: %v", err)
	}

	if ev.Source != schema.SourceSentry {
		t.Errorf("expected source sentry, got %s", ev.Source)
	}
	if ev.EventType != schema.EventAlertTriggered {
		t.Errorf("expected event_type %s, got %s", schema.EventAlertTriggered, ev.EventType)
	}
	if ev.Repository != "gv-factory" {
		t.Errorf("expected repository gv-factory, got %s", ev.Repository)
	}
	if ev.Sender != "sentry-alert-rule" {
		t.Errorf("expected sender sentry-alert-rule, got %s", ev.Sender)
	}
}

func TestIngestor_TraceContextPropagation(t *testing.T) {
	ch := make(chan schema.TriggerEvent, 1)
	dispatcher := NewChannelDispatcher(ch)
	ing := NewIngestor(Config{
		GitHubSecret:          testGitHubSecret,
		DisableFreshnessCheck: true,
	}, dispatcher)

	body := []byte(fixtureGitHubIssueOpened)
	sig := computeHMAC([]byte(testGitHubSecret), body)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	rec := httptest.NewRecorder()
	ing.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d", rec.Code)
	}

	select {
	case ev := <-ch:
		traceData, ok := ev.Payload["_trace"].(map[string]string)
		if !ok || traceData == nil {
			t.Fatalf("expected '_trace' in payload, got %v", ev.Payload["_trace"])
		}
		if traceData["traceparent"] != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
			t.Errorf("expected traceparent preserved, got %q", traceData["traceparent"])
		}
	default:
		t.Fatal("expected event dispatched to channel")
	}
}
