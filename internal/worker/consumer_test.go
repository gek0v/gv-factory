// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
)

type mockGatewayEventCollector struct {
	mu     sync.Mutex
	events []gateway.Event
}

func (m *mockGatewayEventCollector) PublishTaskEvent(ctx context.Context, ev gateway.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
	return nil
}

func (m *mockGatewayEventCollector) getEvents() []gateway.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]gateway.Event, len(m.events))
	copy(copied, m.events)
	return copied
}

func TestConsumer_PoisonPill_Term(t *testing.T) {
	q := NewInMemoryQueue()
	defer q.Close()

	router := NewTaskRouter()
	collector := &mockGatewayEventCollector{}

	consumer := NewConsumer(ConsumerConfig{
		WorkerID:    "test-worker",
		Concurrency: 2,
		Queue:       q,
		Router:      router,
		Gateway:     collector,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("failed to start consumer: %v", err)
	}

	// 1. Send completely broken JSON
	rawMsg := NewInMemoryQueueMessage(TopicWorkTasks, []byte("{invalid-json"))
	consumer.handleQueueMessage(ctx, rawMsg)

	if !rawMsg.IsTermed() {
		t.Fatalf("expected malformed JSON message to be Termed")
	}
	if rawMsg.IsAcked() {
		t.Fatalf("malformed message must not be Acked")
	}

	// 2. Send missing required fields
	invalidTask, _ := json.Marshal(TaskMessage{
		TaskID: "",
		RunID:  "run-1",
	})
	rawMsg2 := NewInMemoryQueueMessage(TopicWorkTasks, invalidTask)
	consumer.handleQueueMessage(ctx, rawMsg2)

	if !rawMsg2.IsTermed() {
		t.Fatalf("expected invalid TaskMessage to be Termed")
	}
}

func TestConsumer_Success_Ack(t *testing.T) {
	q := NewInMemoryQueue()
	defer q.Close()

	router := NewTaskRouter()
	collector := &mockGatewayEventCollector{}

	router.RegisterHandler(TaskTypeWasmPlugin, TaskHandlerFunc(func(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
		return &TaskResult{
			Success: true,
			Output:  "processed successfully",
			Data:    map[string]any{"result": 42},
		}, nil
	}))

	consumer := NewConsumer(ConsumerConfig{
		WorkerID:    "test-worker",
		Concurrency: 2,
		Queue:       q,
		Router:      router,
		Gateway:     collector,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("failed to start consumer: %v", err)
	}

	validTask, _ := json.Marshal(TaskMessage{
		TaskID:   "task-123",
		RunID:    "run-456",
		StepID:   "step-789",
		TaskType: TaskTypeWasmPlugin,
	})

	rawMsg := NewInMemoryQueueMessage(TopicWorkTasks, validTask)
	consumer.handleQueueMessage(ctx, rawMsg)

	if !rawMsg.IsAcked() {
		t.Fatalf("expected successful task message to be Acked")
	}
	if rawMsg.IsTermed() {
		t.Fatalf("successful task message should not be Termed")
	}

	events := collector.getEvents()
	var started, completed bool
	for _, ev := range events {
		if ev.Type == gateway.TopicTaskStarted && ev.TaskID == "task-123" {
			started = true
		}
		if ev.Type == gateway.TopicTaskCompleted && ev.TaskID == "task-123" {
			completed = true
		}
	}

	if !started || !completed {
		t.Fatalf("expected started and completed events, got started=%v completed=%v", started, completed)
	}
}

func TestConsumer_RetryableError_Nak(t *testing.T) {
	q := NewInMemoryQueue()
	defer q.Close()

	router := NewTaskRouter()
	collector := &mockGatewayEventCollector{}

	router.RegisterHandler(TaskTypeAgentStep, TaskHandlerFunc(func(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
		return nil, NewRetryableError(errors.New("connection reset by peer"), 250*time.Millisecond)
	}))

	consumer := NewConsumer(ConsumerConfig{
		WorkerID:    "test-worker",
		Concurrency: 2,
		Queue:       q,
		Router:      router,
		Gateway:     collector,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("failed to start consumer: %v", err)
	}

	taskBytes, _ := json.Marshal(TaskMessage{
		TaskID:   "task-retry",
		RunID:    "run-1",
		StepID:   "step-1",
		TaskType: TaskTypeAgentStep,
	})

	rawMsg := NewInMemoryQueueMessage(TopicWorkTasks, taskBytes)
	consumer.handleQueueMessage(ctx, rawMsg)

	if rawMsg.IsAcked() {
		t.Fatalf("retryable failure must not be Acked")
	}
	if rawMsg.IsTermed() {
		t.Fatalf("retryable failure must not be Termed")
	}
	if rawMsg.NakCount() != 1 {
		t.Fatalf("expected 1 Nak, got %d", rawMsg.NakCount())
	}
	if rawMsg.LastDelay() != 250*time.Millisecond {
		t.Fatalf("expected delay 250ms, got %v", rawMsg.LastDelay())
	}
}

func TestConsumer_ConcurrencyLimiter(t *testing.T) {
	q := NewInMemoryQueue()
	defer q.Close()

	router := NewTaskRouter()
	collector := &mockGatewayEventCollector{}

	var currentConcurrency int32
	var maxObservedConcurrency int32

	router.RegisterHandler(TaskTypeGitSandbox, TaskHandlerFunc(func(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
		cur := atomic.AddInt32(&currentConcurrency, 1)
		defer atomic.AddInt32(&currentConcurrency, -1)

		for {
			oldMax := atomic.LoadInt32(&maxObservedConcurrency)
			if cur <= oldMax || atomic.CompareAndSwapInt32(&maxObservedConcurrency, oldMax, cur) {
				break
			}
		}

		time.Sleep(50 * time.Millisecond)
		return &TaskResult{Success: true}, nil
	}))

	limit := 3
	consumer := NewConsumer(ConsumerConfig{
		WorkerID:    "test-worker",
		Concurrency: limit,
		Queue:       q,
		Router:      router,
		Gateway:     collector,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("failed to start consumer: %v", err)
	}
	defer consumer.Stop()

	// Enqueue 10 tasks
	for i := 0; i < 10; i++ {
		data, _ := json.Marshal(TaskMessage{
			TaskID:   "task-concurrent",
			RunID:    "run-1",
			StepID:   "step-1",
			TaskType: TaskTypeGitSandbox,
		})
		_ = q.Publish(TopicWorkTasks, data)
	}

	// Give enough time to execute
	time.Sleep(300 * time.Millisecond)

	observed := int(atomic.LoadInt32(&maxObservedConcurrency))
	if observed > limit {
		t.Fatalf("observed concurrency %d exceeded limit %d", observed, limit)
	}
	if observed == 0 {
		t.Fatalf("expected concurrency > 0, got 0")
	}
}

func TestConsumer_GracefulDrain(t *testing.T) {
	q := NewInMemoryQueue()
	defer q.Close()

	router := NewTaskRouter()
	collector := &mockGatewayEventCollector{}

	taskStarted := make(chan struct{})
	taskDone := make(chan struct{})

	router.RegisterHandler(TaskTypeWasmPlugin, TaskHandlerFunc(func(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
		close(taskStarted)
		time.Sleep(100 * time.Millisecond)
		close(taskDone)
		return &TaskResult{Success: true}, nil
	}))

	consumer := NewConsumer(ConsumerConfig{
		WorkerID:     "test-worker",
		Concurrency:  2,
		DrainTimeout: 1 * time.Second,
		Queue:        q,
		Router:       router,
		Gateway:      collector,
	})

	ctx, cancel := context.WithCancel(context.Background())
	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	data, _ := json.Marshal(TaskMessage{
		TaskID:   "task-drain",
		RunID:    "run-1",
		StepID:   "step-1",
		TaskType: TaskTypeWasmPlugin,
	})
	_ = q.Publish(TopicWorkTasks, data)

	<-taskStarted
	// Trigger Stop while task is in-flight
	cancel()
	consumer.Stop()

	<-taskDone
	if consumer.ActiveTasks() != 0 {
		t.Fatalf("expected 0 active tasks after drain, got %d", consumer.ActiveTasks())
	}
}
