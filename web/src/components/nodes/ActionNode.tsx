// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { memo } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { ActionNodeData } from '../../types/flow';
import { StatusBadge, getStatusConfig } from '../badges/StatusBadge';
import {
  GitCommitIcon,
  GitBranchIcon,
  GitPullRequestIcon,
  SendIcon,
  ChatIcon,
  ZapIcon,
} from '../icons/Icons';

export const ActionNode: React.FC<NodeProps> = memo(({ data, selected }) => {
  const nodeData = data as unknown as ActionNodeData;
  const statusConfig = getStatusConfig(nodeData.status || 'pending');

  const getSubtypeIcon = () => {
    switch (nodeData.subtype) {
      case 'git_commit':
        return <GitCommitIcon size={16} />;
      case 'git_push':
        return <GitBranchIcon size={16} />;
      case 'open_pr':
        return <GitPullRequestIcon size={16} />;
      case 'slack_notification':
        return <SendIcon size={16} />;
      case 'discord_notification':
        return <ChatIcon size={16} />;
      default:
        return <ZapIcon size={16} />;
    }
  };

  const getSubtypeLabel = () => {
    switch (nodeData.subtype) {
      case 'git_commit':
        return 'Git Commit Mutation';
      case 'git_push':
        return 'Git Push Remote';
      case 'open_pr':
        return 'Open Pull Request';
      case 'slack_notification':
        return 'Slack Webhook Notification';
      case 'discord_notification':
        return 'Discord Bot Notification';
      default:
        return 'Operational Action';
    }
  };

  return (
    <div
      className={`factory-node action-node ${statusConfig.borderClass} ${
        selected ? 'node-selected' : ''
      }`}
    >
      <Handle
        type="target"
        position={Position.Left}
        id="action-in"
        className="factory-handle handle-target handle-action"
      />

      <div className="node-header">
        <div className="node-icon action-icon">{getSubtypeIcon()}</div>
        <div className="node-title-group">
          <span className="node-category">ACTION</span>
          <span className="node-subtype">{getSubtypeLabel()}</span>
        </div>
        <StatusBadge status={nodeData.status || 'pending'} size="sm" />
      </div>

      <div className="node-body">
        <div className="node-title">{nodeData.label || 'Action Mutation'}</div>
        {nodeData.description && (
          <p className="node-description">{nodeData.description}</p>
        )}

        {(nodeData.target || nodeData.branchName) && (
          <div className="node-metadata-pill action-meta">
            {nodeData.branchName && (
              <div>
                <span className="meta-key">Branch: </span>
                <span className="meta-val">{nodeData.branchName}</span>
              </div>
            )}
            {nodeData.target && (
              <div>
                <span className="meta-key">Target: </span>
                <span className="meta-val">{nodeData.target}</span>
              </div>
            )}
          </div>
        )}
      </div>

      <Handle
        type="source"
        position={Position.Right}
        id="action-out"
        className="factory-handle handle-source handle-action"
      />
    </div>
  );
});

ActionNode.displayName = 'ActionNode';
