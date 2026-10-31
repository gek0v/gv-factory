// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gv-factory/gv-factory/pkg/schema"
)

type mockGatewayPublisher struct {
	lastEvent *schema.TriggerEvent
	err       error
}

func (m *mockGatewayPublisher) PublishTrigger(ctx context.Context, ev schema.TriggerEvent) error {
	m.lastEvent = &ev
	return m.err
}

func TestIngestor_Dispatchers(t *testing.T) {
	ev := schema.TriggerEvent{
		ID:        "trig-dispatch",
		Source:    schema.SourceGitHub,
		EventType: schema.EventPush,
	}

	// GatewayDispatcher
	mockPub := &mockGatewayPublisher{}
	gwDisp := NewGatewayDispatcher(mockPub)
	if err := gwDisp.Dispatch(context.Background(), ev); err != nil {
		t.Fatalf("gateway dispatch failed: %v", err)
	}
	if mockPub.lastEvent == nil || mockPub.lastEvent.ID != "trig-dispatch" {
		t.Fatalf("expected event dispatched to gateway publisher")
	}

	// MultiDispatcher
	ch1 := make(chan schema.TriggerEvent, 1)
	ch2 := make(chan schema.TriggerEvent, 1)
	multi := NewMultiDispatcher(NewChannelDispatcher(ch1), NewChannelDispatcher(ch2))
	if err := multi.Dispatch(context.Background(), ev); err != nil {
		t.Fatalf("multi dispatch failed: %v", err)
	}
	if (<-ch1).ID != "trig-dispatch" || (<-ch2).ID != "trig-dispatch" {
		t.Fatalf("expected event delivered to both channels in multi dispatcher")
	}

	// Ingestor returns 500 when dispatcher fails
	failingDispatcher := DispatcherFunc(func(ctx context.Context, ev schema.TriggerEvent) error {
		return errors.New("dispatch sink unavailable")
	})
	ingFailing := NewIngestor(Config{
		GitHubSecret:          testGitHubSecret,
		DisableFreshnessCheck: true,
	}, failingDispatcher)

	body := []byte(fixtureGitHubIssueOpened)
	sig := computeHMAC([]byte(testGitHubSecret), body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "issues")
	rec := httptest.NewRecorder()
	ingFailing.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on dispatcher error, got %d", rec.Code)
	}
}

func TestIngestor_ProcessWebhook_Direct(t *testing.T) {
	ch := make(chan schema.TriggerEvent, 1)
	ing := NewIngestor(Config{
		GitLabSecret:          testGitLabSecret,
		DisableFreshnessCheck: true,
	}, NewChannelDispatcher(ch))

	headers := http.Header{}
	headers.Set("X-Gitlab-Token", testGitLabSecret)
	headers.Set("X-Gitlab-Event", "Merge Request Hook")

	body := []byte(fixtureGitLabMergeRequest)
	ev, err := ing.ProcessWebhook(context.Background(), "gitlab", headers, body)
	if err != nil {
		t.Fatalf("ProcessWebhook failed: %v", err)
	}
	if ev.PRNumber != 105 {
		t.Errorf("expected PR number 105, got %d", ev.PRNumber)
	}

	dispatched := <-ch
	if dispatched.PRNumber != 105 {
		t.Errorf("expected dispatched PR number 105, got %d", dispatched.PRNumber)
	}
}
