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
});
