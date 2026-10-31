// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { memo } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { LogicNodeData } from '../../types/flow';
import { StatusBadge, getStatusConfig } from '../badges/StatusBadge';
import { GitBranchIcon, SlidersIcon, BotIcon } from '../icons/Icons';

export const LogicNode: React.FC<NodeProps> = memo(({ data, selected }) => {
  const nodeData = data as unknown as LogicNodeData;
  const statusConfig = getStatusConfig(nodeData.status || 'pending');

  const getSubtypeIcon = () => {
    switch (nodeData.subtype) {
      case 'if_else':
        return <GitBranchIcon size={16} />;
      case 'threshold':
        return <SlidersIcon size={16} />;
      case 'llm_router':
        return <BotIcon size={16} />;
      default:
        return <GitBranchIcon size={16} />;
    }
  };

  const getSubtypeLabel = () => {
    switch (nodeData.subtype) {
      case 'if_else':
        return 'If / Else Branch';
      case 'threshold':
        return 'Score Threshold Evaluator';
      case 'llm_router':
        return 'LLM Dynamic Router';
      default:
        return 'Conditional Logic';
    }
  };

  return (
    <div
      className={`factory-node logic-node ${statusConfig.borderClass} ${
        selected ? 'node-selected' : ''
      }`}
    >
      <Handle
        type="target"
        position={Position.Left}
        id="logic-in"
        className="factory-handle handle-target handle-logic"
      />

      <div className="node-header">
        <div className="node-icon logic-icon">{getSubtypeIcon()}</div>
        <div className="node-title-group">
          <span className="node-category">LOGIC</span>
          <span className="node-subtype">{getSubtypeLabel()}</span>
        </div>
        <StatusBadge status={nodeData.status || 'pending'} size="sm" />
      </div>

      <div className="node-body">
        <div className="node-title">{nodeData.label || 'Logic Evaluator'}</div>
        {nodeData.description && (
          <p className="node-description">{nodeData.description}</p>
        )}

        {nodeData.subtype === 'if_else' && nodeData.condition && (
          <div className="node-logic-expr">
            <code>{nodeData.condition}</code>
          </div>
        )}

        {nodeData.subtype === 'threshold' && (
          <div className="node-logic-expr">
            <span>Threshold Score: </span>
            <strong>
              {nodeData.thresholdScore !== undefined
                ? `${nodeData.thresholdScore}%`
                : '90%'}
            </strong>
          </div>
        )}

        {nodeData.subtype === 'llm_router' && nodeData.routes && (
          <div className="node-routes-preview">
            {nodeData.routes.map((route, idx) => (
              <span key={idx} className="route-pill">
                {route}
              </span>
            ))}
          </div>
        )}

        <div className="logic-branches">
          {nodeData.subtype === 'if_else' && (
            <>
              <div className="branch-row branch-true">
                <span className="branch-label label-true">True / Pass</span>
                <Handle
                  type="source"
                  position={Position.Right}
                  id="branch-true"
                  className="factory-handle handle-source handle-branch-true"
                />
              </div>
              <div className="branch-row branch-false">
                <span className="branch-label label-false">False / Fail</span>
                <Handle
                  type="source"
                  position={Position.Right}
                  id="branch-false"
                  className="factory-handle handle-source handle-branch-false"
                />
              </div>
            </>
          )}

          {nodeData.subtype === 'threshold' && (
            <>
              <div className="branch-row branch-pass">
                <span className="branch-label label-true">Meets Bar (&ge;)</span>
                <Handle
                  type="source"
                  position={Position.Right}
                  id="branch-pass"
                  className="factory-handle handle-source handle-branch-true"
                />
              </div>
              <div className="branch-row branch-fail">
                <span className="branch-label label-false">Below Bar (&lt;)</span>
                <Handle
                  type="source"
                  position={Position.Right}
                  id="branch-fail"
                  className="factory-handle handle-source handle-branch-false"
                />
              </div>
            </>
          )}

          {nodeData.subtype === 'llm_router' && (
            <div className="branch-row">
              <span className="branch-label">Routed Out</span>
              <Handle
                type="source"
                position={Position.Right}
                id="logic-out"
                className="factory-handle handle-source handle-logic"
              />
            </div>
          )}
        </div>
      </div>
    </div>
  );
});

LogicNode.displayName = 'LogicNode';
