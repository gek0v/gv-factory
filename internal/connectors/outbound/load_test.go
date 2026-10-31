// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
)

func TestDispatcher_ConcurrentLoad(t *testing.T) {
	var receivedCount int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&receivedCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.WorkerCount = 8
	cfg.QueueSize = 2000

	store := NewMemorySubscriptionStore()
	ctx := context.Background()

	// Register 3 active subscribers targeting server
	for i := 1; i <= 3; i++ {
		_, _ = store.Create(ctx, Subscription{
			URL:        server.URL,
			Secret:     "secret-load-test",
			EventTypes: []string{"task.completed"},
			Active:     true,
		})
	}

	audit := NewMemoryDeliveryAuditTrail(2000)
	disp := NewDispatcher(cfg, store, NewMemoryDeadLetterQueue(100), audit)

	goroutinesBefore := runtime.NumGoroutine()
	disp.Start(ctx)

	const totalEvents = 100
	var wg sync.WaitGroup

	// Dispatch 100 events concurrently from 10 goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(routineIdx int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, err := disp.Dispatch(ctx, Payload{
					ID:        generateID("load-evt"),
					Event:     "task.completed",
					Timestamp: time.Now().UnixMilli(),
					RunID:     "run-load",
					Data:      map[string]any{"routine": routineIdx, "idx": j},
				})
				if err != nil {
					t.Errorf("dispatch error: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	// 100 events * 3 subscriptions = 300 deliveries
	const expectedDeliveries = totalEvents * 3
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt64(&receivedCount) < expectedDeliveries && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if atomic.LoadInt64(&receivedCount) != expectedDeliveries {
		t.Fatalf("expected %d deliveries under load, got %d", expectedDeliveries, receivedCount)
	}

	// Graceful shutdown
	if err := disp.Stop(); err != nil {
		t.Fatalf("failed stopping dispatcher: %v", err)
	}

	// Verify worker pool terminated cleanly and no goroutines leaked
	time.Sleep(50 * time.Millisecond)
	goroutinesAfter := runtime.NumGoroutine()

	// Allow small delta for runtime background threads (GC, timer)
	if goroutinesAfter > goroutinesBefore+2 {
		t.Errorf("potential goroutine leak detected: before=%d, after=%d", goroutinesBefore, goroutinesAfter)
	}
}

func TestDispatcher_AttachToGateway(t *testing.T) {
	var mu sync.Mutex
	var receivedEvent string
	var receivedSig string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedEvent = r.Header.Get(HeaderEvent)
		receivedSig = r.Header.Get(HeaderSignature)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	disp := NewDefaultDispatcher()
	ctx := context.Background()

	_, _ = disp.Store().Create(ctx, Subscription{
		URL:        server.URL,
		Secret:     "secret-gw-test",
		EventTypes: []string{"ov.task.completed"},
		Active:     true,
	})
	unsub, err := disp.AttachToGateway(ctx, gw, gateway.TopicTaskCompleted)
	if err != nil {
		t.Fatalf("failed attaching to gateway: %v", err)
	}
	defer unsub()

	// Publish gateway task event
	err = gw.PublishTaskEvent(ctx, gateway.Event{
		Type:      gateway.TopicTaskCompleted,
		RunID:     "run-gw-live",
		TaskID:    "task-compile",
		Payload:   map[string]any{"duration_ms": 120},
		Timestamp: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("failed publishing task event: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ev := receivedEvent
		mu.Unlock()
		if ev != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	ev := receivedEvent
	sig := receivedSig
	mu.Unlock()

	if ev != gateway.TopicTaskCompleted {
		t.Errorf("expected received event %q, got %q", gateway.TopicTaskCompleted, ev)
	}
	if sig == "" {
		t.Errorf("expected HMAC signature to be present on gateway forwarded event")
	}
}

func BenchmarkSign(b *testing.B) {
	secret := "benchmark-secret-key-123"
	payload := []byte(`{"id":"evt-bench","event":"task.completed","timestamp":1730419200000,"run_id":"run-1"}`)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = Sign(secret, payload)
	}
}

func BenchmarkVerifySignature(b *testing.B) {
	secret := "benchmark-secret-key-123"
	payload := []byte(`{"id":"evt-bench","event":"task.completed","timestamp":1730419200000,"run_id":"run-1"}`)
	sig := Sign(secret, payload)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = VerifySignature(secret, payload, sig)
	}
}

func BenchmarkMatchesEvent(b *testing.B) {
	pattern := "task.*"
	event := "task.completed"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = MatchesEvent(pattern, event)
	}
}

func BenchmarkDispatcher_Dispatch(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := NewMemorySubscriptionStore()
	ctx := context.Background()

	_, _ = store.Create(ctx, Subscription{
		URL:        server.URL,
		Secret:     "secret-bench",
		EventTypes: []string{"task.completed"},
		Active:     true,
	})

	disp := NewDefaultDispatcher()
	payload := Payload{
		ID:        "evt-bench",
		Event:     "task.completed",
		Timestamp: time.Now().UnixMilli(),
		RunID:     "run-bench",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = disp.Dispatch(ctx, payload)
	}
}
