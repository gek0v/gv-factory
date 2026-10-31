// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect } from 'vitest';
import type { Edge, Node } from '@xyflow/react';
import type { FactoryNodeData } from '../types/flow';
import { wouldCreateCycle, validateConnection } from './validation';

describe('validation - wouldCreateCycle', () => {
  it('detects self loops', () => {
    const edges: Edge[] = [];
    expect(wouldCreateCycle(edges, 'node-a', 'node-a')).toBe(true);
  });

  it('detects direct 2-node cycles', () => {
    const edges: Edge[] = [{ id: 'e1', source: 'node-a', target: 'node-b' }];
    expect(wouldCreateCycle(edges, 'node-b', 'node-a')).toBe(true);
    expect(wouldCreateCycle(edges, 'node-a', 'node-c')).toBe(false);
  });

  it('detects indirect cycles across transitive chains', () => {
    const edges: Edge[] = [
      { id: 'e1', source: 'a', target: 'b' },
      { id: 'e2', source: 'b', target: 'c' },
      { id: 'e3', source: 'c', target: 'd' },
    ];
    // d -> a creates a cycle
    expect(wouldCreateCycle(edges, 'd', 'a')).toBe(true);
    // d -> b creates a cycle
    expect(wouldCreateCycle(edges, 'd', 'b')).toBe(true);
    // a -> d (feed forward) does not create a cycle
    expect(wouldCreateCycle(edges, 'a', 'd')).toBe(false);
  });
});

describe('validation - validateConnection', () => {
  it('prohibits self loops', () => {
    const nodes: Node<FactoryNodeData>[] = [
      {
        id: 'agent-1',
        position: { x: 0, y: 0 },
        data: {
          nodeType: 'agent',
          role: 'builder_tdd',
          label: 'Builder',
          model: 'gpt-4o',
          status: 'pending',
        },
      },
    ];
    const edges: Edge[] = [];

    const res = validateConnection(
      { source: 'agent-1', target: 'agent-1', sourceHandle: null, targetHandle: null },
      nodes,
      edges
    );
    expect(res.isValid).toBe(false);
    expect(res.reason).toMatch(/Self-connecting loops are prohibited/);
  });

  it('prohibits incoming connections to trigger nodes', () => {
    const nodes: Node<FactoryNodeData>[] = [
      {
        id: 'agent-1',
        position: { x: 0, y: 0 },
        data: {
          nodeType: 'agent',
          role: 'builder_tdd',
          label: 'Builder',
          model: 'gpt-4o',
          status: 'pending',
        },
      },
      {
        id: 'trigger-1',
        position: { x: 100, y: 0 },
        data: {
          nodeType: 'trigger',
          subtype: 'webhook',
          label: 'Webhook',
          status: 'pending',
        },
      },
    ];
    const edges: Edge[] = [];

    const res = validateConnection(
      { source: 'agent-1', target: 'trigger-1', sourceHandle: null, targetHandle: null },
      nodes,
      edges
    );
    expect(res.isValid).toBe(false);
    expect(res.reason).toMatch(/Trigger nodes cannot receive input connections/);
  });

  it('prohibits duplicate connections', () => {
    const nodes: Node<FactoryNodeData>[] = [
      {
        id: 'agent-1',
        position: { x: 0, y: 0 },
        data: {
          nodeType: 'agent',
          role: 'builder_tdd',
          label: 'Builder',
          model: 'gpt-4o',
          status: 'pending',
        },
      },
      {
        id: 'action-1',
        position: { x: 100, y: 0 },
        data: {
          nodeType: 'action',
          subtype: 'git_commit',
          label: 'Commit',
          status: 'pending',
        },
      },
    ];
    const edges: Edge[] = [
      {
        id: 'e1',
        source: 'agent-1',
        target: 'action-1',
        sourceHandle: 'out',
        targetHandle: 'in',
      },
    ];

    const res = validateConnection(
      { source: 'agent-1', target: 'action-1', sourceHandle: 'out', targetHandle: 'in' },
      nodes,
      edges
    );
    expect(res.isValid).toBe(false);
    expect(res.reason).toMatch(/already exists/);
  });

  it('accepts valid forward connections', () => {
    const nodes: Node<FactoryNodeData>[] = [
      {
        id: 'agent-1',
        position: { x: 0, y: 0 },
        data: {
          nodeType: 'agent',
          role: 'builder_tdd',
          label: 'Builder',
          model: 'gpt-4o',
          status: 'pending',
        },
      },
      {
        id: 'action-1',
        position: { x: 100, y: 0 },
        data: {
          nodeType: 'action',
          subtype: 'git_commit',
          label: 'Commit',
          status: 'pending',
        },
      },
    ];
    const edges: Edge[] = [];

    const res = validateConnection(
      { source: 'agent-1', target: 'action-1', sourceHandle: 'out', targetHandle: 'in' },
      nodes,
      edges
    );
    expect(res.isValid).toBe(true);
  });
});

