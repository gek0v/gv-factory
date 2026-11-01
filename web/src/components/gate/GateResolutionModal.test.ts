// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, beforeEach } from 'vitest';
import { useFactoryFlowStore } from '../../store';

describe('Human Gate Decision Resolution', () => {
  beforeEach(() => {
    useFactoryFlowStore.setState({
      activeApproval: {
        runId: 'run-gate-test-202',
        token: 'hmac-test-secret-signature',
        expiresAt: new Date(Date.now() + 100000).toISOString(),
        coveragePercent: 94.2,
        securityAlerts: 0,
        minCoveragePercent: 90.0,
        maxSecurityAlerts: 0,
      },
      showGateModal: true,
    });
  });

  it('approves pipeline run and clears active approval', async () => {
    const store = useFactoryFlowStore.getState();
    const result = await store.resolveApproval('APPROVE');

    expect(result.success).toBe(true);
    expect(result.message).toContain('enqueued to sequential merge queue');

    const updatedState = useFactoryFlowStore.getState();
    expect(updatedState.activeApproval).toBeNull();
    expect(updatedState.showGateModal).toBe(false);

    // Approver node should transition to completed
    const approverNode = updatedState.nodes.find(
      (n) => n.data.role === 'approver' || n.id.includes('approver') || n.id.includes('gate')
    );
    expect(approverNode?.data.status).toBe('completed');
  });

  it('rejects pipeline run and triggers diversion to Pull Request', async () => {
    const store = useFactoryFlowStore.getState();
    const result = await store.resolveApproval('REJECT');

    expect(result.success).toBe(true);
    expect(result.message).toContain('Diverting to GitHub Pull Request');

    const updatedState = useFactoryFlowStore.getState();
    expect(updatedState.activeApproval).toBeNull();
    expect(updatedState.showGateModal).toBe(false);
  });

  it('resolves tool gate approval with APPROVE and updates caller node', async () => {
    const store = useFactoryFlowStore.getState();
    const builderNode = store.nodes[1];
    store.updateNodeStatus(builderNode.id, 'awaiting_approval');

    useFactoryFlowStore.setState({
      activeApproval: {
        runId: 'run-tool-test-1',
        token: 'hmac-tool-approval-token',
        expiresAt: new Date(Date.now() + 60000).toISOString(),
        gateType: 'tool',
        toolName: 'bash_exec',
        toolArguments: { cmd: 'cat /etc/passwd' },
        callerId: builderNode.id,
      },
      showGateModal: true,
    });

    const result = await store.resolveApproval('APPROVE');
    expect(result.success).toBe(true);
    expect(result.message).toContain('approved');

    const updated = useFactoryFlowStore.getState();
    expect(updated.activeApproval).toBeNull();
    expect(updated.showGateModal).toBe(false);

    const callerNode = updated.nodes.find((n) => n.id === builderNode.id);
    expect(callerNode?.data.status).toBe('completed');
  });

  it('resolves tool gate approval with REJECT and rejection reason', async () => {
    const store = useFactoryFlowStore.getState();
    const builderNode = store.nodes[1];
    store.updateNodeStatus(builderNode.id, 'awaiting_approval');

    useFactoryFlowStore.setState({
      activeApproval: {
        runId: 'run-tool-test-2',
        token: 'hmac-tool-approval-token',
        expiresAt: new Date(Date.now() + 60000).toISOString(),
        gateType: 'tool',
        toolName: 'deploy_service',
        toolArguments: { target: 'production' },
        callerId: builderNode.id,
      },
      showGateModal: true,
    });

    const result = await store.resolveApproval('REJECT', 'Direct production deployment not permitted');
    expect(result.success).toBe(true);
    expect(result.message).toContain('rejected');

    const updated = useFactoryFlowStore.getState();
    expect(updated.activeApproval).toBeNull();

    const callerNode = updated.nodes.find((n) => n.id === builderNode.id);
    expect(callerNode?.data.status).toBe('failed');
  });

  it('fails gracefully when /api/gate/resolve returns HTTP 400 error', async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async () =>
      new Response(JSON.stringify({ error: 'HMAC signature expired or invalid' }), {
        status: 400,
        headers: { 'Content-Type': 'application/json' },
      });

    try {
      useFactoryFlowStore.setState({
        activeApproval: {
          runId: 'run-err-test',
          token: 'invalid-token',
          expiresAt: new Date().toISOString(),
          gateType: 'tool',
          toolName: 'bash_exec',
        },
        showGateModal: true,
      });

      const res = await useFactoryFlowStore.getState().resolveApproval('APPROVE');
      expect(res.success).toBe(false);
      expect(res.message).toContain('Failed to resolve approval');

      // Modal and activeApproval should NOT be cleared on HTTP error
      const state = useFactoryFlowStore.getState();
      expect(state.activeApproval).not.toBeNull();
      expect(state.showGateModal).toBe(true);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it('renders and evaluates risk levels accurately', async () => {
    const { getToolRiskLevel } = await import('./GateResolutionModal');
    expect(getToolRiskLevel('bash_exec')).toBe('high');
    expect(getToolRiskLevel('deploy_service')).toBe('critical');
    expect(getToolRiskLevel('database_drop_table')).toBe('critical');
    expect(getToolRiskLevel('git_push_origin')).toBe('high');
    expect(getToolRiskLevel('git_patch')).toBe('sensitive');
    expect(getToolRiskLevel('read_file')).toBe('standard');
    expect(getToolRiskLevel(undefined)).toBe('standard');
  });
});





