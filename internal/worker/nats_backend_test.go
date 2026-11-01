// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestNatsQueueMessage_Wrapper(t *testing.T) {
	// Unit test for natsQueueMessage wrapper methods without requiring broker connection
	nm := &nats.Msg{
		Subject: TopicWorkTasks,
		Data:    []byte(`{"task_id":"123"}`),
	}
	qm := newNatsQueueMessage(nm)

	if string(qm.Data()) != `{"task_id":"123"}` {
		t.Fatalf("unexpected data: %s", string(qm.Data()))
	}
	if qm.Subject() != TopicWorkTasks {
		t.Fatalf("unexpected subject: %s", qm.Subject())
	}

	// Calling Ack on unconnected nats.Msg returns error
	if err := qm.Ack(); err == nil {
		t.Fatalf("expected error on unmanaged nats message, got nil")
	}
	if err := qm.NakWithDelay(100 * time.Millisecond); err == nil {
		t.Fatalf("expected error on unmanaged nats message, got nil")
	}
	if err := qm.Term(); err == nil {
		t.Fatalf("expected error on unmanaged nats message, got nil")
	}
	if err := qm.InProgress(); err == nil {
		t.Fatalf("expected error on unmanaged nats message, got nil")
	}
}

func TestNewQueueBackend_Fallback(t *testing.T) {
	// When NATS is disabled or unreachable, it gracefully falls back to in-memory queue
	backend, isNATS, err := NewQueueBackend(QueueBackendConfig{
		NATSURL:        "nats://127.0.0.1:4222",
		DisableNATS:    true,
		ConnectTimeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer backend.Close()

	if isNATS {
		t.Fatalf("expected fallback to in-memory, but isNATS was true")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	sub, err := backend.Subscribe(ctx, TopicWorkTasks, ConsumerGroup)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	err = backend.Publish(TopicWorkTasks, []byte("ping"))
	if err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	select {
	case msg := <-sub.Messages():
		if string(msg.Data()) != "ping" {
			t.Fatalf("expected ping, got %s", string(msg.Data()))
		}
	case <-ctx.Done():
		t.Fatalf("timeout waiting for fallback message")
	}
}
