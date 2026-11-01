// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestConsumer_AckPolicy_NonRetryableError(t *testing.T) {
	q := NewInMemoryQueue()
	defer q.Close()

	router := NewTaskRouter()
	collector := &mockGatewayEventCollector{}

	router.RegisterHandler(TaskTypeWasmPlugin, TaskHandlerFunc(func(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
		return nil, errors.New("syntax error in wasm bytecode")
	}))

	consumer := NewConsumer(ConsumerConfig{
		WorkerID:    "test-worker",
		Concurrency: 1,
		Queue:       q,
		Router:      router,
		Gateway:     collector,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("failed to start consumer: %v", err)
	}

	task := TaskMessage{
		TaskID:   "task-fatal-err",
		RunID:    "run-1",
		StepID:   "step-1",
		TaskType: TaskTypeWasmPlugin,
	}
	msg := NewInMemoryQueueMessage(TopicWorkTasks, mustMarshal(task))
	consumer.handleQueueMessage(ctx, msg)

	if !msg.IsTermed() {
		t.Fatalf("expected fatal error to trigger Term()")
	}
	if msg.IsAcked() {
		t.Fatalf("expected fatal error to NOT trigger Ack()")
	}
}

func TestConsumer_AckPolicy_ResultUnsuccessful(t *testing.T) {
	q := NewInMemoryQueue()
	defer q.Close()

	router := NewTaskRouter()
	collector := &mockGatewayEventCollector{}

	// Case 1: Result unsuccessful and non-retryable -> Term
	router.RegisterHandler(TaskTypeAgentStep, TaskHandlerFunc(func(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
		return &TaskResult{
			Success:   false,
			Retryable: false,
			Error:     "unsupported step action",
		}, nil
	}))

	consumer := NewConsumer(ConsumerConfig{
		WorkerID:    "test-worker",
		Concurrency: 1,
		Queue:       q,
		Router:      router,
		Gateway:     collector,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("failed to start consumer: %v", err)
	}

	task := TaskMessage{
		TaskID:   "task-res-fail",
		RunID:    "run-1",
		StepID:   "step-1",
		TaskType: TaskTypeAgentStep,
	}
	msg := NewInMemoryQueueMessage(TopicWorkTasks, mustMarshal(task))
	consumer.handleQueueMessage(ctx, msg)

	if !msg.IsTermed() {
		t.Fatalf("expected non-retryable failure result to trigger Term()")
	}

	// Case 2: Result unsuccessful and retryable -> NakWithDelay
	router.RegisterHandler(TaskTypeGitSandbox, TaskHandlerFunc(func(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
		return &TaskResult{
			Success:   false,
			Retryable: true,
			Error:     "git lock contended",
		}, nil
	}))

	task2 := TaskMessage{
		TaskID:   "task-res-retry",
		RunID:    "run-1",
		StepID:   "step-1",
		TaskType: TaskTypeGitSandbox,
	}
	msg2 := NewInMemoryQueueMessage(TopicWorkTasks, mustMarshal(task2))
	consumer.handleQueueMessage(ctx, msg2)

	if !msg2.IsTermed() && msg2.NakCount() == 1 {
		// Pass
	} else {
		t.Fatalf("expected retryable result to trigger Nak, got nakCount=%d termed=%v", msg2.NakCount(), msg2.IsTermed())
	}
}

func TestConsumer_AckPolicy_ContextCanceled(t *testing.T) {
	q := NewInMemoryQueue()
	defer q.Close()

	router := NewTaskRouter()
	collector := &mockGatewayEventCollector{}

	router.RegisterHandler(TaskTypeWasmPlugin, TaskHandlerFunc(func(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
		return nil, context.Canceled
	}))

	consumer := NewConsumer(ConsumerConfig{
		WorkerID: "test-worker",
		Queue:    q,
		Router:   router,
		Gateway:  collector,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	task := TaskMessage{
		TaskID:   "task-cancel",
		RunID:    "run-1",
		StepID:   "step-1",
		TaskType: TaskTypeWasmPlugin,
	}
	msg := NewInMemoryQueueMessage(TopicWorkTasks, mustMarshal(task))
	consumer.handleQueueMessage(ctx, msg)

	if msg.NakCount() != 1 {
		t.Fatalf("expected context.Canceled to trigger Nak, got %d", msg.NakCount())
	}
}

func TestConsumer_NilQueueOrRouter(t *testing.T) {
	c := NewConsumer(ConsumerConfig{})
	err := c.Start(context.Background())
	if err == nil {
		t.Fatalf("expected error starting consumer with nil queue")
	}

	res, err := c.executeTask(context.Background(), &TaskMessage{TaskType: TaskTypeWasmPlugin})
	if err == nil || res != nil {
		t.Fatalf("expected error executing task with nil router")
	}
}

func TestConsumer_HeartbeatWithNilGateway(t *testing.T) {
	c := NewConsumer(ConsumerConfig{})
	err := c.PublishHeartbeat(HeartbeatMessage{})
	if err != nil {
		t.Fatalf("expected nil error when publishing heartbeat with nil gateway")
	}
}

func TestConsumer_CustomTimeoutMS(t *testing.T) {
	q := NewInMemoryQueue()
	defer q.Close()

	router := NewTaskRouter()
	collector := &mockGatewayEventCollector{}

	timeoutOccurred := false
	router.RegisterHandler(TaskTypeAgentStep, TaskHandlerFunc(func(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
		select {
		case <-time.After(200 * time.Millisecond):
			return &TaskResult{Success: true}, nil
		case <-ctx.Done():
			timeoutOccurred = true
			return nil, ctx.Err()
		}
	}))

	consumer := NewConsumer(ConsumerConfig{
		WorkerID: "test-worker",
		Queue:    q,
		Router:   router,
		Gateway:  collector,
	})

	ctx := context.Background()
	task := TaskMessage{
		TaskID:    "task-timeout-custom",
		RunID:     "run-1",
		StepID:    "step-1",
		TaskType:  TaskTypeAgentStep,
		TimeoutMS: 50, // 50ms should trigger timeout
	}
	msg := NewInMemoryQueueMessage(TopicWorkTasks, mustMarshal(task))
	consumer.handleQueueMessage(ctx, msg)

	if !timeoutOccurred {
		t.Fatalf("expected custom TimeoutMS to trigger context cancellation")
	}
}

func mustMarshal(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}
