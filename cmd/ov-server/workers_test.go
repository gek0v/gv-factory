// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/worker"
)

func TestWorkerRegistry_DefaultConfig(t *testing.T) {
	reg := NewWorkerRegistry(context.Background(), nil)
	defer reg.Close()

	if reg.cfg.HeartbeatTTL != 15*time.Second {
		t.Errorf("expected HeartbeatTTL 15s, got %v", reg.cfg.HeartbeatTTL)
	}
	if reg.cfg.RetentionTimeout != 5*time.Minute {
		t.Errorf("expected RetentionTimeout 5m, got %v", reg.cfg.RetentionTimeout)
	}
	if reg.cfg.PurgeInterval != 30*time.Second {
		t.Errorf("expected PurgeInterval 30s, got %v", reg.cfg.PurgeInterval)
	}
}

func TestWorkerRegistry_StatusEvaluation(t *testing.T) {
	now := time.Date(2026, 11, 1, 22, 0, 0, 0, time.UTC)
	currentTime := now
	clock := func() time.Time {
		return currentTime
	}

	reg := NewWorkerRegistry(context.Background(), nil, WorkerRegistryConfig{
		HeartbeatTTL:     15 * time.Second,
		RetentionTimeout: 5 * time.Minute,
		Clock:            clock,
	})
	defer reg.Close()

	// 1. Worker with active < max => ready
	reg.RegisterHeartbeat(worker.HeartbeatMessage{
		WorkerID:       "w-ready",
		ActiveTasks:    1,
		MaxConcurrency: 4,
		UptimeSeconds:  100,
		Timestamp:      currentTime.Unix(),
	})

	// 2. Worker with active >= max => busy
	reg.RegisterHeartbeat(worker.HeartbeatMessage{
		WorkerID:       "w-busy",
		ActiveTasks:    4,
		MaxConcurrency: 4,
		UptimeSeconds:  200,
		Timestamp:      currentTime.Unix(),
	})

	infoReady, ok := reg.GetWorker("w-ready")
	if !ok || infoReady.Status != WorkerStatusReady {
		t.Fatalf("expected w-ready to be %q, got ok=%v, status=%q", WorkerStatusReady, ok, infoReady.Status)
	}

	infoBusy, ok := reg.GetWorker("w-busy")
	if !ok || infoBusy.Status != WorkerStatusBusy {
		t.Fatalf("expected w-busy to be %q, got ok=%v, status=%q", WorkerStatusBusy, ok, infoBusy.Status)
	}

	// 3. Advance clock past TTL (16s) => both should be offline
	currentTime = now.Add(16 * time.Second)

	infoReady, ok = reg.GetWorker("w-ready")
	if !ok || infoReady.Status != WorkerStatusOffline {
		t.Fatalf("expected w-ready to be %q after TTL, got %q", WorkerStatusOffline, infoReady.Status)
	}

	infoBusy, ok = reg.GetWorker("w-busy")
	if !ok || infoBusy.Status != WorkerStatusOffline {
		t.Fatalf("expected w-busy to be %q after TTL, got %q", WorkerStatusOffline, infoBusy.Status)
	}
}

func TestWorkerRegistry_SummaryAndList(t *testing.T) {
	now := time.Date(2026, 11, 1, 22, 0, 0, 0, time.UTC)
	currentTime := now
	clock := func() time.Time {
		return currentTime
	}

	reg := NewWorkerRegistry(context.Background(), nil, WorkerRegistryConfig{
		HeartbeatTTL:     15 * time.Second,
		RetentionTimeout: 5 * time.Minute,
		Clock:            clock,
	})
	defer reg.Close()

	// Initial empty state
	emptySummary := reg.Summary()
	if emptySummary.TotalWorkers != 0 || emptySummary.ActiveWorkers != 0 {
		t.Fatalf("expected 0 workers in empty registry, got %+v", emptySummary)
	}
	emptyList := reg.ListWorkers()
	if emptyList == nil || len(emptyList) != 0 {
		t.Fatalf("expected non-nil empty list, got %v", emptyList)
	}

	// Register 3 workers: 1 ready, 1 busy, 1 to become offline
	reg.RegisterHeartbeat(worker.HeartbeatMessage{
		WorkerID:       "w-1",
		ActiveTasks:    1,
		MaxConcurrency: 4,
		UptimeSeconds:  60,
		Timestamp:      currentTime.Unix(),
	})
	reg.RegisterHeartbeat(worker.HeartbeatMessage{
		WorkerID:       "w-2",
		ActiveTasks:    2,
		MaxConcurrency: 2,
		UptimeSeconds:  120,
		Timestamp:      currentTime.Unix(),
	})
	reg.RegisterHeartbeat(worker.HeartbeatMessage{
		WorkerID:       "w-3",
		ActiveTasks:    0,
		MaxConcurrency: 8,
		UptimeSeconds:  180,
		Timestamp:      currentTime.Unix(),
	})

	summary := reg.Summary()
	if summary.TotalWorkers != 3 {
		t.Errorf("expected 3 total workers, got %d", summary.TotalWorkers)
	}
	if summary.ActiveWorkers != 3 {
		t.Errorf("expected 3 active workers, got %d", summary.ActiveWorkers)
	}
	if summary.TotalCapacity != 14 {
		t.Errorf("expected 14 total capacity (4+2+8), got %d", summary.TotalCapacity)
	}
	if summary.TotalActiveTasks != 3 {
		t.Errorf("expected 3 total active tasks (1+2+0), got %d", summary.TotalActiveTasks)
	}

	// Make w-3 offline by advancing clock for only w-1 and w-2
	currentTime = now.Add(20 * time.Second)
	reg.RegisterHeartbeat(worker.HeartbeatMessage{
		WorkerID:       "w-1",
		ActiveTasks:    2,
		MaxConcurrency: 4,
		UptimeSeconds:  80,
		Timestamp:      currentTime.Unix(),
	})
	reg.RegisterHeartbeat(worker.HeartbeatMessage{
		WorkerID:       "w-2",
		ActiveTasks:    2,
		MaxConcurrency: 2,
		UptimeSeconds:  140,
		Timestamp:      currentTime.Unix(),
	})

	summaryAfter := reg.Summary()
	if summaryAfter.TotalWorkers != 3 {
		t.Errorf("expected 3 total workers, got %d", summaryAfter.TotalWorkers)
	}
	if summaryAfter.ActiveWorkers != 2 {
		t.Errorf("expected 2 active workers (w-3 is offline), got %d", summaryAfter.ActiveWorkers)
	}
	if summaryAfter.TotalCapacity != 6 {
		t.Errorf("expected 6 active capacity (4+2), got %d", summaryAfter.TotalCapacity)
	}
	if summaryAfter.TotalActiveTasks != 4 {
		t.Errorf("expected 4 active tasks (2+2), got %d", summaryAfter.TotalActiveTasks)
	}

	// Verify ListWorkers is sorted by worker_id
	list := reg.ListWorkers()
	if len(list) != 3 {
		t.Fatalf("expected 3 workers in list, got %d", len(list))
	}
	if list[0].WorkerID != "w-1" || list[1].WorkerID != "w-2" || list[2].WorkerID != "w-3" {
		t.Errorf("expected sorted workers [w-1, w-2, w-3], got [%s, %s, %s]",
			list[0].WorkerID, list[1].WorkerID, list[2].WorkerID)
	}
	if list[2].Status != WorkerStatusOffline {
		t.Errorf("expected w-3 status offline, got %s", list[2].Status)
	}
}

func TestWorkerRegistry_PurgeStale(t *testing.T) {
	now := time.Date(2026, 11, 1, 22, 0, 0, 0, time.UTC)
	currentTime := now
	clock := func() time.Time {
		return currentTime
	}

	reg := NewWorkerRegistry(context.Background(), nil, WorkerRegistryConfig{
		HeartbeatTTL:     15 * time.Second,
		RetentionTimeout: 5 * time.Minute,
		Clock:            clock,
	})
	defer reg.Close()

	reg.RegisterHeartbeat(worker.HeartbeatMessage{
		WorkerID:       "w-stale",
		ActiveTasks:    0,
		MaxConcurrency: 2,
		UptimeSeconds:  10,
		Timestamp:      currentTime.Unix(),
	})
	reg.RegisterHeartbeat(worker.HeartbeatMessage{
		WorkerID:       "w-active",
		ActiveTasks:    0,
		MaxConcurrency: 2,
		UptimeSeconds:  10,
		Timestamp:      currentTime.Unix(),
	})

	// Advance clock by 4 minutes -> both still retained
	currentTime = now.Add(4 * time.Minute)
	reg.Purge(currentTime)

	if _, ok := reg.GetWorker("w-stale"); !ok {
		t.Fatalf("w-stale should not be purged at 4m")
	}

	// Refresh w-active
	reg.RegisterHeartbeat(worker.HeartbeatMessage{
		WorkerID:       "w-active",
		ActiveTasks:    0,
		MaxConcurrency: 2,
		UptimeSeconds:  250,
		Timestamp:      currentTime.Unix(),
	})

	// Advance clock to 5m 1s from initial registration
	currentTime = now.Add(5*time.Minute + 1*time.Second)
	reg.Purge(currentTime)

	// w-stale was last seen at t=0, which is > 5m ago => purged!
	if _, ok := reg.GetWorker("w-stale"); ok {
		t.Errorf("w-stale should have been purged after retention timeout")
	}

	// w-active was refreshed at t=4m => still present
	if _, ok := reg.GetWorker("w-active"); !ok {
		t.Errorf("w-active should still exist")
	}
}

func TestWorkerRegistry_GatewaySubscription(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gw := gateway.NewGateway()
	reg := NewWorkerRegistry(ctx, gw)
	defer reg.Close()

	// Publish via Gateway TopicWorkerHeartbeat
	hbPayload := map[string]any{
		"worker_id":       "worker-gateway-1",
		"active_tasks":    2,
		"max_concurrency": 4,
		"uptime_seconds":  45,
		"timestamp":       time.Now().Unix(),
	}

	err := gw.PublishTaskEvent(ctx, gateway.Event{
		Type:      gateway.TopicWorkerHeartbeat,
		RunID:     "worker-gateway-1",
		Payload:   hbPayload,
		Timestamp: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("failed to publish heartbeat: %v", err)
	}

	// Wait briefly for async ingestion
	deadline := time.Now().Add(1 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		if info, ok := reg.GetWorker("worker-gateway-1"); ok && info.ActiveTasks == 2 {
			found = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !found {
		t.Fatalf("worker-gateway-1 not registered from Gateway event")
	}
}

func TestWorkerRegistry_MalformedPayloadGraceful(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gw := gateway.NewGateway()
	reg := NewWorkerRegistry(ctx, gw)
	defer reg.Close()

	// Missing worker_id and bad types should not panic
	_ = gw.PublishTaskEvent(ctx, gateway.Event{
		Type: gateway.TopicWorkerHeartbeat,
		Payload: map[string]any{
			"active_tasks": "not-a-number",
		},
	})

	_ = gw.PublishTaskEvent(ctx, gateway.Event{
		Type:    gateway.TopicWorkerHeartbeat,
		Payload: nil,
	})

	time.Sleep(50 * time.Millisecond)
	if reg.Summary().TotalWorkers != 0 {
		t.Fatalf("expected 0 workers registered from malformed payloads")
	}
}

func TestWorkerRegistry_Concurrency(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := NewWorkerRegistry(ctx, nil)
	defer reg.Close()

	var wg sync.WaitGroup
	workersCount := 30

	for i := 0; i < workersCount; i++ {
		workerID := fmt.Sprintf("worker-%d", i)
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				reg.RegisterHeartbeat(worker.HeartbeatMessage{
					WorkerID:       id,
					ActiveTasks:    j % 5,
					MaxConcurrency: 4,
					UptimeSeconds:  int64(j),
					Timestamp:      time.Now().Unix(),
				})
				_ = reg.Summary()
				_ = reg.ListWorkers()
				_, _ = reg.GetWorker(id)
			}
		}(workerID)
	}

	wg.Wait()

	if reg.Summary().TotalWorkers != workersCount {
		t.Fatalf("expected %d total workers, got %d", workersCount, reg.Summary().TotalWorkers)
	}
}
