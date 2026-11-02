// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { memo, useState, useEffect } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { DecisionGateNodeData, DecisionGateStatus } from '../../types/flow';
import { useFactoryFlowStore } from '../../store';
import { ShieldCheckIcon, AlertTriangleIcon, CheckCircleIcon, XCircleIcon, ClockIcon } from '../icons/Icons';

export function getGateStatusBadge(status?: DecisionGateStatus) {
  switch (status) {
    case 'approved':
      return {
        label: 'Approved',
        className: 'gate-badge-approved',
        color: '#10b981',
        borderClass: 'border-gate-approved',
        Icon: CheckCircleIcon,
      };
    case 'rejected':
      return {
        label: 'Rejected',
        className: 'gate-badge-rejected',
        color: '#ef4444',
        borderClass: 'border-gate-rejected',
        Icon: XCircleIcon,
      };
    case 'timed_out':
      return {
        label: 'Timed Out',
        className: 'gate-badge-timeout',
        color: '#94a3b8',
        borderClass: 'border-gate-timeout',
        Icon: ClockIcon,
      };
    case 'pending_approval':
    default:
      return {
        label: 'Pending Approval',
        className: 'gate-badge-pending',
        color: '#f59e0b',
        borderClass: 'border-gate-pending',
        Icon: AlertTriangleIcon,
      };
  }
}

export const DecisionGateNode: React.FC<NodeProps> = memo(({ data, selected }) => {
  const nodeData = data as unknown as DecisionGateNodeData;
  const { activeApproval, setShowGateModal, resolveApproval } = useFactoryFlowStore();

  const expiresAt = nodeData.expiresAt || (activeApproval?.runId ? activeApproval.expiresAt : undefined);

  const getInitialTimeLeft = () => {
    if (!expiresAt) return null;
    const diffMs = new Date(expiresAt).getTime() - Date.now();
    return Math.max(0, Math.floor(diffMs / 1000));
  };

  const [timeLeftSec, setTimeLeftSec] = useState<number | null>(getInitialTimeLeft);
  const [isSubmitting, setIsSubmitting] = useState(false);

  useEffect(() => {
    if (!expiresAt) {
      setTimeLeftSec(null);
      return;
    }

    const calcRemaining = () => {
      const diffMs = new Date(expiresAt).getTime() - Date.now();
      return Math.max(0, Math.floor(diffMs / 1000));
    };

    setTimeLeftSec(calcRemaining());
    const interval = setInterval(() => {
      setTimeLeftSec(calcRemaining());
    }, 1000);

    return () => clearInterval(interval);
  }, [expiresAt]);

  const isExpired =
    timeLeftSec !== null
      ? timeLeftSec <= 0
      : expiresAt
      ? new Date(expiresAt).getTime() <= Date.now()
      : false;

  const threshold = nodeData.thresholdScore ?? 90;
  const currentCoverage = nodeData.currentCoverage ?? (activeApproval?.coveragePercent ?? 94.2);
  const criticalFindings = nodeData.criticalFindingsCount ?? (activeApproval?.securityAlerts ?? 0);

  const effectiveStatus: DecisionGateStatus =
    isExpired && (!nodeData.gateStatus || nodeData.gateStatus === 'pending_approval')
      ? 'timed_out'
      : nodeData.gateStatus ||
        (nodeData.status === 'completed'
          ? 'approved'
          : nodeData.status === 'failed'
          ? 'rejected'
          : 'pending_approval');

  const statusBadge = getGateStatusBadge(effectiveStatus);

  const handleQuickApprove = async () => {
    setIsSubmitting(true);
    try {
      await resolveApproval('APPROVE');
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleQuickReject = async () => {
    setIsSubmitting(true);
    try {
      await resolveApproval('REJECT', 'Quick rejection from canvas node');
    } finally {
      setIsSubmitting(false);
    }
  };

  const isPending = effectiveStatus === 'pending_approval' && !isExpired;

  return (
    <div
      className={`factory-node decision-gate-node ${statusBadge.borderClass} ${
        selected ? 'node-selected' : ''
      }`}
    >
      <Handle
        type="target"
        position={Position.Left}
        id="gate-in"
        className="factory-handle handle-target handle-gate"
      />

      <div className="node-header">
        <div className="node-icon gate-icon">
          <ShieldCheckIcon size={16} />
        </div>
        <div className="node-title-group">
          <span className="node-category">DECISION GATE</span>
          <span className="node-subtype">HMAC Governance</span>
        </div>
        <div className={`gate-status-pill ${statusBadge.className}`}>
          <statusBadge.Icon size={12} />
          <span>{statusBadge.label}</span>
        </div>
      </div>

      <div className="node-body">
        <div className="node-title">{nodeData.label || 'Decision Gate'}</div>
        {nodeData.description && (
          <p className="node-description">{nodeData.description}</p>
        )}

        {/* Gate Metrics Summary */}
        <div className="gate-metrics-grid">
          <div className="gate-metric-card">
            <span className="gate-metric-label">Min Score</span>
            <span className="gate-metric-value">{threshold}%</span>
          </div>
          <div className="gate-metric-card">
            <span className="gate-metric-label">Observed</span>
            <span
              className={`gate-metric-value ${
                currentCoverage >= threshold ? 'text-success' : 'text-danger'
              }`}
            >
              {currentCoverage}%
            </span>
          </div>
          <div className="gate-metric-card">
            <span className="gate-metric-label">Alerts</span>
            <span
              className={`gate-metric-value ${
                criticalFindings === 0 ? 'text-success' : 'text-danger'
              }`}
            >
              {criticalFindings} Critical
            </span>
          </div>
        </div>

        {/* Countdown / Timeout info */}
        {expiresAt && (
          <div className={`gate-timer-box ${isExpired ? 'timer-expired' : ''}`}>
            <ClockIcon size={12} />
            <span>
              {isExpired ? 'Token Expired' : `Expires in ${timeLeftSec}s`}
            </span>
          </div>
        )}

        {/* Rejection notice if present */}
        {effectiveStatus === 'rejected' && nodeData.rejectionReason && (
          <div className="gate-rejection-notice">
            <span className="notice-title">Rejection Reason:</span>
            <p className="notice-text">{nodeData.rejectionReason}</p>
          </div>
        )}

        {/* Quick action buttons if interactive and pending */}
        {isPending && (
          <div className="gate-actions-row">
            <button
              type="button"
              className="gate-btn gate-btn-inspect"
              onClick={() => setShowGateModal(true)}
              disabled={isSubmitting}
            >
              Inspect &amp; Resolve
            </button>
            <button
              type="button"
              className="gate-btn gate-btn-approve"
              onClick={handleQuickApprove}
              disabled={isSubmitting}
            >
              Quick Approve
            </button>
            <button
              type="button"
              className="gate-btn gate-btn-reject"
              onClick={handleQuickReject}
              disabled={isSubmitting}
            >
              Reject
            </button>
          </div>
        )}

        {/* Dual Branch Output Handles */}
        <div className="gate-dual-branches">
          <div className="branch-row branch-approved-row">
            <span className="branch-label label-approved">Approved Path</span>
            <Handle
              type="source"
              position={Position.Right}
              id="branch-approved"
              className="factory-handle handle-source handle-branch-approved"
            />
          </div>
          <div className="branch-row branch-rejected-row">
            <span className="branch-label label-rejected">Rejected Path</span>
            <Handle
              type="source"
              position={Position.Right}
              id="branch-rejected"
              className="factory-handle handle-source handle-branch-rejected"
            />
          </div>
        </div>
      </div>
    </div>
  );
});

DecisionGateNode.displayName = 'DecisionGateNode';
