// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

func TestDispatcher_EventFiltering(t *testing.T) {
	var taskReceived, prReceived, wildcardReceived, inactiveReceived int32

	serverTask := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&taskReceived, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer serverTask.Close()

	serverPR := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&prReceived, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer serverPR.Close()

	serverWildcard := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&wildcardReceived, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer serverWildcard.Close()

	serverInactive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&inactiveReceived, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer serverInactive.Close()

	store := NewMemorySubscriptionStore()
	ctx := context.Background()

	_, _ = store.Create(ctx, Subscription{
		ID:         "sub-task-filter",
		URL:        serverTask.URL,
		EventTypes: []string{"task.completed"},
		Active:     true,
	})
	_, _ = store.Create(ctx, Subscription{
		ID:         "sub-pr-filter",
		URL:        serverPR.URL,
		EventTypes: []string{"pr.*"},
		Active:     true,
	})
	_, _ = store.Create(ctx, Subscription{
		ID:         "sub-wildcard-filter",
		URL:        serverWildcard.URL,
		EventTypes: []string{"*"},
		Active:     true,
	})
	_, _ = store.Create(ctx, Subscription{
		ID:         "sub-inactive-filter",
		URL:        serverInactive.URL,
		EventTypes: []string{"*"},
		Active:     false,
	})

	disp := NewDispatcher(DefaultConfig(), store, NewMemoryDeadLetterQueue(10), NewMemoryDeliveryAuditTrail(10))

	// Dispatch task.completed
	_, err := disp.Dispatch(ctx, Payload{
		ID:    "evt-1",
		Event: "task.completed",
		RunID: "run-filt-1",
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}

	if atomic.LoadInt32(&taskReceived) != 1 {
		t.Errorf("expected serverTask to receive 1 event, got %d", taskReceived)
	}
	if atomic.LoadInt32(&prReceived) != 0 {
		t.Errorf("expected serverPR to receive 0 events, got %d", prReceived)
	}
	if atomic.LoadInt32(&wildcardReceived) != 1 {
		t.Errorf("expected serverWildcard to receive 1 event, got %d", wildcardReceived)
	}
	if atomic.LoadInt32(&inactiveReceived) != 0 {
		t.Errorf("expected serverInactive to receive 0 events, got %d", inactiveReceived)
	}

	// Dispatch pr.ready_for_review
	_, err = disp.Dispatch(ctx, Payload{
		ID:    "evt-2",
		Event: "pr.ready_for_review",
		RunID: "run-filt-2",
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}

	if atomic.LoadInt32(&prReceived) != 1 {
		t.Errorf("expected serverPR to receive 1 event, got %d", prReceived)
	}
	if atomic.LoadInt32(&taskReceived) != 1 {
		t.Errorf("expected serverTask count unchanged at 1, got %d", taskReceived)
	}
	if atomic.LoadInt32(&wildcardReceived) != 2 {
		t.Errorf("expected serverWildcard to receive 2 total events, got %d", wildcardReceived)
	}
}

func TestDispatcher_GatewayAndTriggerDispatch(t *testing.T) {
	var receivedEvent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedEvent = r.Header.Get(HeaderEvent)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := NewMemorySubscriptionStore()
	ctx := context.Background()
	_, _ = store.Create(ctx, Subscription{
		ID:         "sub-all",
		URL:        server.URL,
		EventTypes: []string{"*"},
		Active:     true,
	})

	disp := NewDispatcher(DefaultConfig(), store, NewMemoryDeadLetterQueue(10), NewMemoryDeliveryAuditTrail(10))

	// Dispatch Gateway Event
	gwEv := gateway.Event{
		Type:      "gate.approval_required",
		RunID:     "run-gw-99",
		TaskID:    "task-eval-1",
		Payload:   map[string]any{"token": "sig-test-123"},
		Timestamp: time.Now().UnixMilli(),
	}
	_, err := disp.DispatchGatewayEvent(ctx, gwEv)
	if err != nil {
		t.Fatalf("failed dispatching gateway event: %v", err)
	}
	if receivedEvent != "gate.approval_required" {
		t.Errorf("expected received event 'gate.approval_required', got %s", receivedEvent)
	}

	// Dispatch Trigger Event
	trigEv := schema.TriggerEvent{
		ID:          "trig-gh-1",
		Source:      schema.SourceGitHub,
		EventType:   schema.EventIssueOpened,
		Repository:  "gek0v/gv-factory",
		IssueNumber: 34,
		Timestamp:   time.Now().UTC(),
	}
	_, err = disp.DispatchTriggerEvent(ctx, trigEv)
	if err != nil {
		t.Fatalf("failed dispatching trigger event: %v", err)
	}
	if receivedEvent != schema.EventIssueOpened {
		t.Errorf("expected received event 'issue.opened', got %s", receivedEvent)
	}
}
