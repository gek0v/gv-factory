// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  dispatchFactoryRun,
  getFactoryRun,
  type FactoryRunRequest,
  type FactoryRunResponse,
  type FactoryRunOutcome,
} from './factory';

describe('Factory API Client (web/src/api/factory.ts)', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  describe('dispatchFactoryRun', () => {
    it('sends POST /api/v1/factory/run with valid payload and returns response', async () => {
      const mockResponse: FactoryRunResponse = {
        run_id: 'run-live-101',
        status: 'queued',
        stage: 'idle',
        message: 'Factory pipeline queued successfully',
        approval_token: 'hmac-token-xyz',
        created_at: '2026-03-30T12:00:00Z',
      };

      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 201,
        json: async () => mockResponse,
      });
      globalThis.fetch = fetchMock;

      const request: FactoryRunRequest = {
        title: 'Implement Auth Middleware',
        description: 'Add JWT verification and RBAC checks',
        model: 'gemini-3.8-flash-high',
        labels: ['backend', 'security', 'p1'],
        constraints: ['Must satisfy 90% test coverage', '0 security alerts'],
      };

      const result = await dispatchFactoryRun(request);

      expect(fetchMock).toHaveBeenCalledTimes(1);
      expect(fetchMock).toHaveBeenCalledWith('/api/v1/factory/run', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Accept: 'application/json',
        },
        body: JSON.stringify(request),
      });
      expect(result).toEqual(mockResponse);
    });

    it('sends POST /api/v1/factory/run with specific target model', async () => {
      const mockResponse: FactoryRunResponse = {
        run_id: 'run-model-test-1',
        status: 'queued',
        stage: 'idle',
      };

      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => mockResponse,
      });
      globalThis.fetch = fetchMock;

      const request: FactoryRunRequest = {
        title: 'Refactor Tree Sitter parser',
        model: 'deepseek-chat',
      };

      const result = await dispatchFactoryRun(request);

      expect(fetchMock).toHaveBeenCalledWith('/api/v1/factory/run', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Accept: 'application/json',
        },
        body: JSON.stringify(request),
      });
      expect(result.run_id).toBe('run-model-test-1');
    });

    it('supports custom baseUrl and customFetch option', async () => {
      const mockResponse: FactoryRunResponse = {
        run_id: 'run-custom-202',
        status: 'running',
        stage: 'building',
      };

      const customFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => mockResponse,
      });

      const request: FactoryRunRequest = {
        title: 'Fix SQLite Lock Contention',
      };

      const result = await dispatchFactoryRun(request, {
        baseUrl: 'http://localhost:8080/',
        customFetch,
      });

      expect(customFetch).toHaveBeenCalledWith('http://localhost:8080/api/v1/factory/run', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Accept: 'application/json',
        },
        body: JSON.stringify(request),
      });
      expect(result).toEqual(mockResponse);
    });

    it('throws validation error if title is empty or missing', async () => {
      await expect(
        dispatchFactoryRun({ title: '' })
      ).rejects.toThrow('Task title is required');

      await expect(
        dispatchFactoryRun({ title: '   ' })
      ).rejects.toThrow('Task title is required');

      await expect(
        // @ts-expect-error Testing invalid runtime input
        dispatchFactoryRun({})
      ).rejects.toThrow('Task title is required');
    });

    it('handles HTTP error with message field in response body', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: false,
        status: 422,
        json: async () => ({ message: 'Unprocessable entity parameter' }),
      });
      globalThis.fetch = fetchMock;

      await expect(
        dispatchFactoryRun({ title: 'Valid Title' })
      ).rejects.toThrow('Unprocessable entity parameter');
    });

    it('falls back to runId when run_id is omitted in response', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ runId: 'camel-case-id', status: 'running' }),
      });
      globalThis.fetch = fetchMock;

      const res = await dispatchFactoryRun({ title: 'Valid Title' });
      expect(res.run_id).toBe('camel-case-id');
    });

    it('handles HTTP error with non-JSON response body', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: false,
        status: 502,
        json: async () => {
          throw new Error('Bad Gateway HTML');
        },
      });
      globalThis.fetch = fetchMock;

      await expect(
        dispatchFactoryRun({ title: 'Valid Title' })
      ).rejects.toThrow('Failed to dispatch factory run (status 502)');
    });

    it('handles network failure', async () => {
      const fetchMock = vi.fn().mockRejectedValue(new Error('Connection refused'));
      globalThis.fetch = fetchMock;

      await expect(
        dispatchFactoryRun({ title: 'Valid Title' })
      ).rejects.toThrow('Network error dispatching factory run: Connection refused');
    });

    it('handles invalid JSON in successful response body', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => {
          throw new Error('Unexpected token < in JSON');
        },
      });
      globalThis.fetch = fetchMock;

      await expect(
        dispatchFactoryRun({ title: 'Valid Title' })
      ).rejects.toThrow('Invalid JSON response from server');
    });

    it('validates response structure ensuring run_id is present', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ status: 'queued' }), // Missing run_id
      });
      globalThis.fetch = fetchMock;

      await expect(
        dispatchFactoryRun({ title: 'Valid Title' })
      ).rejects.toThrow('Server response missing valid run_id');
    });
  });

  describe('getFactoryRun', () => {
    it('sends GET /api/v1/factory/runs/:runId and returns outcome', async () => {
      const mockOutcome: FactoryRunOutcome = {
        run_id: 'run-live-101',
        status: 'running',
        stage: 'building',
        title: 'Implement Auth Middleware',
        description: 'Add JWT verification and RBAC checks',
        approval_token: 'hmac-token-xyz',
      };

      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => mockOutcome,
      });
      globalThis.fetch = fetchMock;

      const outcome = await getFactoryRun('run-live-101');

      expect(fetchMock).toHaveBeenCalledWith('/api/v1/factory/runs/run-live-101', {
        method: 'GET',
        headers: {
          Accept: 'application/json',
        },
      });
      expect(outcome).toEqual(mockOutcome);
    });

    it('properly URI encodes the runId', async () => {
      const mockOutcome: FactoryRunOutcome = {
        run_id: 'run/with/slashes',
        status: 'completed',
        stage: 'completed',
      };

      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => mockOutcome,
      });
      globalThis.fetch = fetchMock;

      await getFactoryRun('run/with/slashes', { baseUrl: 'http://localhost:8080/' });

      expect(fetchMock).toHaveBeenCalledWith(
        'http://localhost:8080/api/v1/factory/runs/run%2Fwith%2Fslashes',
        {
          method: 'GET',
          headers: {
            Accept: 'application/json',
          },
        }
      );
    });

    it('throws validation error if runId is empty or whitespace', async () => {
      await expect(getFactoryRun('')).rejects.toThrow('Run ID is required');
      await expect(getFactoryRun('   ')).rejects.toThrow('Run ID is required');
    });

    it('handles HTTP error status codes (e.g. 404 not found)', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
        json: async () => ({ error: 'Run run-not-found not found' }),
      });
      globalThis.fetch = fetchMock;

      await expect(getFactoryRun('run-not-found')).rejects.toThrow(
        'Run run-not-found not found'
      );
    });

    it('handles network failure when fetching run status', async () => {
      const fetchMock = vi.fn().mockRejectedValue(new Error('DNS resolution failed'));
      globalThis.fetch = fetchMock;

      await expect(getFactoryRun('run-dns-err')).rejects.toThrow(
        'Network error retrieving factory run: DNS resolution failed'
      );
    });

    it('handles invalid JSON body on getFactoryRun', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => {
          throw new Error('Malformed JSON');
        },
      });
      globalThis.fetch = fetchMock;

      await expect(getFactoryRun('run-malformed')).rejects.toThrow(
        'Invalid JSON response from server'
      );
    });

    it('falls back to runId when run_id is omitted in outcome', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ runId: 'camel-outcome-id', status: 'completed', stage: 'completed' }),
      });
      globalThis.fetch = fetchMock;

      const outcome = await getFactoryRun('camel-outcome-id');
      expect(outcome.run_id).toBe('camel-outcome-id');
    });
  });
});
