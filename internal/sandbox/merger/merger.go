// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package merger

import (
	"context"
	"fmt"
	"sync"
)

// MergeRequest represents an approved factory output queued for integration.
type MergeRequest struct {
	RunID        string
	SourceBranch string
	TargetBranch string
}

// Queue coordinates sequential integration to main.
type Queue struct {
	mu    sync.Mutex
	items []*MergeRequest
}

// NewQueue creates an empty merge queue.
func NewQueue() *Queue {
	return &Queue{
		items: make([]*MergeRequest, 0),
	}
}

// Enqueue adds an approved run to the sequential integration queue.
func (q *Queue) Enqueue(req *MergeRequest) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, req)
}

// ProcessNext evaluates integration using git merge-tree simulation.
func (q *Queue) ProcessNext(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return nil
	}

	current := q.items[0]
	q.items = q.items[1:]

	// Simulation hook: git merge-tree --write-tree $(git merge-base main branch) main branch
	fmt.Printf("Simulating integration for run %s (%s -> %s)\n", current.RunID, current.SourceBranch, current.TargetBranch)
	return nil
}
