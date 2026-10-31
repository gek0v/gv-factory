// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
	"errors"
	"sync"
)

// NodeStatus represents the execution state of a node in the DAG.
type NodeStatus string

const (
	StatusPending   NodeStatus = "PENDING"
	StatusRunning   NodeStatus = "RUNNING"
	StatusCompleted NodeStatus = "COMPLETED"
	StatusFailed    NodeStatus = "FAILED"
	StatusBlocked   NodeStatus = "BLOCKED"
)

// Node defines a task or reviewer execution unit in the pipeline.
type Node struct {
	ID        string            `json:"id"`
	PluginURI string            `json:"plugin_uri"`
	Inputs    map[string]any    `json:"inputs"`
	DependsOn []string          `json:"depends_on"`
	Status    NodeStatus        `json:"status"`
}

// Graph represents a dynamic workflow pipeline that can mutate at runtime.
type Graph struct {
	ID    string          `json:"id"`
	Nodes map[string]*Node `json:"nodes"`
}

// Engine executes and orchestrates DAG states.
type Engine struct {
	mu     sync.RWMutex
	graphs map[string]*Graph
}

// NewEngine creates an initialized DAG execution engine.
func NewEngine() *Engine {
	return &Engine{
		graphs: make(map[string]*Graph),
	}
}

// RegisterGraph registers a new pipeline graph in the engine.
func (e *Engine) RegisterGraph(g *Graph) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.graphs[g.ID] = g
}

// MutateNode dynamically updates or inserts a node during runtime execution.
func (e *Engine) MutateNode(graphID string, node *Node) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	g, exists := e.graphs[graphID]
	if !exists {
		return errors.New("graph not found")
	}
	g.Nodes[node.ID] = node
	return nil
}

// Step evaluates ready nodes and advances graph execution.
func (e *Engine) Step(ctx context.Context, graphID string) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	_, exists := e.graphs[graphID]
	if !exists {
		return errors.New("graph not found")
	}
	return nil
}
