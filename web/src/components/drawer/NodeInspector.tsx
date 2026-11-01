// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useState } from 'react';
import { useFactoryFlowStore } from '../../store';
import type { ExecutionStatus, FactoryNodeData } from '../../types/flow';
import { StatusBadge } from '../badges/StatusBadge';
import { TrashIcon, XIcon, SlidersIcon } from '../icons/Icons';
import { LogStreamViewer } from './LogStreamViewer';
import { GitDiffViewer } from './GitDiffViewer';

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
    clearNodeLogs,
    deleteNode,
    setShowGateModal,
  } = useFactoryFlowStore();

  const [activeTab, setActiveTab] = useState<'config' | 'logs' | 'diff'>('config');

  const selectedNode = nodes.find((n) => n.id === selectedNodeId);

  if (!selectedNode) {
    return null;
  }

  const nodeData = selectedNode.data as unknown as FactoryNodeData;
  const currentStatus = nodeData.status || 'pending';
  const logsCount = nodeData.logs?.length || 0;
  const hasDiff = Boolean(nodeData.gitDiff && nodeData.gitDiff.trim().length > 0);

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

      <div className="inspector-tabs">
        <button
          type="button"
          className={`inspector-tab-btn ${activeTab === 'config' ? 'active' : ''}`}
          onClick={() => setActiveTab('config')}
        >
          Config & Meta
        </button>
        <button
          type="button"
          className={`inspector-tab-btn ${activeTab === 'logs' ? 'active' : ''}`}
          onClick={() => setActiveTab('logs')}
        >
          Logs {logsCount > 0 ? `(${logsCount})` : ''}
        </button>
        <button
          type="button"
          className={`inspector-tab-btn ${activeTab === 'diff' ? 'active' : ''}`}
          onClick={() => setActiveTab('diff')}
        >
          Git Diff {hasDiff ? '●' : ''}
        </button>
      </div>

      <div className="inspector-body">
        {activeTab === 'config' && (
          <>
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

            {currentStatus === 'awaiting_approval' && (
              <div className="gate-action-prompt-box">
                <div className="prompt-title">
                  {selectedNode.data.role === 'approver' || !selectedNode.id.includes('builder')
                    ? '⚠ Human Decision Gate Required'
                    : '⚙️ Sensitive MCP Tool Gate Approval Required'}
                </div>
                <p className="prompt-desc">
                  This execution node is suspended awaiting operator authorization.
                </p>
                <button
                  type="button"
                  className="resolve-gate-btn"
                  onClick={() => setShowGateModal(true)}
                >
                  Inspect &amp; Resolve Gate Modal
                </button>
              </div>
            )}

            {(nodeData.executionDurationMs !== undefined || nodeData.tokensUsed !== undefined) && (
              <div className="inspector-field">
                <label className="inspector-label">Execution Telemetry</label>
                <div className="telemetry-grid">
                  {nodeData.executionDurationMs !== undefined && (
                    <div className="telemetry-card">
                      <span className="telemetry-label">Duration</span>
                      <span className="telemetry-val">{(nodeData.executionDurationMs / 1000).toFixed(2)}s</span>
                    </div>
                  )}
                  {nodeData.tokensUsed !== undefined && (
                    <div className="telemetry-card">
                      <span className="telemetry-label">Tokens</span>
                      <span className="telemetry-val">{nodeData.tokensUsed.toLocaleString()}</span>
                    </div>
                  )}
                  {nodeData.startedAt && (
                    <div className="telemetry-card">
                      <span className="telemetry-label">Started</span>
                      <span className="telemetry-val">{new Date(nodeData.startedAt).toLocaleTimeString()}</span>
                    </div>
                  )}
                </div>
              </div>
            )}

            {nodeData.errorMessage && (
              <div className="inspector-field error-field">
                <label className="inspector-label error-label">Execution Error</label>
                <div className="inspector-error-val">{nodeData.errorMessage}</div>
              </div>
            )}

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

            {nodeData.pluginTag && (
              <div className="inspector-field">
                <label className="inspector-label">Extism WASM Plugin URI</label>
                <div className="inspector-code-val">{String(nodeData.pluginTag)}</div>
              </div>
            )}

            {nodeData.nodeType === 'agent' && (
              <div className="inspector-field-group">
                <div className="group-title">Agent Configuration</div>
                <div className="inspector-field">
                  <label className="inspector-label">Role</label>
                  <div className="inspector-readonly-val">
                    {nodeData.role?.toUpperCase()}
                  </div>
                </div>
                {nodeData.model && (
                  <div className="inspector-field">
                    <label className="inspector-label">LLM Model</label>
                    <div className="inspector-readonly-val">{nodeData.model}</div>
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
          </>
        )}

        {activeTab === 'logs' && (
          <LogStreamViewer
            logs={nodeData.logs}
            onClear={() => clearNodeLogs(selectedNode.id)}
          />
        )}

        {activeTab === 'diff' && (
          <GitDiffViewer diff={nodeData.gitDiff || ''} />
        )}
      </div>
    </aside>
  );
};

