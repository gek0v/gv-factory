// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import {
  FactoryRunModal,
  QUICK_MODEL_OPTIONS,
  parseCommaOrNewlineSeparated,
} from './FactoryRunModal';
import { useFactoryFlowStore } from '../../store';
import * as factoryApi from '../../api/factory';

describe('FactoryRunModal Component & Helpers', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useFactoryFlowStore.setState({
      showFactoryRunModal: false,
      activePipelineStage: 'idle',
      activeRunOutcome: null,
      activeRunId: null,
      activeApproval: null,
      showGateModal: false,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('parseCommaOrNewlineSeparated helper', () => {
    it('splits comma and newline separated strings into trimmed lists', () => {
      expect(parseCommaOrNewlineSeparated('backend, security, p1')).toEqual([
        'backend',
        'security',
        'p1',
      ]);
      expect(parseCommaOrNewlineSeparated('task1\ntask2\n\ntask3')).toEqual([
        'task1',
        'task2',
        'task3',
      ]);
      expect(parseCommaOrNewlineSeparated('')).toEqual([]);
      expect(parseCommaOrNewlineSeparated('   ')).toEqual([]);
    });
  });

  describe('Modal rendering', () => {
    it('renders null when showFactoryRunModal is false', () => {
      useFactoryFlowStore.setState({ showFactoryRunModal: false });
      const html = renderToStaticMarkup(React.createElement(FactoryRunModal));
      expect(html).toBe('');
    });

    it('renders modal dialog when showFactoryRunModal is true', () => {
      useFactoryFlowStore.setState({ showFactoryRunModal: true });
      const html = renderToStaticMarkup(React.createElement(FactoryRunModal));

      expect(html).toContain('factory-run-modal-backdrop');
      expect(html).toContain('factory-run-modal-dialog');
      expect(html).toContain('role="dialog"');
      expect(html).toContain('aria-modal="true"');
      expect(html).toContain('Execute Factory Pipeline');
      expect(html).toContain('Task Title');
      expect(html).toContain('Description');
      expect(html).toContain('Target LLM Model');
      expect(html).toContain('Labels');
      expect(html).toContain('Constraints');
      expect(html).toContain('Dispatch Autonomous Run');
    });

    it('renders all quick target LLM model options and pills', () => {
      useFactoryFlowStore.setState({ showFactoryRunModal: true });
      const html = renderToStaticMarkup(React.createElement(FactoryRunModal));

      expect(html).toContain('gemini-3.8-flash-high');
      expect(html).toContain('gpt-4o');
      expect(html).toContain('gpt-4o-mini');
      expect(html).toContain('claude-3-5-sonnet');
      expect(html).toContain('deepseek-chat');
      expect(html).toContain('ollama/llama3.3');
      expect(html).toContain('Custom Model...');
      expect(html).toContain('model-quick-pills');
      expect(html).toContain('btn-model-chip');
    });

    it('verifies QUICK_MODEL_OPTIONS contains all required models', () => {
      const modelValues = QUICK_MODEL_OPTIONS.map((opt) => opt.value);
      expect(modelValues).toContain('gemini-3.8-flash-high');
      expect(modelValues).toContain('gpt-4o');
      expect(modelValues).toContain('gpt-4o-mini');
      expect(modelValues).toContain('claude-3-5-sonnet');
      expect(modelValues).toContain('deepseek-chat');
      expect(modelValues).toContain('ollama/llama3.3');
      expect(modelValues).toContain('custom');
    });

    it('contains close button and cancel button', () => {
      useFactoryFlowStore.setState({ showFactoryRunModal: true });
      const html = renderToStaticMarkup(React.createElement(FactoryRunModal));

      expect(html).toContain('aria-label="Close modal"');
      expect(html).toContain('Cancel');
    });

    it('renders with isOpen prop true even if store is false', () => {
      useFactoryFlowStore.setState({ showFactoryRunModal: false });
      const html = renderToStaticMarkup(
        React.createElement(FactoryRunModal, { isOpen: true })
      );
      expect(html).toContain('factory-run-modal-backdrop');
    });

    it('renders null when isOpen prop is explicitly false', () => {
      useFactoryFlowStore.setState({ showFactoryRunModal: true });
      const html = renderToStaticMarkup(
        React.createElement(FactoryRunModal, { isOpen: false })
      );
      expect(html).toBe('');
    });
  });

  describe('Form submission and dispatching', () => {
    it('dispatches factory run and updates store state', async () => {
      const dispatchSpy = vi.spyOn(factoryApi, 'dispatchFactoryRun').mockResolvedValue({
        run_id: 'run-auton-500',
        status: 'queued',
        stage: 'spec_generated',
        message: 'Pipeline dispatched successfully',
        approval_token: 'hmac-token-secret-500',
      });

      const store = useFactoryFlowStore.getState();
      store.setShowFactoryRunModal(true);

      // Simulate API call from modal
      const res = await factoryApi.dispatchFactoryRun({
        title: 'Implement Live Pipeline Execution',
        description: 'Connect React Flow to backend execution engine',
        labels: ['frontend', 'dag'],
        constraints: ['Zero regressions', '90% test coverage'],
      });

      expect(dispatchSpy).toHaveBeenCalledTimes(1);
      expect(res.run_id).toBe('run-auton-500');

      store.setActiveRunOutcome({
        run_id: res.run_id,
        status: res.status,
        stage: 'spec_generated',
        approval_token: res.approval_token,
      });

      const updated = useFactoryFlowStore.getState();
      expect(updated.activeRunOutcome?.run_id).toBe('run-auton-500');
      expect(updated.activePipelineStage).toBe('spec_generated');

      // SpecWriter node should now be highlighted
      const specNode = updated.nodes.find(
        (n) => n.id.includes('spec') || (n.data.label || '').includes('Spec')
      );
      expect(specNode?.data.highlighted).toBe(true);
    });

    it('dispatches factory run with specified target LLM model', async () => {
      const dispatchSpy = vi.spyOn(factoryApi, 'dispatchFactoryRun').mockResolvedValue({
        run_id: 'run-auton-501',
        status: 'queued',
        stage: 'spec_generated',
      });

      const res = await factoryApi.dispatchFactoryRun({
        title: 'Run with DeepSeek',
        model: 'deepseek-chat',
      });

      expect(dispatchSpy).toHaveBeenCalledWith({
        title: 'Run with DeepSeek',
        model: 'deepseek-chat',
      });
      expect(res.run_id).toBe('run-auton-501');
    });

    it('handles dispatch failure and displays error feedback in UI', async () => {
      vi.spyOn(factoryApi, 'dispatchFactoryRun').mockRejectedValue(
        new Error('Network error connecting to /api/v1/factory/run')
      );

      await expect(
        factoryApi.dispatchFactoryRun({ title: 'Failing Test' })
      ).rejects.toThrow('Network error connecting to /api/v1/factory/run');
    });
  });
});
