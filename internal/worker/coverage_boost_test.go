// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/nats-io/nats.go"
)

type errHeartbeatPublisher struct{}

func (e *errHeartbeatPublisher) PublishHeartbeat(hb HeartbeatMessage) error {
	return errors.New("heartbeat network error")
}

func TestHeartbeatEmitter_ErrorHandlingAndDefaults(t *testing.T) {
	pub := &errHeartbeatPublisher{}
	emitter := NewHeartbeatEmitter(HeartbeatConfig{
		WorkerID:  "worker-err",
		Interval:  10 * time.Millisecond,
		Publisher: pub,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	emitter.Start(ctx)

	// Nil publisher check
	nilPubEmitter := NewHeartbeatEmitter(HeartbeatConfig{})
	nilPubEmitter.emit()
}

func TestQueueBackend_Methods(t *testing.T) {
	backend := &NatsQueueBackend{}
	if err := backend.Publish("test", []byte("data")); err == nil {
		t.Fatalf("expected error publishing to nil js")
	}
	if _, err := backend.Subscribe(context.Background(), "test", "grp"); err == nil {
		t.Fatalf("expected error subscribing to nil js")
	}
	if err := backend.Close(); err != nil {
		t.Fatalf("unexpected error closing empty backend: %v", err)
	}

	sub := &natsSubscription{
		sub: nil,
		ch:  make(chan QueueMessage),
	}
	_ = sub.Messages()
	if err := sub.Unsubscribe(); err != nil {
		t.Fatalf("unexpected err unsubscribing: %v", err)
	}
}

func TestConsumer_ClosedQueue(t *testing.T) {
	q := NewInMemoryQueue()
	_ = q.Close()

	if err := q.Publish(TopicWorkTasks, []byte("x")); err == nil {
		t.Fatalf("expected error publishing to closed queue")
	}
	if _, err := q.Subscribe(context.Background(), TopicWorkTasks, ConsumerGroup); err == nil {
		t.Fatalf("expected error subscribing to closed queue")
	}
}

func TestConsumer_EventEmissionWithoutGateway(t *testing.T) {
	c := NewConsumer(ConsumerConfig{
		Gateway: nil,
	})
	task := TaskMessage{
		TaskID:   "task-1",
		RunID:    "run-1",
		StepID:   "step-1",
		TaskType: TaskTypeWasmPlugin,
	}
	c.emitEvent(context.Background(), gateway.TopicTaskStarted, task, nil)
	c.emitSpan(context.Background(), task, 10, true)
	c.emitSpan(context.Background(), task, 10, false)
}

func TestRetryableError_NilErr(t *testing.T) {
	re := &RetryableError{Err: nil}
	if re.Error() != "retryable error" {
		t.Fatalf("expected 'retryable error', got %q", re.Error())
	}
}

func TestNatsQueueBackend_ConnectError(t *testing.T) {
	// Connect to non-existent NATS server should return in-memory queue fallback
	backend, isNATS, err := NewQueueBackend(QueueBackendConfig{
		NATSURL:        "nats://127.0.0.1:65530",
		ConnectTimeout: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer backend.Close()
	if isNATS {
		t.Fatalf("expected isNATS to be false on connection failure")
	}
}

func TestNatsSubscription_UnsubscribeWithNats(t *testing.T) {
	// Exercise Unsubscribe when underlying nats.Subscription is non-nil
	conn := &nats.Subscription{}
	sub := &natsSubscription{
		sub: conn,
		ch:  make(chan QueueMessage),
	}
	_ = sub.Unsubscribe()
}
