// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/store"
)

// StepLifecycleTracker subscribes to gateway task and run events and persists lifecycle updates in store.Store.
type StepLifecycleTracker struct {
	st     store.Store
	gw     *gateway.Gateway
	unsubs []func()
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

// NewStepLifecycleTracker starts tracking task and run events from the gateway into store.Store.
func NewStepLifecycleTracker(parentCtx context.Context, gw *gateway.Gateway, st store.Store) *StepLifecycleTracker {
	ctx, cancel := context.WithCancel(parentCtx)
	tracker := &StepLifecycleTracker{
		st:     st,
		gw:     gw,
		ctx:    ctx,
		cancel: cancel,
	}

	if gw == nil || st == nil {
		return tracker
	}

	topics := []string{
		gateway.TopicTaskStarted,
		gateway.TopicTaskCompleted,
		gateway.TopicTaskFailed,
		gateway.TopicRunStarted,
		gateway.TopicRunCompleted,
		gateway.TopicRunFailed,
		gateway.TopicRunCancelled,
	}

	for _, topic := range topics {
		ch := make(chan gateway.Event, 64)
		unsub, err := gw.SubscribeTaskEvents(ctx, topic, ch)
		if err != nil {
			log.Printf("ov-server: tracker failed to subscribe to %s: %v", topic, err)
			continue
		}
		tracker.unsubs = append(tracker.unsubs, unsub)

		go tracker.handleEvents(ch)
	}

	return tracker
}

func (t *StepLifecycleTracker) handleEvents(ch chan gateway.Event) {
	for {
		select {
		case <-t.ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			t.processEvent(ev)
		}
	}
}

func (t *StepLifecycleTracker) processEvent(ev gateway.Event) {
	if strings.TrimSpace(ev.RunID) == "" {
		return
	}

	switch ev.Type {
	case gateway.TopicTaskStarted:
		stepName := ev.TaskID
		if ev.Payload != nil {
			if sn, ok := ev.Payload["step_name"].(string); ok && sn != "" {
				stepName = sn
			} else if sid, ok := ev.Payload["step_id"].(string); ok && sid != "" {
				stepName = sid
			}
		}

		createdAt := time.Now().UTC()
		if ev.Timestamp > 0 {
			createdAt = time.UnixMilli(ev.Timestamp).UTC()
		}

		stepID := fmt.Sprintf("%s-started-%d", ev.TaskID, createdAt.UnixNano())
		_ = t.st.RecordStep(t.ctx, &store.Step{
			ID:         stepID,
			RunID:      ev.RunID,
			StepName:   stepName,
			Status:     store.StepStatusRunning,
			DurationMS: 0,
			CreatedAt:  createdAt,
		})

	case gateway.TopicTaskCompleted:
		stepName := ev.TaskID
		var durationMS int64
		output := make(map[string]any)

		if ev.Payload != nil {
			if sn, ok := ev.Payload["step_name"].(string); ok && sn != "" {
				stepName = sn
			} else if sid, ok := ev.Payload["step_id"].(string); ok && sid != "" {
				stepName = sid
			}

			if d, ok := ev.Payload["duration_ms"].(int64); ok {
				durationMS = d
			} else if d, ok := ev.Payload["duration_ms"].(float64); ok {
				durationMS = int64(d)
			} else if d, ok := ev.Payload["duration_ms"].(int); ok {
				durationMS = int64(d)
			}

			if out, ok := ev.Payload["output"]; ok {
				output["output"] = out
			}
			if dt, ok := ev.Payload["data"].(map[string]any); ok {
				output["data"] = dt
			}
		}

		createdAt := time.Now().UTC()
		if ev.Timestamp > 0 {
			createdAt = time.UnixMilli(ev.Timestamp).UTC()
		}

		stepID := fmt.Sprintf("%s-completed-%d", ev.TaskID, createdAt.UnixNano())
		_ = t.st.RecordStep(t.ctx, &store.Step{
			ID:         stepID,
			RunID:      ev.RunID,
			StepName:   stepName,
			Status:     store.StepStatusCompleted,
			DurationMS: durationMS,
			Output:     output,
			CreatedAt:  createdAt,
		})

	case gateway.TopicTaskFailed:
		stepName := ev.TaskID
		var durationMS int64
		errorMsg := ""

		if ev.Payload != nil {
			if sn, ok := ev.Payload["step_name"].(string); ok && sn != "" {
				stepName = sn
			} else if sid, ok := ev.Payload["step_id"].(string); ok && sid != "" {
				stepName = sid
			}

			if d, ok := ev.Payload["duration_ms"].(int64); ok {
				durationMS = d
			} else if d, ok := ev.Payload["duration_ms"].(float64); ok {
				durationMS = int64(d)
			} else if d, ok := ev.Payload["duration_ms"].(int); ok {
				durationMS = int64(d)
			}

			if errStr, ok := ev.Payload["error"].(string); ok {
				errorMsg = errStr
			}
		}

		createdAt := time.Now().UTC()
		if ev.Timestamp > 0 {
			createdAt = time.UnixMilli(ev.Timestamp).UTC()
		}

		stepID := fmt.Sprintf("%s-failed-%d", ev.TaskID, createdAt.UnixNano())
		_ = t.st.RecordStep(t.ctx, &store.Step{
			ID:           stepID,
			RunID:        ev.RunID,
			StepName:     stepName,
			Status:       store.StepStatusFailed,
			DurationMS:   durationMS,
			ErrorMessage: errorMsg,
			CreatedAt:    createdAt,
		})

	case gateway.TopicRunStarted:
		_ = t.st.UpdateRunStatus(t.ctx, ev.RunID, store.RunStatusRunning, nil)

	case gateway.TopicRunCompleted:
		_ = t.st.UpdateRunStatus(t.ctx, ev.RunID, store.RunStatusCompleted, nil)

	case gateway.TopicRunFailed:
		var runErr error
		if ev.Payload != nil {
			if errStr, ok := ev.Payload["error"].(string); ok && errStr != "" {
				runErr = fmt.Errorf("%s", errStr)
			}
		}
		_ = t.st.UpdateRunStatus(t.ctx, ev.RunID, store.RunStatusFailed, runErr)

	case gateway.TopicRunCancelled:
		_ = t.st.UpdateRunStatus(t.ctx, ev.RunID, store.RunStatusCancelled, nil)
	}
}

// Close unsubscribes from all event topics and stops background workers.
func (t *StepLifecycleTracker) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.cancel()
	for _, unsub := range t.unsubs {
		if unsub != nil {
			unsub()
		}
	}
	t.unsubs = nil
}
