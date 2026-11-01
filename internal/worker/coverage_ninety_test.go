// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryQueue_BroadcastWithoutGroup(t *testing.T) {
	q := NewInMemoryQueue()
	defer q.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Subscribe without group (broadcast)
	sub1, err := q.Subscribe(ctx, "test.broadcast", "")
	if err != nil {
		t.Fatalf("failed to subscribe sub1: %v", err)
	}
	defer sub1.Unsubscribe()

	sub2, err := q.Subscribe(ctx, "test.broadcast", "")
	if err != nil {
		t.Fatalf("failed to subscribe sub2: %v", err)
	}
	defer sub2.Unsubscribe()

	if err := q.Publish("test.broadcast", []byte("broadcast-message")); err != nil {
		t.Fatalf("failed to publish broadcast: %v", err)
	}

	// Both should receive the message
	select {
	case m := <-sub1.Messages():
		if string(m.Data()) != "broadcast-message" {
			t.Fatalf("unexpected message in sub1: %s", string(m.Data()))
		}
	case <-ctx.Done():
		t.Fatalf("timeout waiting for sub1")
	}

	select {
	case m := <-sub2.Messages():
		if string(m.Data()) != "broadcast-message" {
			t.Fatalf("unexpected message in sub2: %s", string(m.Data()))
		}
	case <-ctx.Done():
		t.Fatalf("timeout waiting for sub2")
	}
}

func TestInMemoryQueue_PublishNoSubscribers(t *testing.T) {
	q := NewInMemoryQueue()
	defer q.Close()

	// Publish to subject with no subscribers should succeed silently
	if err := q.Publish("unsubscribed.topic", []byte("drop")); err != nil {
		t.Fatalf("unexpected error publishing to unsubscribed topic: %v", err)
	}
}

func TestConsumer_ApplyAckPolicy_RetryableDelayDefault(t *testing.T) {
	c := NewConsumer(ConsumerConfig{})
	msg := NewInMemoryQueueMessage("test", []byte{})

	// RetryableError with delay <= 0 defaults to 500ms
	err := NewRetryableError(context.DeadlineExceeded, 0)
	c.applyAckPolicy(msg, nil, err)

	if msg.NakCount() != 1 {
		t.Fatalf("expected 1 Nak, got %d", msg.NakCount())
	}
	if msg.LastDelay() != 500*time.Millisecond {
		t.Fatalf("expected 500ms default delay, got %v", msg.LastDelay())
	}
}

func TestConsumer_ListenLoopAcquireSlotContextDone(t *testing.T) {
	q := NewInMemoryQueue()
	defer q.Close()

	c := NewConsumer(ConsumerConfig{
		Concurrency: 1,
		Queue:       q,
	})

	ctx, cancel := context.WithCancel(context.Background())

	// Fill the semaphore slot
	c.sem <- struct{}{}

	if err := c.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Publish message that will block on c.sem
	_ = q.Publish(TopicWorkTasks, []byte(`{"task_id":"t1","run_id":"r1","step_id":"s1","task_type":"wasm_plugin"}`))

	time.Sleep(20 * time.Millisecond)
	// Cancel while trying to acquire semaphore in listenLoop
	cancel()
	c.Stop()
}
