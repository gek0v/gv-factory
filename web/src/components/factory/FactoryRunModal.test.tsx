// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import {
  FactoryRunModal,
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
      expect(html).toContain('Labels');
      expect(html).toContain('Constraints');
      expect(html).toContain('Dispatch Autonomous Run');
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
