// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, beforeEach } from 'vitest';
import { useFactoryFlowStore } from '../store';
import { dispatchGatewayEvent } from './dispatcher';
import { GatewayClient } from './gateway';

describe('Real-time Gateway Client & Event Dispatcher', () => {
  beforeEach(() => {
    // Reset store state
    useFactoryFlowStore.setState({
      selectedNodeId: null,
      activeApproval: null,
      showGateModal: false,
      gatewayStatus: 'disconnected',
    });
  });

  it('updates node status to running on step.started', () => {
    const store = useFactoryFlowStore.getState();
    const targetNodeId = store.nodes[1].id;

    dispatchGatewayEvent(
      {
        type: 'step.started',
        task_id: targetNodeId,
        timestamp: Date.now(),
      },
      store
    );

    const updatedNode = useFactoryFlowStore.getState().nodes.find((n) => n.id === targetNodeId);
    expect(updatedNode?.data.status).toBe('running');
    expect(updatedNode?.data.startedAt).toBeDefined();
  });

  it('updates node status to completed with duration and diff on step.completed', () => {
    const store = useFactoryFlowStore.getState();
    const targetNodeId = store.nodes[1].id;
    const testDiff = `--- a/main.go\n+++ b/main.go\n@@ -1,3 +1,4 @@\n+// Added line`;

    dispatchGatewayEvent(
      {
        type: 'step.completed',
        task_id: targetNodeId,
        payload: {
          duration_ms: 1420,
          diff: testDiff,
        },
      },
      store
    );

    const updatedNode = useFactoryFlowStore.getState().nodes.find((n) => n.id === targetNodeId);
    expect(updatedNode?.data.status).toBe('completed');
    expect(updatedNode?.data.executionDurationMs).toBe(1420);
    expect(updatedNode?.data.gitDiff).toBe(testDiff);
  });

  it('updates node status to failed with error message on step.failed', () => {
    const store = useFactoryFlowStore.getState();
    const targetNodeId = store.nodes[1].id;

    dispatchGatewayEvent(
      {
        type: 'step.failed',
        task_id: targetNodeId,
        payload: {
          error: 'Compilation timeout exceeded in sandbox runner',
        },
      },
      store
    );

    const updatedNode = useFactoryFlowStore.getState().nodes.find((n) => n.id === targetNodeId);
    expect(updatedNode?.data.status).toBe('failed');
    expect(updatedNode?.data.errorMessage).toBe('Compilation timeout exceeded in sandbox runner');
  });

  it('triggers gate.approval_required and populates active approval state', () => {
    const store = useFactoryFlowStore.getState();

    dispatchGatewayEvent(
      {
        type: 'gate.approval_required',
        run_id: 'run-gate-test-101',
        payload: {
          token: 'hmac-test-token-sig-abc',
          coverage_percent: 92.4,
          security_alerts: 0,
        },
      },
      store
    );

    const state = useFactoryFlowStore.getState();
    expect(state.activeApproval).not.toBeNull();
    expect(state.activeApproval?.runId).toBe('run-gate-test-101');
    expect(state.activeApproval?.token).toBe('hmac-test-token-sig-abc');
    expect(state.activeApproval?.coveragePercent).toBe(92.4);

    const approverNode = state.nodes.find(
      (n) => n.data.role === 'approver' || n.id.includes('approver') || n.id.includes('gate')
    );
    expect(approverNode?.data.status).toBe('awaiting_approval');
  });

  it('appends stdout and stderr log streams to node log telemetry', () => {
    const store = useFactoryFlowStore.getState();
    const targetNodeId = store.nodes[1].id;

    dispatchGatewayEvent(
      {
        type: 'step.started',
        task_id: targetNodeId,
        payload: {
          logs: [
            { stream: 'stdout', message: 'WASM Extism plugin instantiated' },
            { stream: 'stderr', message: 'Warning: slow AST evaluation' },
          ],
        },
      },
      store
    );

    const updatedNode = useFactoryFlowStore.getState().nodes.find((n) => n.id === targetNodeId);
    expect(updatedNode?.data.logs?.length).toBe(2);
    expect(updatedNode?.data.logs?.[0].stream).toBe('stdout');
    expect(updatedNode?.data.logs?.[0].message).toBe('WASM Extism plugin instantiated');
    expect(updatedNode?.data.logs?.[1].stream).toBe('stderr');
    expect(updatedNode?.data.logs?.[1].message).toBe('Warning: slow AST evaluation');
  });

  it('calculates exponential backoff delays with cap', () => {
    const client = new GatewayClient({
      initialReconnectMs: 1000,
      maxReconnectMs: 8000,
    });

    expect(client.computeNextDelay()).toBe(1000);
    // simulate internal retries
    (client as unknown as { retryCount: number }).retryCount = 1;
    expect(client.computeNextDelay()).toBe(2000);
    (client as unknown as { retryCount: number }).retryCount = 2;
    expect(client.computeNextDelay()).toBe(4000);
    (client as unknown as { retryCount: number }).retryCount = 3;
    expect(client.computeNextDelay()).toBe(8000);
    (client as unknown as { retryCount: number }).retryCount = 4;
    expect(client.computeNextDelay()).toBe(8000); // capped at maxReconnectMs
  });
});
