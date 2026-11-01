// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestInMemoryQueueMessage_AckNakTerm(t *testing.T) {
	msg := NewInMemoryQueueMessage("ov.work.tasks", []byte("hello"))

	if string(msg.Data()) != "hello" {
		t.Fatalf("expected data 'hello', got %s", string(msg.Data()))
	}
	if msg.Subject() != "ov.work.tasks" {
		t.Fatalf("expected subject 'ov.work.tasks', got %s", msg.Subject())
	}

	if msg.IsAcked() || msg.IsTermed() || msg.NakCount() != 0 {
		t.Fatalf("expected pristine message state")
	}

	if err := msg.InProgress(); err != nil {
		t.Fatalf("unexpected in progress error: %v", err)
	}

	if err := msg.NakWithDelay(100 * time.Millisecond); err != nil {
		t.Fatalf("unexpected nak error: %v", err)
	}
	if msg.NakCount() != 1 || msg.LastDelay() != 100*time.Millisecond {
		t.Fatalf("expected 1 nak and 100ms delay, got count %d delay %v", msg.NakCount(), msg.LastDelay())
	}

	if err := msg.Ack(); err != nil {
		t.Fatalf("unexpected ack error: %v", err)
	}
	if !msg.IsAcked() {
		t.Fatalf("expected IsAcked to be true")
	}

	msg2 := NewInMemoryQueueMessage("ov.work.tasks", []byte("bad"))
	if err := msg2.Term(); err != nil {
		t.Fatalf("unexpected term error: %v", err)
	}
	if !msg2.IsTermed() {
		t.Fatalf("expected IsTermed to be true")
	}
}

func TestInMemoryQueue_PublishAndSubscribe(t *testing.T) {
	q := NewInMemoryQueue()
	defer q.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sub, err := q.Subscribe(ctx, TopicWorkTasks, ConsumerGroup)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	const numMessages = 10
	for i := 0; i < numMessages; i++ {
		err := q.Publish(TopicWorkTasks, []byte("msg"))
		if err != nil {
			t.Fatalf("failed to publish: %v", err)
		}
	}

	received := 0
	for i := 0; i < numMessages; i++ {
		select {
		case msg := <-sub.Messages():
			if string(msg.Data()) != "msg" {
				t.Fatalf("unexpected data: %s", string(msg.Data()))
			}
			_ = msg.Ack()
			received++
		case <-ctx.Done():
			t.Fatalf("timeout waiting for message %d", i)
		}
	}

	if received != numMessages {
		t.Fatalf("expected %d messages, got %d", numMessages, received)
	}
}

func TestInMemoryQueue_ConcurrentGroupSubscription(t *testing.T) {
	q := NewInMemoryQueue()
	defer q.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	sub1, err := q.Subscribe(ctx, TopicWorkTasks, ConsumerGroup)
	if err != nil {
		t.Fatalf("failed to subscribe sub1: %v", err)
	}
	defer sub1.Unsubscribe()

	sub2, err := q.Subscribe(ctx, TopicWorkTasks, ConsumerGroup)
	if err != nil {
		t.Fatalf("failed to subscribe sub2: %v", err)
	}
	defer sub2.Unsubscribe()

	const totalMsgs = 20
	var wg sync.WaitGroup
	var count1, count2 int
	var mu sync.Mutex

	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-sub1.Messages():
				mu.Lock()
				count1++
				total := count1 + count2
				mu.Unlock()
				if total == totalMsgs {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case <-sub2.Messages():
				mu.Lock()
				count2++
				total := count1 + count2
				mu.Unlock()
				if total == totalMsgs {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	for i := 0; i < totalMsgs; i++ {
		if err := q.Publish(TopicWorkTasks, []byte("round-robin")); err != nil {
			t.Fatalf("failed to publish: %v", err)
		}
	}

	wg.Wait()

	mu.Lock()
	total := count1 + count2
	mu.Unlock()

	if total != totalMsgs {
		t.Fatalf("expected %d total messages processed across consumers, got %d (sub1: %d, sub2: %d)", totalMsgs, total, count1, count2)
	}
	// Round robin should distribute to both
	if count1 == 0 || count2 == 0 {
		t.Fatalf("expected both queue group subscribers to receive messages, got count1=%d count2=%d", count1, count2)
	}
}
