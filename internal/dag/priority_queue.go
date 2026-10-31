// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"container/heap"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Priority represents the base execution priority of a factory run or task.
type Priority int

const (
	PriorityLow    Priority = 100
	PriorityNormal Priority = 200
	PriorityHigh   Priority = 300
)

func (p Priority) String() string {
	switch p {
	case PriorityHigh:
		return "HIGH"
	case PriorityNormal:
		return "NORMAL"
	case PriorityLow:
		return "LOW"
	default:
		return fmt.Sprintf("PRIORITY(%d)", int(p))
	}
}

// ParsePriority converts a string representation to a Priority enum.
func ParsePriority(s string) Priority {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "HIGH", "EXPRESS", "URGENT", "BUGFIX":
		return PriorityHigh
	case "LOW", "BATCH", "BACKGROUND", "REFACTOR", "DOC":
		return PriorityLow
	default:
		return PriorityNormal
	}
}

// TemplateDefaultPriority determines default priority based on template naming conventions.
func TemplateDefaultPriority(templateName string) Priority {
	name := strings.ToLower(templateName)
	if strings.Contains(name, "bugfix") || strings.Contains(name, "express") || strings.Contains(name, "hotfix") {
		return PriorityHigh
	}
	if strings.Contains(name, "doc") || strings.Contains(name, "refactor") {
		return PriorityLow
	}
	return PriorityNormal
}

// PriorityQueueConfig defines configuration for starvation prevention and priority boosts.
type PriorityQueueConfig struct {
	AgingInterval time.Duration // Interval at which waiting items gain aging points (default: 500ms)
	AgingBoost    int           // Points added per aging interval (default: 25)
	SkipBoost     int           // Points added whenever a higher priority item is dispatched while waiting (default: 20)
}

// QueueItem represents a single unit of work in the priority queue.
type QueueItem struct {
	RunID             string
	TemplateName      string
	Branch            string
	RepoPath          string
	BasePriority      Priority
	QueuedAt          time.Time
	DispatchesSkipped int
	Payload           any
	index             int // Index in the underlying heap
}

// EffectivePriority calculates current score considering age and dispatches skipped.
func (item *QueueItem) EffectivePriority(now time.Time, cfg PriorityQueueConfig) int {
	score := int(item.BasePriority)
	if cfg.AgingInterval > 0 && cfg.AgingBoost > 0 {
		elapsed := now.Sub(item.QueuedAt)
		intervals := int(elapsed / cfg.AgingInterval)
		if intervals > 0 {
			score += intervals * cfg.AgingBoost
		}
	}
	if cfg.SkipBoost > 0 && item.DispatchesSkipped > 0 {
		score += item.DispatchesSkipped * cfg.SkipBoost
	}
	return score
}

type priorityHeap struct {
	items []*QueueItem
	cfg   PriorityQueueConfig
	now   time.Time
}

func (h priorityHeap) Len() int { return len(h.items) }

func (h priorityHeap) Less(i, j int) bool {
	pi := h.items[i].EffectivePriority(h.now, h.cfg)
	pj := h.items[j].EffectivePriority(h.now, h.cfg)
	if pi != pj {
		return pi > pj
	}
	return h.items[i].QueuedAt.Before(h.items[j].QueuedAt)
}

func (h priorityHeap) Swap(i, j int) {
	h.items[i], h.items[j] = h.items[j], h.items[i]
	h.items[i].index = i
	h.items[j].index = j
}

func (h *priorityHeap) Push(x any) {
	n := len(h.items)
	item := x.(*QueueItem)
	item.index = n
	h.items = append(h.items, item)
}

func (h *priorityHeap) Pop() any {
	old := h.items
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	h.items = old[0 : n-1]
	return item
}

// PriorityQueue provides thread-safe priority scheduling with starvation prevention.
type PriorityQueue struct {
	mu  sync.RWMutex
	cfg PriorityQueueConfig
	h   priorityHeap
}

// NewPriorityQueue creates an initialized priority queue with starvation prevention settings.
func NewPriorityQueue(cfg PriorityQueueConfig) *PriorityQueue {
	if cfg.AgingInterval <= 0 {
		cfg.AgingInterval = 500 * time.Millisecond
	}
	if cfg.AgingBoost <= 0 {
		cfg.AgingBoost = 25
	}
	if cfg.SkipBoost <= 0 {
		cfg.SkipBoost = 20
	}
	pq := &PriorityQueue{
		cfg: cfg,
		h: priorityHeap{
			cfg: cfg,
		},
	}
	heap.Init(&pq.h)
	return pq
}

// Config returns the configuration of the priority queue.
func (pq *PriorityQueue) Config() PriorityQueueConfig {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	return pq.cfg
}

// Push enqueues a new item.
func (pq *PriorityQueue) Push(item *QueueItem) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	if item.QueuedAt.IsZero() {
		item.QueuedAt = time.Now()
	}
	pq.h.now = time.Now()
	heap.Push(&pq.h, item)
}

// Pop extracts the highest effective priority item.
func (pq *PriorityQueue) Pop() *QueueItem {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	if len(pq.h.items) == 0 {
		return nil
	}
	pq.h.now = time.Now()
	heap.Init(&pq.h)
	item := heap.Pop(&pq.h).(*QueueItem)
	return item
}

// Peek returns the highest effective priority item without removing it.
func (pq *PriorityQueue) Peek() *QueueItem {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	if len(pq.h.items) == 0 {
		return nil
	}
	now := time.Now()
	var best *QueueItem
	bestScore := -1
	for _, it := range pq.h.items {
		score := it.EffectivePriority(now, pq.cfg)
		if best == nil || score > bestScore || (score == bestScore && it.QueuedAt.Before(best.QueuedAt)) {
			best = it
			bestScore = score
		}
	}
	return best
}

// Remove removes an item by runID (e.g. upon cancellation).
func (pq *PriorityQueue) Remove(runID string) *QueueItem {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	for i, item := range pq.h.items {
		if item.RunID == runID {
			pq.h.now = time.Now()
			removed := heap.Remove(&pq.h, i).(*QueueItem)
			return removed
		}
	}
	return nil
}

// Len returns the number of items in the queue.
func (pq *PriorityQueue) Len() int {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	return len(pq.h.items)
}

// NotifyDispatch increments the skip count of all waiting items except the dispatched one.
func (pq *PriorityQueue) NotifyDispatch(dispatchedRunID string) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	for _, item := range pq.h.items {
		if item.RunID != dispatchedRunID {
			item.DispatchesSkipped++
		}
	}
	pq.h.now = time.Now()
	heap.Init(&pq.h)
}

// All returns a snapshot of all items currently in the queue.
func (pq *PriorityQueue) All() []*QueueItem {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	out := make([]*QueueItem, len(pq.h.items))
	copy(out, pq.h.items)
	return out
}

