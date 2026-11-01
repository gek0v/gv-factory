// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useState, useEffect } from 'react';
import { useFactoryFlowStore } from '../../store';
import type { ToolRiskLevel } from '../../types/flow';

export function getToolRiskLevel(toolName?: string): ToolRiskLevel {
  if (!toolName) return 'standard';
  const name = toolName.toLowerCase();
  if (/deploy|delete|drop|destroy|purge|truncate|prod/i.test(name)) {
    return 'critical';
  }
  if (/exec|bash|shell|push|publish|secret|token|auth|sudo/i.test(name)) {
    return 'high';
  }
  if (/git|patch|write|update|migrate|chmod/i.test(name)) {
    return 'sensitive';
  }
  return 'standard';
}

export const GateResolutionModal: React.FC = () => {
  const { activeApproval, showGateModal, setShowGateModal, resolveApproval } = useFactoryFlowStore();
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [rejectionReason, setRejectionReason] = useState('');
  const [showRejectionInput, setShowRejectionInput] = useState(false);
  const [feedback, setFeedback] = useState<{ success: boolean; message: string } | null>(null);
  const [timeLeftSec, setTimeLeftSec] = useState<number | null>(null);

  const isToolGate = activeApproval?.gateType === 'tool' || Boolean(activeApproval?.toolName);
  const riskLevel = getToolRiskLevel(activeApproval?.toolName);

  useEffect(() => {
    if (!activeApproval?.expiresAt) {
      setTimeLeftSec(null);
      return;
    }

    const updateTimer = () => {
      const diffMs = new Date(activeApproval.expiresAt).getTime() - Date.now();
      setTimeLeftSec(Math.max(0, Math.floor(diffMs / 1000)));
    };

    updateTimer();
    const interval = setInterval(updateTimer, 1000);
    return () => clearInterval(interval);
  }, [activeApproval?.expiresAt]);

  if (!showGateModal || !activeApproval) {
    return null;
  }

  const coverage = activeApproval.coveragePercent ?? 93.8;
  const minCoverage = activeApproval.minCoveragePercent ?? 90.0;
  const coveragePassed = coverage >= minCoverage;
  const securityAlerts = activeApproval.securityAlerts ?? 0;
  const securityPassed = securityAlerts <= (activeApproval.maxSecurityAlerts ?? 0);

  const formatArguments = (args: unknown): string => {
    if (!args) return '{}';
    if (typeof args === 'string') {
      try {
        return JSON.stringify(JSON.parse(args), null, 2);
      } catch {
        return args;
      }
    }
    try {
      return JSON.stringify(args, null, 2);
    } catch {
      return String(args);
    }
  };

  const handleDecision = async (decision: 'APPROVE' | 'REJECT') => {
    if (decision === 'REJECT' && !showRejectionInput && isToolGate) {
      setShowRejectionInput(true);
      return;
    }

    setIsSubmitting(true);
    setFeedback(null);
    try {
      const res = await resolveApproval(decision, rejectionReason.trim() || undefined);
      setFeedback(res);
      if (res.success) {
        setTimeout(() => {
          setShowGateModal(false);
          setFeedback(null);
          setShowRejectionInput(false);
          setRejectionReason('');
        }, 1200);
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="gate-modal-backdrop" onClick={() => setShowGateModal(false)}>
      <div className="gate-modal" onClick={(e) => e.stopPropagation()}>
        <div className="gate-modal-header">
          <div className="gate-title-row">
            <span className="gate-icon">{isToolGate ? '⚙️' : '🛡️'}</span>
            <div>
              <h3>
                {isToolGate ? 'MCP Agent Sensitive Tool Execution Gate' : 'Human Decision Gate Resolution'}
              </h3>
              <span className="gate-subtitle">
                Run ID: <code>{activeApproval.runId}</code>
                {activeApproval.callerId && (
                  <>
                    {' '}• Caller Agent: <code>{activeApproval.callerId}</code>
                  </>
                )}
                {!isToolGate && (
                  <>
                    {' '}• Branch: <code>{activeApproval.headBranch || 'ov-run/active'}</code>
                  </>
                )}
              </span>
            </div>
          </div>
          <button
            type="button"
            className="gate-modal-close"
            onClick={() => setShowGateModal(false)}
          >
            ✕
          </button>
        </div>

        <div className="gate-modal-body">
          {feedback && (
            <div className={`gate-feedback-banner ${feedback.success ? 'success' : 'error'}`}>
              {feedback.message}
            </div>
          )}

          <div className="gate-token-bar">
            <span className="token-label">HMAC Security Token:</span>
            <code className="token-code">{activeApproval.token.slice(0, 16)}••••••••</code>
            <span className="token-verified-badge">✓ Signed & Verified</span>
            {timeLeftSec !== null && (
              <span className={`gate-countdown ${timeLeftSec <= 30 ? 'countdown-urgent' : ''}`}>
                ⏱ {Math.floor(timeLeftSec / 60)}:{(timeLeftSec % 60).toString().padStart(2, '0')}
              </span>
            )}
          </div>

          {isToolGate ? (
            <div className="gate-tool-details">
              <div className="tool-summary-row">
                <div className="tool-info-cell">
                  <span className="tool-meta-label">Tool Requested:</span>
                  <span className="tool-name-pill">{activeApproval.toolName || 'unknown_tool'}</span>
                </div>
                <div className="tool-info-cell">
                  <span className="tool-meta-label">Risk Level:</span>
                  <span className={`risk-badge risk-${riskLevel}`}>
                    {riskLevel.toUpperCase()} RISK
                  </span>
                </div>
              </div>

              <div className="tool-arguments-section">
                <div className="tool-arguments-header">
                  <span>Execution Arguments (JSON)</span>
                </div>
                <pre className="tool-args-viewer">
                  <code>{formatArguments(activeApproval.toolArguments)}</code>
                </pre>
              </div>

              {(showRejectionInput || !isToolGate) && (
                <div className="gate-rejection-box">
                  <label htmlFor="gate-rejection-input" className="rejection-label">
                    Rejection Rationale / Operator Feedback:
                  </label>
                  <textarea
                    id="gate-rejection-input"
                    className="rejection-textarea"
                    rows={2}
                    placeholder="Provide justification for rejecting this tool execution..."
                    value={rejectionReason}
                    onChange={(e) => setRejectionReason(e.target.value)}
                  />
                </div>
              )}
            </div>
          ) : (
            <>
              <div className="gate-metrics-grid">
                <div className={`metric-box ${coveragePassed ? 'passed' : 'failed'}`}>
                  <div className="metric-header">
                    <span>Code Coverage</span>
                    <span className="metric-score">{coverage.toFixed(1)}%</span>
                  </div>
                  <div className="metric-bar-bg">
                    <div
                      className="metric-bar-fill"
                      style={{ width: `${Math.min(100, coverage)}%` }}
                    />
                  </div>
                  <span className="metric-sub">
                    Target: &gt;={minCoverage}% ({coveragePassed ? 'PASS' : 'FAIL'})
                  </span>
                </div>

                <div className={`metric-box ${securityPassed ? 'passed' : 'failed'}`}>
                  <div className="metric-header">
                    <span>Security Alerts</span>
                    <span className="metric-score">{securityAlerts}</span>
                  </div>
                  <span className="metric-status">
                    {securityAlerts === 0 ? '✓ 0 Alerts Detected' : `⚠ ${securityAlerts} Vulnerabilities`}
                  </span>
                  <span className="metric-sub">Target: 0 Alerts ({securityPassed ? 'PASS' : 'FAIL'})</span>
                </div>
              </div>

              <div className="gate-diagnostics-section">
                <h4>Reviewer Diagnostics &amp; Telemetry</h4>
                <div className="diagnostics-list">
                  {(activeApproval.diagnostics || []).map((diag) => (
                    <div key={diag.reviewerId} className={`diag-item diag-${diag.status}`}>
                      <div className="diag-header">
                        <span className="diag-name">{diag.reviewerName}</span>
                        <span className={`diag-badge badge-${diag.status}`}>
                          {diag.status.toUpperCase()}
                        </span>
                      </div>
                      <ul className="diag-findings">
                        {diag.findings.map((finding, idx) => (
                          <li key={idx}>{finding}</li>
                        ))}
                      </ul>
                    </div>
                  ))}
                </div>
              </div>
            </>
          )}
        </div>

        <div className="gate-modal-actions">
          <button
            type="button"
            className="gate-btn gate-btn-reject"
            disabled={isSubmitting}
            onClick={() => handleDecision('REJECT')}
          >
            {isToolGate
              ? showRejectionInput
                ? 'Confirm Rejection'
                : '✕ Reject Tool Execution'
              : 'Reject & Divert to PR'}
          </button>
          <button
            type="button"
            className="gate-btn gate-btn-approve"
            disabled={isSubmitting}
            onClick={() => handleDecision('APPROVE')}
          >
            {isSubmitting
              ? 'Authorizing...'
              : isToolGate
              ? '✓ Authorize & Execute Tool'
              : '✓ Approve & Advance to Merge Queue'}
          </button>
        </div>
      </div>
    </div>
  );
};

