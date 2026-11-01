// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDispatcher_HMACSignatureAndHeaders(t *testing.T) {
	secret := "secret-hmac-test-key"
	var receivedSig, receivedTimestamp, receivedDelivery, receivedEvent, receivedCustom, receivedTraceparent string
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get(HeaderSignature)
		receivedTimestamp = r.Header.Get(HeaderTimestamp)
		receivedDelivery = r.Header.Get(HeaderDelivery)
		receivedEvent = r.Header.Get(HeaderEvent)
		receivedCustom = r.Header.Get("X-Audit-Header")
		receivedTraceparent = r.Header.Get("traceparent")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	disp := NewDefaultDispatcher()
	ctx := context.Background()

	sub := Subscription{
		ID:         "sub-hmac",
		URL:        server.URL,
		Secret:     secret,
		EventTypes: []string{"task.completed"},
		Active:     true,
		Headers:    map[string]string{"X-Audit-Header": "audit-val-123"},
	}

	payload := Payload{
		ID:        "evt-test-1",
		Event:     "task.completed",
		Timestamp: 1730419200000,
		RunID:     "run-hmac-99",
		Data: map[string]any{
			"task_name": "ast-indexer",
			"status":    "ok",
			"_trace": map[string]string{
				"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			},
		},
	}

	rec, err := disp.SendDirect(ctx, sub, payload)
	if err != nil {
		t.Fatalf("unexpected delivery error: %v", err)
	}
	if !rec.Success {
		t.Fatalf("expected successful delivery")
	}

	// 1. Verify headers present
	if receivedSig == "" {
		t.Errorf("missing header %s", HeaderSignature)
	}
	if receivedTimestamp != "1730419200000" {
		t.Errorf("expected timestamp %q, got %q", "1730419200000", receivedTimestamp)
	}
	if receivedDelivery != rec.ID {
		t.Errorf("expected delivery header %q, got %q", rec.ID, receivedDelivery)
	}
	if receivedEvent != "task.completed" {
		t.Errorf("expected event header %q, got %q", "task.completed", receivedEvent)
	}
	if receivedCustom != "audit-val-123" {
		t.Errorf("expected custom header %q, got %q", "audit-val-123", receivedCustom)
	}
	if receivedTraceparent != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Errorf("expected traceparent header preserved in outbound webhook, got %q", receivedTraceparent)
	}

	// 2. Verify HMAC-SHA256 signature
	if !VerifySignature(secret, receivedBody, receivedSig) {
		t.Errorf("HMAC-SHA256 signature verification failed for payload: %s", string(receivedBody))
	}
}

func TestDispatcher_RetryExponentialBackoff_503_429_Recovery(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&attempts, 1)
		if cur == 1 {
			w.WriteHeader(http.StatusServiceUnavailable) // 503 on first try
			_, _ = w.Write([]byte("temporary overload"))
			return
		}
		if cur == 2 {
			w.WriteHeader(http.StatusTooManyRequests) // 429 on second try
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"received"}`))
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.RetryPolicy = RetryPolicy{
		InitialInterval: 5 * time.Millisecond,
		BackoffFactor:   2.0,
		MaxInterval:     50 * time.Millisecond,
		MaxAttempts:     4,
		RequestTimeout:  1 * time.Second,
		Jitter:          false,
	}

	disp := NewDispatcher(cfg, NewMemorySubscriptionStore(), NewMemoryDeadLetterQueue(10), NewMemoryDeliveryAuditTrail(10))
	ctx := context.Background()

	sub := Subscription{
		ID:         "sub-retry",
		URL:        server.URL,
		Secret:     "retry-secret",
		EventTypes: []string{"gate.approval_required"},
		Active:     true,
	}

	payload := Payload{
		ID:        "evt-retry",
		Event:     "gate.approval_required",
		Timestamp: time.Now().UnixMilli(),
		RunID:     "run-retry-101",
	}

	rec, err := disp.SendDirect(ctx, sub, payload)
	if err != nil {
		t.Fatalf("expected recovery after retries, got error: %v", err)
	}

	if !rec.Success {
		t.Errorf("expected delivery record to be successful")
	}
	if rec.Attempts != 3 {
		t.Errorf("expected exactly 3 attempts (503 -> 429 -> 200), got %d", rec.Attempts)
	}

	// Verify no DLQ entry on recovery
	dlqCount, _ := disp.DeadLetterQueue().Count(ctx)
	if dlqCount != 0 {
		t.Errorf("expected 0 DLQ entries on successful recovery, got %d", dlqCount)
	}
}
