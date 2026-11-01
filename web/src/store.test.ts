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

  it('activeRunId and activeRunState lifecycle tracking', () => {
    const store = useFactoryFlowStore.getState();
    expect(store.activeRunId).toBeNull();
    expect(store.activeRunState).toBeNull();

    // Set active run with state
    useFactoryFlowStore.getState().setActiveRun('run-xyz-42', 'RUNNING');
    expect(useFactoryFlowStore.getState().activeRunId).toBe('run-xyz-42');
    expect(useFactoryFlowStore.getState().activeRunState).toBe('RUNNING');

    // Update state transition to COMPLETED
    useFactoryFlowStore.getState().setActiveRun('run-xyz-42', 'COMPLETED');
    expect(useFactoryFlowStore.getState().activeRunState).toBe('COMPLETED');

    // Clear active run
    useFactoryFlowStore.getState().setActiveRun(null);
    expect(useFactoryFlowStore.getState().activeRunId).toBeNull();
    expect(useFactoryFlowStore.getState().activeRunState).toBeNull();
  });

  it('appendNodeLog reactive updates logs by node.id or stepId', () => {
    const firstNode = useFactoryFlowStore.getState().nodes[0];
    expect(firstNode).toBeTruthy();

    useFactoryFlowStore.getState().clearNodeLogs(firstNode.id);
    expect(useFactoryFlowStore.getState().nodes.find((n) => n.id === firstNode.id)?.data.logs).toEqual([]);

    useFactoryFlowStore.getState().appendNodeLog(firstNode.id, 'stdout', 'Starting Extism WASM guest plugin');
    useFactoryFlowStore.getState().appendNodeLog(firstNode.id, 'stderr', 'Warning: memory limit approaching', '2026-11-01T12:00:00Z');

    const updatedNode = useFactoryFlowStore.getState().nodes.find((n) => n.id === firstNode.id);
    expect(updatedNode?.data.logs?.length).toBe(2);
    expect(updatedNode?.data.logs?.[0].stream).toBe('stdout');
    expect(updatedNode?.data.logs?.[0].message).toBe('Starting Extism WASM guest plugin');
    expect(updatedNode?.data.logs?.[1].stream).toBe('stderr');
    expect(updatedNode?.data.logs?.[1].timestamp).toBe('2026-11-01T12:00:00Z');
  });

  it('updateNodeTelemetry updates durations, tokens, and outputs', () => {
    const firstNode = useFactoryFlowStore.getState().nodes[0];
    expect(firstNode).toBeTruthy();

    useFactoryFlowStore.getState().updateNodeTelemetry(firstNode.id, {
      status: 'completed',
      executionDurationMs: 1450,
      tokensUsed: 3500,
      promptTokens: 2500,
      completionTokens: 1000,
      startedAt: '2026-11-01T10:00:00Z',
      completedAt: '2026-11-01T10:00:01.450Z',
    });

    const updated = useFactoryFlowStore.getState().nodes.find((n) => n.id === firstNode.id);
    expect(updated?.data.status).toBe('completed');
    expect(updated?.data.executionDurationMs).toBe(1450);
    expect(updated?.data.tokensUsed).toBe(3500);
    expect(updated?.data.promptTokens).toBe(2500);
    expect(updated?.data.completionTokens).toBe(1000);
  });

  it('manages worker dashboard and workerPool state', () => {
    const store = useFactoryFlowStore.getState();
    expect(store.showWorkerDashboard).toBe(false);
    expect(store.workerPool).toBeNull();

    store.setShowWorkerDashboard(true);
    expect(useFactoryFlowStore.getState().showWorkerDashboard).toBe(true);

    store.updateWorkerHeartbeat({
      worker_id: 'test-w1',
      active_tasks: 3,
      max_concurrency: 6,
      uptime_seconds: 120,
      timestamp: 1761999000,
    });

    const pool = useFactoryFlowStore.getState().workerPool;
    expect(pool).not.toBeNull();
    expect(pool?.total_workers).toBe(1);
    expect(pool?.total_capacity).toBe(6);
    expect(pool?.total_active_tasks).toBe(3);
    expect(pool?.workers[0].worker_id).toBe('test-w1');
  });
});

