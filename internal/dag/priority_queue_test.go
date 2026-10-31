// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestPriorityQueue_PriorityOrder(t *testing.T) {
	pq := NewPriorityQueue(PriorityQueueConfig{})

	now := time.Now()
	pq.Push(&QueueItem{
		RunID:        "run-normal",
		BasePriority: PriorityNormal,
		QueuedAt:     now,
	})
	pq.Push(&QueueItem{
		RunID:        "run-low",
		BasePriority: PriorityLow,
		QueuedAt:     now,
	})
	pq.Push(&QueueItem{
		RunID:        "run-high",
		BasePriority: PriorityHigh,
		QueuedAt:     now,
	})

	if pq.Len() != 3 {
		t.Fatalf("expected queue length 3, got %d", pq.Len())
	}

	first := pq.Pop()
	if first == nil || first.RunID != "run-high" {
		t.Fatalf("expected run-high first, got %v", first)
	}

	second := pq.Pop()
	if second == nil || second.RunID != "run-normal" {
		t.Fatalf("expected run-normal second, got %v", second)
	}

	third := pq.Pop()
	if third == nil || third.RunID != "run-low" {
		t.Fatalf("expected run-low third, got %v", third)
	}

	if pq.Pop() != nil {
		t.Fatalf("expected queue to be empty")
	}
}

func TestPriorityQueue_TemplateDefaultPriorities(t *testing.T) {
	if p := TemplateDefaultPriority("bugfix-express"); p != PriorityHigh {
		t.Errorf("expected PriorityHigh for bugfix-express, got %s", p)
	}
	if p := TemplateDefaultPriority("feature-complete"); p != PriorityNormal {
		t.Errorf("expected PriorityNormal for feature-complete, got %s", p)
	}
	if p := TemplateDefaultPriority("refactor-doc"); p != PriorityLow {
		t.Errorf("expected PriorityLow for refactor-doc, got %s", p)
	}
}

func TestPriorityQueue_StarvationPrevention(t *testing.T) {
	// Configure with noticeable skip boost (50 points per skip)
	cfg := PriorityQueueConfig{
		AgingInterval: 100 * time.Millisecond,
		AgingBoost:    20,
		SkipBoost:     50,
	}
	pq := NewPriorityQueue(cfg)

	// Low priority task (base score 100)
	lowItem := &QueueItem{
		RunID:        "starved-low",
		BasePriority: PriorityLow, // 100
		QueuedAt:     time.Now().Add(-300 * time.Millisecond), // 3 intervals * 20 = 60
	}
	pq.Push(lowItem)

	// Dispatch 3 high priority tasks
	for i := 1; i <= 3; i++ {
		highItem := &QueueItem{
			RunID:        fmt.Sprintf("high-%d", i),
			BasePriority: PriorityHigh, // 300
			QueuedAt:     time.Now(),
		}
		pq.Push(highItem)

		// High priority is popped
		popped := pq.Pop()
		if popped.RunID != highItem.RunID {
			t.Fatalf("expected %s popped, got %s", highItem.RunID, popped.RunID)
		}
		// Notify dispatch of high item -> low item gets skip boost (+50 each time)
		pq.NotifyDispatch(popped.RunID)
	}

	// At this point, lowItem has 3 skips:
	// base 100 + (3 * 50 = 150) + aging (>=60) = >= 310 effective priority!
	// Now enqueue a brand new high item (base 300)
	brandNewHigh := &QueueItem{
		RunID:        "brand-new-high",
		BasePriority: PriorityHigh, // 300
		QueuedAt:     time.Now(),
	}
	pq.Push(brandNewHigh)

	// The low item should now beat the brand new high item because of starvation prevention!
	next := pq.Pop()
	if next == nil || next.RunID != "starved-low" {
		t.Fatalf("expected starved-low to be prioritized due to starvation boost, got %v", next)
	}

	nextHigh := pq.Pop()
	if nextHigh == nil || nextHigh.RunID != "brand-new-high" {
		t.Fatalf("expected brand-new-high second, got %v", nextHigh)
	}
}

func TestPriorityQueue_Remove(t *testing.T) {
	pq := NewPriorityQueue(PriorityQueueConfig{})

	pq.Push(&QueueItem{RunID: "item-1", BasePriority: PriorityNormal})
	pq.Push(&QueueItem{RunID: "item-2", BasePriority: PriorityHigh})
	pq.Push(&QueueItem{RunID: "item-3", BasePriority: PriorityLow})

	removed := pq.Remove("item-2")
	if removed == nil || removed.RunID != "item-2" {
		t.Fatalf("expected to remove item-2, got %v", removed)
	}
	if pq.Len() != 2 {
		t.Fatalf("expected length 2, got %d", pq.Len())
	}

	// Removing non-existent returns nil
	if pq.Remove("non-existent") != nil {
		t.Fatalf("expected nil when removing non-existent item")
	}

	// Next popped should be item-1
	next := pq.Pop()
	if next == nil || next.RunID != "item-1" {
		t.Fatalf("expected item-1 next, got %v", next)
	}
}

func TestPriorityQueue_ConcurrentRace(t *testing.T) {
	pq := NewPriorityQueue(PriorityQueueConfig{
		AgingInterval: 5 * time.Millisecond,
		AgingBoost:    5,
		SkipBoost:     10,
	})

	const numWorkers = 8
	const opsPerWorker = 50
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				runID := fmt.Sprintf("run-%d-%d", workerID, i)
				pq.Push(&QueueItem{
					RunID:        runID,
					BasePriority: Priority(100 + (i%3)*100),
					QueuedAt:     time.Now(),
				})
				_ = pq.Peek()
				_ = pq.All()
				popped := pq.Pop()
				if popped != nil {
					pq.NotifyDispatch(popped.RunID)
				}
			}
		}(w)
	}

	wg.Wait()
}
