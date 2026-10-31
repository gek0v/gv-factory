// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

var (
	// ErrCyclicDependency is returned when a cycle is detected in the DAG.
	ErrCyclicDependency = errors.New("cyclic dependency detected")

	// ErrGraphNotFound is returned when the requested graph does not exist in the engine.
	ErrGraphNotFound = errors.New("graph not found")

	// ErrNodeNotFound is returned when a node does not exist in the graph.
	ErrNodeNotFound = errors.New("node not found")

	// ErrInvalidTransition is returned when an illegal state transition is attempted on a node.
	ErrInvalidTransition = errors.New("invalid state transition")

	// ErrDependencyMissing is returned when a node depends on a non-existent node ID.
	ErrDependencyMissing = errors.New("dependency not found in graph")
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
	ID        string         `json:"id"`
	PluginURI string         `json:"plugin_uri"`
	Inputs    map[string]any `json:"inputs"`
	DependsOn []string       `json:"depends_on"`
	Status    NodeStatus     `json:"status"`
}

// Clone creates a shallow-to-medium copy of Node suitable for safe inspection.
func (n *Node) Clone() *Node {
	if n == nil {
		return nil
	}
	deps := make([]string, len(n.DependsOn))
	copy(deps, n.DependsOn)

	var inputs map[string]any
	if n.Inputs != nil {
		inputs = make(map[string]any, len(n.Inputs))
		for k, v := range n.Inputs {
			inputs[k] = v
		}
	}

	return &Node{
		ID:        n.ID,
		PluginURI: n.PluginURI,
		Inputs:    inputs,
		DependsOn: deps,
		Status:    n.Status,
	}
}

// Graph represents a dynamic workflow pipeline that can mutate at runtime.
type Graph struct {
	ID    string           `json:"id"`
	Nodes map[string]*Node `json:"nodes"`
}

// Validate detects cycles and missing dependencies in the graph.
func (g *Graph) Validate() error {
	if g == nil || len(g.Nodes) == 0 {
		return nil
	}

	for _, node := range g.Nodes {
		for _, depID := range node.DependsOn {
			if depID == node.ID {
				return fmt.Errorf("%w: node %q depends on itself", ErrCyclicDependency, node.ID)
			}
			if _, exists := g.Nodes[depID]; !exists {
				return fmt.Errorf("%w: node %q depends on missing %q", ErrDependencyMissing, node.ID, depID)
			}
		}
	}

	state := make(map[string]int, len(g.Nodes))
	var dfs func(nodeID string) error
	dfs = func(nodeID string) error {
		state[nodeID] = 1
		node := g.Nodes[nodeID]
		if node != nil {
			for _, depID := range node.DependsOn {
				if state[depID] == 1 {
					return fmt.Errorf("%w: cycle involving node %q and dependency %q", ErrCyclicDependency, nodeID, depID)
				}
				if state[depID] == 0 {
					if err := dfs(depID); err != nil {
						return err
					}
				}
			}
		}
		state[nodeID] = 2
		return nil
	}

	for id := range g.Nodes {
		if state[id] == 0 {
			if err := dfs(id); err != nil {
				return err
			}
		}
	}

	return nil
}

// TopologicalSort returns the node IDs in valid topological execution order.
func (g *Graph) TopologicalSort() ([]string, error) {
	if g == nil || len(g.Nodes) == 0 {
		return []string{}, nil
	}

	if err := g.Validate(); err != nil {
		return nil, err
	}

	inDegree := make(map[string]int, len(g.Nodes))
	dependents := make(map[string][]string, len(g.Nodes))

	for id, node := range g.Nodes {
		inDegree[id] = len(node.DependsOn)
		for _, depID := range node.DependsOn {
			dependents[depID] = append(dependents[depID], id)
		}
	}

	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)

	var order []string
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		order = append(order, curr)

		var newlyReady []string
		for _, next := range dependents[curr] {
			inDegree[next]--
			if inDegree[next] == 0 {
				newlyReady = append(newlyReady, next)
			}
		}
		sort.Strings(newlyReady)
		queue = append(queue, newlyReady...)
	}

	if len(order) != len(g.Nodes) {
		return nil, ErrCyclicDependency
	}

	return order, nil
}

// GetReadyNodes returns all nodes whose status is PENDING and whose dependencies are all COMPLETED.
func (g *Graph) GetReadyNodes() []*Node {
	if g == nil {
		return nil
	}

	var ready []*Node
	for _, node := range g.Nodes {
		if node.Status != StatusPending {
			continue
		}

		allCompleted := true
		for _, depID := range node.DependsOn {
			depNode, exists := g.Nodes[depID]
			if !exists || depNode.Status != StatusCompleted {
				allCompleted = false
				break
			}
		}

		if allCompleted {
			ready = append(ready, node)
		}
	}

	sort.Slice(ready, func(i, j int) bool {
		return ready[i].ID < ready[j].ID
	})

	return ready
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

// Validate checks whether a registered graph has cycles or invalid dependencies.
func (e *Engine) Validate(graphID string) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	g, exists := e.graphs[graphID]
	if !exists {
		return ErrGraphNotFound
	}
	return g.Validate()
}

// TopologicalSort returns node IDs in valid execution order for the specified graph.
func (e *Engine) TopologicalSort(graphID string) ([]string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	g, exists := e.graphs[graphID]
	if !exists {
		return nil, ErrGraphNotFound
	}
	return g.TopologicalSort()
}

// GetReadyNodes returns all pending nodes whose dependencies are all completed.
func (e *Engine) GetReadyNodes(graphID string) ([]*Node, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	g, exists := e.graphs[graphID]
	if !exists {
		return nil, ErrGraphNotFound
	}

	ready := g.GetReadyNodes()
	result := make([]*Node, len(ready))
	for i, n := range ready {
		result[i] = n.Clone()
	}
	return result, nil
}

// GetGraph retrieves a snapshot of the graph.
func (e *Engine) GetGraph(graphID string) (*Graph, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	g, exists := e.graphs[graphID]
	if !exists {
		return nil, ErrGraphNotFound
	}

	clonedNodes := make(map[string]*Node, len(g.Nodes))
	for k, v := range g.Nodes {
		clonedNodes[k] = v.Clone()
	}

	return &Graph{
		ID:    g.ID,
		Nodes: clonedNodes,
	}, nil
}

// GetNode retrieves a specific node from a graph.
func (e *Engine) GetNode(graphID, nodeID string) (*Node, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	g, exists := e.graphs[graphID]
	if !exists {
		return nil, ErrGraphNotFound
	}

	n, exists := g.Nodes[nodeID]
	if !exists {
		return nil, ErrNodeNotFound
	}

	return n.Clone(), nil
}

// IsValidTransition checks whether a state transition is legal according to the DAG state machine.
func IsValidTransition(from, to NodeStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case StatusPending:
		return to == StatusRunning || to == StatusBlocked || to == StatusFailed
	case StatusRunning:
		return to == StatusCompleted || to == StatusFailed
	case StatusFailed:
		return to == StatusPending
	case StatusBlocked:
		return to == StatusPending
	default:
		return false
	}
}

// SetNodeStatus performs a thread-safe state transition on a node.
func (e *Engine) SetNodeStatus(graphID, nodeID string, status NodeStatus) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	g, exists := e.graphs[graphID]
	if !exists {
		return ErrGraphNotFound
	}

	node, exists := g.Nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	if !IsValidTransition(node.Status, status) {
		return fmt.Errorf("%w: cannot transition from %s to %s", ErrInvalidTransition, node.Status, status)
	}

	node.Status = status
	return nil
}

// MutateNode dynamically updates or inserts a node during runtime execution.
// It verifies that adding or updating this node does not introduce any cyclic dependency
// or invalid references. If validation fails, changes are completely rolled back.
func (e *Engine) MutateNode(graphID string, node *Node) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	g, exists := e.graphs[graphID]
	if !exists {
		return ErrGraphNotFound
	}

	if node == nil || node.ID == "" {
		return errors.New("node and node ID cannot be empty")
	}

	previousNode, hadPrevious := g.Nodes[node.ID]
	g.Nodes[node.ID] = node

	if err := g.Validate(); err != nil {
		if hadPrevious {
			g.Nodes[node.ID] = previousNode
		} else {
			delete(g.Nodes, node.ID)
		}
		return err
	}

	return nil
}

// Step evaluates ready nodes and advances graph execution.
// It checks for pending nodes whose dependencies have failed or become blocked,
// and marks those pending nodes as StatusBlocked.
func (e *Engine) Step(ctx context.Context, graphID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	g, exists := e.graphs[graphID]
	if !exists {
		return ErrGraphNotFound
	}

	for _, node := range g.Nodes {
		if node.Status != StatusPending {
			continue
		}
		for _, depID := range node.DependsOn {
			depNode, exists := g.Nodes[depID]
			if exists && (depNode.Status == StatusFailed || depNode.Status == StatusBlocked) {
				node.Status = StatusBlocked
				break
			}
		}
	}

	return nil
}
