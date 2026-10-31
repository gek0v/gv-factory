// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { memo } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { TriggerNodeData } from '../../types/flow';
import { StatusBadge, getStatusConfig } from '../badges/StatusBadge';
import {
  WebhookIcon,
  GitHubIcon,
  ChatIcon,
  ClockIcon,
  ZapIcon,
} from '../icons/Icons';

export const TriggerNode: React.FC<NodeProps> = memo(({ data, selected }) => {
  const nodeData = data as unknown as TriggerNodeData;
  const statusConfig = getStatusConfig(nodeData.status || 'pending');

  const getSubtypeIcon = () => {
    switch (nodeData.subtype) {
      case 'webhook':
        return <WebhookIcon size={16} />;
      case 'github_issue':
        return <GitHubIcon size={16} />;
      case 'chat_listener':
        return <ChatIcon size={16} />;
      case 'cron':
        return <ClockIcon size={16} />;
      default:
        return <ZapIcon size={16} />;
    }
  };

  const getSubtypeLabel = () => {
    switch (nodeData.subtype) {
      case 'webhook':
        return 'Webhook Event';
      case 'github_issue':
        return 'GitHub Issue';
      case 'chat_listener':
        return 'Chat Listener';
      case 'cron':
        return 'Cron Schedule';
      default:
        return 'Inbound Trigger';
    }
  };

  return (
    <div
      className={`factory-node trigger-node ${statusConfig.borderClass} ${
        selected ? 'node-selected' : ''
      }`}
    >
      <div className="node-header">
        <div className="node-icon trigger-icon">{getSubtypeIcon()}</div>
        <div className="node-title-group">
          <span className="node-category">TRIGGER</span>
          <span className="node-subtype">{getSubtypeLabel()}</span>
        </div>
        <StatusBadge status={nodeData.status || 'pending'} size="sm" />
      </div>

      <div className="node-body">
        <div className="node-title">{nodeData.label || 'Inbound Trigger'}</div>
        {nodeData.description && (
          <p className="node-description">{nodeData.description}</p>
        )}
        {(nodeData.eventSource || nodeData.cronSchedule) && (
          <div className="node-metadata-pill">
            <span className="meta-key">
              {nodeData.subtype === 'cron' ? 'Cron:' : 'Source:'}
            </span>
            <span className="meta-val">
              {nodeData.subtype === 'cron'
                ? nodeData.cronSchedule
                : nodeData.eventSource}
            </span>
          </div>
        )}
      </div>

      <Handle
        type="source"
        position={Position.Right}
        id="trigger-out"
        className="factory-handle handle-source handle-trigger"
      />
    </div>
  );
});

TriggerNode.displayName = 'TriggerNode';
