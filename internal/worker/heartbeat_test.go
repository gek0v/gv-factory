// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type mockHeartbeatPublisher struct {
	mu       sync.Mutex
	messages []HeartbeatMessage
}

func (m *mockHeartbeatPublisher) PublishHeartbeat(hb HeartbeatMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, hb)
	return nil
}

func (m *mockHeartbeatPublisher) getCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.messages)
}

func (m *mockHeartbeatPublisher) getLast() HeartbeatMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		return HeartbeatMessage{}
	}
	return m.messages[len(m.messages)-1]
}

func TestHeartbeatLoop(t *testing.T) {
	pub := &mockHeartbeatPublisher{}
	startTime := time.Now().Add(-10 * time.Second)

	var activeTasks int64 = 2
	maxConcurrency := 4
	workerID := "worker-test-1"

	ctx, cancel := context.WithCancel(context.Background())

	hb := NewHeartbeatEmitter(HeartbeatConfig{
		WorkerID:       workerID,
		Interval:       20 * time.Millisecond,
		StartTime:      startTime,
		MaxConcurrency: maxConcurrency,
		GetActiveTasks: func() int {
			return int(activeTasks)
		},
		Publisher: pub,
	})

	go hb.Start(ctx)

	time.Sleep(75 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	count := pub.getCount()
	if count < 2 {
		t.Fatalf("expected at least 2 heartbeats, got %d", count)
	}

	last := pub.getLast()
	if last.WorkerID != workerID {
		t.Fatalf("expected worker ID %q, got %q", workerID, last.WorkerID)
	}
	if last.ActiveTasks != 2 {
		t.Fatalf("expected active tasks 2, got %d", last.ActiveTasks)
	}
	if last.MaxConcurrency != maxConcurrency {
		t.Fatalf("expected max concurrency %d, got %d", maxConcurrency, last.MaxConcurrency)
	}
	if last.UptimeSeconds < 10 {
		t.Fatalf("expected uptime >= 10s, got %d", last.UptimeSeconds)
	}
	if last.Timestamp == 0 {
		t.Fatalf("expected non-zero timestamp")
	}

	// Verify JSON valid
	_, err := json.Marshal(last)
	if err != nil {
		t.Fatalf("failed to marshal heartbeat: %v", err)
	}
}
