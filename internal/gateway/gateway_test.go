// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/pkg/schema"
)

func TestGateway_InMemoryFallback(t *testing.T) {
	gw := NewGatewayWithConfig(Config{
		NATSURL:        "nats://127.0.0.1:49999", // non-existent NATS port
		ConnectTimeout: 50 * time.Millisecond,
	})
	defer gw.Close()

	if !gw.IsInMemory() {
		t.Fatalf("expected gateway to fall back to in-memory mode")
	}
}

func TestGateway_PublishAndSubscribe(t *testing.T) {
	gw := NewGatewayWithConfig(Config{DisableNATS: true})
	defer gw.Close()

	ctx := context.Background()
	ch := make(chan Event, 5)

	unsub, err := gw.SubscribeTaskEvents(ctx, TopicTaskStarted, ch)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	defer unsub()

	ev := Event{
		Type:   TopicTaskStarted,
		RunID:  "run-101",
		TaskID: "task-spec",
		Payload: map[string]any{
			"message": "started",
		},
	}

	if err := gw.PublishTaskEvent(ctx, ev); err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	select {
	case received := <-ch:
		if received.RunID != "run-101" || received.TaskID != "task-spec" {
			t.Errorf("unexpected event content: %+v", received)
		}
		if received.Timestamp == 0 {
			t.Errorf("expected automatic timestamp assignment")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for event")
	}
}

func TestGateway_WildcardMatching(t *testing.T) {
	gw := NewGatewayWithConfig(Config{DisableNATS: true})
	defer gw.Close()

	ctx := context.Background()
	taskAllCh := make(chan Event, 10)
	globalCh := make(chan Event, 10)

	unsub1, _ := gw.SubscribeTaskEvents(ctx, TopicAllTasks, taskAllCh)
	defer unsub1()
	unsub2, _ := gw.SubscribeTaskEvents(ctx, TopicAllEvents, globalCh)
	defer unsub2()

	events := []Event{
		{Type: TopicTaskPending, RunID: "r1", TaskID: "t1"},
		{Type: TopicTaskCompleted, RunID: "r1", TaskID: "t1"},
		{Type: TopicRunCompleted, RunID: "r1"},
	}

	for _, ev := range events {
		if err := gw.PublishTaskEvent(ctx, ev); err != nil {
			t.Fatalf("publish failed: %v", err)
		}
	}

	// taskAllCh (ov.task.*) should receive TopicTaskPending and TopicTaskCompleted (2 events)
	// globalCh (ov.>) should receive all 3 events
	receivedTasks := 0
	for i := 0; i < 2; i++ {
		select {
		case <-taskAllCh:
			receivedTasks++
		case <-time.After(200 * time.Millisecond):
		}
	}
	if receivedTasks != 2 {
		t.Errorf("expected 2 task events, got %d", receivedTasks)
	}

	receivedGlobal := 0
	for i := 0; i < 3; i++ {
		select {
		case <-globalCh:
			receivedGlobal++
		case <-time.After(200 * time.Millisecond):
		}
	}
	if receivedGlobal != 3 {
		t.Errorf("expected 3 global events, got %d", receivedGlobal)
	}
}

func TestGateway_TopicTaskLogAndSpanConstants(t *testing.T) {
	if TopicTaskLog != "ov.task.log" {
		t.Errorf("expected TopicTaskLog to be 'ov.task.log', got '%s'", TopicTaskLog)
	}
	if TopicTaskSpan != "ov.task.span" {
		t.Errorf("expected TopicTaskSpan to be 'ov.task.span', got '%s'", TopicTaskSpan)
	}

	if !matchesTopic(TopicAllTasks, TopicTaskLog) {
		t.Errorf("expected matchesTopic(%s, %s) to be true", TopicAllTasks, TopicTaskLog)
	}
	if !matchesTopic(TopicAllTasks, TopicTaskSpan) {
		t.Errorf("expected matchesTopic(%s, %s) to be true", TopicAllTasks, TopicTaskSpan)
	}
	if !matchesTopic(TopicAllEvents, TopicTaskLog) {
		t.Errorf("expected matchesTopic(%s, %s) to be true", TopicAllEvents, TopicTaskLog)
	}
	if !matchesTopic(TopicAllEvents, TopicTaskSpan) {
		t.Errorf("expected matchesTopic(%s, %s) to be true", TopicAllEvents, TopicTaskSpan)
	}
}

func TestGateway_TaskLogAndSpanPayloadSerialization(t *testing.T) {
	logPayload := TaskLogPayload{
		Stream:    "stdout",
		Message:   "line 1\nline 2",
		Timestamp: 1234567890,
	}
	data, err := json.Marshal(logPayload)
	if err != nil {
		t.Fatalf("failed to marshal TaskLogPayload: %v", err)
	}
	var unmarshaledLog TaskLogPayload
	if err := json.Unmarshal(data, &unmarshaledLog); err != nil {
		t.Fatalf("failed to unmarshal TaskLogPayload: %v", err)
	}
	if unmarshaledLog.Stream != "stdout" || unmarshaledLog.Message != "line 1\nline 2" || unmarshaledLog.Timestamp != 1234567890 {
		t.Errorf("unexpected unmarshaled log payload: %+v", unmarshaledLog)
	}

	spanPayload := TaskSpanPayload{
		DurationMS: 1500,
		TraceID:    "trace-abc",
		SpanID:     "span-xyz",
		NodeType:   "step",
		Status:     "COMPLETED",
		Tokens:     42,
	}
	data, err = json.Marshal(spanPayload)
	if err != nil {
		t.Fatalf("failed to marshal TaskSpanPayload: %v", err)
	}
	var unmarshaledSpan TaskSpanPayload
	if err := json.Unmarshal(data, &unmarshaledSpan); err != nil {
		t.Fatalf("failed to unmarshal TaskSpanPayload: %v", err)
	}
	if unmarshaledSpan.DurationMS != 1500 || unmarshaledSpan.TraceID != "trace-abc" || unmarshaledSpan.Tokens != 42 {
		t.Errorf("unexpected unmarshaled span payload: %+v", unmarshaledSpan)
	}
}

func TestHub_RunScopedBroadcastIsolation(t *testing.T) {
	hub := NewHub()
	defer hub.Close()

	chGlobal := make(chan []byte, 5)
	chRun1 := make(chan []byte, 5)
	chRun2 := make(chan []byte, 5)

	hub.Register(chGlobal)
	defer hub.Unregister(chGlobal)

	hub.RegisterRun("run-1", chRun1)
	defer hub.UnregisterRun("run-1", chRun1)

	hub.RegisterRun("run-2", chRun2)
	defer hub.UnregisterRun("run-2", chRun2)

	msgRun1 := []byte(`{"run_id":"run-1","message":"hello run 1"}`)
	hub.BroadcastRun("run-1", msgRun1)

	// chGlobal should receive msgRun1
	select {
	case received := <-chGlobal:
		if string(received) != string(msgRun1) {
			t.Errorf("expected global to receive %s, got %s", msgRun1, received)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timeout waiting for msgRun1 on global channel")
	}

	// chRun1 should receive msgRun1
	select {
	case received := <-chRun1:
		if string(received) != string(msgRun1) {
			t.Errorf("expected run-1 to receive %s, got %s", msgRun1, received)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timeout waiting for msgRun1 on run-1 channel")
	}

	// chRun2 should NOT receive msgRun1
	select {
	case received := <-chRun2:
		t.Errorf("run-2 received unexpected message: %s", received)
	case <-time.After(50 * time.Millisecond):
		// Expected
	}
}

func TestHub_ClientUnregisterCleanup(t *testing.T) {
	hub := NewHub()
	defer hub.Close()

	ch1 := make(chan []byte, 1)
	ch2 := make(chan []byte, 1)

	hub.RegisterRun("run-x", ch1)
	hub.RegisterRun("run-x", ch2)

	if count := hub.RunClientCount("run-x"); count != 2 {
		t.Errorf("expected 2 clients for run-x, got %d", count)
	}

	hub.UnregisterRun("run-x", ch1)
	if count := hub.RunClientCount("run-x"); count != 1 {
		t.Errorf("expected 1 client for run-x, got %d", count)
	}

	hub.UnregisterRun("run-x", ch2)
	if count := hub.RunClientCount("run-x"); count != 0 {
		t.Errorf("expected 0 clients for run-x, got %d", count)
	}
}

type dummyFlusherResponseWriter struct {
	header     http.Header
	buf        []byte
	flushed    bool
	statusCode int
}

func (d *dummyFlusherResponseWriter) Header() http.Header {
	if d.header == nil {
		d.header = make(http.Header)
	}
	return d.header
}

func (d *dummyFlusherResponseWriter) Write(b []byte) (int, error) {
	d.buf = append(d.buf, b...)
	return len(b), nil
}

func (d *dummyFlusherResponseWriter) WriteHeader(statusCode int) {
	d.statusCode = statusCode
}

func (d *dummyFlusherResponseWriter) Flush() {
	d.flushed = true
}

func TestHub_ServeHTTPRun_SSEHeadersAndStreaming(t *testing.T) {
	hub := NewHub()
	defer hub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/runs/run-99/events", nil)
	w := &dummyFlusherResponseWriter{}

	done := make(chan struct{})
	go func() {
		hub.ServeHTTPRun(w, req, "run-99")
		close(done)
	}()

	// Wait briefly for client registration
	time.Sleep(20 * time.Millisecond)
	if count := hub.RunClientCount("run-99"); count != 1 {
		t.Fatalf("expected 1 registered client, got %d", count)
	}

	hub.BroadcastRun("run-99", []byte(`{"type":"ov.task.log","run_id":"run-99"}`))

	time.Sleep(20 * time.Millisecond)

	// Cancel context to simulate client disconnect
	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("ServeHTTPRun did not exit after context cancellation")
	}

	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", w.Header().Get("Content-Type"))
	}
	if !w.flushed {
		t.Errorf("expected Flush() to be called")
	}
	if string(w.buf) == "" {
		t.Errorf("expected buffer to contain SSE data, got empty")
	}
	if hub.RunClientCount("run-99") != 0 {
		t.Errorf("expected 0 clients after disconnect, got %d", hub.RunClientCount("run-99"))
	}
}

func TestGateway_WebSocketHubFanOut(t *testing.T) {
	gw := NewGatewayWithConfig(Config{DisableNATS: true})
	defer gw.Close()

	hub := gw.Hub()
	if hub == nil {
		t.Fatal("expected non-nil hub")
	}

	clientCh1 := make(chan []byte, 5)
	clientCh2 := make(chan []byte, 5)

	hub.Register(clientCh1)
	hub.Register(clientCh2)
	defer func() {
		hub.Unregister(clientCh1)
		hub.Unregister(clientCh2)
	}()

	if hub.ClientCount() != 2 {
		t.Errorf("expected 2 clients, got %d", hub.ClientCount())
	}

	ev := Event{
		Type:   TopicTaskCompleted,
		RunID:  "run-ws",
		TaskID: "task-build",
	}

	if err := gw.PublishTaskEvent(context.Background(), ev); err != nil {
		t.Fatalf("PublishTaskEvent failed: %v", err)
	}

	for i, ch := range []chan []byte{clientCh1, clientCh2} {
		select {
		case data := <-ch:
			var parsed Event
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("client %d received invalid json: %v", i, err)
			}
			if parsed.RunID != "run-ws" {
				t.Errorf("client %d got wrong run ID: %s", i, parsed.RunID)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("client %d timed out", i)
		}
	}
}

func TestGateway_UnsubscribeAndCleanup(t *testing.T) {
	gw := NewGatewayWithConfig(Config{DisableNATS: true})
	defer gw.Close()

	ch := make(chan Event, 5)
	unsub, err := gw.SubscribeTaskEvents(context.Background(), TopicTaskStarted, ch)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	unsub()

	// Publishing should no longer deliver to ch
	_ = gw.PublishTaskEvent(context.Background(), Event{Type: TopicTaskStarted, RunID: "after-unsub"})

	select {
	case ev := <-ch:
		t.Fatalf("expected no event after unsubscribe, got %+v", ev)
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}

func TestGateway_ConcurrentPublishSubscribeRace(t *testing.T) {
	gw := NewGatewayWithConfig(Config{DisableNATS: true})
	defer gw.Close()

	const iterations = 50
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = gw.PublishTaskEvent(context.Background(), Event{
				Type:   TopicTaskStarted,
				RunID:  "concurrent-run",
				TaskID: "task",
			})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			ch := make(chan Event, 10)
			unsub, err := gw.SubscribeTaskEvents(context.Background(), TopicTaskStarted, ch)
			if err == nil {
				unsub()
			}
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			hubClient := make(chan []byte, 10)
			gw.Hub().Register(hubClient)
			gw.Hub().Broadcast([]byte(`{"ping":"pong"}`))
			gw.Hub().Unregister(hubClient)
		}
	}()

	wg.Wait()
}

func TestGateway_PublishTrigger(t *testing.T) {
	gw := NewGatewayWithConfig(Config{DisableNATS: true})
	defer gw.Close()

	chGitHub := make(chan Event, 5)
	chAllTriggers := make(chan Event, 5)

	_, err := gw.SubscribeTaskEvents(context.Background(), TopicTriggerPrefix+"github", chGitHub)
	if err != nil {
		t.Fatalf("failed to subscribe to github triggers: %v", err)
	}

	_, err = gw.SubscribeTaskEvents(context.Background(), TopicAllTriggers, chAllTriggers)
	if err != nil {
		t.Fatalf("failed to subscribe to all triggers: %v", err)
	}

	ev := schema.TriggerEvent{
		ID:          "trig-gh-1",
		Source:      schema.SourceGitHub,
		EventType:   schema.EventIssueOpened,
		Repository:  "gek0v/gv-factory",
		IssueNumber: 33,
		Sender:      "gek0v",
		Timestamp:   time.Now().UTC(),
	}

	if err := gw.PublishTrigger(context.Background(), ev); err != nil {
		t.Fatalf("PublishTrigger failed: %v", err)
	}

	select {
	case received := <-chGitHub:
		if received.Type != "factory.triggers.github" {
			t.Errorf("expected topic factory.triggers.github, got %s", received.Type)
		}
		if received.RunID != "trig-gh-1" {
			t.Errorf("expected RunID trig-gh-1, got %s", received.RunID)
		}
		if received.Payload["event_type"] != schema.EventIssueOpened {
			t.Errorf("expected payload event_type %s, got %v", schema.EventIssueOpened, received.Payload["event_type"])
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for github trigger event")
	}

	select {
	case received := <-chAllTriggers:
		if received.Type != "factory.triggers.github" {
			t.Errorf("expected wildcard match topic factory.triggers.github, got %s", received.Type)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for wildcard trigger event")
	}
}
