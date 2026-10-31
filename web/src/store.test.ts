// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect } from 'vitest';
import { BackgroundVariant } from '@xyflow/react';
import { useFactoryFlowStore } from './store';

describe('useFactoryFlowStore', () => {
  it('initial state has nodes and edges', () => {
    const state = useFactoryFlowStore.getState();
    expect(state.nodes.length).toBeGreaterThanOrEqual(5);
    expect(state.edges.length).toBeGreaterThanOrEqual(4);
  });

  it('addNode and deleteNode', () => {
    const initialCount = useFactoryFlowStore.getState().nodes.length;

    useFactoryFlowStore.getState().addNode({
      nodeType: 'agent',
      role: 'reviewer',
      label: 'Test Reviewer',
      model: 'gpt-4o',
      status: 'pending',
    });

    const stateAfterAdd = useFactoryFlowStore.getState();
    expect(stateAfterAdd.nodes.length).toBe(initialCount + 1);
    const createdId = stateAfterAdd.selectedNodeId;
    expect(createdId).toBeTruthy();

    // Delete the node
    useFactoryFlowStore.getState().deleteNode(createdId!);
    const stateAfterDelete = useFactoryFlowStore.getState();
    expect(stateAfterDelete.nodes.length).toBe(initialCount);
    expect(stateAfterDelete.selectedNodeId).toBeNull();
  });

  it('updateNodeStatus reactive updates for all status badges', () => {
    const firstNode = useFactoryFlowStore.getState().nodes[0];
    expect(firstNode).toBeTruthy();

    const statuses = [
      'pending',
      'running',
      'completed',
      'failed',
      'awaiting_approval',
    ] as const;

    for (const status of statuses) {
      useFactoryFlowStore.getState().updateNodeStatus(firstNode.id, status);
      const updated = useFactoryFlowStore
        .getState()
        .nodes.find((n) => n.id === firstNode.id);
      expect(updated?.data.status).toBe(status);
    }
  });

  it('updateNodeData updates metadata fields', () => {
    const firstNode = useFactoryFlowStore.getState().nodes[0];
    expect(firstNode).toBeTruthy();

    useFactoryFlowStore.getState().updateNodeData(firstNode.id, {
      label: 'Updated Custom Label',
      description: 'Updated description for testing',
    });

    const updated = useFactoryFlowStore
      .getState()
      .nodes.find((n) => n.id === firstNode.id);
    expect(updated?.data.label).toBe('Updated Custom Label');
    expect(updated?.data.description).toBe('Updated description for testing');
  });

  it('backgroundVariant switcher', () => {
    useFactoryFlowStore
      .getState()
      .setBackgroundVariant(BackgroundVariant.Lines);
    expect(useFactoryFlowStore.getState().backgroundVariant).toBe(
      BackgroundVariant.Lines
    );

    useFactoryFlowStore
      .getState()
      .setBackgroundVariant(BackgroundVariant.Cross);
    expect(useFactoryFlowStore.getState().backgroundVariant).toBe(
      BackgroundVariant.Cross
    );
  });

  it('onConnect DAG validation prevents cycle', () => {
    const state = useFactoryFlowStore.getState();
    const edgesCount = state.edges.length;

    // Attempt to connect from trigger to another node in reverse (e.g. agent-spec-writer -> trigger-ingest)
    state.onConnect({
      source: 'agent-spec-writer',
      target: 'trigger-ingest',
      sourceHandle: 'agent-out',
      targetHandle: null,
    });

    // Count should not increase because it connects to a trigger and would form a cycle
    expect(useFactoryFlowStore.getState().edges.length).toBe(edgesCount);
  });
});

