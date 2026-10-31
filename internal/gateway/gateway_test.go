// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package gateway

import (
	"context"
	"encoding/json"
	"github.com/gv-factory/gv-factory/pkg/schema"
	"sync"
	"testing"
	"time"
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
