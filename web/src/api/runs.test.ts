// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
  serializeCanvasToTemplate,
  validateCanvasGraph,
  exportPipelineTemplate,
  submitPipelineRun,
  cancelPipelineRun,
} from './runs';
import type { FactoryFlowNode, FactoryFlowEdge } from '../types/flow';
import { defaultGatewayClient } from './gateway';

describe('api/runs', () => {
  const sampleNodes: FactoryFlowNode[] = [
    {
      id: 'trigger-1',
      type: 'trigger',
      position: { x: 0, y: 0 },
      data: {
        label: 'GitHub Webhook Ingestion',
        nodeType: 'trigger',
        subtype: 'webhook',
        status: 'pending',
        pluginTag: 'extism://plugins/inbound-webhook.wasm',
      },
    },
    {
      id: 'builder-1',
      type: 'agent',
      position: { x: 200, y: 0 },
      data: {
        label: 'TDD Builder Agent',
        nodeType: 'agent',
        role: 'builder_tdd',
        model: 'claude-3-5-sonnet',
        status: 'pending',
        pluginTag: 'extism://plugins/builder-agent.wasm',
      },
    },
    {
      id: 'approver-1',
      type: 'logic',
      position: { x: 400, y: 0 },
      data: {
        label: 'Quality Gate & Approver',
        nodeType: 'logic',
        subtype: 'threshold',
        role: 'approver' as any,
        status: 'pending',
        pluginTag: 'extism://plugins/gate-evaluator.wasm',
      },
    },
  ];

  const sampleEdges: FactoryFlowEdge[] = [
    {
      id: 'e1',
      source: 'trigger-1',
      target: 'builder-1',
    },
    {
      id: 'e2',
      source: 'builder-1',
      target: 'approver-1',
    },
  ];

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  describe('serializeCanvasToTemplate', () => {
    it('serializes nodes and edges to standard FactoryTemplate', () => {
      const template = serializeCanvasToTemplate(sampleNodes, sampleEdges, {
        metadata: { name: 'sample-flow', version: '1.2.3' },
      });

      expect(template.apiVersion).toBe('factory.ov.io/v1alpha1');
      expect(template.kind).toBe('FactoryTemplate');
      expect(template.metadata.name).toBe('sample-flow');
      expect(template.metadata.version).toBe('1.2.3');
      expect(template.spec.entrypoint).toBe('trigger-1');
      expect(template.spec.steps).toHaveLength(3);

      const builderStep = template.spec.steps.find((s) => s.id === 'builder-1');
      expect(builderStep?.depends_on).toEqual(['trigger-1']);
      expect(builderStep?.plugin).toBe('extism://plugins/builder-agent.wasm');
    });
  });

  describe('validateCanvasGraph', () => {
    it('returns valid for acyclic connected graph with entrypoint', () => {
      const res = validateCanvasGraph(sampleNodes, sampleEdges);
      expect(res.isValid).toBe(true);
      expect(res.errors).toHaveLength(0);
      expect(res.template).toBeDefined();
    });

    it('rejects empty canvas', () => {
      const res = validateCanvasGraph([], []);
      expect(res.isValid).toBe(false);
      expect(res.errors).toContain('Canvas is empty: at least one node is required to build a pipeline.');
    });

    it('detects cycles in edges', () => {
      const cyclicEdges: FactoryFlowEdge[] = [
        ...sampleEdges,
        { id: 'cycle', source: 'approver-1', target: 'builder-1' },
      ];
      const res = validateCanvasGraph(sampleNodes, cyclicEdges);
      expect(res.isValid).toBe(false);
      expect(res.errors.some((err) => err.toLowerCase().includes('cycle'))).toBe(true);
    });

    it('warns or rejects when plugin tag is invalid or missing', () => {
      const badNodes: FactoryFlowNode[] = [
        {
          id: 'bad-1',
          type: 'agent',
          position: { x: 0, y: 0 },
          data: {
            label: 'Bad Node',
            nodeType: 'agent',
            role: 'builder_tdd',
            model: 'claude-3-5-sonnet',
            status: 'pending',
            pluginTag: 'invalid-tag',
          },
        },
      ];
      const res = validateCanvasGraph(badNodes, []);
      expect(res.isValid).toBe(false);
      expect(res.errors.some((err) => err.includes('extism://'))).toBe(true);
    });
  });

  describe('exportPipelineTemplate', () => {
    it('exports formatted JSON string', () => {
      const json = exportPipelineTemplate(sampleNodes, sampleEdges, 'json');
      expect(typeof json).toBe('string');
      const parsed = JSON.parse(json);
      expect(parsed.kind).toBe('FactoryTemplate');
      expect(parsed.spec.steps).toHaveLength(3);
    });

    it('exports formatted YAML string', () => {
      const yaml = exportPipelineTemplate(sampleNodes, sampleEdges, 'yaml');
      expect(typeof yaml).toBe('string');
      expect(yaml).toContain('apiVersion: factory.ov.io/v1alpha1');
      expect(yaml).toContain('kind: FactoryTemplate');
    });
  });

  describe('submitPipelineRun', () => {
    it('throws validation error if graph is invalid without calling fetch', async () => {
      const mockFetch = vi.fn();
      await expect(submitPipelineRun([], [], {}, mockFetch)).rejects.toThrow(
        /Canvas validation failed/i
      );
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it('submits valid graph to POST /api/v1/runs and ensures SSE connection', async () => {
      const connectSpy = vi.spyOn(defaultGatewayClient, 'connect').mockImplementation(() => {});

      const mockResponse = {
        success: true,
        run_id: 'run-live-456',
        state: 'QUEUED',
        template_name: 'canvas-exported-factory',
        branch: 'ov-run/run-live-456',
        queued_at: new Date().toISOString(),
      };

      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 201,
        json: async () => mockResponse,
      });

      const res = await submitPipelineRun(
        sampleNodes,
        sampleEdges,
        { runId: 'run-live-456', branch: 'feat/test-branch' },
        mockFetch
      );

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const [url, init] = mockFetch.mock.calls[0];
      expect(url).toBe('/api/v1/runs');
      expect(init.method).toBe('POST');
      expect(init.headers['Content-Type']).toBe('application/json');

      const body = JSON.parse(init.body);
      expect(body.run_id).toBe('run-live-456');
      expect(body.branch).toBe('feat/test-branch');
      expect(body.template.kind).toBe('FactoryTemplate');

      expect(connectSpy).toHaveBeenCalledTimes(1);
      expect(res.run_id).toBe('run-live-456');
      expect(res.state).toBe('QUEUED');
    });

    it('handles server rejection 400/500 with descriptive error message', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
        json: async () => ({ error: 'Invalid template: cyclic graph detected' }),
      });

      await expect(
        submitPipelineRun(sampleNodes, sampleEdges, {}, mockFetch)
      ).rejects.toThrow('Invalid template: cyclic graph detected');
    });

    it('handles network failure gracefully', async () => {
      const mockFetch = vi.fn().mockRejectedValue(new Error('Network error: server unreachable'));

      await expect(
        submitPipelineRun(sampleNodes, sampleEdges, {}, mockFetch)
      ).rejects.toThrow('Network error: server unreachable');
    });
  });

  describe('cancelPipelineRun', () => {
    it('invokes POST /api/v1/runs/{id}/cancel with success response', async () => {
      const mockResponse = {
        success: true,
        run_id: 'run-live-456',
        state: 'CANCELLED',
        message: 'Run cancelled successfully',
      };

      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => mockResponse,
      });

      const res = await cancelPipelineRun('run-live-456', mockFetch);
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/v1/runs/run-live-456/cancel',
        expect.objectContaining({ method: 'POST' })
      );
      expect(res.success).toBe(true);
      expect(res.state).toBe('CANCELLED');
    });

    it('handles 404 run not found error', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
        json: async () => ({ error: 'Run run-live-456 not found' }),
      });

      await expect(cancelPipelineRun('run-live-456', mockFetch)).rejects.toThrow(
        'Run run-live-456 not found'
      );
    });
  });
});
