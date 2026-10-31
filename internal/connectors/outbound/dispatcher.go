// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Dispatcher processes factory lifecycle events and broadcasts signed webhooks to registered subscribers.
type Dispatcher struct {
	mu        sync.RWMutex
	cfg       Config
	store     SubscriptionStore
	dlq       DeadLetterQueue
	audit     DeliveryAuditTrail
	client    *http.Client
	jobs      chan DeliveryJob
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
	running   bool
	closed    bool
	closeOnce sync.Once
}

// NewDispatcher constructs a webhook dispatcher engine with custom store, DLQ, and audit components.
func NewDispatcher(cfg Config, store SubscriptionStore, dlq DeadLetterQueue, audit DeliveryAuditTrail) *Dispatcher {
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 5
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1000
	}
	if cfg.RetryPolicy.MaxAttempts <= 0 {
		cfg.RetryPolicy = DefaultRetryPolicy()
	}
	if cfg.SignaturePrefix == "" {
		cfg.SignaturePrefix = DefaultSignaturePrefix
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		}
	}

	if store == nil {
		store = NewMemorySubscriptionStore()
	}
	if dlq == nil {
		dlq = NewMemoryDeadLetterQueue(1000)
	}
	if audit == nil {
		audit = NewMemoryDeliveryAuditTrail(5000)
	}

	return &Dispatcher{
		cfg:    cfg,
		store:  store,
		dlq:    dlq,
		audit:  audit,
		client: client,
		jobs:   make(chan DeliveryJob, cfg.QueueSize),
	}
}

// NewDefaultDispatcher constructs a turnkey dispatcher using standard in-memory storage and audit mechanisms.
func NewDefaultDispatcher() *Dispatcher {
	return NewDispatcher(DefaultConfig(), NewMemorySubscriptionStore(), NewMemoryDeadLetterQueue(1000), NewMemoryDeliveryAuditTrail(5000))
}

// Store returns the underlying subscription store.
func (d *Dispatcher) Store() SubscriptionStore {
	return d.store
}

// DeadLetterQueue returns the attached dead-letter queue.
func (d *Dispatcher) DeadLetterQueue() DeadLetterQueue {
	return d.dlq
}

// AuditTrail returns the attached delivery audit trail.
func (d *Dispatcher) AuditTrail() DeliveryAuditTrail {
	return d.audit
}

// Start boots the asynchronous delivery worker pool.
func (d *Dispatcher) Start(ctx context.Context) {
	d.mu.Lock()
	if d.running || d.closed {
		d.mu.Unlock()
		return
	}
	d.running = true
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.mu.Unlock()

	for i := 0; i < d.cfg.WorkerCount; i++ {
		d.wg.Add(1)
		go d.workerLoop()
	}
}

// Stop gracefully shuts down the worker pool and waits for pending jobs to complete.
func (d *Dispatcher) Stop() error {
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		d.running = false
		if d.cancel != nil {
			d.cancel()
		}
		close(d.jobs)
		d.mu.Unlock()

		d.wg.Wait()

		if transport, ok := d.client.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	})
	return nil
}

// Close is an alias for Stop to fulfill io.Closer.
func (d *Dispatcher) Close() error {
	return d.Stop()
}

// Dispatch finds all active subscriptions matching payload.Event and routes delivery jobs to workers.
// If the worker pool is not started, deliveries are executed synchronously.
func (d *Dispatcher) Dispatch(ctx context.Context, payload Payload) ([]string, error) {
	d.mu.RLock()
	if d.closed {
		d.mu.RUnlock()
		return nil, ErrDispatcherClosed
	}
	isRunning := d.running
	d.mu.RUnlock()

	if payload.ID == "" {
		payload.ID = generateID("evt")
	}
	if payload.Timestamp == 0 {
		payload.Timestamp = time.Now().UnixMilli()
	}

	subs, err := d.store.GetActiveSubscriptionsForEvent(ctx, payload.Event)
	if err != nil {
		return nil, fmt.Errorf("failed fetching subscriptions for event %q: %w", payload.Event, err)
	}

	if len(subs) == 0 {
		return []string{}, nil
	}

	deliveryIDs := make([]string, 0, len(subs))

	for _, sub := range subs {
		deliveryID := generateID("del")
		deliveryIDs = append(deliveryIDs, deliveryID)

		job := DeliveryJob{
			ID:           deliveryID,
			Subscription: sub,
			Payload:      payload,
			EnqueuedAt:   time.Now().UTC(),
		}

		if isRunning {
			select {
			case d.jobs <- job:
			case <-ctx.Done():
				return deliveryIDs, ctx.Err()
			default:
				diagnostic := fmt.Sprintf("Delivery queue full (capacity %d), dropped job for subscription %s", d.cfg.QueueSize, sub.ID)
				_ = d.dlq.Enqueue(ctx, DeadLetterRecord{
					ID:             generateID("dlq"),
					DeliveryID:     deliveryID,
					SubscriptionID: sub.ID,
					EventID:        payload.ID,
					EventType:      payload.Event,
					TargetURL:      sub.URL,
					Payload:        payload,
					Attempts:       0,
					StatusCode:     0,
					Error:          ErrQueueFull.Error(),
					Diagnostic:     diagnostic,
					CreatedAt:      time.Now().UTC(),
				})
			}
		} else {
			_, _ = d.executeDelivery(ctx, deliveryID, sub, payload)
		}
	}

	return deliveryIDs, nil
}

// DispatchEvent constructs a payload and dispatches it to matching subscribers.
func (d *Dispatcher) DispatchEvent(ctx context.Context, eventType string, runID string, data map[string]any) ([]string, error) {
	return d.Dispatch(ctx, Payload{
		ID:        generateID("evt"),
		Event:     eventType,
		Timestamp: time.Now().UnixMilli(),
		RunID:     runID,
		Data:      data,
	})
}
