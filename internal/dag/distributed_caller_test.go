// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/worker"
	pdk "github.com/gv-factory/gv-factory/plugins-sdk/go"
)

type mockTaskPublisher struct {
	mu        sync.Mutex
	published []struct {
		Subject string
		Data    []byte
	}
	publishErr error
}

func (m *mockTaskPublisher) Publish(subject string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishErr != nil {
		return m.publishErr
	}
	m.published = append(m.published, struct {
		Subject string
		Data    []byte
	}{Subject: subject, Data: data})
	return nil
}

func (m *mockTaskPublisher) getPublished() []struct {
	Subject string
	Data    []byte
} {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]struct {
		Subject string
		Data    []byte
	}, len(m.published))
	copy(copied, m.published)
	return copied
}

func TestDistributedStepCaller_Success(t *testing.T) {
	gw := gateway.NewGateway()
	queue := &mockTaskPublisher{}
	fallback := newMockPluginCaller()

	caller := NewDistributedStepCaller(DistributedCallerConfig{
		TaskQueue:       queue,
		EventSubscriber: gw,
		FallbackCaller:  fallback,
		DefaultTimeout:  5 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	runID := "run-101"
	taskID := "step-build"

	go func() {
		// Wait until task is published
		for {
			if len(queue.getPublished()) > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		_ = gw.Publish(context.Background(), gateway.TopicTaskCompleted, gateway.Event{
			Type:      gateway.TopicTaskCompleted,
			RunID:     runID,
			TaskID:    taskID,
			Timestamp: time.Now().UnixMilli(),
			Payload: map[string]any{
				"output":      "build successful",
				"data":        map[string]any{"artifact": "app.wasm"},
				"duration_ms": 150,
			},
		})
	}()

	res, err := caller.CallPDK(ctx, "wasm://build-plugin", "execute", pdk.Context{
		RunID:  runID,
		TaskID: taskID,
		Configs: map[string]string{
			"task_type": "wasm_plugin",
			"target":    "wasm",
		},
	})

	if err != nil {
		t.Fatalf("expected CallPDK to succeed, got error: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil pdk.Result")
	}
	if !res.Success {
		t.Errorf("expected res.Success to be true, got false")
	}
	if res.Output != "build successful" {
		t.Errorf("expected output 'build successful', got %q", res.Output)
	}
	if res.Data["artifact"] != "app.wasm" {
		t.Errorf("expected data artifact 'app.wasm', got %v", res.Data["artifact"])
	}

	// Verify published TaskMessage
	published := queue.getPublished()
	if len(published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(published))
	}
	if published[0].Subject != gateway.TopicWorkTasks {
		t.Errorf("expected subject %s, got %s", gateway.TopicWorkTasks, published[0].Subject)
	}

	var taskMsg worker.TaskMessage
	if err := json.Unmarshal(published[0].Data, &taskMsg); err != nil {
		t.Fatalf("failed to unmarshal TaskMessage: %v", err)
	}
	if taskMsg.RunID != runID || taskMsg.TaskID != taskID {
		t.Errorf("unexpected TaskMessage runID=%s taskID=%s", taskMsg.RunID, taskMsg.TaskID)
	}
	if taskMsg.TaskType != worker.TaskTypeWasmPlugin {
		t.Errorf("expected task_type %s, got %s", worker.TaskTypeWasmPlugin, taskMsg.TaskType)
	}
}

func TestDistributedStepCaller_TaskFailed(t *testing.T) {
	gw := gateway.NewGateway()
	queue := &mockTaskPublisher{}
	fallback := newMockPluginCaller()

	caller := NewDistributedStepCaller(DistributedCallerConfig{
		TaskQueue:       queue,
		EventSubscriber: gw,
		FallbackCaller:  fallback,
		DefaultTimeout:  5 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	runID := "run-102"
	taskID := "step-test"

	go func() {
		for {
			if len(queue.getPublished()) > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		_ = gw.Publish(context.Background(), gateway.TopicTaskFailed, gateway.Event{
			Type:      gateway.TopicTaskFailed,
			RunID:     runID,
			TaskID:    taskID,
			Timestamp: time.Now().UnixMilli(),
			Payload: map[string]any{
				"error":       "compilation error at line 42",
				"duration_ms": 200,
			},
		})
	}()

	res, err := caller.CallPDK(ctx, "wasm://test-plugin", "execute", pdk.Context{
		RunID:  runID,
		TaskID: taskID,
		Configs: map[string]string{
			"task_type": "agent_step",
		},
	})

	if err == nil {
		t.Fatal("expected error on task failure, got nil")
	}
	if res == nil {
		t.Fatal("expected non-nil pdk.Result even on failure")
	}
	if res.Success {
		t.Errorf("expected res.Success to be false")
	}
	if res.Output != "compilation error at line 42" {
		t.Errorf("expected output to contain error message, got %q", res.Output)
	}
}

func TestDistributedStepCaller_FallbackInProcess(t *testing.T) {
	fallback := newMockPluginCaller()
	fallback.outputs["step-fb1"] = "fallback executed successfully"
	fallback.outputs["step-fb2"] = "fallback executed successfully"

	// Case 1: TaskQueue is nil
	caller1 := NewDistributedStepCaller(DistributedCallerConfig{
		TaskQueue:       nil,
		EventSubscriber: gateway.NewGateway(),
		FallbackCaller:  fallback,
	})
	res1, err1 := caller1.CallPDK(context.Background(), "wasm://fallback-plugin", "execute", pdk.Context{
		RunID:  "run-fb1",
		TaskID: "step-fb1",
	})
	if err1 != nil || res1 == nil || res1.Output != "fallback executed successfully" {
		t.Fatalf("expected fallback execution, got res=%v, err=%v", res1, err1)
	}

	// Case 2: FallbackLocal is explicitly true
	queue := &mockTaskPublisher{}
	caller2 := NewDistributedStepCaller(DistributedCallerConfig{
		TaskQueue:       queue,
		EventSubscriber: gateway.NewGateway(),
		FallbackCaller:  fallback,
		FallbackLocal:   true,
	})
	res2, err2 := caller2.CallPDK(context.Background(), "wasm://fallback-plugin", "execute", pdk.Context{
		RunID:  "run-fb2",
		TaskID: "step-fb2",
	})
	if err2 != nil || res2 == nil || res2.Output != "fallback executed successfully" {
		t.Fatalf("expected fallback execution, got res=%v, err=%v", res2, err2)
	}
	if len(queue.getPublished()) > 0 {
		t.Errorf("expected no task published to queue in FallbackLocal mode")
	}
}

func TestDistributedStepCaller_FallbackOnPublishError(t *testing.T) {
	gw := gateway.NewGateway()
	queue := &mockTaskPublisher{publishErr: errors.New("connection refused")}
	fallback := newMockPluginCaller()
	fallback.outputs["step-1"] = "rescued by fallback"

	caller := NewDistributedStepCaller(DistributedCallerConfig{
		TaskQueue:       queue,
		EventSubscriber: gw,
		FallbackCaller:  fallback,
	})

	res, err := caller.CallPDK(context.Background(), "wasm://resilient-plugin", "execute", pdk.Context{
		RunID:  "run-pub-err",
		TaskID: "step-1",
	})

	if err != nil {
		t.Fatalf("expected publish error to fallback transparently, got error: %v", err)
	}
	if res == nil || res.Output != "rescued by fallback" {
		t.Fatalf("expected output 'rescued by fallback', got %v", res)
	}
}

func TestDistributedStepCaller_ContextTimeout(t *testing.T) {
	gw := gateway.NewGateway()
	queue := &mockTaskPublisher{}
	fallback := newMockPluginCaller()

	caller := NewDistributedStepCaller(DistributedCallerConfig{
		TaskQueue:       queue,
		EventSubscriber: gw,
		FallbackCaller:  fallback,
		DefaultTimeout:  50 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := caller.CallPDK(ctx, "wasm://slow-plugin", "execute", pdk.Context{
		RunID:  "run-timeout",
		TaskID: "step-slow",
	})

	if err == nil {
		t.Fatal("expected context timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestDistributedStepCaller_ChannelClosedAndNoFallback(t *testing.T) {
	// Test no fallback configured
	callerNoFB := NewDistributedStepCaller(DistributedCallerConfig{
		FallbackLocal: true,
	})
	_, err := callerNoFB.CallPDK(context.Background(), "wasm://p", "fn", pdk.Context{})
	if err == nil || !strings.Contains(err.Error(), "no fallback caller configured") {
		t.Errorf("expected no fallback error, got %v", err)
	}
}

type errSubscriber struct{}

func (e *errSubscriber) SubscribeTaskEvents(ctx context.Context, topic string, ch chan gateway.Event) (func(), error) {
	return nil, errors.New("subscription failed")
}

func TestDistributedStepCaller_SubscribeError(t *testing.T) {
	fallback := newMockPluginCaller()
	fallback.outputs["step-sub-err"] = "fallback from sub error"

	caller := NewDistributedStepCaller(DistributedCallerConfig{
		TaskQueue:       &mockTaskPublisher{},
		EventSubscriber: &errSubscriber{},
		FallbackCaller:  fallback,
	})

	res, err := caller.CallPDK(context.Background(), "wasm://p", "fn", pdk.Context{
		TaskID: "step-sub-err",
	})
	if err != nil || res == nil || res.Output != "fallback from sub error" {
		t.Fatalf("expected fallback on subscription failure, got res=%v, err=%v", res, err)
	}

	// Without fallback
	callerNoFB := NewDistributedStepCaller(DistributedCallerConfig{
		TaskQueue:       &mockTaskPublisher{},
		EventSubscriber: &errSubscriber{},
	})
	_, err2 := callerNoFB.CallPDK(context.Background(), "wasm://p", "fn", pdk.Context{
		TaskID: "step-sub-err",
	})
	if err2 == nil || !strings.Contains(err2.Error(), "failed to subscribe") {
		t.Fatalf("expected error without fallback, got %v", err2)
	}
}

func TestDistributedStepCaller_CoverageBoost(t *testing.T) {
	engine := NewEngine()
	g := &Graph{
		ID: "g-test",
		Nodes: map[string]*Node{
			"n1": {ID: "n1", Status: StatusPending},
		},
	}
	engine.RegisterGraph(g)
	if err := engine.Validate("g-test"); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if err := engine.Validate("non-existent"); err == nil {
		t.Fatalf("expected error for non-existent graph")
	}

	// Priority queue
	if ParsePriority("HIGH") != PriorityHigh {
		t.Errorf("expected PriorityHigh")
	}
	if ParsePriority("LOW") != PriorityLow {
		t.Errorf("expected PriorityLow")
	}
	if ParsePriority("UNKNOWN") != PriorityNormal {
		t.Errorf("expected PriorityNormal")
	}
	pq := NewPriorityQueue(PriorityQueueConfig{AgingBoost: 30})
	if pq.Config().AgingBoost != 30 {
		t.Errorf("expected 30 AgingBoost")
	}

	// Orchestrator ActiveCount and QueuedCount and ListRuns
	orch := NewMultiFactoryOrchestrator(
		OrchestratorConfig{},
		nil,
		engine,
		nil,
		gateway.NewGateway(),
		DispatcherConfig{},
	)
	defer orch.Stop()
	if orch.ActiveCount() != 0 {
		t.Errorf("expected 0 active count")
	}
	if orch.QueuedCount() != 0 {
		t.Errorf("expected 0 queued count")
	}
	if len(orch.ListRuns()) != 0 {
		t.Errorf("expected 0 runs")
	}
}

func TestDistributedStepCaller_CorrelationIsolation(t *testing.T) {
	gw := gateway.NewGateway()
	queue := &mockTaskPublisher{}

	caller := NewDistributedStepCaller(DistributedCallerConfig{
		TaskQueue:       queue,
		EventSubscriber: gw,
		DefaultTimeout:  3 * time.Second,
	})

	var wg sync.WaitGroup
	results := make([]*pdk.Result, 3)
	errorsList := make([]error, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			runID := "run-iso"
			taskID := "task-" + string(rune('A'+idx))
			res, err := caller.CallPDK(context.Background(), "wasm://plugin", "execute", pdk.Context{
				RunID:  runID,
				TaskID: taskID,
			})
			results[idx] = res
			errorsList[idx] = err
		}(i)
	}

	// Wait until all 3 tasks are published
	for {
		if len(queue.getPublished()) >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Publish completions in reverse order
	for i := 2; i >= 0; i-- {
		taskID := "task-" + string(rune('A'+i))
		_ = gw.Publish(context.Background(), gateway.TopicTaskCompleted, gateway.Event{
			Type:      gateway.TopicTaskCompleted,
			RunID:     "run-iso",
			TaskID:    taskID,
			Timestamp: time.Now().UnixMilli(),
			Payload: map[string]any{
				"output": "completed-" + taskID,
			},
		})
	}

	wg.Wait()

	for i := 0; i < 3; i++ {
		taskID := "task-" + string(rune('A'+i))
		if errorsList[i] != nil {
			t.Errorf("task %d failed: %v", i, errorsList[i])
		}
		if results[i] == nil || results[i].Output != "completed-"+taskID {
			t.Errorf("task %d expected output completed-%s, got %v", i, taskID, results[i])
		}
	}
}
