// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useState, useMemo, useRef, useEffect } from 'react';
import { useFactoryFlowStore } from '../store';
import { CANONICAL_TEMPLATES } from '../templates/catalog';
import { templateToCanvas } from '../templates/parser';
import { canvasToTemplate, exportTemplateToYaml, exportTemplateToJson } from '../templates/exporter';
import { validateFactoryTemplate } from '../templates/validator';
import { submitPipelineRun, cancelPipelineRun } from '../api/runs';
import { simulatePipelineRun } from '../api/simulator';

export interface ToolbarNotification {
  type: 'success' | 'error' | 'info';
  message: string;
}

export const Toolbar: React.FC = () => {
  const {
    nodes,
    edges,
    setNodes,
    setEdges,
    activeRunId,
    activeRunState,
    setActiveRun,
    setShowFactoryRunModal,
  } = useFactoryFlowStore();
  const [selectedTemplateKey, setSelectedTemplateKey] = useState<string>('feature-complete');
  const [showExportModal, setShowExportModal] = useState<boolean>(false);
  const [exportFormat, setExportFormat] = useState<'yaml' | 'json'>('json');
  const [copied, setCopied] = useState<boolean>(false);

  // Execution state
  const [isRunningServer, setIsRunningServer] = useState<boolean>(false);
  const [isCancelling, setIsCancelling] = useState<boolean>(false);
  const [isSimulating, setIsSimulating] = useState<boolean>(false);
  const [runDropdownOpen, setRunDropdownOpen] = useState<boolean>(false);
  const [notification, setNotification] = useState<ToolbarNotification | null>(null);

  const dropdownRef = useRef<HTMLDivElement>(null);

  // Close dropdown on outside click
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as HTMLElement)) {
        setRunDropdownOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, []);

  // Clear notification automatically after 6 seconds
  useEffect(() => {
    if (!notification) return;
    const timer = setTimeout(() => {
      setNotification(null);
    }, 6000);
    return () => clearTimeout(timer);
  }, [notification]);

  // Derive template from current canvas to validate in real-time
  const currentTemplate = useMemo(() => {
    return canvasToTemplate(nodes, edges);
  }, [nodes, edges]);

  const validation = useMemo(() => {
    return validateFactoryTemplate(currentTemplate);
  }, [currentTemplate]);

  const exportedText = useMemo(() => {
    if (exportFormat === 'yaml') {
      return exportTemplateToYaml(currentTemplate);
    }
    return exportTemplateToJson(currentTemplate);
  }, [currentTemplate, exportFormat]);

  const handleLoadTemplate = (key: string) => {
    setSelectedTemplateKey(key);
    const entry = CANONICAL_TEMPLATES[key];
    if (entry) {
      const parsed = templateToCanvas(entry.template);
      setNodes(parsed.nodes);
      setEdges(parsed.edges);
    }
  };

  const handleCopy = () => {
    navigator.clipboard.writeText(exportedText);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleDownload = () => {
    const filename = `factory-pipeline.${exportFormat}`;
    const blob = new Blob([exportedText], {
      type: exportFormat === 'yaml' ? 'text/yaml' : 'application/json',
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
  };

  const handleOpenRunModal = () => {
    setRunDropdownOpen(false);
    setShowFactoryRunModal(true);
  };

  const handleRunServerLive = async () => {
    setRunDropdownOpen(false);

    if (!validation.isValid) {
      setNotification({
        type: 'error',
        message: `Cannot run invalid pipeline: ${validation.errors.join('; ')}`,
      });
      return;
    }

    try {
      setIsRunningServer(true);
      setNotification({
        type: 'info',
        message: 'Submitting pipeline run to server...',
      });

      const response = await submitPipelineRun(nodes, edges);
      setActiveRun(response.run_id, response.state);
      setNotification({
        type: 'success',
        message: `Run ${response.run_id} initiated (${response.state}). Streaming execution live over SSE.`,
      });
    } catch (err) {
      setNotification({
        type: 'error',
        message: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setIsRunningServer(false);
    }
  };

  const handleCancelRun = async () => {
    if (!activeRunId) return;

    try {
      setIsCancelling(true);
      const res = await cancelPipelineRun(activeRunId);
      setActiveRun(activeRunId, 'CANCELLED');
      setNotification({
        type: 'info',
        message: res.message || `Run ${activeRunId} cancelled by user.`,
      });
    } catch (err) {
      setNotification({
        type: 'error',
        message: `Failed to cancel run: ${err instanceof Error ? err.message : String(err)}`,
      });
    } finally {
      setIsCancelling(false);
    }
  };

  const handleSimulateLocal = async () => {
    setRunDropdownOpen(false);
    try {
      setIsSimulating(true);
      setNotification({
        type: 'info',
        message: 'Simulating pipeline execution locally...',
      });
      await simulatePipelineRun();
      setNotification({
        type: 'success',
        message: 'Local simulation completed.',
      });
    } catch (err) {
      setNotification({
        type: 'error',
        message: `Simulation error: ${String(err)}`,
      });
    } finally {
      setIsSimulating(false);
    }
  };

  return (
    <div className="toolbar-container">
      {notification && (
        <div className={`toolbar-toast toast-${notification.type}`} role="alert">
          <span className="toast-icon">
            {notification.type === 'success' ? '✓' : notification.type === 'error' ? '⚠' : 'ℹ'}
          </span>
          <span className="toast-text">{notification.message}</span>
          <button
            type="button"
            className="toast-close"
            onClick={() => setNotification(null)}
            aria-label="Close notification"
          >
            ✕
          </button>
        </div>
      )}

      <div className="template-toolbar">
        {/* Run Controls Menu */}
        <div className="run-controls-dropdown-wrapper" ref={dropdownRef}>
          <div className="run-split-button">
            <button
              type="button"
              className="run-primary-btn"
              title="Run Pipeline (Autonomous Execution Modal)"
              disabled={isRunningServer || isSimulating}
              onClick={handleOpenRunModal}
            >
              ▶ Run Pipeline
            </button>
            <button
              type="button"
              className="run-dropdown-toggle-btn"
              title="Pipeline Execution Options"
              onClick={() => setRunDropdownOpen((prev) => !prev)}
            >
              ▾
            </button>
          </div>

          {runDropdownOpen && (
            <div className="run-dropdown-menu">
              <button
                type="button"
                className="run-dropdown-item"
                disabled={isRunningServer || isSimulating}
                onClick={handleOpenRunModal}
              >
                <div className="dropdown-item-title">▶ Run Pipeline (Modal)</div>
                <div className="dropdown-item-desc">Execute pipeline with custom title, labels, and constraints</div>
              </button>
              <button
                type="button"
                className="run-dropdown-item"
                disabled={isRunningServer || isSimulating}
                onClick={handleRunServerLive}
              >
                <div className="dropdown-item-title">⚡ Quick Run (Live SSE)</div>
                <div className="dropdown-item-desc">Executes current canvas on ov-server backend and streams via SSE</div>
              </button>
              <button
                type="button"
                className="run-dropdown-item"
                disabled={isRunningServer || isSimulating}
                onClick={handleSimulateLocal}
              >
                <div className="dropdown-item-title">⚡ Simulate Run (Local)</div>
                <div className="dropdown-item-desc">Offline client-side simulation for fast UI verification</div>
              </button>
            </div>
          )}
        </div>

        {/* Template Selector */}
        <div className="template-select-group">
          <label htmlFor="template-select" className="template-label">
            Template:
          </label>
          <select
            id="template-select"
            className="template-dropdown"
            value={selectedTemplateKey}
            onChange={(e) => handleLoadTemplate(e.target.value)}
          >
            {Object.entries(CANONICAL_TEMPLATES).map(([key, item]) => (
              <option key={key} value={key}>
                {item.label}
              </option>
            ))}
          </select>
        </div>

        {/* Active Run Lifecycle Pill & Controls */}
        {activeRunId && (
          <div className="active-run-pill-group" role="status" aria-label="Active Run Lifecycle Tracker">
            <div className={`active-run-pill run-state-${activeRunState?.toLowerCase() || 'unknown'}`}>
              <span className={`run-state-dot dot-${activeRunState?.toLowerCase() || 'unknown'}`} />
              <span className="run-id-label" title={activeRunId}>
                Run: {activeRunId.length > 14 ? `${activeRunId.slice(0, 12)}…` : activeRunId}
              </span>
              <span className="run-state-badge">[{activeRunState || 'UNKNOWN'}]</span>
            </div>

            {(activeRunState === 'QUEUED' || activeRunState === 'RUNNING') && (
              <button
                type="button"
                className="cancel-run-btn"
                title="Cancel running pipeline on server"
                disabled={isCancelling}
                onClick={handleCancelRun}
                aria-label="Cancel Active Run"
              >
                {isCancelling ? 'Cancelling…' : '■ Cancel Run'}
              </button>
            )}

            {(activeRunState === 'COMPLETED' || activeRunState === 'FAILED' || activeRunState === 'CANCELLED') && (
              <button
                type="button"
                className="clear-run-btn"
                title="Clear run tracker"
                onClick={() => setActiveRun(null)}
                aria-label="Dismiss Run"
              >
                ✕
              </button>
            )}
          </div>
        )}

        {/* Validation Status Pill */}
        <div className="template-validation-pill-container" title={validation.errors.join('\n')}>
          {validation.isValid ? (
            <span className="template-valid-pill">✓ Valid Template</span>
          ) : (
            <span className="template-invalid-pill" title={validation.errors.join('; ')}>
              ⚠ {validation.errors.length} {validation.errors.length === 1 ? 'Error' : 'Errors'}
            </span>
          )}
        </div>

        {/* Export DAG Button */}
        <button
          type="button"
          className="template-export-btn"
          title="Export current canvas DAG as JSON/YAML template"
          onClick={() => setShowExportModal(true)}
        >
          <span>Export DAG</span>
        </button>

        {/* Export Modal */}
        {showExportModal && (
          <div className="export-modal-backdrop" onClick={() => setShowExportModal(false)}>
            <div className="export-modal" onClick={(e) => e.stopPropagation()}>
              <div className="export-modal-header">
                <h3>Export Factory Template</h3>
                <button
                  type="button"
                  className="export-modal-close"
                  onClick={() => setShowExportModal(false)}
                >
                  ✕
                </button>
              </div>

              <div className="export-format-selector">
                <button
                  type="button"
                  className={`format-btn ${exportFormat === 'json' ? 'active' : ''}`}
                  onClick={() => setExportFormat('json')}
                >
                  JSON (factory-pipeline.json)
                </button>
                <button
                  type="button"
                  className={`format-btn ${exportFormat === 'yaml' ? 'active' : ''}`}
                  onClick={() => setExportFormat('yaml')}
                >
                  YAML (factory-pipeline.yaml)
                </button>
              </div>

              <pre className="export-code-view">
                <code>{exportedText}</code>
              </pre>

              <div className="export-modal-actions">
                <button type="button" className="copy-btn" onClick={handleCopy}>
                  {copied ? '✓ Copied!' : 'Copy to Clipboard'}
                </button>
                <button type="button" className="download-btn" onClick={handleDownload}>
                  Download {exportFormat === 'json' ? 'factory-pipeline.json' : 'factory-pipeline.yaml'}
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};
