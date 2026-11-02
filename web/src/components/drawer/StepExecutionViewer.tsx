// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useState, useEffect, useCallback } from 'react';
import { useFactoryFlowStore } from '../../store';
import { fetchRunSteps } from '../../api/steps';
import type {
  RunStep,
  RunStepStatus,
  StepSandboxTelemetry,
  StepResourceLimits,
  ToolCallTelemetry,
  ToolCallStatus,
  AgentReasoningBlock,
} from '../../types/worker';
import {
  RefreshCwIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  AlertCircleIcon,
  CheckCircleIcon,
  ShieldIcon,
  CpuIcon,
  ZapIcon,
} from '../icons/Icons';

export interface StepExecutionViewerProps {
  nodeId: string;
  stepName?: string;
}

export function formatDuration(durationMs?: number): string {
  if (durationMs === undefined || durationMs === null || isNaN(durationMs)) {
    return '—';
  }
  if (durationMs < 1000) {
    return `${Math.round(durationMs)}ms`;
  }
  const totalSeconds = durationMs / 1000;
  if (totalSeconds < 60) {
    return `${totalSeconds.toFixed(1)}s`;
  }
  const minutes = Math.floor(totalSeconds / 60);
  const remainingSeconds = Math.round(totalSeconds % 60);
  return `${minutes}m ${remainingSeconds}s`;
}

export function getStatusBadgeClass(status: RunStepStatus): string {
  switch (status.toUpperCase()) {
    case 'COMPLETED':
      return 'step-badge-completed';
    case 'RUNNING':
      return 'step-badge-running';
    case 'FAILED':
      return 'step-badge-failed';
    case 'SKIPPED':
      return 'step-badge-skipped';
    case 'PENDING':
    default:
      return 'step-badge-pending';
  }
}

export function getSandboxBadgeClass(mode?: string): string {
  switch (mode?.toLowerCase()) {
    case 'oci':
      return 'step-sandbox-oci';
    case 'microvm':
      return 'step-sandbox-microvm';
    case 'host':
      return 'step-sandbox-host';
    case 'auto':
      return 'step-sandbox-auto';
    default:
      return 'step-sandbox-none';
  }
}

export function getSandboxBadgeLabel(mode?: string): string {
  switch (mode?.toLowerCase()) {
    case 'oci':
      return 'OCI Container';
    case 'microvm':
      return 'MicroVM';
    case 'host':
      return 'Host In-Process';
    case 'auto':
      return 'Auto Sandbox';
    default:
      return '';
  }
}

export function getToolCallStatusBadgeClass(status: ToolCallStatus): string {
  switch (status) {
    case 'executing':
      return 'tool-status-executing';
    case 'gate-waiting':
      return 'tool-status-gate-waiting';
    case 'completed':
      return 'tool-status-completed';
    case 'failed':
      return 'tool-status-failed';
    default:
      return '';
  }
}

export interface CognitiveTelemetry {
  reasoning?: string;
  toolCalls: ToolCallTelemetry[];
}

export function extractStepCognitiveTelemetry(step: RunStep): CognitiveTelemetry {
  const out = (step.output || {}) as Record<string, unknown>;

  // Extract reasoning chain
  let reasoningStr: string | undefined = undefined;
  if (typeof step.reasoning === 'string' && step.reasoning.trim().length > 0) {
    reasoningStr = step.reasoning;
  } else if (Array.isArray(step.reasoning)) {
    reasoningStr = step.reasoning.map((b) => (typeof b === 'string' ? b : b.content)).join('\n\n');
  } else if (typeof out.reasoning === 'string' && out.reasoning.trim().length > 0) {
    reasoningStr = out.reasoning;
  } else if (Array.isArray(out.reasoning)) {
    reasoningStr = (out.reasoning as (string | AgentReasoningBlock)[])
      .map((b) => (typeof b === 'string' ? b : b.content))
      .join('\n\n');
  }

  // Extract tool calls
  let toolCallsList: ToolCallTelemetry[] = [];
  if (Array.isArray(step.tool_calls)) {
    toolCallsList = step.tool_calls;
  } else if (Array.isArray(out.tool_calls)) {
    toolCallsList = out.tool_calls as ToolCallTelemetry[];
  } else if (Array.isArray(out.toolCalls)) {
    toolCallsList = out.toolCalls as ToolCallTelemetry[];
  }

  return {
    reasoning: reasoningStr,
    toolCalls: toolCallsList,
  };
}

export function extractStepSandboxTelemetry(step: RunStep): StepSandboxTelemetry | null {
  const out = (step.output || {}) as Record<string, unknown>;
  const rawLimits = (step.resource_limits || out.resource_limits || {}) as StepResourceLimits;

  const mode = (step.sandbox_mode || out.sandbox_mode || (out.sandbox_enabled ? 'oci' : undefined)) as string | undefined;
  const containerId = (step.container_id || out.container_id) as string | undefined;
  const cgroups = typeof step.cgroups_enforced === 'boolean'
    ? step.cgroups_enforced
    : typeof out.cgroups_enforced === 'boolean'
      ? (out.cgroups_enforced as boolean)
      : undefined;
  const engine = (step.runtime_engine || out.runtime_engine) as string | undefined;
  const network = (out.network_policy || out.networkPolicy) as string | undefined;
  const security = (out.security_level || out.securityLevel) as string | undefined;

  const hasLimits = Boolean(rawLimits.cpu || rawLimits.memory || rawLimits.pids);

  if (!mode && !containerId && cgroups === undefined && !engine && !hasLimits && !network && !security) {
    return null;
  }

  return {
    sandbox_mode: mode || (engine && engine !== 'host' ? 'oci' : 'host'),
    container_id: containerId,
    cgroups_enforced: cgroups,
    resource_limits: hasLimits ? rawLimits : undefined,
    runtime_engine: engine,
    network_policy: network,
    security_level: security,
  };
}

export const StepExecutionViewer: React.FC<StepExecutionViewerProps> = ({ nodeId, stepName }) => {
  const { activeRunId } = useFactoryFlowStore();
  const [steps, setSteps] = useState<RunStep[]>([]);
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [expandedStepIds, setExpandedStepIds] = useState<Record<string, boolean>>({});

  const loadSteps = useCallback(async () => {
    if (!activeRunId) {
      setSteps([]);
      setError(null);
      return;
    }

    try {
      setLoading(true);
      setError(null);
      const data = await fetchRunSteps(activeRunId);
      setSteps(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [activeRunId]);

  useEffect(() => {
    loadSteps();
  }, [loadSteps]);

  const toggleExpand = (stepId: string) => {
    setExpandedStepIds((prev) => ({
      ...prev,
      [stepId]: !prev[stepId],
    }));
  };

  // Display all steps in chronological order; individual steps matching the selected node are highlighted
  const relevantSteps = steps;

  if (!activeRunId) {
    return (
      <div className="steps-empty-container">
        <div className="steps-empty-icon">⏳</div>
        <div className="steps-empty-title">No Active Factory Run</div>
        <p className="steps-empty-desc">
          Trigger or simulate a factory run from the top toolbar to trace granular step executions and telemetry.
        </p>
      </div>
    );
  }

  return (
    <div className="step-execution-viewer" aria-label="Step Execution Inspection">
      <div className="steps-toolbar">
        <div className="steps-run-tag" title={`Run ID: ${activeRunId}`}>
          <span className="steps-run-label">Run:</span>
          <span className="steps-run-id">{activeRunId.length > 12 ? `${activeRunId.slice(0, 10)}…` : activeRunId}</span>
        </div>
        <button
          type="button"
          className="steps-refresh-btn"
          onClick={loadSteps}
          disabled={loading}
          title="Refresh Steps"
        >
          <RefreshCwIcon size={14} className={loading ? 'icon-spin' : ''} />
          <span>{loading ? 'Refreshing…' : 'Refresh'}</span>
        </button>
      </div>

      {error && (
        <div className="steps-error-banner" role="alert">
          <AlertCircleIcon size={16} />
          <div className="steps-error-text">{error}</div>
        </div>
      )}

      {loading && steps.length === 0 && (
        <div className="steps-loading-state">
          <RefreshCwIcon size={18} className="icon-spin" />
          <span>Fetching step telemetry…</span>
        </div>
      )}

      {!loading && relevantSteps.length === 0 && !error && (
        <div className="steps-empty-state">
          <span>No steps recorded yet for this run.</span>
        </div>
      )}

      <div className="steps-list">
        {relevantSteps.map((step) => {
          const isExpanded = Boolean(expandedStepIds[step.id]);
          const isNodeMatch =
            (stepName && step.step_name.toLowerCase() === stepName.toLowerCase()) ||
            step.step_name.toLowerCase() === nodeId.toLowerCase();
          const isDistributed =
            step.output && (step.output.distributed === true || step.output.worker_id !== undefined);
          const sandboxTelemetry = extractStepSandboxTelemetry(step);
          const sandboxBadgeLabel = getSandboxBadgeLabel(sandboxTelemetry?.sandbox_mode);

          return (
            <div
              key={step.id}
              className={`step-item ${isNodeMatch ? 'step-item-active-node' : ''}`}
            >
              <div className="step-item-header" onClick={() => toggleExpand(step.id)}>
                <div className="step-item-title-col">
                  <button
                    type="button"
                    className="step-expand-caret"
                    aria-label={isExpanded ? 'Collapse step' : 'Expand step'}
                  >
                    {isExpanded ? <ChevronDownIcon size={14} /> : <ChevronRightIcon size={14} />}
                  </button>
                  <span className="step-name" title={step.step_name}>
                    {step.step_name}
                  </span>
                  {isNodeMatch && <span className="step-current-tag">This Node</span>}
                </div>

                <div className="step-item-meta-col">
                  {sandboxBadgeLabel && (
                    <span
                      className={`step-badge ${getSandboxBadgeClass(sandboxTelemetry?.sandbox_mode)}`}
                      title={`Isolation Mode: ${sandboxBadgeLabel}`}
                    >
                      {sandboxBadgeLabel}
                    </span>
                  )}
                  <span className={`step-badge ${getStatusBadgeClass(step.status)}`}>
                    {step.status}
                  </span>
                  <span className="step-duration" title="Execution Duration">
                    {formatDuration(step.duration_ms)}
                  </span>
                  <span
                    className={`step-target-badge ${isDistributed ? 'target-distributed' : 'target-inprocess'}`}
                    title={isDistributed ? 'Executed via Distributed Worker' : 'Executed In-Process'}
                  >
                    {isDistributed ? 'Distributed Worker' : 'In-Process'}
                  </span>
                </div>
              </div>

              {step.status === 'FAILED' && step.error_message && (
                <div className="step-error-banner">
                  <AlertCircleIcon size={14} className="step-error-icon" />
                  <div className="step-error-content">
                    <div className="step-error-label">Execution Error:</div>
                    <code className="step-error-code">{step.error_message}</code>
                  </div>
                </div>
              )}

              {isExpanded && (
                <div className="step-details-body">
                  <div className="step-detail-row">
                    <span className="detail-label">Step ID:</span>
                    <span className="detail-value mono">{step.id}</span>
                  </div>
                  <div className="step-detail-row">
                    <span className="detail-label">Created At:</span>
                    <span className="detail-value">{new Date(step.created_at).toLocaleString()}</span>
                  </div>

                  {sandboxTelemetry && (
                    <div className="step-sandbox-card">
                      <div className="step-sandbox-header">
                        <ShieldIcon size={14} />
                        <span>Sandbox &amp; Security Telemetry</span>
                      </div>
                      <div className="step-sandbox-grid">
                        {sandboxTelemetry.runtime_engine && (
                          <div className="sandbox-metric-cell">
                            <span className="sandbox-metric-label">Runtime Engine</span>
                            <span className="sandbox-metric-value engine-pill">
                              {sandboxTelemetry.runtime_engine}
                            </span>
                          </div>
                        )}
                        {sandboxTelemetry.container_id && (
                          <div className="sandbox-metric-cell">
                            <span className="sandbox-metric-label">Container ID</span>
                            <span
                              className="sandbox-metric-value mono container-id-tag"
                              title={sandboxTelemetry.container_id}
                            >
                              {sandboxTelemetry.container_id.length > 12
                                ? `${sandboxTelemetry.container_id.slice(0, 12)}…`
                                : sandboxTelemetry.container_id}
                            </span>
                          </div>
                        )}
                        {sandboxTelemetry.cgroups_enforced !== undefined && (
                          <div className="sandbox-metric-cell">
                            <span className="sandbox-metric-label">cgroups v2</span>
                            <span
                              className={`sandbox-metric-value cgroups-pill ${
                                sandboxTelemetry.cgroups_enforced ? 'enforced' : 'disabled'
                              }`}
                            >
                              {sandboxTelemetry.cgroups_enforced ? 'Enforced' : 'Disabled'}
                            </span>
                          </div>
                        )}
                        {sandboxTelemetry.network_policy && (
                          <div className="sandbox-metric-cell">
                            <span className="sandbox-metric-label">Network Posture</span>
                            <span className="sandbox-metric-value">
                              {sandboxTelemetry.network_policy.toUpperCase()}
                            </span>
                          </div>
                        )}
                        {sandboxTelemetry.security_level && (
                          <div className="sandbox-metric-cell">
                            <span className="sandbox-metric-label">Security Posture</span>
                            <span className="sandbox-metric-value">
                              {sandboxTelemetry.security_level.toUpperCase()}
                            </span>
                          </div>
                        )}
                        {sandboxTelemetry.resource_limits && (
                          <div className="sandbox-metric-cell full-width">
                            <span className="sandbox-metric-label">Resource Quotas</span>
                            <div className="sandbox-limits-chips">
                              {sandboxTelemetry.resource_limits.cpu && (
                                <span className="limit-chip">
                                  CPU: {sandboxTelemetry.resource_limits.cpu} vCPU
                                </span>
                              )}
                              {sandboxTelemetry.resource_limits.memory && (
                                <span className="limit-chip">
                                  Mem: {sandboxTelemetry.resource_limits.memory}
                                </span>
                              )}
                              {sandboxTelemetry.resource_limits.pids !== undefined && (
                                <span className="limit-chip">
                                  PIDs: {sandboxTelemetry.resource_limits.pids}
                                </span>
                              )}
                            </div>
                          </div>
                        )}
                      </div>
                    </div>
                  )}

                  {/* Cognitive Reasoning Chain & Tool Calls */}
                  {(() => {
                    const cognitive = extractStepCognitiveTelemetry(step);
                    return (
                      <>
                        {cognitive.reasoning && (
                          <div className="reasoning-chain-box">
                            <div
                              className="reasoning-chain-header"
                              onClick={(e) => {
                                e.stopPropagation();
                                const box = e.currentTarget.nextElementSibling as HTMLElement | null;
                                if (box) {
                                  box.style.display = box.style.display === 'none' ? 'block' : 'none';
                                }
                              }}
                            >
                              <div className="reasoning-title-left">
                                <CpuIcon size={13} />
                                <span>Agent Reasoning Chain (CoT)</span>
                              </div>
                              <span style={{ fontSize: '0.65rem', opacity: 0.8 }}>Toggle View</span>
                            </div>
                            <div className="reasoning-body">{cognitive.reasoning}</div>
                          </div>
                        )}

                        {cognitive.toolCalls.length > 0 && (
                          <div className="tool-calls-container">
                            <div className="tool-calls-header">
                              <ZapIcon size={14} />
                              <span>MCP Tool Invocations ({cognitive.toolCalls.length})</span>
                            </div>
                            {cognitive.toolCalls.map((tc) => (
                              <div key={tc.id} className="tool-call-card">
                                <div
                                  className="tool-call-card-header"
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    const body = e.currentTarget.nextElementSibling as HTMLElement | null;
                                    if (body) {
                                      body.style.display = body.style.display === 'none' ? 'flex' : 'none';
                                    }
                                  }}
                                >
                                  <div className="tool-call-name-group">
                                    <span className="tool-name-badge">{tc.name}</span>
                                  </div>
                                  <div className="tool-call-meta">
                                    <span className={`tool-status-badge ${getToolCallStatusBadgeClass(tc.status)}`}>
                                      {tc.status}
                                    </span>
                                    {tc.durationMs !== undefined && (
                                      <span className="step-duration">{formatDuration(tc.durationMs)}</span>
                                    )}
                                  </div>
                                </div>
                                <div className="tool-call-body">
                                  {tc.arguments !== undefined && (
                                    <div>
                                      <div className="tool-args-label">Arguments:</div>
                                      <pre className="tool-args-pre">
                                        {typeof tc.arguments === 'string'
                                          ? tc.arguments
                                          : JSON.stringify(tc.arguments, null, 2)}
                                      </pre>
                                    </div>
                                  )}
                                  {tc.result !== undefined && (
                                    <div>
                                      <div className="tool-result-label">Result:</div>
                                      <pre className="tool-result-pre">
                                        {typeof tc.result === 'string'
                                          ? tc.result
                                          : JSON.stringify(tc.result, null, 2)}
                                      </pre>
                                    </div>
                                  )}
                                  {tc.errorMessage && (
                                    <div className="step-error-banner" style={{ margin: 0 }}>
                                      <AlertCircleIcon size={12} className="step-error-icon" />
                                      <code className="step-error-code">{tc.errorMessage}</code>
                                    </div>
                                  )}
                                </div>
                              </div>
                            ))}
                          </div>
                        )}
                      </>
                    );
                  })()}

                  {step.output && Object.keys(step.output).length > 0 ? (
                    <div className="step-output-container">
                      <div className="step-output-header">
                        <CheckCircleIcon size={14} />
                        <span>Step Output Payload:</span>
                      </div>
                      <pre className="step-output-json">
                        {JSON.stringify(step.output, null, 2)}
                      </pre>
                    </div>
                  ) : (
                    <div className="step-no-output">No structured output payload returned.</div>
                  )}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};
