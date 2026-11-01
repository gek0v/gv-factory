// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { memo } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { AgentNodeData } from '../../types/flow';
import { StatusBadge, getStatusConfig } from '../badges/StatusBadge';
import {
  BotIcon,
  BrainIcon,
  CodeIcon,
  ShieldIcon,
  LayersIcon,
  PaletteIcon,
  CheckCircleIcon,
} from '../icons/Icons';

export const AgentNode: React.FC<NodeProps> = memo(({ data, selected }) => {
  const nodeData = data as unknown as AgentNodeData;
  const statusConfig = getStatusConfig(nodeData.status || 'pending');

  const getRoleIcon = () => {
    switch (nodeData.role) {
      case 'spec_writer':
        return <BrainIcon size={16} />;
      case 'builder_tdd':
        return <CodeIcon size={16} />;
      case 'reviewer':
        switch (nodeData.reviewerSpecialty) {
          case 'security':
            return <ShieldIcon size={16} />;
          case 'arch':
            return <LayersIcon size={16} />;
          case 'ui':
            return <PaletteIcon size={16} />;
          case 'code':
          default:
            return <BotIcon size={16} />;
        }
      case 'approver':
        return <CheckCircleIcon size={16} />;
      default:
        return <BotIcon size={16} />;
    }
  };

  const getRoleLabel = () => {
    switch (nodeData.role) {
      case 'spec_writer':
        return 'Spec Writer Agent';
      case 'builder_tdd':
        return 'Builder TDD Agent';
      case 'reviewer':
        return `Reviewer (${(nodeData.reviewerSpecialty || 'Code').toUpperCase()})`;
      case 'approver':
        return 'Approver Gate Agent';
      default:
        return 'Autonomous Agent';
    }
  };

  return (
    <div
      className={`factory-node agent-node ${statusConfig.borderClass} ${
        selected ? 'node-selected' : ''
      }`}
    >
      <Handle
        type="target"
        position={Position.Left}
        id="agent-in"
        className="factory-handle handle-target handle-agent"
      />

      <div className="node-header">
        <div className="node-icon agent-icon">{getRoleIcon()}</div>
        <div className="node-title-group">
          <span className="node-category">AGENT</span>
          <span className="node-subtype">{getRoleLabel()}</span>
        </div>
        <StatusBadge status={nodeData.status || 'pending'} size="sm" />
      </div>

      <div className="node-body">
        <div className="node-title">{nodeData.label || 'Autonomous Agent'}</div>
        {nodeData.description && (
          <p className="node-description">{nodeData.description}</p>
        )}

        <div className="node-tags-container">
          {nodeData.model && (
            <span className="node-tag tag-model" title={`LLM Model: ${nodeData.model}`}>
              <BotIcon size={11} className="tag-icon" />
              {nodeData.model}
            </span>
          )}
          {nodeData.pluginTag && (
            <span
              className="node-tag tag-plugin"
              title={`WASM Plugin: ${nodeData.pluginTag}`}
            >
              <CodeIcon size={11} className="tag-icon" />
              {nodeData.pluginTag}
            </span>
          )}
        </div>

        {nodeData.sandboxEnabled && (
          <div className="node-sandbox-badge">
            <span className="sandbox-dot" />
            <span>Rootless Worktree Sandbox</span>
          </div>
        )}

        {nodeData.status === 'awaiting_approval' && (
          <div className="node-awaiting-badge">
            <span className="awaiting-dot" />
            <span>Awaiting Operator Approval</span>
          </div>
        )}
      </div>

      <Handle
        type="source"
        position={Position.Right}
        id="agent-out"
        className="factory-handle handle-source handle-agent"
      />
    </div>
  );
});

AgentNode.displayName = 'AgentNode';
