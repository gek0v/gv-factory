// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"context"
	"testing"
	"time"
)

func TestMemoryDeadLetterQueue(t *testing.T) {
	ctx := context.Background()
	dlq := NewMemoryDeadLetterQueue(3)

	for i := 1; i <= 4; i++ {
		err := dlq.Enqueue(ctx, DeadLetterRecord{
			SubscriptionID: "sub-1",
			EventID:        "evt-1",
			EventType:      "task.completed",
			Attempts:       5,
			StatusCode:     503,
			Error:          "Service Unavailable",
		})
		if err != nil {
			t.Fatalf("unexpected error enqueuing: %v", err)
		}
	}

	count, _ := dlq.Count(ctx)
	if count != 3 {
		t.Errorf("expected DLQ count capped at 3, got %d", count)
	}

	records, _ := dlq.List(ctx, 2)
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	bySub, _ := dlq.GetBySubscription(ctx, "sub-1", 10)
	if len(bySub) != 3 {
		t.Errorf("expected 3 records by sub, got %d", len(bySub))
	}

	_ = dlq.Clear(ctx)
	countAfter, _ := dlq.Count(ctx)
	if countAfter != 0 {
		t.Errorf("expected 0 count after clear, got %d", countAfter)
	}
}

func TestMemoryDeliveryAuditTrail(t *testing.T) {
	ctx := context.Background()
	audit := NewMemoryDeliveryAuditTrail(3)

	for i := 1; i <= 4; i++ {
		err := audit.Record(ctx, DeliveryRecord{
			SubscriptionID: "sub-audit",
			EventID:        "evt-audit",
			EventType:      "pr.ready_for_review",
			Success:        true,
			Attempts:       1,
			StatusCode:     200,
			Duration:       50 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("unexpected error recording audit: %v", err)
		}
	}

	count, _ := audit.Count(ctx)
	if count != 3 {
		t.Errorf("expected audit count capped at 3, got %d", count)
	}

	byEvent, _ := audit.GetByEvent(ctx, "evt-audit")
	if len(byEvent) != 3 {
		t.Errorf("expected 3 records by event, got %d", len(byEvent))
	}

	_ = audit.Clear(ctx)
	countAfter, _ := audit.Count(ctx)
	if countAfter != 0 {
		t.Errorf("expected 0 count after clear, got %d", countAfter)
	}
}
