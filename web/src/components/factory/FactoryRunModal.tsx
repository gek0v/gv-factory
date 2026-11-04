// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useState } from 'react';
import { useFactoryFlowStore } from '../../store';
import { dispatchFactoryRun, type FactoryRunResponse } from '../../api/factory';

export const QUICK_MODEL_OPTIONS = [
  { value: 'gemini-3.8-flash-high', label: 'gemini-3.8-flash-high' },
  { value: 'gpt-4o', label: 'gpt-4o' },
  { value: 'gpt-4o-mini', label: 'gpt-4o-mini' },
  { value: 'claude-3-5-sonnet', label: 'claude-3-5-sonnet' },
  { value: 'deepseek-chat', label: 'deepseek-chat' },
  { value: 'ollama/llama3.3', label: 'ollama/llama3.3' },
  { value: 'custom', label: 'Custom Model...' },
] as const;

export function parseCommaOrNewlineSeparated(value: string): string[] {
  if (!value) return [];
  return value
    .split(/[\r\n,]+/)
    .map((item) => item.trim())
    .filter((item) => item.length > 0);
}

export interface FactoryRunModalProps {
  isOpen?: boolean;
  onClose?: () => void;
}

export const FactoryRunModal: React.FC<FactoryRunModalProps> = ({ isOpen, onClose }) => {
  const store = useFactoryFlowStore();
  const storeOpen = typeof window === 'undefined' ? useFactoryFlowStore.getState().showFactoryRunModal : store.showFactoryRunModal;
  const isModalOpen = isOpen !== undefined ? isOpen : storeOpen;
  const closeModal = onClose || (() => (typeof window === 'undefined' ? useFactoryFlowStore.getState().setShowFactoryRunModal(false) : store.setShowFactoryRunModal(false)));

  const {
    setActiveRun,
    setActivePipelineStage,
    setActiveRunOutcome,
  } = typeof window === 'undefined' ? useFactoryFlowStore.getState() : store;

  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [model, setModel] = useState<string>('gemini-3.8-flash-high');
  const [customModel, setCustomModel] = useState('');
  const [labels, setLabels] = useState('');
  const [constraints, setConstraints] = useState('');
  const [repositoryUrl, setRepositoryUrl] = useState('');
  const [baseBranch, setBaseBranch] = useState('main');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [successResponse, setSuccessResponse] = useState<FactoryRunResponse | null>(null);

  if (!isModalOpen) {
    return null;
  }

  const handleClose = () => {
    if (isSubmitting) return;
    closeModal();
    setErrorMessage(null);
    setSuccessResponse(null);
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!title.trim()) {
      setErrorMessage('Task Title is required');
      return;
    }

    setIsSubmitting(true);
    setErrorMessage(null);
    setSuccessResponse(null);

    try {
      const parsedLabels = parseCommaOrNewlineSeparated(labels);
      const parsedConstraints = parseCommaOrNewlineSeparated(constraints);
      const effectiveModel = model === 'custom' ? customModel.trim() : model.trim();
      const trimmedRepo = repositoryUrl.trim();
      const trimmedBranch = baseBranch.trim() || 'main';

      const response = await dispatchFactoryRun({
        title: title.trim(),
        description: description.trim() || undefined,
        model: effectiveModel || undefined,
        labels: parsedLabels.length > 0 ? parsedLabels : undefined,
        constraints: parsedConstraints.length > 0 ? parsedConstraints : undefined,
        repository_url: trimmedRepo || undefined,
        base_branch: trimmedBranch || undefined,
      });

      setSuccessResponse(response);

      const stage = response.stage || 'spec_generated';
      setActiveRun(response.run_id, 'RUNNING');
      setActiveRunOutcome({
        run_id: response.run_id,
        status: response.status,
        stage,
        title: title.trim(),
        description: description.trim() || undefined,
        approval_token: response.approval_token || response.approvalToken,
        repository_url: trimmedRepo || undefined,
        base_branch: trimmedBranch || undefined,
        metadata: {
          ...(trimmedRepo ? { repository_url: trimmedRepo } : {}),
          ...(trimmedBranch ? { base_branch: trimmedBranch } : {}),
        },
      });
      setActivePipelineStage(stage);

      setTimeout(() => {
        closeModal();
        setTitle('');
        setDescription('');
        setModel('gemini-3.8-flash-high');
        setCustomModel('');
        setLabels('');
        setConstraints('');
        setRepositoryUrl('');
        setBaseBranch('main');
        setSuccessResponse(null);
      }, 1200);
    } catch (err) {
      setErrorMessage(err instanceof Error ? err.message : String(err));
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div
      className="factory-run-modal-backdrop"
      onClick={handleClose}
      role="dialog"
      aria-modal="true"
      aria-labelledby="factory-run-modal-title"
    >
      <div
        className="factory-run-modal-dialog"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="factory-run-modal-header">
          <div className="factory-run-modal-title-group">
            <span className="factory-run-modal-icon">⚡</span>
            <div>
              <h2 id="factory-run-modal-title">Execute Factory Pipeline</h2>
              <p className="factory-run-modal-subtitle">
                Dispatch autonomous multi-agent task execution across canvas nodes
              </p>
            </div>
          </div>
          <button
            type="button"
            className="factory-run-modal-close"
            onClick={handleClose}
            aria-label="Close modal"
            disabled={isSubmitting}
          >
            ✕
          </button>
        </div>

        {errorMessage && (
          <div className="factory-run-alert alert-error" role="alert">
            <span className="alert-icon">⚠</span>
            <span>{errorMessage}</span>
          </div>
        )}

        {successResponse && (
          <div className="factory-run-alert alert-success" role="alert">
            <span className="alert-icon">✓</span>
            <div>
              <strong>Run Dispatched: {successResponse.run_id}</strong>
              <p>Illuminating active pipeline stage ({successResponse.stage || 'spec_generated'})</p>
            </div>
          </div>
        )}

        <form onSubmit={handleSubmit} className="factory-run-form">
          <div className="form-field-group">
            <label htmlFor="task-title" className="form-label">
              Task Title <span className="label-required">*</span>
            </label>
            <input
              id="task-title"
              type="text"
              className="form-input"
              placeholder="e.g., Implement Live Factory Execution from Canvas"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              disabled={isSubmitting}
              autoFocus
              required
            />
          </div>

          <div className="form-field-group">
            <label htmlFor="task-description" className="form-label">
              Description
            </label>
            <textarea
              id="task-description"
              className="form-textarea"
              rows={3}
              placeholder="Detailed instructions, requirements, or architecture context for agents..."
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              disabled={isSubmitting}
            />
          </div>

          <div className="form-field-group">
            <label htmlFor="target-llm-model" className="form-label">
              Target LLM Model
            </label>
            <select
              id="target-llm-model"
              className="form-input"
              value={model}
              onChange={(e) => setModel(e.target.value)}
              disabled={isSubmitting}
            >
              {QUICK_MODEL_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </select>

            <div
              className="model-quick-pills"
              style={{ display: 'flex', flexWrap: 'wrap', gap: '0.35rem', marginTop: '0.35rem' }}
            >
              {QUICK_MODEL_OPTIONS.map((opt) => (
                <button
                  key={opt.value}
                  type="button"
                  className={`btn-model-chip ${model === opt.value ? 'active' : ''}`}
                  onClick={() => setModel(opt.value)}
                  disabled={isSubmitting}
                  style={{
                    fontSize: '0.72rem',
                    padding: '0.2rem 0.5rem',
                    borderRadius: '4px',
                    border: model === opt.value ? '1px solid #38bdf8' : '1px solid #334155',
                    background: model === opt.value ? 'rgba(56, 189, 248, 0.15)' : '#0f172a',
                    color: model === opt.value ? '#38bdf8' : '#94a3b8',
                    cursor: isSubmitting ? 'not-allowed' : 'pointer',
                  }}
                >
                  {opt.label}
                </button>
              ))}
            </div>

            {model === 'custom' && (
              <input
                id="custom-model-input"
                type="text"
                className="form-input"
                style={{ marginTop: '0.4rem' }}
                placeholder="Enter custom model identifier (e.g. qwen2.5-coder, mistral-large)"
                value={customModel}
                onChange={(e) => setCustomModel(e.target.value)}
                disabled={isSubmitting}
              />
            )}
          </div>

          <div className="form-row">
            <div className="form-field-group flex-2">
              <label htmlFor="repository-url" className="form-label">
                Repository URL (GitHub/GitLab)
              </label>
              <input
                id="repository-url"
                type="text"
                className="form-input"
                placeholder="https://github.com/org/repo.git"
                value={repositoryUrl}
                onChange={(e) => setRepositoryUrl(e.target.value)}
                disabled={isSubmitting}
              />
            </div>

            <div className="form-field-group flex-1">
              <label htmlFor="base-branch" className="form-label">
                Base Branch
              </label>
              <input
                id="base-branch"
                type="text"
                className="form-input"
                placeholder="main"
                value={baseBranch}
                onChange={(e) => setBaseBranch(e.target.value)}
                disabled={isSubmitting}
              />
            </div>
          </div>

          <div className="form-row">
            <div className="form-field-group flex-1">
              <label htmlFor="task-labels" className="form-label">
                Labels
              </label>
              <input
                id="task-labels"
                type="text"
                className="form-input"
                placeholder="frontend, canvas, live-exec (comma-separated)"
                value={labels}
                onChange={(e) => setLabels(e.target.value)}
                disabled={isSubmitting}
              />
            </div>

            <div className="form-field-group flex-1">
              <label htmlFor="task-constraints" className="form-label">
                Constraints
              </label>
              <input
                id="task-constraints"
                type="text"
                className="form-input"
                placeholder=">= 90% test coverage, REUSE compliant"
                value={constraints}
                onChange={(e) => setConstraints(e.target.value)}
                disabled={isSubmitting}
              />
            </div>
          </div>

          <div className="factory-run-modal-footer">
            <button
              type="button"
              className="btn-modal-cancel"
              onClick={handleClose}
              disabled={isSubmitting}
            >
              Cancel
            </button>
            <button
              type="submit"
              className="btn-modal-dispatch"
              disabled={isSubmitting || !title.trim()}
            >
              {isSubmitting ? (
                <>
                  <span className="btn-spinner" />
                  <span>Dispatching...</span>
                </>
              ) : (
                <>
                  <span>▶</span>
                  <span>Dispatch Autonomous Run</span>
                </>
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};
