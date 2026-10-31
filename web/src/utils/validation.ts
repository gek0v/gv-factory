// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import type { Connection, Edge, Node } from '@xyflow/react';
import type { FactoryNodeData } from '../types/flow';

/**
 * Checks if adding an edge from source to target would create a cycle in the DAG.
 * Uses Depth-First Search (DFS) traversing outgoing edges starting from `target`.
 * If `source` is reached, a cycle would be formed.
 */
export const wouldCreateCycle = (
  edges: Edge[],
  sourceId: string,
  targetId: string
): boolean => {
  if (sourceId === targetId) {
    return true;
  }

  // Build adjacency list of current graph
  const adj = new Map<string, string[]>();
  for (const edge of edges) {
    if (!adj.has(edge.source)) {
      adj.set(edge.source, []);
    }
    adj.get(edge.source)!.push(edge.target);
  }

  // Traverse from targetId to see if we can reach sourceId
  const visited = new Set<string>();
  const stack = [targetId];

  while (stack.length > 0) {
    const current = stack.pop()!;
    if (current === sourceId) {
      return true;
    }

    if (!visited.has(current)) {
      visited.add(current);
      const neighbors = adj.get(current) || [];
      for (const neighbor of neighbors) {
        if (!visited.has(neighbor)) {
          stack.push(neighbor);
        }
      }
    }
  }

  return false;
};

/**
 * Validates whether a proposed connection is valid according to DAG rules
 * and typed node constraints.
 */
export const validateConnection = (
  connection: Connection | Edge,
  nodes: Node<FactoryNodeData>[],
  edges: Edge[]
): { isValid: boolean; reason?: string } => {
  const { source, target, sourceHandle, targetHandle } = connection;

  if (!source || !target) {
    return { isValid: false, reason: 'Source or target is missing.' };
  }

  // 1. Prohibit self-connections
  if (source === target) {
    return { isValid: false, reason: 'Self-connecting loops are prohibited in DAG.' };
  }

  // 2. Check for duplicate connections
  const alreadyExists = edges.some(
    (e) =>
      e.source === source &&
      e.target === target &&
      e.sourceHandle === sourceHandle &&
      e.targetHandle === targetHandle
  );
  if (alreadyExists) {
    return { isValid: false, reason: 'Connection already exists.' };
  }

  // 3. Typed validation based on node types
  const sourceNode = nodes.find((n) => n.id === source);
  const targetNode = nodes.find((n) => n.id === target);

  if (!sourceNode || !targetNode) {
    return { isValid: false, reason: 'Source or target node not found.' };
  }

  if (targetNode.data?.nodeType === 'trigger') {
    return {
      isValid: false,
      reason: 'Trigger nodes cannot receive input connections.',
    };
  }

  // 4. DAG Cycle Detection
  if (wouldCreateCycle(edges, source, target)) {
    return {
      isValid: false,
      reason: 'Connection rejected: would introduce a circular dependency (cycle) in the DAG.',
    };
  }

  return { isValid: true };
};

