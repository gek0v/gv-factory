// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/worker"
)

// Worker operational statuses.
const (
	WorkerStatusReady   = "ready"   // Heartbeat received within TTL and ActiveTasks < MaxConcurrency
	WorkerStatusBusy    = "busy"    // Heartbeat received within TTL and ActiveTasks >= MaxConcurrency
	WorkerStatusOffline = "offline" // No heartbeat received within TTL
)

// WorkerInfo represents the projected telemetry state of a single worker.
type WorkerInfo struct {
	WorkerID        string    `json:"worker_id"`
	Status          string    `json:"status"`
	ActiveTasks     int       `json:"active_tasks"`
	MaxConcurrency  int       `json:"max_concurrency"`
	UptimeSeconds   int64     `json:"uptime_seconds"`
	LastHeartbeatAt time.Time `json:"last_heartbeat_at"`
}

// WorkerSummary provides aggregate metrics for the active worker pool.
type WorkerSummary struct {
	TotalWorkers     int `json:"total_workers"`
	ActiveWorkers    int `json:"active_workers"`
	TotalCapacity    int `json:"total_capacity"`
	TotalActiveTasks int `json:"total_active_tasks"`
}

// WorkersResponse represents the payload returned by GET /api/v1/workers.
type WorkersResponse struct {
	Workers          []WorkerInfo `json:"workers"`
	TotalWorkers     int          `json:"total_workers"`
	ActiveWorkers    int          `json:"active_workers"`
	TotalCapacity    int          `json:"total_capacity"`
	TotalActiveTasks int          `json:"total_active_tasks"`
}

// WorkerRegistryConfig configures timeouts and clock for WorkerRegistry.
type WorkerRegistryConfig struct {
	HeartbeatTTL     time.Duration
	RetentionTimeout time.Duration
	PurgeInterval    time.Duration
	Clock            func() time.Time
}

type workerEntry struct {
	hb       worker.HeartbeatMessage
	lastSeen time.Time
}

// WorkerRegistry maintains a thread-safe in-memory cache of worker heartbeats and statuses.
type WorkerRegistry struct {
	mu        sync.RWMutex
	cfg       WorkerRegistryConfig
	workers   map[string]workerEntry
	gw        *gateway.Gateway
	ctx       context.Context
	cancel    context.CancelFunc
	unsub     func()
	closeOnce sync.Once
}

// DefaultWorkerRegistryConfig provides standard production intervals.
func DefaultWorkerRegistryConfig() WorkerRegistryConfig {
	return WorkerRegistryConfig{
		HeartbeatTTL:     15 * time.Second,
		RetentionTimeout: 5 * time.Minute,
		PurgeInterval:    30 * time.Second,
		Clock:            func() time.Time { return time.Now().UTC() },
	}
}

// NewWorkerRegistry creates and starts a WorkerRegistry subscribed to Gateway heartbeat events.
func NewWorkerRegistry(parentCtx context.Context, gw *gateway.Gateway, opts ...WorkerRegistryConfig) *WorkerRegistry {
	cfg := DefaultWorkerRegistryConfig()
	if len(opts) > 0 {
		userCfg := opts[0]
		if userCfg.HeartbeatTTL > 0 {
			cfg.HeartbeatTTL = userCfg.HeartbeatTTL
		}
		if userCfg.RetentionTimeout > 0 {
			cfg.RetentionTimeout = userCfg.RetentionTimeout
		}
		if userCfg.PurgeInterval > 0 {
			cfg.PurgeInterval = userCfg.PurgeInterval
		}
		if userCfg.Clock != nil {
			cfg.Clock = userCfg.Clock
		}
	}

	ctx, cancel := context.WithCancel(parentCtx)
	reg := &WorkerRegistry{
		cfg:     cfg,
		workers: make(map[string]workerEntry),
		gw:      gw,
		ctx:     ctx,
		cancel:  cancel,
	}

	if gw != nil {
		hbCh := make(chan gateway.Event, 100)
		unsub, err := gw.SubscribeTaskEvents(ctx, gateway.TopicWorkerHeartbeat, hbCh)
		if err == nil {
			reg.unsub = unsub
			go reg.listenLoop(hbCh)
		}
	}

	if cfg.PurgeInterval > 0 {
		go reg.purgeLoop()
	}

	return reg
}

func (r *WorkerRegistry) clockNow() time.Time {
	if r.cfg.Clock != nil {
		return r.cfg.Clock()
	}
	return time.Now().UTC()
}

func (r *WorkerRegistry) listenLoop(ch chan gateway.Event) {
	for {
		select {
		case <-r.ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			r.handleGatewayHeartbeat(ev)
		}
	}
}

func (r *WorkerRegistry) purgeLoop() {
	ticker := time.NewTicker(r.cfg.PurgeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.Purge(r.clockNow())
		}
	}
}

func (r *WorkerRegistry) handleGatewayHeartbeat(ev gateway.Event) {
	if ev.Payload == nil {
		return
	}

	workerID := ""
	if w, ok := ev.Payload["worker_id"].(string); ok {
		workerID = strings.TrimSpace(w)
	}
	if workerID == "" {
		workerID = strings.TrimSpace(ev.RunID)
	}
	if workerID == "" {
		return
	}

	activeTasks := parseIntField(ev.Payload["active_tasks"])
	maxConcurrency := parseIntField(ev.Payload["max_concurrency"])
	uptimeSeconds := parseInt64Field(ev.Payload["uptime_seconds"])

	timestamp := parseInt64Field(ev.Payload["timestamp"])
	if timestamp == 0 && ev.Timestamp > 0 {
		timestamp = ev.Timestamp / 1000
	}

	hb := worker.HeartbeatMessage{
		WorkerID:       workerID,
		ActiveTasks:    activeTasks,
		MaxConcurrency: maxConcurrency,
		UptimeSeconds:  uptimeSeconds,
		Timestamp:      timestamp,
	}

	r.RegisterHeartbeat(hb)
}

func parseIntField(val any) int {
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func parseInt64Field(val any) int64 {
	switch v := val.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return 0
	}
}

// RegisterHeartbeat updates or inserts a worker's heartbeat record.
func (r *WorkerRegistry) RegisterHeartbeat(hb worker.HeartbeatMessage) {
	if strings.TrimSpace(hb.WorkerID) == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.clockNow()
	r.workers[hb.WorkerID] = workerEntry{
		hb:       hb,
		lastSeen: now,
	}
}

func (r *WorkerRegistry) computeStatus(entry workerEntry, now time.Time) string {
	if now.Sub(entry.lastSeen) > r.cfg.HeartbeatTTL {
		return WorkerStatusOffline
	}
	if entry.hb.ActiveTasks >= entry.hb.MaxConcurrency {
		return WorkerStatusBusy
	}
	return WorkerStatusReady
}

func (r *WorkerRegistry) toWorkerInfo(entry workerEntry, now time.Time) WorkerInfo {
	lastHeartbeatAt := entry.lastSeen
	if entry.hb.Timestamp > 0 {
		lastHeartbeatAt = time.Unix(entry.hb.Timestamp, 0).UTC()
	}

	return WorkerInfo{
		WorkerID:        entry.hb.WorkerID,
		Status:          r.computeStatus(entry, now),
		ActiveTasks:     entry.hb.ActiveTasks,
		MaxConcurrency:  entry.hb.MaxConcurrency,
		UptimeSeconds:   entry.hb.UptimeSeconds,
		LastHeartbeatAt: lastHeartbeatAt,
	}
}

// GetWorker looks up a worker by its ID and evaluates its current status.
func (r *WorkerRegistry) GetWorker(id string) (WorkerInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.workers[id]
	if !ok {
		return WorkerInfo{}, false
	}

	return r.toWorkerInfo(entry, r.clockNow()), true
}

// ListWorkers returns all known workers sorted ascending by worker_id.
func (r *WorkerRegistry) ListWorkers() []WorkerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := r.clockNow()
	list := make([]WorkerInfo, 0, len(r.workers))
	for _, entry := range r.workers {
		list = append(list, r.toWorkerInfo(entry, now))
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].WorkerID < list[j].WorkerID
	})

	return list
}

// Summary calculates current aggregate capacity and activity stats.
func (r *WorkerRegistry) Summary() WorkerSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := r.clockNow()
	var summary WorkerSummary
	summary.TotalWorkers = len(r.workers)

	for _, entry := range r.workers {
		status := r.computeStatus(entry, now)
		if status == WorkerStatusReady || status == WorkerStatusBusy {
			summary.ActiveWorkers++
			summary.TotalCapacity += entry.hb.MaxConcurrency
			summary.TotalActiveTasks += entry.hb.ActiveTasks
		}
	}

	return summary
}

// Purge removes workers whose inactivity exceeds RetentionTimeout.
func (r *WorkerRegistry) Purge(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, entry := range r.workers {
		if now.Sub(entry.lastSeen) > r.cfg.RetentionTimeout {
			delete(r.workers, id)
		}
	}
}

// Close gracefully stops the subscription and background tasks.
func (r *WorkerRegistry) Close() {
	r.closeOnce.Do(func() {
		if r.unsub != nil {
			r.unsub()
		}
		r.cancel()
	})
}
