// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import {
  CostBudgetModal,
  CostBudgetButton,
  calculateBudgetPercentage,
  getBudgetStatus,
} from './CostBudgetModal';
import { CostBadge, formatUSD, formatTokenCount } from './CostBadge';
import { useFactoryFlowStore } from '../../store';
import * as telemetryApi from '../../api/telemetry';
import type { RunCostSummary } from '../../types/telemetry';

describe('Cost & Budget Guardrails Components and Helpers', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useFactoryFlowStore.setState({
      showCostBudgetModal: false,
    } as any);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('Pure Formatters & Computation Helpers', () => {
    it('formatUSD handles various currency amounts accurately', () => {
      expect(formatUSD(0)).toBe('$0.00');
      expect(formatUSD(-5)).toBe('$0.00');
      expect(formatUSD(NaN)).toBe('$0.00');
      expect(formatUSD(0.0004)).toBe('$0.0004');
      expect(formatUSD(0.0085)).toBe('$0.0085');
      expect(formatUSD(1.5)).toBe('$1.50');
      expect(formatUSD(12.3456)).toBe('$12.35');
    });

    it('formatTokenCount formats token integers cleanly', () => {
      expect(formatTokenCount(0)).toBe('0');
      expect(formatTokenCount(-10)).toBe('0');
      expect(formatTokenCount(NaN)).toBe('0');
      expect(formatTokenCount(450)).toBe('450');
      expect(formatTokenCount(1500)).toBe('1.5k');
      expect(formatTokenCount(45200)).toBe('45.2k');
      expect(formatTokenCount(1200000)).toBe('1.2M');
    });

    it('calculateBudgetPercentage computes clamped percentages', () => {
      expect(calculateBudgetPercentage(0, 10)).toBe(0);
      expect(calculateBudgetPercentage(5, 10)).toBe(50);
      expect(calculateBudgetPercentage(10, 10)).toBe(100);
      expect(calculateBudgetPercentage(15, 10)).toBe(100);
      expect(calculateBudgetPercentage(5, 0)).toBe(0);
      expect(calculateBudgetPercentage(-2, 10)).toBe(0);
    });

    it('getBudgetStatus evaluates normal, warning, and exceeded states', () => {
      // 1. Nil summary fallback
      expect(getBudgetStatus(null).status).toBe('normal');

      // 2. Normal usage (30% consumed)
      const normalSummary: RunCostSummary = {
        run_id: 'run-normal',
        prompt_tokens: 3000,
        completion_tokens: 1000,
        total_tokens: 4000,
        total_cost_usd: 3.0,
        guardrail: { max_budget_usd: 10.0, max_tokens: 100000 },
        exceeded: false,
        warning_triggered: false,
        models: {},
      };
      const normalRes = getBudgetStatus(normalSummary);
      expect(normalRes.status).toBe('normal');
      expect(normalRes.color).toBe('#10b981');

      // 3. Warning usage (85% consumed)
      const warnSummary: RunCostSummary = {
        ...normalSummary,
        total_cost_usd: 8.5,
        warning_triggered: true,
      };
      const warnRes = getBudgetStatus(warnSummary);
      expect(warnRes.status).toBe('warning');
      expect(warnRes.color).toBe('#f59e0b');
      expect(warnRes.message).toContain('85%');

      // 4. Exceeded usage
      const exceededSummary: RunCostSummary = {
        ...normalSummary,
        total_cost_usd: 11.2,
        exceeded: true,
        exceeded_reason: 'cost limit breached: $11.20 > $10.00',
      };
      const exceededRes = getBudgetStatus(exceededSummary);
      expect(exceededRes.status).toBe('exceeded');
      expect(exceededRes.color).toBe('#ef4444');
      expect(exceededRes.message).toContain('cost limit breached');
    });
  });

  describe('CostBadge Component', () => {
    it('renders normal badge with formatted cost and tokens', () => {
      const summary: RunCostSummary = {
        run_id: 'run-badge-1',
        prompt_tokens: 5000,
        completion_tokens: 2500,
        total_tokens: 7500,
        total_cost_usd: 0.05,
        guardrail: { max_budget_usd: 5.0 },
        exceeded: false,
        warning_triggered: false,
        models: {},
      };

      const html = renderToStaticMarkup(
        React.createElement(CostBadge, { summary, showTokens: true })
      );

      expect(html).toContain('cost-badge-normal');
      expect(html).toContain('$0.05');
      expect(html).toContain('7.5k tok');
    });

    it('renders warning badge styling when warning is active', () => {
      const html = renderToStaticMarkup(
        React.createElement(CostBadge, {
          totalCostUSD: 8.5,
          totalTokens: 85000,
          warning: true,
        })
      );

      expect(html).toContain('cost-badge-warning');
      expect(html).toContain('$8.50');
      expect(html).toContain('85.0k tok');
    });

    it('renders exceeded badge styling when limit is breached', () => {
      const html = renderToStaticMarkup(
        React.createElement(CostBadge, {
          totalCostUSD: 15.0,
          totalTokens: 120000,
          exceeded: true,
        })
      );

      expect(html).toContain('cost-badge-exceeded');
      expect(html).toContain('LIMIT');
    });

    it('handles custom size and hidden tokens prop', () => {
      const html = renderToStaticMarkup(
        React.createElement(CostBadge, {
          totalCostUSD: 1.25,
          totalTokens: 1000,
          size: 'sm',
          showTokens: false,
        })
      );

      expect(html).toContain('$1.25');
      expect(html).not.toContain('cost-badge-tokens');
    });
  });

  describe('CostBudgetModal Component Rendering', () => {
    const mockSummary: RunCostSummary = {
      run_id: 'run-test-modal',
      task_id: 'task-test-456',
      prompt_tokens: 25000,
      completion_tokens: 5000,
      total_tokens: 30000,
      total_cost_usd: 0.15,
      guardrail: {
        max_budget_usd: 1.0,
        max_tokens: 100000,
        warning_threshold: 0.8,
      },
      exceeded: false,
      warning_triggered: false,
      models: {
        'claude-3-5-sonnet': {
          model: 'claude-3-5-sonnet',
          display_name: 'Claude 3.5 Sonnet',
          prompt_tokens: 25000,
          completion_tokens: 5000,
          total_tokens: 30000,
          cost_usd: 0.15,
        },
      },
      updated_at: '2026-11-02T22:00:00Z',
    };

    it('renders null when closed', () => {
      const html = renderToStaticMarkup(
        React.createElement(CostBudgetModal, { isOpen: false })
      );
      expect(html).toBe('');
    });

    it('renders modal dialog when isOpen is true', () => {
      const html = renderToStaticMarkup(
        React.createElement(CostBudgetModal, {
          isOpen: true,
          summary: mockSummary,
        })
      );

      expect(html).toContain('Cost &amp; Token Budget Guardrails');
      expect(html).toContain('run-test-modal');
      expect(html).toContain('Input (Prompt)');
      expect(html).toContain('Output (Completion)');
      expect(html).toContain('25.0k');
      expect(html).toContain('5.0k');
      expect(html).toContain('30.0k');
      expect(html).toContain('$0.15');
      expect(html).toContain('Claude 3.5 Sonnet');
    });

    it('renders live budget bar and warning banner when quota limit approached', () => {
      const warningSummary: RunCostSummary = {
        ...mockSummary,
        total_cost_usd: 0.85,
        warning_triggered: true,
      };

      const html = renderToStaticMarkup(
        React.createElement(CostBudgetModal, {
          isOpen: true,
          summary: warningSummary,
        })
      );

      expect(html).toContain('Warning: Quota Approaching');
      expect(html).toContain('85% Consumed');
    });

    it('renders critical alert when budget guardrail is exceeded', () => {
      const exceededSummary: RunCostSummary = {
        ...mockSummary,
        total_cost_usd: 1.5,
        exceeded: true,
        exceeded_reason: 'cost limit breached: $1.5000 > $1.00',
      };

      const html = renderToStaticMarkup(
        React.createElement(CostBudgetModal, {
          isOpen: true,
          summary: exceededSummary,
        })
      );

      expect(html).toContain('Guardrail Breached');
      expect(html).toContain('cost limit breached');
    });

    it('renders empty model message when no models recorded yet', () => {
      const emptyModelsSummary: RunCostSummary = {
        ...mockSummary,
        models: {},
      };

      const html = renderToStaticMarkup(
        React.createElement(CostBudgetModal, {
          isOpen: true,
          summary: emptyModelsSummary,
        })
      );

      expect(html).toContain('No model telemetry recorded yet');
    });

    it('integrates with store showCostBudgetModal state', () => {
      useFactoryFlowStore.setState({ showCostBudgetModal: true });
      const html = renderToStaticMarkup(
        React.createElement(CostBudgetModal, { summary: mockSummary })
      );
      expect(html).toContain('Cost &amp; Token Budget Guardrails');
    });
  });

  describe('CostBudgetButton Component', () => {
    it('renders CostBadge with toggle behavior on store', () => {
      const html = renderToStaticMarkup(
        React.createElement(CostBudgetButton, {
          summary: {
            run_id: 'btn-run',
            prompt_tokens: 1000,
            completion_tokens: 500,
            total_tokens: 1500,
            total_cost_usd: 0.02,
            guardrail: {},
            exceeded: false,
            warning_triggered: false,
            models: {},
          },
        })
      );

      expect(html).toContain('cost-badge');
      expect(html).toContain('$0.02');
    });

    it('toggles store state', () => {
      expect(useFactoryFlowStore.getState().showCostBudgetModal).toBe(false);
      useFactoryFlowStore.getState().setShowCostBudgetModal(true);
      expect(useFactoryFlowStore.getState().showCostBudgetModal).toBe(true);
      useFactoryFlowStore.getState().setShowCostBudgetModal(false);
      expect(useFactoryFlowStore.getState().showCostBudgetModal).toBe(false);
    });
  });

  describe('Telemetry API Client Methods', () => {
    it('fetchCostsOverview fetches aggregate telemetry', async () => {
      const mockOverview = {
        runs: [],
        total_runs: 0,
        total_tokens: 0,
        total_cost_usd: 0,
        total_prompt_tokens: 0,
        total_completion_tokens: 0,
      };

      const customFetch = vi.fn().mockResolvedValue({
        ok: true,
        json: async () => mockOverview,
      });

      const res = await telemetryApi.fetchCostsOverview({
        baseUrl: 'http://test:8080',
        customFetch: customFetch as any,
      });

      expect(res).toEqual(mockOverview);
      expect(customFetch).toHaveBeenCalledWith(
        'http://test:8080/api/v1/telemetry/costs',
        expect.any(Object)
      );
    });

    it('fetchRunCost fetches single run telemetry and handles error response', async () => {
      const customFetchErr = vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
        json: async () => ({ error: 'run not found' }),
      });

      await expect(
        telemetryApi.fetchRunCost('run-404', { customFetch: customFetchErr as any })
      ).rejects.toThrow('run not found');
    });

    it('updateRunGuardrail posts updated limits', async () => {
      const updatedSummary = {
        run_id: 'run-guard',
        guardrail: { max_budget_usd: 25.0 },
      };

      const customFetch = vi.fn().mockResolvedValue({
        ok: true,
        json: async () => updatedSummary,
      });

      const res = await telemetryApi.updateRunGuardrail(
        'run-guard',
        { max_budget_usd: 25.0 },
        { customFetch: customFetch as any }
      );

      expect(res).toEqual(updatedSummary);
      expect(customFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/telemetry/costs/run-guard/guardrail'),
        expect.objectContaining({ method: 'POST' })
      );
    });
  });
});
