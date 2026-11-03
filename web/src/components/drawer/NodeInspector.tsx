// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useState, useEffect } from 'react';
import { useFactoryFlowStore } from '../../store';
import type {
  ExecutionStatus,
  FactoryNodeData,
  SandboxRuntime,
  SandboxNetworkPolicy,
  LLMProvider,
  AgentNodeData,
  ThinkingLevel,
} from '../../types/flow';
import {
  fetchSkills,
  fetchTools,
  fetchPlugins,
  fetchAgentTemplates,
  createAgentTemplate,
  DEFAULT_SKILLS,
  DEFAULT_TOOLS,
  DEFAULT_PLUGINS,
  DEFAULT_AGENT_TEMPLATES,
  type Skill,
  type Tool,
  type Plugin,
  type AgentTemplate,
} from '../../api/registry';
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

const THINKING_LEVELS: { value: ThinkingLevel; label: string; desc: string }[] = [
  { value: 'off', label: 'Off', desc: 'Direct response, CoT disabled' },
  { value: 'minimal', label: 'Minimal', desc: '~1k tokens reasoning scratchpad' },
  { value: 'low', label: 'Low', desc: '~2k tokens reasoning budget' },
  { value: 'medium', label: 'Medium', desc: '~4k tokens reasoning budget' },
  { value: 'high', label: 'High', desc: '~8k deep reasoning' },
  { value: 'max', label: 'Max', desc: '~16k-32k max reasoning depth' },
];

export const NodeInspector: React.FC = () => {
  const store = typeof window === 'undefined' ? useFactoryFlowStore.getState() : useFactoryFlowStore();
  const {
    nodes,
    selectedNodeId,
    setSelectedNodeId,
    updateNodeData,
    updateNodeStatus,
    clearNodeLogs,
    deleteNode,
    setShowGateModal,
  } = store;

  const [activeTab, setActiveTab] = useState<'config' | 'steps' | 'logs' | 'diff'>('config');

  // Registry state for agent templates, skills, tools, and plugins
  const [availableSkills, setAvailableSkills] = useState<Skill[]>(DEFAULT_SKILLS);
  const [availableTools, setAvailableTools] = useState<Tool[]>(DEFAULT_TOOLS);
  const [availablePlugins, setAvailablePlugins] = useState<Plugin[]>(DEFAULT_PLUGINS);
  const [availableTemplates, setAvailableTemplates] = useState<AgentTemplate[]>(DEFAULT_AGENT_TEMPLATES);

  // State for "Save as Template" inline form
  const [isSavingTemplate, setIsSavingTemplate] = useState(false);
  const [templateNameInput, setTemplateNameInput] = useState('');
  const [templateDescInput, setTemplateDescInput] = useState('');
  const [saveSuccessMsg, setSaveSuccessMsg] = useState<string | null>(null);

  useEffect(() => {
    let isMounted = true;
    Promise.all([
      fetchSkills().catch(() => DEFAULT_SKILLS),
      fetchTools().catch(() => DEFAULT_TOOLS),
      fetchPlugins().catch(() => DEFAULT_PLUGINS),
      fetchAgentTemplates().catch(() => DEFAULT_AGENT_TEMPLATES),
    ]).then(([s, t, p, tpls]) => {
      if (isMounted) {
        if (s && s.length > 0) setAvailableSkills(s);
        if (t && t.length > 0) setAvailableTools(t);
        if (p && p.length > 0) setAvailablePlugins(p);
        if (tpls && tpls.length > 0) setAvailableTemplates(tpls);
      }
    });
    return () => {
      isMounted = false;
    };
  }, []);

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
                  {nodeData.nodeType === 'decision_gate' || selectedNode.data.role === 'approver' || !selectedNode.id.includes('builder')
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

            {nodeData.nodeType === 'decision_gate' && (
              <div className="inspector-field-group gate-field-group">
                <div className="group-title">
                  <ShieldIcon size={14} className="inspector-title-icon" />
                  <span>Gate Governance &amp; Thresholds</span>
                </div>
                <div className="inspector-field">
                  <label className="inspector-label" htmlFor="gate-threshold-input">
                    Required Minimum Coverage (%)
                  </label>
                  <input
                    id="gate-threshold-input"
                    type="number"
                    min="0"
                    max="100"
                    className="inspector-input"
                    value={nodeData.thresholdScore ?? 90}
                    onChange={(e) =>
                      updateNodeData(selectedNode.id, {
                        thresholdScore: Number(e.target.value),
                      })
                    }
                  />
                </div>
                <div className="inspector-field">
                  <label className="inspector-label" htmlFor="gate-max-alerts-input">
                    Max Security Alerts Allowed
                  </label>
                  <input
                    id="gate-max-alerts-input"
                    type="number"
                    min="0"
                    className="inspector-input"
                    value={nodeData.maxSecurityAlerts ?? 0}
                    onChange={(e) =>
                      updateNodeData(selectedNode.id, {
                        maxSecurityAlerts: Number(e.target.value),
                      })
                    }
                  />
                </div>
                {nodeData.diagnostics && nodeData.diagnostics.length > 0 && (
                  <div className="inspector-field">
                    <label className="inspector-label">Security &amp; Review Diagnostics</label>
                    <div className="gate-diagnostics-list">
                      {nodeData.diagnostics.map((diag, i) => (
                        <div key={i} className={`gate-diagnostic-card diag-${diag.status}`}>
                          <div className="diag-header">
                            <span className="diag-name">{diag.reviewerName}</span>
                            <span className="diag-status">{diag.status.toUpperCase()}</span>
                          </div>
                          <ul className="diag-findings">
                            {diag.findings.map((f, fi) => (
                              <li key={fi}>{f}</li>
                            ))}
                          </ul>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
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

            {nodeData.nodeType === 'agent' && (() => {
              const agentData = nodeData as AgentNodeData;
              const currentSkills = agentData.skills || [];
              const currentTools = agentData.tools || agentData.llmConfig?.authorizedTools || ['bash_exec', 'git_read', 'file_writer', 'repo_search'];
              const currentPlugins = agentData.plugins || (agentData.pluginTag ? [agentData.pluginTag] : []);
              const currentThinking = agentData.thinking || 'off';

              return (
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

                  {/* 1. Agent Template Selector (auto-fills skills, tools, model, prompt, thinking) */}
                  <div className="inspector-field">
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                      <label className="inspector-label" htmlFor="agent-template-select">
                        Agent Template
                      </label>
                      {agentData.templateId && (
                        <span className="template-badge" title={`Template ID: ${agentData.templateId}`}>
                          ★ {availableTemplates.find((t) => t.id === agentData.templateId)?.name || agentData.templateId}
                        </span>
                      )}
                    </div>
                    <select
                      id="agent-template-select"
                      className="inspector-select"
                      value={agentData.templateId || ''}
                      onChange={(e) => {
                        const selectedTplId = e.target.value;
                        if (!selectedTplId) {
                          updateNodeData(selectedNode.id, { templateId: undefined });
                          return;
                        }
                        const tpl = availableTemplates.find((t) => t.id === selectedTplId);
                        if (tpl) {
                          const currentLlm = agentData.llmConfig || {
                            provider: 'anthropic',
                            model: tpl.model,
                            temperature: tpl.temperature ?? 0.2,
                            maxIterations: tpl.maxIterations ?? 10,
                            authorizedTools: tpl.tools ? [...tpl.tools] : [],
                          };
                          updateNodeData(selectedNode.id, {
                            templateId: tpl.id,
                            model: tpl.model,
                            skills: tpl.skills ? [...tpl.skills] : [],
                            tools: tpl.tools ? [...tpl.tools] : [],
                            plugins: tpl.plugins ? [...tpl.plugins] : [],
                            thinking: tpl.thinking || 'off',
                            prompt: tpl.prompt || '',
                            pluginTag: tpl.plugins?.[0] || agentData.pluginTag,
                            llmConfig: {
                              ...currentLlm,
                              model: tpl.model,
                              temperature: tpl.temperature ?? currentLlm.temperature,
                              maxIterations: tpl.maxIterations ?? currentLlm.maxIterations,
                              authorizedTools: tpl.tools ? [...tpl.tools] : currentLlm.authorizedTools,
                            },
                          });
                        }
                      }}
                    >
                      <option value="">-- Apply a Preset Agent Template --</option>
                      {availableTemplates.map((tpl) => (
                        <option key={tpl.id} value={tpl.id}>
                          {tpl.name} ({tpl.model})
                        </option>
                      ))}
                    </select>
                    <p style={{ margin: '0.25rem 0 0 0', fontSize: '0.7rem', color: 'var(--text-muted)' }}>
                      Applying a template auto-populates skills, tools, model, thinking, and prompt.
                    </p>
                  </div>

                  {/* System Prompt & Instructions */}
                  <div className="inspector-field">
                    <label className="inspector-label" htmlFor="agent-prompt-input">
                      Agent System Prompt &amp; Instructions
                    </label>
                    <textarea
                      id="agent-prompt-input"
                      className="inspector-textarea"
                      rows={3}
                      value={agentData.prompt || ''}
                      onChange={(e) => updateNodeData(selectedNode.id, { prompt: e.target.value })}
                      placeholder="Core cognitive prompt, role instructions, and behavioral rules..."
                    />
                  </div>

                  {/* Provider Selector */}
                  <div className="inspector-field">
                    <label className="inspector-label" htmlFor="llm-provider-select">
                      LLM Provider
                    </label>
                    <select
                      id="llm-provider-select"
                      className="inspector-select"
                      value={agentData.llmConfig?.provider || 'anthropic'}
                      onChange={(e) => {
                        const newProvider = e.target.value as LLMProvider;
                        const currentLlm = agentData.llmConfig || {
                          provider: 'anthropic',
                          model: agentData.model || 'claude-3-5-sonnet',
                          temperature: 0.2,
                          maxIterations: 10,
                          authorizedTools: currentTools,
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
                        MODEL_PRESETS.some((p) => p.value === agentData.model)
                          ? agentData.model
                          : 'custom'
                      }
                      onChange={(e) => {
                        const val = e.target.value;
                        const currentLlm = agentData.llmConfig || {
                          provider: 'anthropic',
                          model: agentData.model || 'claude-3-5-sonnet',
                          temperature: 0.2,
                          maxIterations: 10,
                          authorizedTools: currentTools,
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

                    {(!MODEL_PRESETS.some((p) => p.value === agentData.model) ||
                      agentData.model === 'custom') && (
                      <div style={{ marginTop: '0.5rem' }}>
                        <input
                          type="text"
                          className="inspector-input"
                          placeholder="e.g. qwen2.5-coder:32b, llama-3.3-70b-instruct"
                          value={agentData.model === 'custom' ? '' : agentData.model}
                          onChange={(e) => {
                            const val = e.target.value;
                            const currentLlm = agentData.llmConfig || {
                              provider: 'anthropic',
                              model: val,
                              temperature: 0.2,
                              maxIterations: 10,
                              authorizedTools: currentTools,
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

                  {/* 2. Skills Multi-Tag Selector with removable badges and tooltips */}
                  <div className="inspector-field">
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                      <label className="inspector-label">Skills Pool</label>
                      <span style={{ fontSize: '0.7rem', color: '#7dd3fc' }}>
                        {currentSkills.length} active
                      </span>
                    </div>
                    <div className="multi-tag-box">
                      <div className="multi-tag-list" aria-label="Active Agent Skills">
                        {currentSkills.length === 0 ? (
                          <span style={{ fontSize: '0.72rem', color: 'var(--text-muted)', fontStyle: 'italic' }}>
                            No skills attached. Pick from pool below.
                          </span>
                        ) : (
                          currentSkills.map((skillId) => {
                            const skillObj = availableSkills.find((s) => s.id === skillId);
                            const skillName = skillObj?.name || skillId;
                            const skillDesc = skillObj?.description || `Skill ID: ${skillId}`;
                            return (
                              <span
                                key={skillId}
                                className="removable-badge badge-skill"
                                title={skillDesc}
                                data-testid={`skill-badge-${skillId}`}
                              >
                                <span>{skillName}</span>
                                <button
                                  type="button"
                                  className="removable-badge-remove"
                                  onClick={() => {
                                    const updatedSkills = currentSkills.filter((s) => s !== skillId);
                                    updateNodeData(selectedNode.id, { skills: updatedSkills });
                                  }}
                                  title={`Remove ${skillName}`}
                                  aria-label={`Remove skill ${skillName}`}
                                >
                                  ✕
                                </button>
                              </span>
                            );
                          })
                        )}
                      </div>

                      <select
                        id="add-skill-select"
                        className="inspector-select"
                        value=""
                        onChange={(e) => {
                          const toAdd = e.target.value;
                          if (toAdd && !currentSkills.includes(toAdd)) {
                            updateNodeData(selectedNode.id, {
                              skills: [...currentSkills, toAdd],
                            });
                          }
                        }}
                      >
                        <option value="" disabled>+ Add skill from pool...</option>
                        {availableSkills
                          .filter((s) => !currentSkills.includes(s.id))
                          .map((s) => (
                            <option key={s.id} value={s.id} title={s.description}>
                              {s.name} ({s.category || 'General'})
                            </option>
                          ))}
                      </select>

                      {/* Quick pool chips */}
                      <div className="tag-pool-chips">
                        {availableSkills
                          .filter((s) => !currentSkills.includes(s.id))
                          .slice(0, 4)
                          .map((s) => (
                            <button
                              key={s.id}
                              type="button"
                              className="pool-chip-btn"
                              title={s.description}
                              onClick={() => {
                                updateNodeData(selectedNode.id, {
                                  skills: [...currentSkills, s.id],
                                });
                              }}
                            >
                              <span>+</span>
                              <span>{s.name}</span>
                            </button>
                          ))}
                      </div>
                    </div>
                  </div>

                  {/* 3. Authorized Tools & MCPs Multi-Tag Selector */}
                  <div className="inspector-field">
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                      <label className="inspector-label">Authorized Tools &amp; MCPs</label>
                      <span style={{ fontSize: '0.7rem', color: '#c7d2fe' }}>
                        {currentTools.length} active
                      </span>
                    </div>
                    <div className="multi-tag-box">
                      <div className="multi-tag-list" aria-label="Active Tools and MCPs">
                        {currentTools.length === 0 ? (
                          <span style={{ fontSize: '0.72rem', color: 'var(--text-muted)', fontStyle: 'italic' }}>
                            No tools authorized.
                          </span>
                        ) : (
                          currentTools.map((toolId) => {
                            const toolObj = availableTools.find((t) => t.id === toolId);
                            const toolName = toolObj?.name || toolId;
                            const toolDesc = toolObj?.description || `Tool ID: ${toolId}`;
                            const risk = toolObj?.riskLevel || 'standard';
                            return (
                              <span
                                key={toolId}
                                className="removable-badge badge-tool"
                                title={`${toolDesc} [Risk: ${risk}]`}
                                data-testid={`tool-badge-${toolId}`}
                              >
                                <span>{toolName}</span>
                                <span className={`tag-risk-pill risk-${risk}`}>{risk}</span>
                                <button
                                  type="button"
                                  className="removable-badge-remove"
                                  onClick={() => {
                                    const updatedTools = currentTools.filter((t) => t !== toolId);
                                    const currentLlm = agentData.llmConfig || {
                                      provider: 'anthropic',
                                      model: agentData.model,
                                      temperature: 0.2,
                                      maxIterations: 10,
                                      authorizedTools: currentTools,
                                    };
                                    updateNodeData(selectedNode.id, {
                                      tools: updatedTools,
                                      llmConfig: {
                                        ...currentLlm,
                                        authorizedTools: updatedTools,
                                      },
                                    });
                                  }}
                                  title={`Remove ${toolName}`}
                                  aria-label={`Remove tool ${toolName}`}
                                >
                                  ✕
                                </button>
                              </span>
                            );
                          })
                        )}
                      </div>

                      <select
                        id="add-tool-select"
                        className="inspector-select"
                        value=""
                        onChange={(e) => {
                          const toAdd = e.target.value;
                          if (toAdd && !currentTools.includes(toAdd)) {
                            const updated = [...currentTools, toAdd];
                            const currentLlm = agentData.llmConfig || {
                              provider: 'anthropic',
                              model: agentData.model,
                              temperature: 0.2,
                              maxIterations: 10,
                              authorizedTools: currentTools,
                            };
                            updateNodeData(selectedNode.id, {
                              tools: updated,
                              llmConfig: {
                                ...currentLlm,
                                authorizedTools: updated,
                              },
                            });
                          }
                        }}
                      >
                        <option value="" disabled>+ Add tool / MCP from pool...</option>
                        {availableTools
                          .filter((t) => !currentTools.includes(t.id))
                          .map((t) => (
                            <option key={t.id} value={t.id} title={t.description}>
                              {t.name} [{t.category?.toUpperCase() || 'TOOL'}] - {t.riskLevel || 'standard'}
                            </option>
                          ))}
                      </select>

                      {/* Quick tool toggle badges for common tools */}
                      <div className="tools-badge-picker">
                        {PREDEFINED_TOOLS.map((tool) => {
                          const isEnabled = currentTools.includes(tool.id);
                          return (
                            <button
                              key={tool.id}
                              type="button"
                              className={`tool-badge-btn ${isEnabled ? 'enabled' : 'disabled'}`}
                              title={`${tool.desc} (Click to toggle)`}
                              onClick={() => {
                                const updatedTools = isEnabled
                                  ? currentTools.filter((t) => t !== tool.id)
                                  : [...currentTools, tool.id];
                                const currentLlm = agentData.llmConfig || {
                                  provider: 'anthropic',
                                  model: agentData.model || 'claude-3-5-sonnet',
                                  temperature: 0.2,
                                  maxIterations: 10,
                                  authorizedTools: currentTools,
                                };
                                updateNodeData(selectedNode.id, {
                                  tools: updatedTools,
                                  llmConfig: {
                                    ...currentLlm,
                                    authorizedTools: updatedTools,
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

                  {/* 4. Plugins / Extensions Selector */}
                  <div className="inspector-field">
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                      <label className="inspector-label">Plugins &amp; WASM Extensions</label>
                      <span style={{ fontSize: '0.7rem', color: '#6ee7b7' }}>
                        {currentPlugins.length} attached
                      </span>
                    </div>
                    <div className="multi-tag-box">
                      <div className="multi-tag-list" aria-label="Active Plugins">
                        {currentPlugins.length === 0 ? (
                          <span style={{ fontSize: '0.72rem', color: 'var(--text-muted)', fontStyle: 'italic' }}>
                            No WASM plugins attached.
                          </span>
                        ) : (
                          currentPlugins.map((pluginUri) => {
                            const pObj = availablePlugins.find((p) => p.uri === pluginUri || p.id === pluginUri);
                            const pName = pObj?.name || pluginUri.replace('extism://plugins/', '');
                            return (
                              <span
                                key={pluginUri}
                                className="removable-badge badge-plugin"
                                title={pObj?.description || pluginUri}
                                data-testid={`plugin-badge-${pluginUri}`}
                              >
                                <span>{pName}</span>
                                <button
                                  type="button"
                                  className="removable-badge-remove"
                                  onClick={() => {
                                    const updated = currentPlugins.filter((p) => p !== pluginUri);
                                    updateNodeData(selectedNode.id, {
                                      plugins: updated,
                                      pluginTag: updated[0] || '',
                                    });
                                  }}
                                  title={`Remove ${pName}`}
                                  aria-label={`Remove plugin ${pName}`}
                                >
                                  ✕
                                </button>
                              </span>
                            );
                          })
                        )}
                      </div>

                      <select
                        id="add-plugin-select"
                        className="inspector-select"
                        value=""
                        onChange={(e) => {
                          const toAdd = e.target.value;
                          if (toAdd && !currentPlugins.includes(toAdd)) {
                            const updated = [...currentPlugins, toAdd];
                            updateNodeData(selectedNode.id, {
                              plugins: updated,
                              pluginTag: updated[0] || '',
                            });
                          }
                        }}
                      >
                        <option value="" disabled>+ Attach WASM plugin...</option>
                        {availablePlugins
                          .filter((p) => !currentPlugins.includes(p.uri || p.id))
                          .map((p) => (
                            <option key={p.id} value={p.uri || p.id} title={p.description}>
                              {p.name} (v{p.version || '1.0'})
                            </option>
                          ))}
                      </select>
                    </div>
                  </div>

                  {/* 5. Thinking Level Selector (off, minimal, low, medium, high, max) */}
                  <div className="inspector-field">
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                      <label className="inspector-label">Thinking Level (CoT Budget)</label>
                      <span className="inspector-code-val" style={{ padding: '0.1rem 0.4rem', fontSize: '0.75rem', textTransform: 'uppercase' }}>
                        {currentThinking}
                      </span>
                    </div>
                    <div className="thinking-level-grid">
                      {THINKING_LEVELS.map((tLevel) => (
                        <button
                          key={tLevel.value}
                          type="button"
                          className={`thinking-btn ${currentThinking === tLevel.value ? 'active' : ''}`}
                          onClick={() => updateNodeData(selectedNode.id, { thinking: tLevel.value })}
                          title={tLevel.desc}
                        >
                          {tLevel.label}
                        </button>
                      ))}
                    </div>
                    <p style={{ margin: '0.25rem 0 0 0', fontSize: '0.7rem', color: 'var(--text-muted)' }}>
                      {THINKING_LEVELS.find((t) => t.value === currentThinking)?.desc}
                    </p>
                  </div>

                  {/* Temperature Slider */}
                  <div className="inspector-field">
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                      <label className="inspector-label" htmlFor="llm-temperature-slider">
                        Temperature
                      </label>
                      <span className="inspector-code-val" style={{ padding: '0.1rem 0.4rem', fontSize: '0.75rem' }}>
                        {(agentData.llmConfig?.temperature ?? 0.2).toFixed(2)}
                      </span>
                    </div>
                    <input
                      id="llm-temperature-slider"
                      type="range"
                      min="0.0"
                      max="2.0"
                      step="0.05"
                      className="inspector-range"
                      value={agentData.llmConfig?.temperature ?? 0.2}
                      onChange={(e) => {
                        const temp = parseFloat(e.target.value);
                        const currentLlm = agentData.llmConfig || {
                          provider: 'anthropic',
                          model: agentData.model || 'claude-3-5-sonnet',
                          temperature: 0.2,
                          maxIterations: 10,
                          authorizedTools: currentTools,
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
                        {agentData.llmConfig?.maxIterations ?? 10}
                      </span>
                    </div>
                    <input
                      id="llm-max-iterations"
                      type="range"
                      min="1"
                      max="50"
                      step="1"
                      className="inspector-range"
                      value={agentData.llmConfig?.maxIterations ?? 10}
                      onChange={(e) => {
                        const iters = parseInt(e.target.value, 10);
                        const currentLlm = agentData.llmConfig || {
                          provider: 'anthropic',
                          model: agentData.model || 'claude-3-5-sonnet',
                          temperature: 0.2,
                          maxIterations: 10,
                          authorizedTools: currentTools,
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

                  {/* 6. Save current agent configuration as new template */}
                  <div className="save-template-card">
                    {!isSavingTemplate ? (
                      <button
                        type="button"
                        className="save-template-btn"
                        onClick={() => {
                          setTemplateNameInput(`${agentData.label || 'Custom'} Template`);
                          setTemplateDescInput(agentData.description || 'Custom agent configuration template');
                          setIsSavingTemplate(true);
                          setSaveSuccessMsg(null);
                        }}
                      >
                        💾 Save Agent as New Template
                      </button>
                    ) : (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.45rem' }}>
                        <label className="inspector-label" style={{ margin: 0 }}>Save as Agent Template</label>
                        <input
                          type="text"
                          className="inspector-input"
                          placeholder="Template Name..."
                          value={templateNameInput}
                          onChange={(e) => setTemplateNameInput(e.target.value)}
                        />
                        <input
                          type="text"
                          className="inspector-input"
                          placeholder="Short description..."
                          value={templateDescInput}
                          onChange={(e) => setTemplateDescInput(e.target.value)}
                        />
                        <div style={{ display: 'flex', gap: '0.5rem', marginTop: '0.25rem' }}>
                          <button
                            type="button"
                            className="save-template-btn"
                            style={{ flex: 1, background: '#10b981', borderColor: '#059669', color: '#fff' }}
                            onClick={async () => {
                              if (!templateNameInput.trim()) return;
                              try {
                                const created = await createAgentTemplate({
                                  name: templateNameInput.trim(),
                                  description: templateDescInput.trim() || 'Custom agent template',
                                  role: agentData.role,
                                  model: agentData.model || 'claude-3-5-sonnet',
                                  prompt: agentData.prompt,
                                  skills: currentSkills,
                                  tools: currentTools,
                                  plugins: currentPlugins,
                                  thinking: currentThinking,
                                  temperature: agentData.llmConfig?.temperature ?? 0.2,
                                  maxIterations: agentData.llmConfig?.maxIterations ?? 10,
                                });
                                setAvailableTemplates((prev) => [created, ...prev.filter((t) => t.id !== created.id)]);
                                updateNodeData(selectedNode.id, { templateId: created.id });
                                setIsSavingTemplate(false);
                                setSaveSuccessMsg(`Template "${created.name}" saved!`);
                                setTimeout(() => setSaveSuccessMsg(null), 3500);
                              } catch (err) {
                                console.error('Failed to create template', err);
                              }
                            }}
                          >
                            Confirm Save
                          </button>
                          <button
                            type="button"
                            className="inspector-btn-secondary"
                            style={{ padding: '0.35rem 0.6rem', fontSize: '0.75rem', background: '#374151', border: 'none', color: '#e5e7eb', borderRadius: '4px', cursor: 'pointer' }}
                            onClick={() => setIsSavingTemplate(false)}
                          >
                            Cancel
                          </button>
                        </div>
                      </div>
                    )}
                    {saveSuccessMsg && (
                      <div style={{ fontSize: '0.75rem', color: '#34d399', textAlign: 'center', fontWeight: 500 }}>
                        {saveSuccessMsg}
                      </div>
                    )}
                  </div>
                </div>
              );
            })()}

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

