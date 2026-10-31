// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"context"
	"sync"
	"time"
)

// DeadLetterQueue defines persistence and querying for unrecoverable webhook delivery failures.
type DeadLetterQueue interface {
	Enqueue(ctx context.Context, record DeadLetterRecord) error
	List(ctx context.Context, limit int) ([]DeadLetterRecord, error)
	GetBySubscription(ctx context.Context, subID string, limit int) ([]DeadLetterRecord, error)
	Count(ctx context.Context) (int, error)
	Clear(ctx context.Context) error
}

// MemoryDeadLetterQueue is a thread-safe, bounded in-memory dead-letter queue.
type MemoryDeadLetterQueue struct {
	mu          sync.RWMutex
	records     []DeadLetterRecord
	maxCapacity int
}

// NewMemoryDeadLetterQueue creates a new in-memory DLQ with the given maximum capacity.
// If maxCapacity <= 0, a default of 1000 is used.
func NewMemoryDeadLetterQueue(maxCapacity int) *MemoryDeadLetterQueue {
	if maxCapacity <= 0 {
		maxCapacity = 1000
	}
	return &MemoryDeadLetterQueue{
		records:     make([]DeadLetterRecord, 0, 64),
		maxCapacity: maxCapacity,
	}
}

// Enqueue stores a failed delivery record in the dead-letter queue.
func (q *MemoryDeadLetterQueue) Enqueue(ctx context.Context, record DeadLetterRecord) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if record.ID == "" {
		record.ID = generateID("dlq")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}

	// If at capacity, evict oldest record
	if len(q.records) >= q.maxCapacity {
		q.records = q.records[1:]
	}

	q.records = append(q.records, record)
	return nil
}

// List returns the most recent dead-letter records, limited to limit (newest first).
func (q *MemoryDeadLetterQueue) List(ctx context.Context, limit int) ([]DeadLetterRecord, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	n := len(q.records)
	if n == 0 {
		return []DeadLetterRecord{}, nil
	}

	if limit <= 0 || limit > n {
		limit = n
	}

	result := make([]DeadLetterRecord, 0, limit)
	for i := n - 1; i >= n-limit; i-- {
		result = append(result, q.records[i])
	}
	return result, nil
}

// GetBySubscription returns dead-letter records for a specific subscription (newest first).
func (q *MemoryDeadLetterQueue) GetBySubscription(ctx context.Context, subID string, limit int) ([]DeadLetterRecord, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	var matching []DeadLetterRecord
	for i := len(q.records) - 1; i >= 0; i-- {
		if q.records[i].SubscriptionID == subID {
			matching = append(matching, q.records[i])
			if limit > 0 && len(matching) >= limit {
				break
			}
		}
	}
	return matching, nil
}

// Count returns the total number of dead-letter records currently queued.
func (q *MemoryDeadLetterQueue) Count(ctx context.Context) (int, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.records), nil
}

// Clear resets the dead-letter queue.
func (q *MemoryDeadLetterQueue) Clear(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.records = make([]DeadLetterRecord, 0, 64)
	return nil
}
