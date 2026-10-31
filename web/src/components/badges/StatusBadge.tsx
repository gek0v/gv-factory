// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React from 'react';
import type { ExecutionStatus } from '../../types/flow';
import {
  CheckIcon,
  XCircleIcon,
  AlertTriangleIcon,
  LoaderIcon,
  ClockIcon,
} from '../icons/Icons';

export interface StatusBadgeProps {
  status: ExecutionStatus;
  size?: 'sm' | 'md';
  showLabel?: boolean;
}

export const getStatusConfig = (status: ExecutionStatus) => {
  switch (status) {
    case 'pending':
      return {
        label: 'Pending',
        className: 'status-pending',
        borderClass: 'border-pending',
        Icon: ClockIcon,
        color: '#64748b',
        bgColor: 'rgba(100, 116, 139, 0.15)',
      };
    case 'running':
      return {
        label: 'Running',
        className: 'status-running',
        borderClass: 'border-running',
        Icon: LoaderIcon,
        color: '#38bdf8',
        bgColor: 'rgba(56, 189, 248, 0.15)',
      };
    case 'completed':
      return {
        label: 'Completed',
        className: 'status-completed',
        borderClass: 'border-completed',
        Icon: CheckIcon,
        color: '#10b981',
        bgColor: 'rgba(16, 185, 129, 0.15)',
      };
    case 'failed':
      return {
        label: 'Failed',
        className: 'status-failed',
        borderClass: 'border-failed',
        Icon: XCircleIcon,
        color: '#ef4444',
        bgColor: 'rgba(239, 68, 68, 0.15)',
      };
    case 'awaiting_approval':
      return {
        label: 'Action Required',
        className: 'status-awaiting-approval',
        borderClass: 'border-awaiting-approval',
        Icon: AlertTriangleIcon,
        color: '#f59e0b',
        bgColor: 'rgba(245, 158, 11, 0.15)',
      };
    default:
      return {
        label: status,
        className: 'status-pending',
        borderClass: 'border-pending',
        Icon: ClockIcon,
        color: '#64748b',
        bgColor: 'rgba(100, 116, 139, 0.15)',
      };
  }
};

export const StatusBadge: React.FC<StatusBadgeProps> = ({
  status,
  size = 'md',
  showLabel = true,
}) => {
  const config = getStatusConfig(status);
  const { Icon, label, className } = config;

  return (
    <div className={`status-badge ${className} size-${size}`} title={`Execution status: ${label}`}>
      <span className="status-indicator">
        <Icon size={size === 'sm' ? 12 : 14} />
      </span>
      {showLabel && <span className="status-text">{label}</span>}
    </div>
  );
};
