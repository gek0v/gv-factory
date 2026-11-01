// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"time"
)

// HeartbeatPublisher delivers heartbeat messages to listeners or brokers.
type HeartbeatPublisher interface {
	PublishHeartbeat(hb HeartbeatMessage) error
}

// HeartbeatConfig holds configuration for the heartbeat emitter.
type HeartbeatConfig struct {
	WorkerID       string
	Interval       time.Duration
	StartTime      time.Time
	MaxConcurrency int
	GetActiveTasks func() int
	Publisher      HeartbeatPublisher
}

// HeartbeatEmitter runs a background heartbeat routine.
type HeartbeatEmitter struct {
	cfg HeartbeatConfig
}

// NewHeartbeatEmitter constructs a new HeartbeatEmitter.
func NewHeartbeatEmitter(cfg HeartbeatConfig) *HeartbeatEmitter {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.StartTime.IsZero() {
		cfg.StartTime = time.Now()
	}
	return &HeartbeatEmitter{
		cfg: cfg,
	}
}

// Start executes the periodic heartbeat loop until ctx is cancelled.
func (e *HeartbeatEmitter) Start(ctx context.Context) {
	ticker := time.NewTicker(e.cfg.Interval)
	defer ticker.Stop()

	// Emit initial heartbeat
	e.emit()

	for {
		select {
		case <-ticker.C:
			e.emit()
		case <-ctx.Done():
			return
		}
	}
}

func (e *HeartbeatEmitter) emit() {
	if e.cfg.Publisher == nil {
		return
	}

	active := 0
	if e.cfg.GetActiveTasks != nil {
		active = e.cfg.GetActiveTasks()
	}

	uptime := int64(time.Since(e.cfg.StartTime).Seconds())

	hb := HeartbeatMessage{
		WorkerID:       e.cfg.WorkerID,
		ActiveTasks:    active,
		MaxConcurrency: e.cfg.MaxConcurrency,
		UptimeSeconds:  uptime,
		Timestamp:      time.Now().UnixMilli(),
	}

	_ = e.cfg.Publisher.PublishHeartbeat(hb)
}
