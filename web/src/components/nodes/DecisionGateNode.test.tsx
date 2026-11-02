// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { DecisionGateNode, getGateStatusBadge } from './DecisionGateNode';
import { useFactoryFlowStore } from '../../store';
import type { DecisionGateNodeData } from '../../types/flow';

// Mock react-flow handles and context
vi.mock('@xyflow/react', async () => {
  const actual = await vi.importActual<typeof import('@xyflow/react')>('@xyflow/react');
  return {
    ...actual,
    Handle: ({ id, type, position, className }: any) =>
      React.createElement('div', {
        'data-testid': `handle-${id}`,
        'data-type': type,
        'data-position': position,
        className,
      }),
  };
});

describe('DecisionGateNode', () => {
  beforeEach(() => {
    useFactoryFlowStore.setState({
      activeApproval: null,
      showGateModal: false,
    });
  });

  it('computes gate status badges correctly', () => {
    expect(getGateStatusBadge('approved').label).toBe('Approved');
    expect(getGateStatusBadge('approved').color).toBe('#10b981');

    expect(getGateStatusBadge('rejected').label).toBe('Rejected');
    expect(getGateStatusBadge('rejected').color).toBe('#ef4444');

    expect(getGateStatusBadge('timed_out').label).toBe('Timed Out');

    expect(getGateStatusBadge('pending_approval').label).toBe('Pending Approval');
  });

  it('renders default pending_approval state with threshold and security metrics', () => {
    const data: DecisionGateNodeData = {
      nodeType: 'decision_gate',
      label: 'Security & Quality Gate',
      description: 'Requires HMAC authorization before merge',
      status: 'awaiting_approval',
      gateStatus: 'pending_approval',
      thresholdScore: 90,
      currentCoverage: 94.2,
      criticalFindingsCount: 0,
      maxSecurityAlerts: 0,
    };

    const html = renderToStaticMarkup(
      React.createElement(DecisionGateNode, {
        id: 'gate-1',
        data: data as any,
        selected: false,
        type: 'decision_gate',
        zIndex: 1,
        isConnectable: true,
        xPos: 0,
        yPos: 0,
        dragging: false,
      } as any)
    );

    expect(html).toContain('Security &amp; Quality Gate');
    expect(html).toContain('DECISION GATE');
    expect(html).toContain('Pending Approval');
    expect(html).toContain('90%');
    expect(html).toContain('94.2%');
    expect(html).toContain('0 Critical');
  });

  it('renders dual output handles and branch labels', () => {
    const data: DecisionGateNodeData = {
      nodeType: 'decision_gate',
      label: 'Approval Gate',
      status: 'awaiting_approval',
      gateStatus: 'pending_approval',
    };

    const html = renderToStaticMarkup(
      React.createElement(DecisionGateNode, {
        id: 'gate-handles',
        data: data as any,
        selected: false,
        type: 'decision_gate',
        zIndex: 1,
        isConnectable: true,
        xPos: 0,
        yPos: 0,
        dragging: false,
      } as any)
    );

    expect(html).toContain('Approved Path');
    expect(html).toContain('Rejected Path');
    expect(html).toContain('handle-branch-approved');
    expect(html).toContain('handle-branch-rejected');
    expect(html).toContain('handle-gate');
  });

  it('renders approved status and updates visual badge', () => {
    const data: DecisionGateNodeData = {
      nodeType: 'decision_gate',
      label: 'Production Gate',
      status: 'completed',
      gateStatus: 'approved',
    };

    const html = renderToStaticMarkup(
      React.createElement(DecisionGateNode, {
        id: 'gate-approved',
        data: data as any,
        selected: false,
        type: 'decision_gate',
        zIndex: 1,
        isConnectable: true,
        xPos: 0,
        yPos: 0,
        dragging: false,
      } as any)
    );

    expect(html).toContain('Approved');
    expect(html).toContain('border-gate-approved');
  });

  it('renders rejected status with rejection reason', () => {
    const data: DecisionGateNodeData = {
      nodeType: 'decision_gate',
      label: 'Compliance Gate',
      status: 'failed',
      gateStatus: 'rejected',
      rejectionReason: 'Security alert limit exceeded',
    };

    const html = renderToStaticMarkup(
      React.createElement(DecisionGateNode, {
        id: 'gate-rejected',
        data: data as any,
        selected: false,
        type: 'decision_gate',
        zIndex: 1,
        isConnectable: true,
        xPos: 0,
        yPos: 0,
        dragging: false,
      } as any)
    );

    expect(html).toContain('Rejected');
    expect(html).toContain('border-gate-rejected');
    expect(html).toContain('Security alert limit exceeded');
  });

  it('supports store interaction and triggers actions', async () => {
    const resolveSpy = vi.fn().mockResolvedValue({ success: true, message: 'Approved' });
    useFactoryFlowStore.setState({
      resolveApproval: resolveSpy,
      activeApproval: {
        runId: 'run-120',
        token: 'hmac-secret-token-xyz',
        expiresAt: new Date(Date.now() + 100_000).toISOString(),
        coveragePercent: 95,
      },
    });

    const store = useFactoryFlowStore.getState();
    const result = await store.resolveApproval('APPROVE');
    expect(resolveSpy).toHaveBeenCalledWith('APPROVE');
    expect(result.success).toBe(true);
  });

  it('renders quick action buttons when node is in pending status', () => {
    const futureTime = new Date(Date.now() + 50_000).toISOString();
    const data: DecisionGateNodeData = {
      nodeType: 'decision_gate',
      label: 'Interactive Gate Node',
      status: 'awaiting_approval',
      gateStatus: 'pending_approval',
      expiresAt: futureTime,
    };

    const html = renderToStaticMarkup(
      React.createElement(DecisionGateNode, {
        id: 'gate-pending-actions',
        data: data as any,
        selected: true,
        type: 'decision_gate',
        zIndex: 1,
        isConnectable: true,
        xPos: 0,
        yPos: 0,
        dragging: false,
      } as any)
    );

    expect(html).toContain('Inspect &amp; Resolve');
    expect(html).toContain('Quick Approve');
    expect(html).toContain('Reject');
    expect(html).toContain('Expires in');
  });

  it('renders expired state when timer has run out', () => {
    const pastTime = new Date(Date.now() - 50_000).toISOString();
    const data: DecisionGateNodeData = {
      nodeType: 'decision_gate',
      label: 'Timed Out Gate',
      status: 'awaiting_approval',
      gateStatus: 'pending_approval',
      expiresAt: pastTime,
    };

    const html = renderToStaticMarkup(
      React.createElement(DecisionGateNode, {
        id: 'gate-timed-out',
        data: data as any,
        selected: false,
        type: 'decision_gate',
        zIndex: 1,
        isConnectable: true,
        xPos: 0,
        yPos: 0,
        dragging: false,
      } as any)
    );

    expect(html).toContain('Token Expired');
    expect(html).toContain('timer-expired');
  });
});
