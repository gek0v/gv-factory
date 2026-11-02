// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useState } from 'react';
import { useFactoryFlowStore } from '../../store';
import type { ExecutionStatus, FactoryNodeData, SandboxRuntime, SandboxNetworkPolicy, LLMProvider } from '../../types/flow';
import { StatusBadge } from '../badges/StatusBadge';
import { TrashIcon, XIcon, SlidersIcon, ShieldIcon, CpuIcon } from '../icons/Icons';
import { LogStreamViewer } from './LogStreamViewer';
import { GitDiffViewer } from './GitDiffViewer';
import { StepExecutionViewer } from './StepExecutionViewer';

const PROVIDER_OPTIONS: { value: LLMProvider; label: string }[] = [
  { value: 'anthropic', label: 'Anthropic Claude' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'ollama', label: 'Ollama / Local' },
];

const MODEL_PRESETS: { value: string; label: string }[] = [
  { value: 'claude-3-5-sonnet', label: 'Claude 3.5 Sonnet' },
  { value: 'gpt-4o', label: 'GPT-4o' },
  { value: 'deepseek-r1', label: 'DeepSeek-R1' },
  { value: 'qwen2.5-coder', label: 'Qwen 2.5 Coder' },
  { value: 'custom', label: 'Custom Model Name...' },
];

const PREDEFINED_TOOLS: { id: string; label: string; desc: string }[] = [
  { id: 'bash_exec', label: 'bash_exec', desc: 'Shell command execution' },
  { id: 'git_read', label: 'git_read', desc: 'Git status & diff reading' },
  { id: 'file_writer', label: 'file_writer', desc: 'Codebase file modifications' },
  { id: 'repo_search', label: 'repo_search', desc: 'Ast-grep & ripgrep code queries' },
  { id: 'ast_grep', label: 'ast_grep', desc: 'Structural AST query engine' },
  { id: 'test_runner', label: 'test_runner', desc: 'Sandbox test runner' },
];

const STATUS_OPTIONS: { status: ExecutionStatus; label: string }[] = [
  { status: 'pending', label: 'Pending' },
  { status: 'running', label: 'Running' },
  { status: 'completed', label: 'Completed' },
  { status: 'failed', label: 'Failed' },
  { status: 'awaiting_approval', label: 'Action Required' },
];

const RUNTIME_OPTIONS: { value: SandboxRuntime; label: string }[] = [
  { value: 'auto', label: 'Auto (Best Available / OCI)' },
  { value: 'oci', label: 'Container (Podman / Docker Rootless)' },
  { value: 'host', label: 'Host (In-Process Isolated)' },
  { value: 'microvm', label: 'MicroVM (Strict Hardware Isolation)' },
];

const CPU_LIMIT_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: 'Uncapped (Host Default)' },
  { value: '0.5', label: '0.5 vCPU' },
  { value: '1.0', label: '1.0 vCPU' },
  { value: '2.0', label: '2.0 vCPU' },
  { value: '4.0', label: '4.0 vCPU' },
];

const MEMORY_LIMIT_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: 'Uncapped (Host Default)' },
  { value: '256m', label: '256 MB' },
  { value: '512m', label: '512 MB' },
  { value: '1024m', label: '1024 MB (1GB)' },
  { value: '2048m', label: '2048 MB (2GB)' },
];

const NETWORK_POLICY_OPTIONS: { value: SandboxNetworkPolicy; label: string }[] = [
  { value: 'none', label: 'Isolated (No egress / Loopback only)' },
  { value: 'bridge', label: 'Outbound / Bridge (DNS + HTTPS)' },
  { value: 'host', label: 'Host Network (Unrestricted)' },
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

  const [activeTab, setActiveTab] = useState<'config' | 'steps' | 'logs' | 'diff'>('config');

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
          className={`inspector-tab-btn ${activeTab === 'steps' ? 'active' : ''}`}
          onClick={() => setActiveTab('steps')}
        >
          Steps
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
              <div className="inspector-field-group llm-field-group">
                <div className="group-title llm-title-row">
                  <div className="llm-title-left">
                    <CpuIcon size={14} className="llm-title-icon" />
                    <span>LLM &amp; Cognitive Configuration</span>
                  </div>
                  <span className="llm-tech-badge">Autonomous CoT</span>
                </div>

                <div className="inspector-field">
                  <label className="inspector-label">Role</label>
                  <div className="inspector-readonly-val">
                    {nodeData.role?.toUpperCase()}
                  </div>
                </div>

                {/* Provider Selector */}
                <div className="inspector-field">
                  <label className="inspector-label" htmlFor="llm-provider-select">
                    LLM Provider
                  </label>
                  <select
                    id="llm-provider-select"
                    className="inspector-select"
                    value={nodeData.llmConfig?.provider || 'anthropic'}
                    onChange={(e) => {
                      const newProvider = e.target.value as LLMProvider;
                      const currentLlm = nodeData.llmConfig || {
                        provider: 'anthropic',
                        model: nodeData.model || 'claude-3-5-sonnet',
                        temperature: 0.2,
                        maxIterations: 10,
                        authorizedTools: ['bash_exec', 'git_read', 'file_writer', 'repo_search'],
                      };
                      updateNodeData(selectedNode.id, {
                        llmConfig: {
                          ...currentLlm,
                          provider: newProvider,
                        },
                      });
                    }}
                  >
                    {PROVIDER_OPTIONS.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                </div>

                {/* Model Selector & Custom Input */}
                <div className="inspector-field">
                  <label className="inspector-label" htmlFor="llm-model-select">
                    Model Selection
                  </label>
                  <select
                    id="llm-model-select"
                    className="inspector-select"
                    value={
                      MODEL_PRESETS.some((p) => p.value === nodeData.model)
                        ? nodeData.model
                        : 'custom'
                    }
                    onChange={(e) => {
                      const val = e.target.value;
                      const currentLlm = nodeData.llmConfig || {
                        provider: 'anthropic',
                        model: nodeData.model || 'claude-3-5-sonnet',
                        temperature: 0.2,
                        maxIterations: 10,
                        authorizedTools: ['bash_exec', 'git_read', 'file_writer', 'repo_search'],
                      };
                      if (val !== 'custom') {
                        updateNodeData(selectedNode.id, {
                          model: val,
                          llmConfig: {
                            ...currentLlm,
                            model: val,
                          },
                        });
                      }
                    }}
                  >
                    {MODEL_PRESETS.map((preset) => (
                      <option key={preset.value} value={preset.value}>
                        {preset.label}
                      </option>
                    ))}
                  </select>

                  {(!MODEL_PRESETS.some((p) => p.value === nodeData.model) ||
                    nodeData.model === 'custom') && (
                    <div style={{ marginTop: '0.5rem' }}>
                      <input
                        type="text"
                        className="inspector-input"
                        placeholder="e.g. qwen2.5-coder:32b, llama-3.3-70b-instruct"
                        value={nodeData.model === 'custom' ? '' : nodeData.model}
                        onChange={(e) => {
                          const val = e.target.value;
                          const currentLlm = nodeData.llmConfig || {
                            provider: 'anthropic',
                            model: val,
                            temperature: 0.2,
                            maxIterations: 10,
                            authorizedTools: ['bash_exec', 'git_read', 'file_writer', 'repo_search'],
                          };
                          updateNodeData(selectedNode.id, {
                            model: val,
                            llmConfig: {
                              ...currentLlm,
                              model: val,
                            },
                          });
                        }}
                      />
                    </div>
                  )}
                </div>

                {/* Temperature Slider */}
                <div className="inspector-field">
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <label className="inspector-label" htmlFor="llm-temperature-slider">
                      Temperature
                    </label>
                    <span className="inspector-code-val" style={{ padding: '0.1rem 0.4rem', fontSize: '0.75rem' }}>
                      {(nodeData.llmConfig?.temperature ?? 0.2).toFixed(2)}
                    </span>
                  </div>
                  <input
                    id="llm-temperature-slider"
                    type="range"
                    min="0.0"
                    max="2.0"
                    step="0.05"
                    className="inspector-range"
                    value={nodeData.llmConfig?.temperature ?? 0.2}
                    onChange={(e) => {
                      const temp = parseFloat(e.target.value);
                      const currentLlm = nodeData.llmConfig || {
                        provider: 'anthropic',
                        model: nodeData.model || 'claude-3-5-sonnet',
                        temperature: 0.2,
                        maxIterations: 10,
                        authorizedTools: ['bash_exec', 'git_read', 'file_writer', 'repo_search'],
                      };
                      updateNodeData(selectedNode.id, {
                        llmConfig: {
                          ...currentLlm,
                          temperature: temp,
                        },
                      });
                    }}
                  />
                  <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.7rem', color: 'var(--text-muted)' }}>
                    <span>0.0 (Deterministic)</span>
                    <span>1.0 (Balanced)</span>
                    <span>2.0 (Creative)</span>
                  </div>
                </div>

                {/* Max Reasoning Iterations */}
                <div className="inspector-field">
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <label className="inspector-label" htmlFor="llm-max-iterations">
                      Max Reasoning Iterations
                    </label>
                    <span className="inspector-code-val" style={{ padding: '0.1rem 0.4rem', fontSize: '0.75rem' }}>
                      {nodeData.llmConfig?.maxIterations ?? 10}
                    </span>
                  </div>
                  <input
                    id="llm-max-iterations"
                    type="range"
                    min="1"
                    max="50"
                    step="1"
                    className="inspector-range"
                    value={nodeData.llmConfig?.maxIterations ?? 10}
                    onChange={(e) => {
                      const iters = parseInt(e.target.value, 10);
                      const currentLlm = nodeData.llmConfig || {
                        provider: 'anthropic',
                        model: nodeData.model || 'claude-3-5-sonnet',
                        temperature: 0.2,
                        maxIterations: 10,
                        authorizedTools: ['bash_exec', 'git_read', 'file_writer', 'repo_search'],
                      };
                      updateNodeData(selectedNode.id, {
                        llmConfig: {
                          ...currentLlm,
                          maxIterations: iters,
                        },
                      });
                    }}
                  />
                  <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.7rem', color: 'var(--text-muted)' }}>
                    <span>1 turn</span>
                    <span>10 (default)</span>
                    <span>50 turns</span>
                  </div>
                </div>

                {/* Authorized MCP Tools Picker */}
                <div className="inspector-field">
                  <label className="inspector-label">Authorized MCP Tools</label>
                  <p style={{ margin: '0 0 0.5rem 0', fontSize: '0.75rem', color: 'var(--text-muted)' }}>
                    Grant sandboxed tool invocation access to the agent cognitive runtime:
                  </p>
                  <div className="tools-badge-picker">
                    {PREDEFINED_TOOLS.map((tool) => {
                      const currentTools = nodeData.llmConfig?.authorizedTools ?? [
                        'bash_exec',
                        'git_read',
                        'file_writer',
                        'repo_search',
                      ];
                      const isEnabled = currentTools.includes(tool.id);

                      return (
                        <button
                          key={tool.id}
                          type="button"
                          className={`tool-badge-btn ${isEnabled ? 'enabled' : 'disabled'}`}
                          title={`${tool.desc} (Click to toggle)`}
                          onClick={() => {
                            const newTools = isEnabled
                              ? currentTools.filter((t) => t !== tool.id)
                              : [...currentTools, tool.id];
                            const currentLlm = nodeData.llmConfig || {
                              provider: 'anthropic',
                              model: nodeData.model || 'claude-3-5-sonnet',
                              temperature: 0.2,
                              maxIterations: 10,
                              authorizedTools: currentTools,
                            };
                            updateNodeData(selectedNode.id, {
                              llmConfig: {
                                ...currentLlm,
                                authorizedTools: newTools,
                              },
                            });
                          }}
                        >
                          <span className="tool-indicator">{isEnabled ? '✓' : '+'}</span>
                          <span className="tool-label">{tool.label}</span>
                        </button>
                      );
                    })}
                  </div>
                </div>
              </div>
            )}

            <div className="inspector-field-group sandbox-field-group">
              <div className="group-title sandbox-title-row">
                <div className="sandbox-title-left">
                  <ShieldIcon size={14} className="sandbox-title-icon" />
                  <span>Sandbox &amp; Security Isolation</span>
                </div>
                <span className="sandbox-tech-badge">OCI / cgroups v2</span>
              </div>

              <div className="inspector-field">
                <label className="inspector-label" htmlFor="sandbox-runtime-select">
                  Runtime Environment
                </label>
                <select
                  id="sandbox-runtime-select"
                  className="inspector-select"
                  value={nodeData.sandboxConfig?.runtime || 'auto'}
                  onChange={(e) =>
                    updateNodeData(selectedNode.id, {
                      sandboxConfig: {
                        ...(nodeData.sandboxConfig || {}),
                        runtime: e.target.value as SandboxRuntime,
                      },
                    })
                  }
                >
                  {RUNTIME_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {opt.label}
                    </option>
                  ))}
                </select>
              </div>

              <div className="sandbox-quotas-grid">
                <div className="inspector-field">
                  <label className="inspector-label" htmlFor="sandbox-cpu-select">
                    CPU Limit
                  </label>
                  <select
                    id="sandbox-cpu-select"
                    className="inspector-select"
                    value={nodeData.sandboxConfig?.cpuLimit || ''}
                    onChange={(e) =>
                      updateNodeData(selectedNode.id, {
                        sandboxConfig: {
                          ...(nodeData.sandboxConfig || {}),
                          cpuLimit: e.target.value,
                        },
                      })
                    }
                  >
                    {CPU_LIMIT_OPTIONS.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                </div>

                <div className="inspector-field">
                  <label className="inspector-label" htmlFor="sandbox-memory-select">
                    Memory Ceiling
                  </label>
                  <select
                    id="sandbox-memory-select"
                    className="inspector-select"
                    value={nodeData.sandboxConfig?.memoryLimit || ''}
                    onChange={(e) =>
                      updateNodeData(selectedNode.id, {
                        sandboxConfig: {
                          ...(nodeData.sandboxConfig || {}),
                          memoryLimit: e.target.value,
                        },
                      })
                    }
                  >
                    {MEMORY_LIMIT_OPTIONS.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                </div>
              </div>

              <div className="inspector-field">
                <label className="inspector-label" htmlFor="sandbox-pids-input">
                  Max Process Count (PIDs Limit)
                </label>
                <input
                  id="sandbox-pids-input"
                  type="number"
                  min="16"
                  max="4096"
                  placeholder="e.g. 256 (Empty for uncapped)"
                  className="inspector-input"
                  value={nodeData.sandboxConfig?.pidsLimit !== undefined ? nodeData.sandboxConfig.pidsLimit : ''}
                  onChange={(e) => {
                    const parsed = e.target.value.trim() ? parseInt(e.target.value, 10) : undefined;
                    updateNodeData(selectedNode.id, {
                      sandboxConfig: {
                        ...(nodeData.sandboxConfig || {}),
                        pidsLimit: isNaN(parsed as number) ? undefined : parsed,
                      },
                    });
                  }}
                />
              </div>

              <div className="inspector-field">
                <label className="inspector-label" htmlFor="sandbox-network-select">
                  Network Policy
                </label>
                <select
                  id="sandbox-network-select"
                  className="inspector-select"
                  value={nodeData.sandboxConfig?.networkPolicy || 'bridge'}
                  onChange={(e) =>
                    updateNodeData(selectedNode.id, {
                      sandboxConfig: {
                        ...(nodeData.sandboxConfig || {}),
                        networkPolicy: e.target.value as SandboxNetworkPolicy,
                      },
                    })
                  }
                >
                  {NETWORK_POLICY_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {opt.label}
                    </option>
                  ))}
                </select>
              </div>

              <div className="inspector-field">
                <label className="inspector-label">Security Posture</label>
                <div className="security-posture-toggle-group">
                  <button
                    type="button"
                    className={`posture-btn ${
                      (nodeData.sandboxConfig?.securityLevel || 'standard') === 'standard'
                        ? 'active'
                        : ''
                    }`}
                    onClick={() =>
                      updateNodeData(selectedNode.id, {
                        sandboxConfig: {
                          ...(nodeData.sandboxConfig || {}),
                          securityLevel: 'standard',
                        },
                      })
                    }
                  >
                    Standard
                  </button>
                  <button
                    type="button"
                    className={`posture-btn strict-posture ${
                      nodeData.sandboxConfig?.securityLevel === 'strict' ? 'active' : ''
                    }`}
                    onClick={() =>
                      updateNodeData(selectedNode.id, {
                        sandboxConfig: {
                          ...(nodeData.sandboxConfig || {}),
                          securityLevel: 'strict',
                        },
                      })
                    }
                    title="Rootless user + drop all capabilities + read-only rootfs + no new privileges"
                  >
                    Strict (Rootless + Drop Caps)
                  </button>
                </div>
              </div>
            </div>

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

        {activeTab === 'steps' && (
          <StepExecutionViewer
            nodeId={selectedNode.id}
            stepName={(nodeData as { role?: string; stepName?: string }).role || selectedNode.id}
          />
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

