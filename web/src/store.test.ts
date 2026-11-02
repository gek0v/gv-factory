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

  it('appendNodeLog splits multiline chunk messages into individual log entries', () => {
    const firstNode = useFactoryFlowStore.getState().nodes[0];
    expect(firstNode).toBeTruthy();

    useFactoryFlowStore.getState().clearNodeLogs(firstNode.id);
    const chunk = 'Line 1: building module\nLine 2: downloading crates\r\nLine 3: finished\n';
    useFactoryFlowStore.getState().appendNodeLog(firstNode.id, 'stdout', chunk, '2026-11-02T10:00:00Z');

    const updatedNode = useFactoryFlowStore.getState().nodes.find((n) => n.id === firstNode.id);
    expect(updatedNode?.data.logs?.length).toBe(3);
    expect(updatedNode?.data.logs?.[0].message).toBe('Line 1: building module');
    expect(updatedNode?.data.logs?.[1].message).toBe('Line 2: downloading crates');
    expect(updatedNode?.data.logs?.[2].message).toBe('Line 3: finished');
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

  describe('Live Factory Execution store state and stage illumination', () => {
    it('manages showFactoryRunModal state', () => {
      const store = useFactoryFlowStore.getState();
      expect(store.showFactoryRunModal).toBe(false);

      store.setShowFactoryRunModal(true);
      expect(useFactoryFlowStore.getState().showFactoryRunModal).toBe(true);

      store.setShowFactoryRunModal(false);
      expect(useFactoryFlowStore.getState().showFactoryRunModal).toBe(false);
    });

    it('highlights SpecWriter / Ingestion node when activePipelineStage is spec_generated', () => {
      const store = useFactoryFlowStore.getState();
      expect(store.activePipelineStage).toBe('idle');

      store.setActivePipelineStage('spec_generated');
      const nodes = useFactoryFlowStore.getState().nodes;

      const specNode = nodes.find(
        (n) => n.id.includes('spec') || (n.data.label || '').includes('Spec')
      );
      expect(specNode).toBeDefined();
      expect(specNode?.data.highlighted).toBe(true);
      expect(specNode?.selected).toBe(true);

      const builderNode = nodes.find(
        (n) => n.id.includes('builder') || (n.data.label || '').includes('Builder')
      );
      expect(builderNode?.data.highlighted).toBe(false);
    });

    it('highlights Builder / TDD node when activePipelineStage is building', () => {
      const store = useFactoryFlowStore.getState();
      store.setActivePipelineStage('building');
      const nodes = useFactoryFlowStore.getState().nodes;

      const builderNode = nodes.find(
        (n) => n.id.includes('builder') || (n.data.label || '').includes('Builder')
      );
      expect(builderNode).toBeDefined();
      expect(builderNode?.data.highlighted).toBe(true);
      expect(builderNode?.selected).toBe(true);
      expect(builderNode?.data.status).toBe('running');
    });

    it('highlights Reviewer / Approver node when activePipelineStage is reviewing', () => {
      const store = useFactoryFlowStore.getState();
      store.setActivePipelineStage('reviewing');
      const nodes = useFactoryFlowStore.getState().nodes;

      const reviewerOrApprover = nodes.find(
        (n) =>
          n.id.includes('reviewer') ||
          n.id.includes('approver') ||
          (n.data.label || '').includes('Approver') ||
          (n.data.label || '').includes('Reviewer')
      );
      expect(reviewerOrApprover).toBeDefined();
      expect(reviewerOrApprover?.data.highlighted).toBe(true);
    });

    it('highlights DecisionGate node when activePipelineStage is gate_waiting', () => {
      const store = useFactoryFlowStore.getState();
      store.setActivePipelineStage('gate_waiting');
      const nodes = useFactoryFlowStore.getState().nodes;

      const gateNode = nodes.find(
        (n) =>
          n.type === 'decision_gate' ||
          n.data.nodeType === 'decision_gate' ||
          (n.data.label || '').includes('Approver')
      );
      expect(gateNode).toBeDefined();
      expect(gateNode?.data.highlighted).toBe(true);
      expect(gateNode?.data.status).toBe('awaiting_approval');
    });

    it('auto-opens GateResolutionModal when stage is gate_waiting and approval token/run ID is present', () => {
      const store = useFactoryFlowStore.getState();
      store.setActiveRunOutcome({
        run_id: 'run-gate-auto-open-101',
        status: 'awaiting_approval',
        stage: 'gate_waiting',
        approval_token: 'hmac-secret-signature-gate',
      });

      const updated = useFactoryFlowStore.getState();
      expect(updated.showGateModal).toBe(true);
      expect(updated.activeApproval).toBeDefined();
      expect(updated.activeApproval?.runId).toBe('run-gate-auto-open-101');
      expect(updated.activeApproval?.token).toBe('hmac-secret-signature-gate');
    });

    it('sets activeRunOutcome and synchronizes activePipelineStage', () => {
      const store = useFactoryFlowStore.getState();
      store.setActiveRunOutcome({
        run_id: 'run-sync-test',
        status: 'building',
        stage: 'building',
      });

      const updated = useFactoryFlowStore.getState();
      expect(updated.activeRunOutcome?.run_id).toBe('run-sync-test');
      expect(updated.activePipelineStage).toBe('building');
    });
  });
});

