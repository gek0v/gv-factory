// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"errors"
	"time"
)

// Standard HTTP headers for outbound webhook verification and replay defense.
const (
	HeaderSignature = "X-OV-Signature-256"
	HeaderTimestamp = "X-OV-Timestamp"
	HeaderDelivery  = "X-OV-Delivery"
	HeaderEvent     = "X-OV-Event"
)

// Standard factory lifecycle event types broadcast to external endpoints.
const (
	EventTaskCompleted        = "task.completed"
	EventTaskStarted          = "task.started"
	EventTaskFailed           = "task.failed"
	EventTaskPending          = "task.pending"
	EventPRReadyForReview     = "pr.ready_for_review"
	EventGateApprovalRequired = "gate.approval_required"
	EventFactoryFailed        = "factory.failed"
	EventFactoryCompleted     = "factory.completed"
	EventRunQueued            = "run.queued"
	EventRunStarted           = "run.started"
	EventRunCompleted         = "run.completed"
	EventRunFailed            = "run.failed"
	EventMergerDivertedToPR   = "merger.diverted_to_pr"
)

// Common outbound dispatcher errors.
var (
	ErrSubscriptionNotFound = errors.New("subscription not found")
	ErrSubscriptionExists   = errors.New("subscription already exists")
	ErrInvalidSubscription = errors.New("invalid subscription: url is required")
	ErrDispatcherClosed     = errors.New("outbound webhook dispatcher is closed")
	ErrQueueFull            = errors.New("outbound webhook delivery queue is full")
	ErrMaxRetriesExceeded   = errors.New("maximum retry attempts exceeded")
	ErrPermanentClientError = errors.New("permanent client error: delivery aborted")
)

// Subscription models an external subscriber listening for factory lifecycle webhooks.
type Subscription struct {
	ID         string            `json:"id" yaml:"id"`
	URL        string            `json:"url" yaml:"url"`
	Secret     string            `json:"secret" yaml:"secret"`
	EventTypes []string          `json:"event_types" yaml:"event_types"`
	Headers    map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Active     bool              `json:"active" yaml:"active"`
	CreatedAt  time.Time         `json:"created_at" yaml:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at" yaml:"updated_at"`
}

// Payload represents the standardized outbound payload envelope sent to external webhook endpoints.
type Payload struct {
	ID        string         `json:"id"`
	Event     string         `json:"event"`
	Timestamp int64          `json:"timestamp"`
	RunID     string         `json:"run_id"`
	Data      map[string]any `json:"data"`
}

// DeliveryRecord captures the audit history of a single outbound webhook delivery attempt sequence.
type DeliveryRecord struct {
	ID             string        `json:"id"`
	SubscriptionID string        `json:"subscription_id"`
	EventID        string        `json:"event_id"`
	EventType      string        `json:"event_type"`
	TargetURL      string        `json:"target_url"`
	Payload        Payload       `json:"payload"`
	Attempts       int           `json:"attempts"`
	StatusCode     int           `json:"status_code"`
	ResponseBody   string        `json:"response_body,omitempty"`
	Error          string        `json:"error,omitempty"`
	Success        bool          `json:"success"`
	Duration       time.Duration `json:"duration"`
	CreatedAt      time.Time     `json:"created_at"`
	CompletedAt    time.Time     `json:"completed_at"`
}

// DeadLetterRecord captures diagnostic details of permanently failed webhook deliveries.
type DeadLetterRecord struct {
	ID             string    `json:"id"`
	DeliveryID     string    `json:"delivery_id"`
	SubscriptionID string    `json:"subscription_id"`
	EventID        string    `json:"event_id"`
	EventType      string    `json:"event_type"`
	TargetURL      string    `json:"target_url"`
	Payload        Payload   `json:"payload"`
	Attempts       int       `json:"attempts"`
	StatusCode     int       `json:"status_code"`
	Error          string    `json:"error"`
	Diagnostic     string    `json:"diagnostic,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// DeliveryJob represents an in-flight delivery unit routed to the worker pool.
type DeliveryJob struct {
	ID           string
	Subscription Subscription
	Payload      Payload
	EnqueuedAt   time.Time
}
