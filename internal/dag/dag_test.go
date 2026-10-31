// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
	"errors"
	"testing"
)

func TestEngineGraphRegistration(t *testing.T) {
	eng := NewEngine()

	g := &Graph{
		ID: "pipeline-1",
		Nodes: map[string]*Node{
			"spec-writer": {
				ID:        "spec-writer",
				PluginURI: "extism://plugins/spec-writer.wasm",
				Status:    StatusPending,
			},
			"builder": {
				ID:        "builder",
				PluginURI: "extism://plugins/builder-tdd.wasm",
				DependsOn: []string{"spec-writer"},
				Status:    StatusPending,
			},
		},
	}

	eng.RegisterGraph(g)

	if err := eng.Step(context.Background(), "pipeline-1"); err != nil {
		t.Fatalf("unexpected step error: %v", err)
	}

	// Mutate node
	updated := &Node{
		ID:        "builder",
		PluginURI: "extism://plugins/builder-tdd-v2.wasm",
		DependsOn: []string{"spec-writer"},
		Status:    StatusRunning,
	}

	if err := eng.MutateNode("pipeline-1", updated); err != nil {
		t.Fatalf("unexpected error mutating node: %v", err)
	}
}

func TestTopologicalSort_SequentialAndParallel(t *testing.T) {
	// Diamond DAG:
	//       start
	//      /     \
	//   branch-a branch-b
	//      \     /
	//        end
	g := &Graph{
		ID: "diamond",
		Nodes: map[string]*Node{
			"start": {
				ID:     "start",
				Status: StatusPending,
			},
			"branch-a": {
				ID:        "branch-a",
				DependsOn: []string{"start"},
				Status:    StatusPending,
			},
			"branch-b": {
				ID:        "branch-b",
				DependsOn: []string{"start"},
				Status:    StatusPending,
			},
			"end": {
				ID:        "end",
				DependsOn: []string{"branch-a", "branch-b"},
				Status:    StatusPending,
			},
		},
	}

	order, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}

	if len(order) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(order))
	}
	if order[0] != "start" {
		t.Errorf("expected 'start' first, got %s", order[0])
	}
	if order[3] != "end" {
		t.Errorf("expected 'end' last, got %s", order[3])
	}

	// Verify positions
	pos := make(map[string]int)
	for i, id := range order {
		pos[id] = i
	}

	if pos["start"] >= pos["branch-a"] || pos["start"] >= pos["branch-b"] {
		t.Errorf("start must precede branch nodes")
	}
	if pos["branch-a"] >= pos["end"] || pos["branch-b"] >= pos["end"] {
		t.Errorf("branch nodes must precede end")
	}
}

func TestCycleDetection(t *testing.T) {
	t.Run("self-cycle", func(t *testing.T) {
		g := &Graph{
			ID: "self-loop",
			Nodes: map[string]*Node{
				"A": {ID: "A", DependsOn: []string{"A"}},
			},
		}
		err := g.Validate()
		if !errors.Is(err, ErrCyclicDependency) {
			t.Fatalf("expected ErrCyclicDependency, got %v", err)
		}
		_, sortErr := g.TopologicalSort()
		if !errors.Is(sortErr, ErrCyclicDependency) {
			t.Fatalf("expected ErrCyclicDependency from sort, got %v", sortErr)
		}
	})

	t.Run("two-node cycle", func(t *testing.T) {
		g := &Graph{
			ID: "two-cycle",
			Nodes: map[string]*Node{
				"A": {ID: "A", DependsOn: []string{"B"}},
				"B": {ID: "B", DependsOn: []string{"A"}},
			},
		}
		err := g.Validate()
		if !errors.Is(err, ErrCyclicDependency) {
			t.Fatalf("expected ErrCyclicDependency, got %v", err)
		}
	})

	t.Run("three-node indirect cycle", func(t *testing.T) {
		g := &Graph{
			ID: "three-cycle",
			Nodes: map[string]*Node{
				"A": {ID: "A", DependsOn: []string{"B"}},
				"B": {ID: "B", DependsOn: []string{"C"}},
				"C": {ID: "C", DependsOn: []string{"A"}},
			},
		}
		err := g.Validate()
		if !errors.Is(err, ErrCyclicDependency) {
			t.Fatalf("expected ErrCyclicDependency, got %v", err)
		}
	})

	t.Run("missing dependency", func(t *testing.T) {
		g := &Graph{
			ID: "missing-dep",
			Nodes: map[string]*Node{
				"A": {ID: "A", DependsOn: []string{"NON_EXISTENT"}},
			},
		}
		err := g.Validate()
		if !errors.Is(err, ErrDependencyMissing) {
			t.Fatalf("expected ErrDependencyMissing, got %v", err)
		}
	})
}

