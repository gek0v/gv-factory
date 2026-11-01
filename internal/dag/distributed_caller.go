// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/worker"
	pdk "github.com/gv-factory/gv-factory/plugins-sdk/go"
)

// TaskPublisher abstracts publication of binary task messages to a broker or queue.
type TaskPublisher interface {
	Publish(subject string, data []byte) error
}

// TaskEventSubscriber abstracts subscription to task lifecycle events.
type TaskEventSubscriber interface {
	SubscribeTaskEvents(ctx context.Context, topic string, ch chan gateway.Event) (func(), error)
}

// DistributedCallerConfig specifies dependencies and policies for DistributedStepCaller.
type DistributedCallerConfig struct {
	TaskQueue       TaskPublisher
	EventSubscriber TaskEventSubscriber
	FallbackCaller  PluginCaller
	DefaultTimeout  time.Duration
	FallbackLocal   bool
	TopicWorkTasks  string
}

// DistributedStepCaller dispatches task steps over NATS JetStream and correlates worker responses.
type DistributedStepCaller struct {
	cfg DistributedCallerConfig
}

// NewDistributedStepCaller instantiates a new distributed step caller.
func NewDistributedStepCaller(cfg DistributedCallerConfig) *DistributedStepCaller {
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 5 * time.Minute
	}
	if cfg.TopicWorkTasks == "" {
		cfg.TopicWorkTasks = gateway.TopicWorkTasks
	}
	return &DistributedStepCaller{
		cfg: cfg,
	}
}

// CallPDK dispatches the step task as worker.TaskMessage over the broker and awaits its completion.
// If the broker or subscriber are not configured, FallbackLocal is true, or message publishing fails,
// it falls back seamlessly to FallbackCaller.
func (d *DistributedStepCaller) CallPDK(ctx context.Context, pluginRef string, functionName string, req pdk.Context) (*pdk.Result, error) {
	if d.cfg.FallbackLocal || d.cfg.TaskQueue == nil || d.cfg.EventSubscriber == nil {
		if d.cfg.FallbackCaller != nil {
			return d.cfg.FallbackCaller.CallPDK(ctx, pluginRef, functionName, req)
		}
		return nil, errors.New("distributed execution unavailable and no fallback caller configured")
	}

	taskType := worker.TaskTypeWasmPlugin
	if tt, ok := req.Configs["task_type"]; ok && tt != "" {
		taskType = tt
	}

	stepID := req.TaskID
	if sid, ok := req.Configs["step_id"]; ok && sid != "" {
		stepID = sid
	}

	payloadData := map[string]any{
		"plugin_ref":    pluginRef,
		"function_name": functionName,
		"configs":       req.Configs,
	}
	payloadBytes, err := json.Marshal(payloadData)
	if err != nil {
		if d.cfg.FallbackCaller != nil {
			return d.cfg.FallbackCaller.CallPDK(ctx, pluginRef, functionName, req)
		}
		return nil, fmt.Errorf("failed to marshal task payload: %w", err)
	}

	var timeoutMS int64
	if deadline, ok := ctx.Deadline(); ok {
		timeoutMS = time.Until(deadline).Milliseconds()
	} else {
		timeoutMS = d.cfg.DefaultTimeout.Milliseconds()
	}

	taskMsg := worker.TaskMessage{
		TaskID:    req.TaskID,
		RunID:     req.RunID,
		StepID:    stepID,
		TaskType:  taskType,
		Payload:   payloadBytes,
		TimeoutMS: timeoutMS,
	}

	msgBytes, err := json.Marshal(taskMsg)
	if err != nil {
		if d.cfg.FallbackCaller != nil {
			return d.cfg.FallbackCaller.CallPDK(ctx, pluginRef, functionName, req)
		}
		return nil, fmt.Errorf("failed to marshal TaskMessage: %w", err)
	}

	eventCh := make(chan gateway.Event, 32)
	unsub, err := d.cfg.EventSubscriber.SubscribeTaskEvents(ctx, gateway.TopicAllTasks, eventCh)
	if err != nil {
		if d.cfg.FallbackCaller != nil {
			return d.cfg.FallbackCaller.CallPDK(ctx, pluginRef, functionName, req)
		}
		return nil, fmt.Errorf("failed to subscribe to task events: %w", err)
	}
	defer unsub()

	if err := d.cfg.TaskQueue.Publish(d.cfg.TopicWorkTasks, msgBytes); err != nil {
		if d.cfg.FallbackCaller != nil {
			return d.cfg.FallbackCaller.CallPDK(ctx, pluginRef, functionName, req)
		}
		return nil, fmt.Errorf("failed to publish task message: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ev, ok := <-eventCh:
			if !ok {
				return nil, errors.New("event subscriber channel closed")
			}

			if ev.RunID != req.RunID {
				continue
			}

			// Correlate with task or step ID
			matchesID := ev.TaskID == req.TaskID || ev.TaskID == stepID
			if !matchesID && ev.Payload != nil {
				if tID, ok := ev.Payload["task_id"].(string); ok && (tID == req.TaskID || tID == stepID) {
					matchesID = true
				} else if sID, ok := ev.Payload["step_id"].(string); ok && (sID == req.TaskID || sID == stepID) {
					matchesID = true
				}
			}

			if !matchesID {
				continue
			}

			switch ev.Type {
			case gateway.TopicTaskCompleted:
				output := ""
				var data map[string]any
				if ev.Payload != nil {
					if out, ok := ev.Payload["output"].(string); ok {
						output = out
					}
					if d, ok := ev.Payload["data"].(map[string]any); ok {
						data = d
					}
				}
				return &pdk.Result{
					Success: true,
					Output:  output,
					Data:    data,
				}, nil

			case gateway.TopicTaskFailed:
				errMsg := "task execution failed"
				if ev.Payload != nil {
					if errStr, ok := ev.Payload["error"].(string); ok && strings.TrimSpace(errStr) != "" {
						errMsg = errStr
					} else if out, ok := ev.Payload["output"].(string); ok && strings.TrimSpace(out) != "" {
						errMsg = out
					}
				}
				return &pdk.Result{
					Success: false,
					Output:  errMsg,
				}, fmt.Errorf("%s", errMsg)
			}
		}
	}
}
