// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
)

// EventPublisher abstracts emitting event telemetry.
type EventPublisher interface {
	PublishTaskEvent(ctx context.Context, ev gateway.Event) error
}

// ConsumerConfig specifies settings for the worker task queue consumer.
type ConsumerConfig struct {
	WorkerID          string
	Concurrency       int
	DrainTimeout      time.Duration
	HeartbeatInterval time.Duration
	Queue             QueueBackend
	Router            *TaskRouter
	Gateway           EventPublisher
	DefaultTimeout    time.Duration
}

// Consumer consumes tasks from a QueueBackend, enforces concurrency limits,
// and evaluates JetStream Ack/Nak/Term policies.
type Consumer struct {
	cfg            ConsumerConfig
	sub            QueueSubscription
	sem            chan struct{}
	inFlightWg     sync.WaitGroup
	activeCount    atomic.Int64
	startTime      time.Time
	running        atomic.Bool
	stopOnce       sync.Once
	listenCtx      context.Context
	listenCancel   context.CancelFunc
	taskCtx        context.Context
	taskCancel     context.CancelFunc
	heartbeat      *HeartbeatEmitter
	defaultTimeout time.Duration
}

// NewConsumer initializes a distributed task consumer.
func NewConsumer(cfg ConsumerConfig) *Consumer {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	if cfg.DrainTimeout <= 0 {
		cfg.DrainTimeout = 15 * time.Second
	}
	if cfg.WorkerID == "" {
		cfg.WorkerID = fmt.Sprintf("ov-worker-%d", time.Now().UnixNano())
	}
	timeout := cfg.DefaultTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	c := &Consumer{
		cfg:            cfg,
		sem:            make(chan struct{}, cfg.Concurrency),
		startTime:      time.Now(),
		defaultTimeout: timeout,
	}

	hbInterval := cfg.HeartbeatInterval
	if hbInterval <= 0 {
		hbInterval = 5 * time.Second
	}

	c.heartbeat = NewHeartbeatEmitter(HeartbeatConfig{
		WorkerID:       cfg.WorkerID,
		Interval:       hbInterval,
		StartTime:      c.startTime,
		MaxConcurrency: cfg.Concurrency,
		GetActiveTasks: c.ActiveTasks,
		Publisher:      c,
	})

	return c
}

// ActiveTasks returns current number of tasks executing.
func (c *Consumer) ActiveTasks() int {
	return int(c.activeCount.Load())
}

// PublishHeartbeat sends heartbeat payload via Gateway publisher.
func (c *Consumer) PublishHeartbeat(hb HeartbeatMessage) error {
	if c.cfg.Gateway == nil {
		return nil
	}
	payload := map[string]any{
		"worker_id":       hb.WorkerID,
		"active_tasks":    hb.ActiveTasks,
		"max_concurrency": hb.MaxConcurrency,
		"uptime_seconds":  hb.UptimeSeconds,
		"timestamp":       hb.Timestamp,
	}
	return c.cfg.Gateway.PublishTaskEvent(context.Background(), gateway.Event{
		Type:      TopicWorkerHeartbeat,
		RunID:     hb.WorkerID,
		Payload:   payload,
		Timestamp: hb.Timestamp,
	})
}

// Start begins listening on the task queue and emitting heartbeats.
func (c *Consumer) Start(ctx context.Context) error {
	if c.cfg.Queue == nil {
		return errors.New("queue backend is nil")
	}

	c.taskCtx, c.taskCancel = context.WithCancel(context.Background())
	c.listenCtx, c.listenCancel = context.WithCancel(ctx)
	c.running.Store(true)

	sub, err := c.cfg.Queue.Subscribe(c.listenCtx, TopicWorkTasks, ConsumerGroup)
	if err != nil {
		c.listenCancel()
		c.taskCancel()
		return fmt.Errorf("failed to subscribe to %s: %w", TopicWorkTasks, err)
	}
	c.sub = sub

	// Start heartbeat routine linked to listenCtx
	go c.heartbeat.Start(c.listenCtx)

	// Start queue message consumption loop
	go c.listenLoop(c.taskCtx)

	return nil
}

func (c *Consumer) listenLoop(taskBaseCtx context.Context) {
	msgs := c.sub.Messages()
	for {
		select {
		case <-c.listenCtx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}

			// Acquire concurrency slot before registering inFlightWg
			select {
			case c.sem <- struct{}{}:
			case <-c.listenCtx.Done():
				// Worker stopping, return task to queue
				_ = msg.NakWithDelay(1 * time.Second)
				return
			}

			c.inFlightWg.Add(1)
			c.activeCount.Add(1)

			go func(qm QueueMessage) {
				defer func() {
					c.activeCount.Add(-1)
					<-c.sem
					c.inFlightWg.Done()
				}()

				c.handleQueueMessage(taskBaseCtx, qm)
			}(msg)
		}
	}
}

func (c *Consumer) handleQueueMessage(ctx context.Context, msg QueueMessage) {
	var task TaskMessage
	if err := json.Unmarshal(msg.Data(), &task); err != nil {
		// Poison pill: corrupted JSON
		_ = msg.Term()
		return
	}

	if err := task.Validate(); err != nil {
		// Contract violation: fatal validation error
		_ = msg.Term()
		return
	}

	// Calculate task execution timeout
	taskTimeout := c.defaultTimeout
	if task.TimeoutMS > 0 {
		taskTimeout = time.Duration(task.TimeoutMS) * time.Millisecond
	}

	taskCtx, cancel := context.WithTimeout(ctx, taskTimeout)
	defer cancel()

	// Notify task started with fallback timeout context
	eventCtx, eventCancel := context.WithTimeout(context.Background(), 2*time.Second)
	c.emitEvent(eventCtx, gateway.TopicTaskStarted, task, map[string]any{
		"worker_id": c.cfg.WorkerID,
	})
	eventCancel()

	startTs := time.Now()
	res, err := c.executeTask(taskCtx, &task)
	durationMs := time.Since(startTs).Milliseconds()

	// Telemetry span with independent context to avoid deadline cancellation issues
	spanCtx, spanCancel := context.WithTimeout(context.Background(), 2*time.Second)
	c.emitSpan(spanCtx, task, durationMs, err == nil && (res == nil || res.Success))
	spanCancel()

	// JetStream Ack/Nak/Term policy decision
	c.applyAckPolicy(msg, res, err)
}

func (c *Consumer) executeTask(ctx context.Context, task *TaskMessage) (*TaskResult, error) {
	if c.cfg.Router == nil {
		return nil, errors.New("no task router configured")
	}
	return c.cfg.Router.Dispatch(ctx, task)
}

func (c *Consumer) applyAckPolicy(msg QueueMessage, res *TaskResult, err error) {
	if err != nil {
		var retryErr *RetryableError
		if errors.As(err, &retryErr) {
			delay := retryErr.Delay
			if delay <= 0 {
				delay = 500 * time.Millisecond
			}
			_ = msg.NakWithDelay(delay)
			return
		}

		if errors.Is(err, context.Canceled) {
			_ = msg.NakWithDelay(1 * time.Second)
			return
		}

		// Non-retryable fatal failure
		_ = msg.Term()
		return
	}

	if res != nil && !res.Success {
		if res.Retryable {
			_ = msg.NakWithDelay(500 * time.Millisecond)
			return
		}
		_ = msg.Term()
		return
	}

	_ = msg.Ack()
}

func (c *Consumer) emitEvent(ctx context.Context, eventType string, task TaskMessage, extra map[string]any) {
	if c.cfg.Gateway == nil {
		return
	}
	payload := map[string]any{
		"task_id":   task.TaskID,
		"step_id":   task.StepID,
		"task_type": task.TaskType,
	}
	for k, v := range extra {
		payload[k] = v
	}

	_ = c.cfg.Gateway.PublishTaskEvent(ctx, gateway.Event{
		Type:      eventType,
		RunID:     task.RunID,
		TaskID:    task.TaskID,
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
}

func (c *Consumer) emitSpan(ctx context.Context, task TaskMessage, durationMs int64, success bool) {
	if c.cfg.Gateway == nil {
		return
	}
	status := "success"
	if !success {
		status = "failed"
	}
	_ = c.cfg.Gateway.PublishTaskEvent(ctx, gateway.Event{
		Type:   gateway.TopicTaskSpan,
		RunID:  task.RunID,
		TaskID: task.TaskID,
		Payload: map[string]any{
			"duration_ms": durationMs,
			"node_type":   task.TaskType,
			"status":      status,
		},
		Timestamp: time.Now().UnixMilli(),
	})
	if success {
		c.emitEvent(ctx, gateway.TopicTaskCompleted, task, map[string]any{
			"status": "completed",
		})
	} else {
		c.emitEvent(ctx, gateway.TopicTaskFailed, task, map[string]any{
			"status": "failed",
		})
	}
}

// Stop gracefully stops consumer message ingestion and drains in-flight tasks.
// Stop gracefully stops consumer message ingestion and drains in-flight tasks.
func (c *Consumer) Stop() {
	c.stopOnce.Do(func() {
		// Stop receiving new messages
		if c.sub != nil {
			_ = c.sub.Unsubscribe()
		}
		if c.listenCancel != nil {
			c.listenCancel()
		}

		// Wait for active in-flight tasks up to DrainTimeout
		done := make(chan struct{})
		go func() {
			c.inFlightWg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(c.cfg.DrainTimeout):
			// Force cancel remaining in-flight tasks after drain timeout expires
			if c.taskCancel != nil {
				c.taskCancel()
			}
			<-done
		}

		c.running.Store(false)
	})
}
