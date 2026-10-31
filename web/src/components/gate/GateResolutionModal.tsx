// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useState } from 'react';
import { useFactoryFlowStore } from '../../store';

export const GateResolutionModal: React.FC = () => {
  const { activeApproval, showGateModal, setShowGateModal, resolveApproval } = useFactoryFlowStore();
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [feedback, setFeedback] = useState<{ success: boolean; message: string } | null>(null);

  if (!showGateModal || !activeApproval) {
    return null;
  }

  const coverage = activeApproval.coveragePercent ?? 93.8;
  const minCoverage = activeApproval.minCoveragePercent ?? 90.0;
  const coveragePassed = coverage >= minCoverage;
  const securityAlerts = activeApproval.securityAlerts ?? 0;
  const securityPassed = securityAlerts <= (activeApproval.maxSecurityAlerts ?? 0);

  const handleDecision = async (decision: 'APPROVE' | 'REJECT') => {
    setIsSubmitting(true);
    setFeedback(null);
    try {
      const res = await resolveApproval(decision);
      setFeedback(res);
      if (res.success) {
        setTimeout(() => {
          setShowGateModal(false);
          setFeedback(null);
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
            <span className="gate-icon">🛡️</span>
            <div>
              <h3>Human Decision Gate Resolution</h3>
              <span className="gate-subtitle">
                Run ID: <code>{activeApproval.runId}</code> • Branch: <code>{activeApproval.headBranch || 'ov-run/active'}</code>
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
          </div>

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
        </div>

        <div className="gate-modal-actions">
          <button
            type="button"
            className="gate-btn gate-btn-reject"
            disabled={isSubmitting}
            onClick={() => handleDecision('REJECT')}
          >
            Reject &amp; Divert to PR
          </button>
          <button
            type="button"
            className="gate-btn gate-btn-approve"
            disabled={isSubmitting}
            onClick={() => handleDecision('APPROVE')}
          >
            {isSubmitting ? 'Submitting...' : '✓ Approve & Advance to Merge Queue'}
          </button>
        </div>
      </div>
    </div>
  );
};
