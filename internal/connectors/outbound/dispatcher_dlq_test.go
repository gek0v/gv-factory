// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDispatcher_PermanentFailure_DeadLetterQueue(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable) // 503 always
		_, _ = w.Write([]byte("service continuously down"))
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.RetryPolicy = RetryPolicy{
		InitialInterval: 5 * time.Millisecond,
		BackoffFactor:   2.0,
		MaxInterval:     20 * time.Millisecond,
		MaxAttempts:     3,
		RequestTimeout:  500 * time.Millisecond,
		Jitter:          false,
	}

	disp := NewDispatcher(cfg, NewMemorySubscriptionStore(), NewMemoryDeadLetterQueue(10), NewMemoryDeliveryAuditTrail(10))
	ctx := context.Background()

	sub := Subscription{
		ID:         "sub-dlq",
		URL:        server.URL,
		Secret:     "dlq-secret",
		EventTypes: []string{"factory.failed"},
		Active:     true,
	}

	payload := Payload{
		ID:        "evt-dlq",
		Event:     "factory.failed",
		Timestamp: time.Now().UnixMilli(),
		RunID:     "run-dlq-500",
		Data:      map[string]any{"reason": "oom_killed"},
	}

	rec, err := disp.SendDirect(ctx, sub, payload)
	if err == nil {
		t.Fatalf("expected error after exhausted retries, got nil")
	}
	if rec.Success {
		t.Errorf("expected delivery record to indicate failure")
	}
	if rec.Attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", rec.Attempts)
	}

	// Verify Dead Letter Queue entry with full failure diagnostics
	dlqCount, _ := disp.DeadLetterQueue().Count(ctx)
	if dlqCount != 1 {
		t.Fatalf("expected 1 DLQ entry, got %d", dlqCount)
	}

	dlqRecords, _ := disp.DeadLetterQueue().List(ctx, 1)
	if len(dlqRecords) != 1 {
		t.Fatalf("expected 1 record returned, got %d", len(dlqRecords))
	}

	dlqRec := dlqRecords[0]
	if dlqRec.SubscriptionID != "sub-dlq" {
		t.Errorf("expected subscription ID 'sub-dlq', got %s", dlqRec.SubscriptionID)
	}
	if dlqRec.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status code 503, got %d", dlqRec.StatusCode)
	}
	if dlqRec.Attempts != 3 {
		t.Errorf("expected 3 attempts in DLQ record, got %d", dlqRec.Attempts)
	}
	if !strings.Contains(dlqRec.Diagnostic, "Delivery failed after 3 attempts") {
		t.Errorf("expected diagnostic message, got %q", dlqRec.Diagnostic)
	}
}

func TestDispatcher_PermanentClientError_NoFutileRetries(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest) // 400 Bad Request
		_, _ = w.Write([]byte("invalid payload schema"))
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.RetryPolicy = RetryPolicy{
		InitialInterval: 5 * time.Millisecond,
		BackoffFactor:   2.0,
		MaxInterval:     20 * time.Millisecond,
		MaxAttempts:     5,
		RequestTimeout:  500 * time.Millisecond,
	}

	disp := NewDispatcher(cfg, NewMemorySubscriptionStore(), NewMemoryDeadLetterQueue(10), NewMemoryDeliveryAuditTrail(10))
	ctx := context.Background()

	sub := Subscription{
		ID:         "sub-client-err",
		URL:        server.URL,
		EventTypes: []string{"task.failed"},
		Active:     true,
	}

	payload := Payload{
		ID:        "evt-bad-req",
		Event:     "task.failed",
		Timestamp: time.Now().UnixMilli(),
		RunID:     "run-client-400",
	}

	rec, err := disp.SendDirect(ctx, sub, payload)
	if !errors.Is(err, ErrPermanentClientError) {
		t.Fatalf("expected ErrPermanentClientError, got %v", err)
	}

	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected only 1 attempt for permanent 400 error (no futile retries), got %d", attempts)
	}
	if rec.Attempts != 1 {
		t.Errorf("expected record attempts to be 1, got %d", rec.Attempts)
	}

	dlqCount, _ := disp.DeadLetterQueue().Count(ctx)
	if dlqCount != 1 {
		t.Errorf("expected 1 DLQ entry, got %d", dlqCount)
	}
}
