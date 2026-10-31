// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import type { FactoryTemplate } from './types';

export const bugfixExpressTemplate: FactoryTemplate = {
  apiVersion: 'factory.ov.io/v1alpha1',
  kind: 'FactoryTemplate',
  metadata: {
    name: 'bugfix-express',
    version: '0.1.0',
    description:
      'Lightweight pipeline for log error remediation with automated fast-track to main upon passing tests.',
  },
  spec: {
    entrypoint: 'error-log-ingestion',
    steps: [
      {
        id: 'error-log-ingestion',
        name: 'Error Log Ingestion',
        plugin: 'extism://plugins/ingestion.wasm',
        config: { sources: ['sentry', 'datadog', 'github_issue', 'log_stream'] },
      },
      {
        id: 'bug-localizer',
        name: 'Bug Localizer (Trace & AST)',
        plugin: 'extism://plugins/spec-writer.wasm',
        depends_on: ['error-log-ingestion'],
        config: { stack_trace_analysis: true, output_format: 'reproduction-test' },
      },
      {
        id: 'builder-tdd',
        name: 'Fast TDD Patch Runner',
        plugin: 'extism://plugins/builder-tdd.wasm',
        depends_on: ['bug-localizer'],
        config: { mode: 'red-green-refactor', max_iterations: 3 },
      },
      {
        id: 'code-verifier',
        name: 'Automated Test & Lint Verifier',
        plugin: 'extism://plugins/reviewer-code.wasm',
        depends_on: ['builder-tdd'],
        config: { clean_context: true, strict_linter: true },
      },
      {
        id: 'fast-track-merger',
        name: 'Fast-Track Merge Gate',
        plugin: 'extism://plugins/approver.wasm',
        depends_on: ['code-verifier'],
        config: { auto_fast_track: true, feedback_loop_limit: 2 },
      },
    ],
    gates: {
      min_coverage_percent: 85.0,
      max_security_alerts: 0,
      require_approval: false,
    },
  },
};

export const featureCompleteTemplate: FactoryTemplate = {
  apiVersion: 'factory.ov.io/v1alpha1',
  kind: 'FactoryTemplate',
  metadata: {
    name: 'feature-complete',
    version: '0.1.0',
    description:
      'Comprehensive pipeline with SDD spec-writer, builder-tdd, 4 parallel reviewers (security, arch, uiux, code), and mandatory human approval gate.',
  },
  spec: {
    entrypoint: 'ingestion',
    steps: [
      {
        id: 'ingestion',
        name: 'Feature Ingestion & Backlog Normalizer',
        plugin: 'extism://plugins/ingestion.wasm',
        config: { sources: ['github_issue', 'jira', 'product_spec'] },
      },
      {
        id: 'spec-writer',
        name: 'SDD Spec Writer & Acceptance Criteria',
        plugin: 'extism://plugins/spec-writer.wasm',
        depends_on: ['ingestion'],
        config: { memory_retrieval: true, output_format: 'sdd-tdd' },
      },
      {
        id: 'builder-tdd',
        name: 'Builder TDD Sandbox Runner',
        plugin: 'extism://plugins/builder-tdd.wasm',
        depends_on: ['spec-writer'],
        config: { mode: 'red-green-refactor', max_iterations: 5 },
      },
      {
        id: 'security-reviewer',
        name: 'Security Reviewer (OWASP & Secrets)',
        plugin: 'extism://plugins/reviewer-security.wasm',
        parallel: true,
        depends_on: ['builder-tdd'],
        config: { clean_context: true },
      },
      {
        id: 'arch-reviewer',
        name: 'Architecture & Standards Reviewer',
        plugin: 'extism://plugins/reviewer-arch.wasm',
        parallel: true,
        depends_on: ['builder-tdd'],
        config: { clean_context: true },
      },
      {
        id: 'ui-ux-reviewer',
        name: 'UI/UX & Accessibility Reviewer',
        plugin: 'extism://plugins/reviewer-uiux.wasm',
        parallel: true,
        depends_on: ['builder-tdd'],
        config: { clean_context: true },
      },
      {
        id: 'code-reviewer',
        name: 'Code Quality & Coverage Reviewer',
        plugin: 'extism://plugins/reviewer-code.wasm',
        parallel: true,
        depends_on: ['builder-tdd'],
        config: { clean_context: true },
      },
      {
        id: 'gate-approver',
        name: 'Approver & Human Decision Gate',
        plugin: 'extism://plugins/approver.wasm',
        depends_on: ['security-reviewer', 'arch-reviewer', 'ui-ux-reviewer', 'code-reviewer'],
        config: { feedback_loop_limit: 3 },
      },
    ],
    gates: {
      min_coverage_percent: 90.0,
      max_security_alerts: 0,
      require_approval: true,
    },
  },
};

export const refactorDocTemplate: FactoryTemplate = {
  apiVersion: 'factory.ov.io/v1alpha1',
  kind: 'FactoryTemplate',
  metadata: {
    name: 'refactor-doc',
    version: '0.1.0',
    description:
      'Non-destructive pipeline focusing on AST complexity analysis, documentation sync, and zero business logic alteration.',
  },
  spec: {
    entrypoint: 'refactor-trigger',
    steps: [
      {
        id: 'refactor-trigger',
        name: 'Refactor Event Trigger',
        plugin: 'extism://plugins/ingestion.wasm',
        config: { sources: ['cron', 'codebase_recon', 'manual_trigger'] },
      },
      {
        id: 'ast-analyzer',
        name: 'Tree-sitter AST & Complexity Analyzer',
        plugin: 'extism://plugins/ast-analyzer.wasm',
        depends_on: ['refactor-trigger'],
        config: { max_cyclomatic_complexity: 15, enable_ast_grep: true },
      },
      {
        id: 'doc-sync',
        name: 'Documentation & Architecture Synchronizer',
        plugin: 'extism://plugins/doc-sync.wasm',
        depends_on: ['ast-analyzer'],
        config: { sync_adr: true, generate_tsdoc: true },
      },
      {
        id: 'verify-no-logic-change',
        name: 'Invariance & Regression Verifier',
        plugin: 'extism://plugins/builder-tdd.wasm',
        depends_on: ['doc-sync'],
        config: { mode: 'regression-invariance', require_zero_behavioral_diff: true },
      },
      {
        id: 'approver',
        name: 'Non-Destructive Refactor Approver',
        plugin: 'extism://plugins/approver.wasm',
        depends_on: ['verify-no-logic-change'],
        config: { fast_track_docs_only: true },
      },
    ],
    gates: {
      min_coverage_percent: 90.0,
      max_security_alerts: 0,
      require_approval: true,
    },
  },
};

export const CANONICAL_TEMPLATES: Record<string, { label: string; template: FactoryTemplate }> = {
  'bugfix-express': {
    label: 'Bugfix Express (Fast-Track)',
    template: bugfixExpressTemplate,
  },
  'feature-complete': {
    label: 'Feature Complete (4 Reviewers + Gate)',
    template: featureCompleteTemplate,
  },
  'refactor-doc': {
    label: 'Refactor & Docs (Non-Destructive)',
    template: refactorDocTemplate,
  },
};
