// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useState, useEffect, useCallback } from 'react';
import { useFactoryFlowStore } from '../../store';
import { fetchRunSteps } from '../../api/steps';
import type { RunStep, RunStepStatus } from '../../types/worker';
import { RefreshCwIcon, ChevronDownIcon, ChevronRightIcon, AlertCircleIcon, CheckCircleIcon } from '../icons/Icons';

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
