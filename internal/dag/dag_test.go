// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
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
		Status:    StatusRunning,
	}

	if err := eng.MutateNode("pipeline-1", updated); err != nil {
		t.Fatalf("unexpected error mutating node: %v", err)
	}
}
