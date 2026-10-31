// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React from 'react';
import { useFactoryFlowStore } from '../../store';
import type { ExecutionStatus, FactoryNodeData } from '../../types/flow';
import { StatusBadge } from '../badges/StatusBadge';
import { TrashIcon, XIcon, SlidersIcon } from '../icons/Icons';

const STATUS_OPTIONS: { status: ExecutionStatus; label: string }[] = [
  { status: 'pending', label: 'Pending' },
  { status: 'running', label: 'Running' },
  { status: 'completed', label: 'Completed' },
  { status: 'failed', label: 'Failed' },
  { status: 'awaiting_approval', label: 'Action Required' },
];

export const NodeInspector: React.FC = () => {
  const {
    nodes,
    selectedNodeId,
    setSelectedNodeId,
    updateNodeData,
    updateNodeStatus,
    deleteNode,
  } = useFactoryFlowStore();

  const selectedNode = nodes.find((n) => n.id === selectedNodeId);

  if (!selectedNode) {
    return null;
  }

  const nodeData = selectedNode.data as unknown as FactoryNodeData;
  const currentStatus = nodeData.status || 'pending';

  return (
    <aside className="node-inspector" aria-label="Node Configuration Drawer">
      <div className="inspector-header">
        <div className="inspector-title-row">
          <SlidersIcon size={16} className="inspector-title-icon" />
          <h3 className="inspector-title">Node Inspector</h3>
        </div>
        <button
          type="button"
          className="inspector-close-btn"
          onClick={() => setSelectedNodeId(null)}
          title="Close Inspector"
        >
          <XIcon size={16} />
        </button>
      </div>

      <div className="inspector-body">
        <div className="inspector-field">
          <div className="inspector-meta-row">
            <span className={`inspector-type-badge type-${nodeData.nodeType}`}>
              {nodeData.nodeType.toUpperCase()}
            </span>
            <span className="inspector-id">ID: {selectedNode.id}</span>
          </div>
        </div>

        <div className="inspector-field">
          <label className="inspector-label">
            Execution Status
            <StatusBadge status={currentStatus} size="sm" />
          </label>
          <div className="status-selector-grid">
            {STATUS_OPTIONS.map((opt) => (
              <button
                key={opt.status}
                type="button"
                className={`status-btn status-btn-${opt.status} ${
                  currentStatus === opt.status ? 'active' : ''
                }`}
                onClick={() => updateNodeStatus(selectedNode.id, opt.status)}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>

        <div className="inspector-field">
          <label className="inspector-label" htmlFor="node-label-input">
            Node Label
          </label>
          <input
            id="node-label-input"
            type="text"
            className="inspector-input"
            value={nodeData.label || ''}
            onChange={(e) =>
              updateNodeData(selectedNode.id, { label: e.target.value })
            }
          />
        </div>

        <div className="inspector-field">
          <label className="inspector-label" htmlFor="node-desc-input">
            Description
          </label>
          <textarea
            id="node-desc-input"
            className="inspector-textarea"
            rows={3}
            value={nodeData.description || ''}
            onChange={(e) =>
              updateNodeData(selectedNode.id, { description: e.target.value })
            }
            placeholder="Execution summary..."
          />
        </div>

        {nodeData.nodeType === 'agent' && (
          <div className="inspector-field-group">
            <div className="group-title">Agent Configuration</div>
            <div className="inspector-field">
              <label className="inspector-label">Role</label>
              <div className="inspector-readonly-val">
                {nodeData.role?.toUpperCase()}
              </div>
            </div>
            <div className="inspector-field">
              <label className="inspector-label">LLM Model</label>
              <div className="inspector-readonly-val">{nodeData.model}</div>
            </div>
            {nodeData.pluginTag && (
              <div className="inspector-field">
                <label className="inspector-label">Extism WASM Plugin</label>
                <div className="inspector-readonly-val">{nodeData.pluginTag}</div>
              </div>
            )}
          </div>
        )}

        {nodeData.nodeType === 'logic' && (
          <div className="inspector-field-group">
            <div className="group-title">Branching Configuration</div>
            {nodeData.subtype === 'if_else' && nodeData.condition && (
              <div className="inspector-field">
                <label className="inspector-label">Condition Rule</label>
                <div className="inspector-code-val">{nodeData.condition}</div>
              </div>
            )}
            {nodeData.subtype === 'threshold' && (
              <div className="inspector-field">
                <label className="inspector-label">Quality Threshold</label>
                <div className="inspector-readonly-val">
                  {nodeData.thresholdScore}% minimum score
                </div>
              </div>
            )}
          </div>
        )}

        {nodeData.nodeType === 'action' && (
          <div className="inspector-field-group">
            <div className="group-title">Mutation Configuration</div>
            {nodeData.branchName && (
              <div className="inspector-field">
                <label className="inspector-label">Worktree Git Branch</label>
                <div className="inspector-code-val">{nodeData.branchName}</div>
              </div>
            )}
            {nodeData.target && (
              <div className="inspector-field">
                <label className="inspector-label">Target Destination</label>
                <div className="inspector-readonly-val">{nodeData.target}</div>
              </div>
            )}
          </div>
        )}

        <div className="inspector-actions">
          <button
            type="button"
            className="inspector-delete-btn"
            onClick={() => deleteNode(selectedNode.id)}
          >
            <TrashIcon size={14} />
            <span>Delete Node from DAG</span>
          </button>
        </div>
      </div>
    </aside>
  );
};

