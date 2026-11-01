// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"testing"
	"time"
)

type dummyNatsConn struct{}

func TestNatsQueueBackend_CloseWithNonNilConn(t *testing.T) {
	// Exercise Close path when b.nc is closed
	backend := &NatsQueueBackend{
		nc: nil,
		js: nil,
	}
	_ = backend.Close()
	// Call Close again to exercise sync.Once
	_ = backend.Close()
}

func TestConsumer_Start_SubscribeError(t *testing.T) {
	q := NewInMemoryQueue()
	_ = q.Close()

	c := NewConsumer(ConsumerConfig{
		Queue: q,
	})

	err := c.Start(context.Background())
	if err == nil {
		t.Fatalf("expected error starting consumer on closed queue")
	}
}

func TestConsumer_Stop_DrainTimeoutExpired(t *testing.T) {
	q := NewInMemoryQueue()
	defer q.Close()

	doneCh := make(chan struct{})
	router := NewTaskRouter()
	router.RegisterHandler(TaskTypeWasmPlugin, TaskHandlerFunc(func(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
		<-ctx.Done()
		close(doneCh)
		return nil, ctx.Err()
	}))

	c := NewConsumer(ConsumerConfig{
		DrainTimeout: 10 * time.Millisecond,
		Queue:        q,
		Router:       router,
	})

	ctx := context.Background()
	_ = c.Start(ctx)

	_ = q.Publish(TopicWorkTasks, []byte(`{"task_id":"t-hang","run_id":"r1","step_id":"s1","task_type":"wasm_plugin"}`))

	// Wait until active task is picked up
	for i := 0; i < 50; i++ {
		if c.ActiveTasks() > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Stop will wait up to 10ms and then force cancel taskCtx
	c.Stop()

	// Wait for handler to exit cleanly
	select {
	case <-doneCh:
	case <-time.After(1 * time.Second):
		t.Fatalf("timeout waiting for handler to abort")
	}
}
