// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, beforeEach, vi } from 'vitest';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import {
  ExecutionTimelineDrawer,
  TimelineButton,
  formatStageTitle,
  getDecisionBadgeClass,
  getSeverityBadgeClass,
  formatDurationMs,
  extractTimelineStages,
  type SpecStagePayload,
  type BuildStagePayload,
  type ReviewStagePayload,
  type DecisionStagePayload,
} from './ExecutionTimelineDrawer';
import { useFactoryFlowStore } from '../../store';

describe('ExecutionTimelineDrawer and Helpers (TDD)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useFactoryFlowStore.setState({
      showExecutionTimeline: false,
      executionEvents: [],
      showGateModal: false,
    });
  });

  describe('Pure formatters and helpers', () => {
    it('formatStageTitle formats stage keys into human-readable stage titles', () => {
      expect(formatStageTitle('spec_generated')).toBe('Spec Synthesis');
      expect(formatStageTitle('build_completed')).toBe('TDD Build Loop');
      expect(formatStageTitle('review_decided')).toBe('Reviewers & Audit');
      expect(formatStageTitle('pipeline_completed')).toBe('Gate & Decision');
      expect(formatStageTitle('unknown_stage')).toBe('unknown_stage');
    });

    it('getDecisionBadgeClass maps decisions to correct CSS classes', () => {
      expect(getDecisionBadgeClass('APPROVED')).toBe('decision-badge-approved');
      expect(getDecisionBadgeClass('approved')).toBe('decision-badge-approved');
      expect(getDecisionBadgeClass('RETRY')).toBe('decision-badge-retry');
      expect(getDecisionBadgeClass('retry')).toBe('decision-badge-retry');
      expect(getDecisionBadgeClass('HUMAN_GATE')).toBe('decision-badge-human-gate');
      expect(getDecisionBadgeClass('human_gate')).toBe('decision-badge-human-gate');
      expect(getDecisionBadgeClass('REJECTED')).toBe('decision-badge-rejected');
      expect(getDecisionBadgeClass('rejected')).toBe('decision-badge-rejected');
      expect(getDecisionBadgeClass('unknown')).toBe('decision-badge-default');
    });

    it('getSeverityBadgeClass maps severity levels to CSS classes', () => {
      expect(getSeverityBadgeClass('clean')).toBe('severity-badge-clean');
      expect(getSeverityBadgeClass('low')).toBe('severity-badge-low');
      expect(getSeverityBadgeClass('medium')).toBe('severity-badge-medium');
      expect(getSeverityBadgeClass('high')).toBe('severity-badge-high');
      expect(getSeverityBadgeClass('critical')).toBe('severity-badge-critical');
      expect(getSeverityBadgeClass(undefined)).toBe('severity-badge-clean');
      expect(getSeverityBadgeClass('custom-unknown')).toBe('severity-badge-clean');
    });

    it('formatDurationMs formats millisecond values', () => {
      expect(formatDurationMs(undefined)).toBe('—');
      expect(formatDurationMs(null as unknown as number)).toBe('—');
      expect(formatDurationMs(0)).toBe('0ms');
      expect(formatDurationMs(450)).toBe('450ms');
      expect(formatDurationMs(2000)).toBe('2s');
      expect(formatDurationMs(1500)).toBe('1.5s');
      expect(formatDurationMs(65000)).toBe('1m 5s');
    });

    it('extractTimelineStages parses executionEvents cleanly and provides defaults', () => {
      const emptyEvents = [
        { stage: 'spec_generated', timestamp: '2026-11-02T10:00:00Z', payload: {} },
        { stage: 'build_completed', timestamp: '2026-11-02T10:01:00Z', payload: {} },
        { stage: 'review_decided', timestamp: '2026-11-02T10:02:00Z', payload: {} },
        { stage: 'pipeline_completed', timestamp: '2026-11-02T10:03:00Z', payload: {} },
        { stage: 'other_stage', timestamp: '2026-11-02T10:04:00Z', payload: {} },
      ];

      const emptyStages = extractTimelineStages(emptyEvents);
      expect(emptyStages.spec?.title).toBe('Specification Synthesis');
      expect(emptyStages.spec?.impactedFiles).toEqual([]);
      expect(emptyStages.build?.phases).toHaveLength(3);
      expect(emptyStages.review?.consolidatedScore).toBe(95);
      expect(emptyStages.review?.reviewers).toHaveLength(2);
      expect(emptyStages.decision?.decision).toBe('APPROVED');

      const customEvents = [
        {
          stage: 'spec_generated',
          timestamp: '2026-11-02T10:00:00Z',
          payload: {
            title: 'Build Live Timeline Feature',
            impactedFiles: ['web/src/components/timeline/ExecutionTimelineDrawer.tsx'],
            specMarkdown: '# Feature Spec\nDetailed spec contents here.',
          },
        },
        {
          stage: 'build_completed',
          timestamp: '2026-11-02T10:01:00Z',
          payload: {
            phases: [
              { phase: 'red', name: 'Red Phase', exitCode: 1, durationMs: 800 },
              { phase: 'green', name: 'Green Phase', exitCode: 0, durationMs: 1400 },
              { phase: 'refactor', name: 'Refactor Phase', exitCode: 0, durationMs: 900 },
            ],
            filesModified: ['web/src/components/timeline/ExecutionTimelineDrawer.tsx'],
            diff: '--- a/file\n+++ b/file\n@@ -1,1 +1,2 @@\n+new line',
          },
        },
        {
          stage: 'review_decided',
          timestamp: '2026-11-02T10:02:00Z',
          payload: {
            consolidatedScore: 96,
            severity: 'clean',
            reviewers: [
              {
                id: 'sec-rev',
                name: 'Security Reviewer',
                severity: 'clean',
                score: 98,
                findings: ['0 vulnerabilities found'],
              },
              {
                id: 'arch-rev',
                name: 'Architecture Reviewer',
                severity: 'low',
                score: 94,
                findings: ['Component isolation adhered to'],
              },
            ],
          },
        },
        {
          stage: 'pipeline_completed',
          timestamp: '2026-11-02T10:03:00Z',
          payload: {
            decision: 'HUMAN_GATE',
            totalDurationMs: 3100,
            requiresHumanGate: true,
          },
        },
      ];

      const stages = extractTimelineStages(customEvents);
      expect(stages.spec?.title).toBe('Build Live Timeline Feature');
      expect(stages.spec?.impactedFiles).toHaveLength(1);
      expect(stages.build?.phases).toHaveLength(3);
      expect(stages.build?.diff).toContain('+new line');
      expect(stages.review?.reviewers).toHaveLength(2);
      expect(stages.review?.consolidatedScore).toBe(96);
      expect(stages.decision?.decision).toBe('HUMAN_GATE');
      expect(stages.decision?.totalDurationMs).toBe(3100);
    });
  });

  describe('TimelineButton Component', () => {
    it('renders button with icon and label', () => {
      const html = renderToStaticMarkup(React.createElement(TimelineButton));
      expect(html).toContain('timeline-trigger-btn');
      expect(html).toContain('Execution Timeline');
      expect(html).toContain('aria-label="Toggle Execution Timeline"');
    });

    it('displays event count badge when execution events exist', () => {
      useFactoryFlowStore.setState({
        executionEvents: [
          { stage: 'spec_generated', timestamp: '2026-11-02T10:00:00Z', payload: {} },
          { stage: 'build_completed', timestamp: '2026-11-02T10:01:00Z', payload: {} },
        ],
      });

      const html = renderToStaticMarkup(React.createElement(TimelineButton));
      expect(html).toContain('timeline-event-count');
      expect(html).toContain('2');
    });

    it('supports custom props for isOpen, onClick and eventCount', () => {
      const handleClick = vi.fn();
      const html = renderToStaticMarkup(
        React.createElement(TimelineButton, {
          isOpen: true,
          onClick: handleClick,
          eventCount: 7,
        })
      );
      expect(html).toContain('timeline-trigger-btn active');
      expect(html).toContain('7');
    });

    it('toggles showExecutionTimeline store flag on click', () => {
      expect(useFactoryFlowStore.getState().showExecutionTimeline).toBe(false);
      useFactoryFlowStore.getState().setShowExecutionTimeline(true);
      expect(useFactoryFlowStore.getState().showExecutionTimeline).toBe(true);
      useFactoryFlowStore.getState().setShowExecutionTimeline(false);
      expect(useFactoryFlowStore.getState().showExecutionTimeline).toBe(false);
    });
  });

  describe('ExecutionTimelineDrawer Component Rendering', () => {
    it('renders null when showExecutionTimeline is false', () => {
      useFactoryFlowStore.setState({ showExecutionTimeline: false });
      const html = renderToStaticMarkup(React.createElement(ExecutionTimelineDrawer));
      expect(html).toBe('');
    });

    it('renders null when isOpen is explicitly false', () => {
      useFactoryFlowStore.setState({ showExecutionTimeline: true });
      const html = renderToStaticMarkup(
        React.createElement(ExecutionTimelineDrawer, { isOpen: false })
      );
      expect(html).toBe('');
    });

    it('renders empty drawer when showExecutionTimeline is true and no events present', () => {
      useFactoryFlowStore.setState({ showExecutionTimeline: true, executionEvents: [] });
      const html = renderToStaticMarkup(React.createElement(ExecutionTimelineDrawer));
      expect(html).toContain('execution-timeline-drawer');
      expect(html).toContain('Execution Timeline &amp; Stage Inspector');
      expect(html).toContain('No execution events recorded yet');
    });

    it('renders Stage 1 Spec Synthesis details with impacted files and markdown preview toggle', () => {
      const specPayload: SpecStagePayload = {
        title: 'Feature: Automated Worktree Cleanup',
        impactedFiles: ['internal/sandbox/worktree.go', 'internal/sandbox/pool.go'],
        specMarkdown: '## Spec: Automated Worktree Cleanup\n- Prunes old git worktrees.',
      };

      useFactoryFlowStore.setState({
        showExecutionTimeline: true,
        executionEvents: [
          {
            stage: 'spec_generated',
            timestamp: '2026-11-02T10:00:00Z',
            payload: specPayload,
          },
        ],
      });

      const html = renderToStaticMarkup(React.createElement(ExecutionTimelineDrawer));
      expect(html).toContain('Spec Synthesis');
      expect(html).toContain('Feature: Automated Worktree Cleanup');
      expect(html).toContain('Impacted files:');
      expect(html).toContain('2');
      expect(html).toContain('internal/sandbox/worktree.go');
      expect(html).toContain('internal/sandbox/pool.go');
      expect(html).toContain('Toggle Spec Markdown');
    });

    it('renders Stage 2 TDD Build Loop breakdown, modified files, and Git diff viewer', () => {
      const buildPayload: BuildStagePayload = {
        phases: [
          { phase: 'red', name: 'Red (Unit Tests Fail)', exitCode: 1, durationMs: 950 },
          { phase: 'green', name: 'Green (Code Implemented)', exitCode: 0, durationMs: 2300 },
          { phase: 'refactor', name: 'Refactor (Code Quality)', exitCode: 0, durationMs: 1100 },
        ],
        filesModified: ['pkg/sandbox/manager.go', 'pkg/sandbox/manager_test.go'],
        diff: '--- a/pkg/sandbox/manager.go\n+++ b/pkg/sandbox/manager.go\n@@ -10,2 +10,4 @@\n+func PruneWorktrees() error {\n+  return nil\n+}',
      };

      useFactoryFlowStore.setState({
        showExecutionTimeline: true,
        executionEvents: [
          {
            stage: 'build_completed',
            timestamp: '2026-11-02T10:01:00Z',
            payload: buildPayload,
          },
        ],
      });

      const html = renderToStaticMarkup(React.createElement(ExecutionTimelineDrawer));
      expect(html).toContain('TDD Build Loop');
      expect(html).toContain('phase-badge-red');
      expect(html).toContain('Red (Unit Tests Fail)');
      expect(html).toContain('exit: 1');
      expect(html).toContain('950ms');
      expect(html).toContain('phase-badge-green');
      expect(html).toContain('Green (Code Implemented)');
      expect(html).toContain('exit: 0');
      expect(html).toContain('2.3s');
      expect(html).toContain('phase-badge-refactor');
      expect(html).toContain('Refactor (Code Quality)');
      expect(html).toContain('pkg/sandbox/manager.go');
      expect(html).toContain('modified-file-clickable');
      expect(html).toContain('✏️ Edit');
      expect(html).toContain('diff-container');
      expect(html).toContain('func PruneWorktrees');
    });

    it('renders Stage 3 Reviewers & Audit with Security and Architecture cards and findings', () => {
      const reviewPayload: ReviewStagePayload = {
        consolidatedScore: 97,
        severity: 'clean',
        reviewers: [
          {
            id: 'sec-audit',
            name: 'Security Reviewer',
            severity: 'clean',
            score: 100,
            findings: [
              'Zero OWASP Top 10 vulnerabilities detected',
              'Rootless container isolation active',
            ],
          },
          {
            id: 'arch-audit',
            name: 'Architecture Reviewer',
            severity: 'low',
            score: 94,
            findings: [
              'Domain boundary respected between sandbox and gateway',
              'Small advisory: consider memoizing pool state',
            ],
          },
        ],
      };

      useFactoryFlowStore.setState({
        showExecutionTimeline: true,
        executionEvents: [
          {
            stage: 'review_decided',
            timestamp: '2026-11-02T10:02:00Z',
            payload: reviewPayload,
          },
        ],
      });

      const html = renderToStaticMarkup(React.createElement(ExecutionTimelineDrawer));
      expect(html).toContain('Reviewers &amp; Audit');
      expect(html).toContain('Consolidated Score:');
      expect(html).toContain('97%');
      expect(html).toContain('Security Reviewer');
      expect(html).toContain('100%');
      expect(html).toContain('Zero OWASP Top 10 vulnerabilities detected');
      expect(html).toContain('Architecture Reviewer');
      expect(html).toContain('94%');
      expect(html).toContain('Domain boundary respected between sandbox and gateway');
    });

    it('renders reviewer card without findings with clean indicator', () => {
      const reviewPayload: ReviewStagePayload = {
        consolidatedScore: 99,
        severity: 'clean',
        reviewers: [
          {
            id: 'clean-reviewer',
            name: 'Pristine Reviewer',
            severity: 'clean',
            score: 99,
            findings: [],
          },
        ],
      };

      const html = renderToStaticMarkup(
        React.createElement(ExecutionTimelineDrawer, {
          isOpen: true,
          events: [
            {
              stage: 'review_decided',
              timestamp: '2026-11-02T10:02:00Z',
              payload: reviewPayload,
            },
          ],
        })
      );
      expect(html).toContain('No findings reported. Verified clean.');
    });

    it('renders Stage 4 Gate & Decision with APPROVED decision banner and total duration', () => {
      const decisionPayload: DecisionStagePayload = {
        decision: 'APPROVED',
        totalDurationMs: 8400,
        requiresHumanGate: false,
        summary: 'All checks passed automatically via Fast-Track.',
      };

      useFactoryFlowStore.setState({
        showExecutionTimeline: true,
        executionEvents: [
          {
            stage: 'pipeline_completed',
            timestamp: '2026-11-02T10:03:00Z',
            payload: decisionPayload,
          },
        ],
      });

      const html = renderToStaticMarkup(React.createElement(ExecutionTimelineDrawer));
      expect(html).toContain('Gate &amp; Decision');
      expect(html).toContain('decision-badge-approved');
      expect(html).toContain('APPROVED');
      expect(html).toContain('8.4s');
      expect(html).toContain('All checks passed automatically via Fast-Track.');
      expect(html).not.toContain('Resolve Gate Action');
    });

    it('renders Stage 4 Gate & Decision with RETRY decision banner', () => {
      const decisionPayload: DecisionStagePayload = {
        decision: 'RETRY',
        totalDurationMs: 5000,
        summary: 'Unit test suite failed. Triggering automated retry iteration.',
      };

      const html = renderToStaticMarkup(
        React.createElement(ExecutionTimelineDrawer, {
          isOpen: true,
          events: [
            {
              stage: 'pipeline_completed',
              timestamp: '2026-11-02T10:03:00Z',
              payload: decisionPayload,
            },
          ],
        })
      );
      expect(html).toContain('decision-badge-retry');
      expect(html).toContain('RETRY');
      expect(html).toContain('Unit test suite failed');
    });

    it('renders Stage 4 Gate & Decision with HUMAN_GATE banner and Open Gate Modal trigger button', () => {
      const decisionPayload: DecisionStagePayload = {
        decision: 'HUMAN_GATE',
        totalDurationMs: 12500,
        requiresHumanGate: true,
        summary: 'Sensitive operation requires human authorization before merge.',
      };

      useFactoryFlowStore.setState({
        showExecutionTimeline: true,
        executionEvents: [
          {
            stage: 'pipeline_completed',
            timestamp: '2026-11-02T10:03:00Z',
            payload: decisionPayload,
          },
        ],
      });

      const html = renderToStaticMarkup(React.createElement(ExecutionTimelineDrawer));
      expect(html).toContain('decision-badge-human-gate');
      expect(html).toContain('HUMAN_GATE');
      expect(html).toContain('12.5s');
      expect(html).toContain('Resolve Gate Action');
    });

    it('renders chronological stage timeline with active/completed indicators when multiple stages present', () => {
      useFactoryFlowStore.setState({
        showExecutionTimeline: true,
        executionEvents: [
          {
            stage: 'spec_generated',
            timestamp: '2026-11-02T10:00:00Z',
            payload: { title: 'Spec Title', impactedFiles: ['file1.ts'] },
          },
          {
            stage: 'build_completed',
            timestamp: '2026-11-02T10:01:00Z',
            payload: {
              phases: [{ phase: 'red', name: 'Red', exitCode: 1, durationMs: 500 }],
              filesModified: ['file1.ts'],
            },
          },
          {
            stage: 'review_decided',
            timestamp: '2026-11-02T10:02:00Z',
            payload: {
              consolidatedScore: 92,
              reviewers: [{ id: 'sec', name: 'Security Reviewer', score: 92, severity: 'clean', findings: [] }],
            },
          },
          {
            stage: 'pipeline_completed',
            timestamp: '2026-11-02T10:03:00Z',
            payload: { decision: 'APPROVED', totalDurationMs: 4000 },
          },
        ],
      });

      const html = renderToStaticMarkup(React.createElement(ExecutionTimelineDrawer));
      expect(html).toContain('stage-card-spec');
      expect(html).toContain('stage-card-build');
      expect(html).toContain('stage-card-review');
      expect(html).toContain('stage-card-decision');
    });

    it('renders interactive gate banner with "Aprobar y Mergear" and "Rechazar" buttons when status is gate_waiting', () => {
      useFactoryFlowStore.setState({
        showExecutionTimeline: true,
        executionEvents: [
          {
            stage: 'gate_waiting',
            timestamp: '2026-11-02T10:05:00Z',
            payload: {
              status: 'gate_waiting',
              run_id: 'task-interactive-101',
              token: 'task-interactive-101:1700000000:abcdeffedcba1234',
              diff: 'diff --git a/pkg.go b/pkg.go',
              reason: 'Security sign-off required for privilege escalation',
            },
          },
        ],
      });

      const html = renderToStaticMarkup(React.createElement(ExecutionTimelineDrawer));
      expect(html).toContain('gate-waiting-interactive-card');
      expect(html).toContain('gate_waiting');
      expect(html).toContain('Security sign-off required for privilege escalation');
      expect(html).toContain('Aprobar y Mergear');
      expect(html).toContain('Rechazar');
      expect(html).toContain('task-interactive-');
    });

    it('extractTimelineStages extracts gate_waiting event diff and reason correctly', () => {
      const stages = extractTimelineStages([
        {
          stage: 'gate_waiting',
          timestamp: '2026-11-02T10:05:00Z',
          payload: {
            status: 'gate_waiting',
            run_id: 'run-stage-test',
            token: 'test-token-value',
            diff: '--- a/main.go\n+++ b/main.go\n@@ -1,1 +1,2 @@\n+func New() {}',
            reason: 'Manual QA check required',
          },
        },
      ]);

      expect(stages.decision?.status).toBe('gate_waiting');
      expect(stages.decision?.decision).toBe('HUMAN_GATE');
      expect(stages.decision?.requiresHumanGate).toBe(true);
      expect(stages.decision?.token).toBe('test-token-value');
      expect(stages.decision?.reason).toBe('Manual QA check required');
      expect(stages.build?.diff).toContain('func New() {}');
    });
  });
});
