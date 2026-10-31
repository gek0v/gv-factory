// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestGetReadyNodes_Progression(t *testing.T) {
	eng := NewEngine()

	g := &Graph{
		ID: "pipeline-progression",
		Nodes: map[string]*Node{
			"step-1": {
				ID:     "step-1",
				Status: StatusPending,
			},
			"step-2": {
				ID:        "step-2",
				DependsOn: []string{"step-1"},
				Status:    StatusPending,
			},
			"step-3": {
				ID:        "step-3",
				DependsOn: []string{"step-2"},
				Status:    StatusPending,
			},
		},
	}
	eng.RegisterGraph(g)

	// Step 1: Only step-1 should be ready
	ready, err := eng.GetReadyNodes("pipeline-progression")
	if err != nil {
		t.Fatalf("GetReadyNodes failed: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != "step-1" {
		t.Fatalf("expected only step-1 ready, got %+v", ready)
	}

	// Transition step-1: PENDING -> RUNNING -> COMPLETED
	if err := eng.SetNodeStatus("pipeline-progression", "step-1", StatusRunning); err != nil {
		t.Fatalf("failed transition to RUNNING: %v", err)
	}
	// While RUNNING, step-2 is NOT ready
	ready, _ = eng.GetReadyNodes("pipeline-progression")
	if len(ready) != 0 {
		t.Fatalf("expected 0 ready nodes while step-1 is running, got %d", len(ready))
	}

	// Complete step-1
	if err := eng.SetNodeStatus("pipeline-progression", "step-1", StatusCompleted); err != nil {
		t.Fatalf("failed transition to COMPLETED: %v", err)
	}

	// Now step-2 should be ready
	ready, _ = eng.GetReadyNodes("pipeline-progression")
	if len(ready) != 1 || ready[0].ID != "step-2" {
		t.Fatalf("expected step-2 ready, got %+v", ready)
	}
}

func TestStateTransitions_ValidAndInvalid(t *testing.T) {
	eng := NewEngine()
	g := &Graph{
		ID: "state-test",
		Nodes: map[string]*Node{
			"task-a": {ID: "task-a", Status: StatusPending},
		},
	}
	eng.RegisterGraph(g)

	// Illegal: PENDING -> COMPLETED directly without running
	err := eng.SetNodeStatus("state-test", "task-a", StatusCompleted)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition for PENDING->COMPLETED, got %v", err)
	}

	// Legal: PENDING -> RUNNING
	if err := eng.SetNodeStatus("state-test", "task-a", StatusRunning); err != nil {
		t.Fatalf("expected success for PENDING->RUNNING, got %v", err)
	}

	// Legal: RUNNING -> FAILED
	if err := eng.SetNodeStatus("state-test", "task-a", StatusFailed); err != nil {
		t.Fatalf("expected success for RUNNING->FAILED, got %v", err)
	}

	// Legal: FAILED -> PENDING (retry)
	if err := eng.SetNodeStatus("state-test", "task-a", StatusPending); err != nil {
		t.Fatalf("expected success for FAILED->PENDING retry, got %v", err)
	}
}

func TestMutateNode_DynamicRollbackOnCycle(t *testing.T) {
	eng := NewEngine()
	g := &Graph{
		ID: "dynamic-graph",
		Nodes: map[string]*Node{
			"node-a": {ID: "node-a", Status: StatusPending},
			"node-b": {ID: "node-b", DependsOn: []string{"node-a"}, Status: StatusPending},
		},
	}
	eng.RegisterGraph(g)

	// Mutate node-a to depend on node-b (would create cycle: a -> b -> a)
	badNode := &Node{
		ID:        "node-a",
		DependsOn: []string{"node-b"},
		Status:    StatusPending,
	}

	err := eng.MutateNode("dynamic-graph", badNode)
	if !errors.Is(err, ErrCyclicDependency) {
		t.Fatalf("expected ErrCyclicDependency, got %v", err)
	}

	// Verify rollback: node-a should still have empty DependsOn
	savedA, err := eng.GetNode("dynamic-graph", "node-a")
	if err != nil {
		t.Fatalf("failed to get node-a: %v", err)
	}
	if len(savedA.DependsOn) != 0 {
		t.Fatalf("expected node-a to be rolled back to 0 dependencies, got %v", savedA.DependsOn)
	}

	// Valid mutation: add node-c depending on node-b
	goodNode := &Node{
		ID:        "node-c",
		DependsOn: []string{"node-b"},
		Status:    StatusPending,
	}
	if err := eng.MutateNode("dynamic-graph", goodNode); err != nil {
		t.Fatalf("expected valid mutation to succeed, got %v", err)
	}

	order, err := eng.TopologicalSort("dynamic-graph")
	if err != nil {
		t.Fatalf("topological sort failed after mutation: %v", err)
	}
	if len(order) != 3 || order[2] != "node-c" {
		t.Fatalf("expected 3 nodes with node-c last, got %v", order)
	}
}

func TestStep_EvaluationAndBlocking(t *testing.T) {
	eng := NewEngine()
	g := &Graph{
		ID: "step-test",
		Nodes: map[string]*Node{
			"task-1": {ID: "task-1", Status: StatusPending},
			"task-2": {ID: "task-2", DependsOn: []string{"task-1"}, Status: StatusPending},
		},
	}
	eng.RegisterGraph(g)

	ctx := context.Background()

	// Initial Step(): task-1 should become RUNNING
	if err := eng.Step(ctx, "step-test"); err != nil {
		t.Fatalf("Step failed: %v", err)
	}
	t1, _ := eng.GetNode("step-test", "task-1")
	if t1.Status != StatusRunning {
		t.Fatalf("expected task-1 to be RUNNING, got %s", t1.Status)
	}

	// Fail task-1
	_ = eng.SetNodeStatus("step-test", "task-1", StatusFailed)

	// Step() should notice failed dependency and mark task-2 as BLOCKED
	if err := eng.Step(ctx, "step-test"); err != nil {
		t.Fatalf("Step failed: %v", err)
	}
	t2, _ := eng.GetNode("step-test", "task-2")
	if t2.Status != StatusBlocked {
		t.Fatalf("expected task-2 to be BLOCKED, got %s", t2.Status)
	}
}

func TestEngine_ConcurrentAccessRace(t *testing.T) {
	eng := NewEngine()
	g := &Graph{
		ID: "concurrent-graph",
		Nodes: map[string]*Node{
			"root": {ID: "root", Status: StatusCompleted},
		},
	}
	eng.RegisterGraph(g)

	const workerCount = 20
	var wg sync.WaitGroup
	wg.Add(workerCount)

	for i := 0; i < workerCount; i++ {
		go func(idx int) {
			defer wg.Done()
			node := &Node{
				ID:        string(rune('A' + idx)),
				DependsOn: []string{"root"},
				Status:    StatusPending,
			}
			_ = eng.MutateNode("concurrent-graph", node)
			_, _ = eng.GetReadyNodes("concurrent-graph")
			_, _ = eng.TopologicalSort("concurrent-graph")
			_ = eng.Step(context.Background(), "concurrent-graph")
		}(i)
	}

	wg.Wait()
}
