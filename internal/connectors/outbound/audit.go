// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"context"
	"sync"
	"time"
)

// DeliveryAuditTrail captures history and metrics for webhook delivery attempts.
type DeliveryAuditTrail interface {
	Record(ctx context.Context, record DeliveryRecord) error
	List(ctx context.Context, limit int) ([]DeliveryRecord, error)
	GetBySubscription(ctx context.Context, subID string, limit int) ([]DeliveryRecord, error)
	GetByEvent(ctx context.Context, eventID string) ([]DeliveryRecord, error)
	Count(ctx context.Context) (int, error)
	Clear(ctx context.Context) error
}

// MemoryDeliveryAuditTrail provides a thread-safe, bounded in-memory delivery audit store.
type MemoryDeliveryAuditTrail struct {
	mu          sync.RWMutex
	records     []DeliveryRecord
	maxCapacity int
}

// NewMemoryDeliveryAuditTrail constructs a delivery audit trail with the given capacity limit.
// If maxCapacity <= 0, a default of 5000 is used.
func NewMemoryDeliveryAuditTrail(maxCapacity int) *MemoryDeliveryAuditTrail {
	if maxCapacity <= 0 {
		maxCapacity = 5000
	}
	return &MemoryDeliveryAuditTrail{
		records:     make([]DeliveryRecord, 0, 128),
		maxCapacity: maxCapacity,
	}
}

// Record logs a completed delivery attempt sequence into the audit trail.
func (a *MemoryDeliveryAuditTrail) Record(ctx context.Context, record DeliveryRecord) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if record.ID == "" {
		record.ID = generateID("del")
	}
	if record.CompletedAt.IsZero() {
		record.CompletedAt = time.Now().UTC()
	}

	if len(a.records) >= a.maxCapacity {
		a.records = a.records[1:]
	}

	a.records = append(a.records, record)
	return nil
}

// List returns the latest delivery records up to limit (newest first).
func (a *MemoryDeliveryAuditTrail) List(ctx context.Context, limit int) ([]DeliveryRecord, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	n := len(a.records)
	if n == 0 {
		return []DeliveryRecord{}, nil
	}

	if limit <= 0 || limit > n {
		limit = n
	}

	result := make([]DeliveryRecord, 0, limit)
	for i := n - 1; i >= n-limit; i-- {
		result = append(result, a.records[i])
	}
	return result, nil
}

// GetBySubscription returns delivery history for a particular subscription.
func (a *MemoryDeliveryAuditTrail) GetBySubscription(ctx context.Context, subID string, limit int) ([]DeliveryRecord, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var matching []DeliveryRecord
	for i := len(a.records) - 1; i >= 0; i-- {
		if a.records[i].SubscriptionID == subID {
			matching = append(matching, a.records[i])
			if limit > 0 && len(matching) >= limit {
				break
			}
		}
	}
	return matching, nil
}

// GetByEvent returns delivery history for a specific lifecycle event ID.
func (a *MemoryDeliveryAuditTrail) GetByEvent(ctx context.Context, eventID string) ([]DeliveryRecord, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var matching []DeliveryRecord
	for i := len(a.records) - 1; i >= 0; i-- {
		if a.records[i].EventID == eventID {
			matching = append(matching, a.records[i])
		}
	}
	return matching, nil
}

// Count returns the current count of delivery records in the audit trail.
func (a *MemoryDeliveryAuditTrail) Count(ctx context.Context) (int, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.records), nil
}

// Clear flushes all delivery records from memory.
func (a *MemoryDeliveryAuditTrail) Clear(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = make([]DeliveryRecord, 0, 128)
	return nil
}
