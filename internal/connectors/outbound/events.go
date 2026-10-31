// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"context"
	"fmt"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

// DispatchGatewayEvent transforms an internal gateway.Event into an outbound payload and dispatches it.
func (d *Dispatcher) DispatchGatewayEvent(ctx context.Context, ev gateway.Event) ([]string, error) {
	ts := ev.Timestamp
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}
	payloadData := make(map[string]any, len(ev.Payload)+2)
	for k, v := range ev.Payload {
		payloadData[k] = v
	}
	if ev.TaskID != "" {
		payloadData["task_id"] = ev.TaskID
	}
	if ev.RunID != "" {
		payloadData["run_id"] = ev.RunID
	}

	return d.Dispatch(ctx, Payload{
		ID:        generateID("evt"),
		Event:     ev.Type,
		Timestamp: ts,
		RunID:     ev.RunID,
		Data:      payloadData,
	})
}

// DispatchTriggerEvent converts a normalized schema.TriggerEvent into an outbound payload and dispatches it.
func (d *Dispatcher) DispatchTriggerEvent(ctx context.Context, ev schema.TriggerEvent) ([]string, error) {
	ts := ev.UnixMilli()
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}
	data := make(map[string]any, len(ev.Payload)+8)
	for k, v := range ev.Payload {
		data[k] = v
	}
	data["source"] = ev.Source
	data["event_type"] = ev.EventType
	if ev.Repository != "" {
		data["repository"] = ev.Repository
	}
	if ev.Reference != "" {
		data["reference"] = ev.Reference
	}
	if ev.IssueNumber > 0 {
		data["issue_number"] = ev.IssueNumber
	}
	if ev.PRNumber > 0 {
		data["pr_number"] = ev.PRNumber
	}
	if ev.Sender != "" {
		data["sender"] = ev.Sender
	}

	return d.Dispatch(ctx, Payload{
		ID:        ev.ID,
		Event:     ev.EventType,
		Timestamp: ts,
		RunID:     ev.ID,
		Data:      data,
	})
}

// AttachToGateway subscribes to topics on the given gateway and automatically forwards events to the dispatcher.
func (d *Dispatcher) AttachToGateway(ctx context.Context, gw *gateway.Gateway, topics ...string) (func(), error) {
	if gw == nil {
		return nil, fmt.Errorf("gateway cannot be nil")
	}

	if len(topics) == 0 {
		topics = []string{gateway.TopicAllEvents}
	}

	eventCh := make(chan gateway.Event, 256)
	unsubs := make([]func(), 0, len(topics))

	for _, topic := range topics {
		unsub, err := gw.SubscribeTaskEvents(ctx, topic, eventCh)
		if err != nil {
			for _, u := range unsubs {
				u()
			}
			return nil, err
		}
		unsubs = append(unsubs, unsub)
	}

	workerCtx, workerCancel := context.WithCancel(ctx)
	go func() {
		for {
			select {
			case <-workerCtx.Done():
				return
			case ev, ok := <-eventCh:
				if !ok {
					return
				}
				_, _ = d.DispatchGatewayEvent(workerCtx, ev)
			}
		}
	}()

	return func() {
		workerCancel()
		for _, u := range unsubs {
			u()
		}
	}, nil
}

