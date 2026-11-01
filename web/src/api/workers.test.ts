// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fetchWorkerPool } from './workers';
import type { WorkerPoolResponse } from '../types/worker';

describe('fetchWorkerPool API client', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('fetches worker pool successfully returning WorkerPoolResponse', async () => {
    const mockResponse: WorkerPoolResponse = {
      workers: [
        {
          worker_id: 'worker-node-1',
          status: 'ready',
          active_tasks: 2,
          max_concurrency: 8,
          uptime_seconds: 3600,
          last_heartbeat: '2026-11-01T12:00:00Z',
          timestamp: 1761998400,
        },
        {
          worker_id: 'worker-node-2',
          status: 'busy',
          active_tasks: 4,
          max_concurrency: 4,
          uptime_seconds: 1800,
          last_heartbeat: '2026-11-01T12:00:05Z',
          timestamp: 1761998405,
        },
      ],
      total_workers: 2,
      active_workers: 2,
      total_capacity: 12,
      total_active_tasks: 6,
    };

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => mockResponse,
    });
    globalThis.fetch = fetchMock;

    const result = await fetchWorkerPool();
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/workers', {
      headers: { Accept: 'application/json' },
    });
    expect(result).toEqual(mockResponse);
  });

  it('honors custom baseUrl option', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ workers: [] }),
    });
    globalThis.fetch = fetchMock;

    await fetchWorkerPool({ baseUrl: 'http://127.0.0.1:8080' });
    expect(fetchMock).toHaveBeenCalledWith('http://127.0.0.1:8080/api/v1/workers', {
      headers: { Accept: 'application/json' },
    });
  });

  it('computes aggregated cluster metrics if omitted by backend or partial payload', async () => {
    const rawData = {
      workers: [
        {
          worker_id: 'w1',
          status: 'ready',
          active_tasks: 1,
          max_concurrency: 5,
          uptime_seconds: 120,
        },
        {
          worker_id: 'w2',
          status: 'offline',
          active_tasks: 0,
          max_concurrency: 5,
          uptime_seconds: 0,
        },
        {
          worker_id: 'w3',
          status: 'busy',
          active_tasks: 3,
          max_concurrency: 3,
          uptime_seconds: 500,
        },
      ],
    };

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => rawData,
    });
    globalThis.fetch = fetchMock;

    const result = await fetchWorkerPool();
    expect(result.total_workers).toBe(3);
    expect(result.active_workers).toBe(2); // ready + busy
    expect(result.total_capacity).toBe(13); // 5 + 5 + 3
    expect(result.total_active_tasks).toBe(4); // 1 + 0 + 3
  });

  it('handles empty response gracefully', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => null,
    });
    globalThis.fetch = fetchMock;

    const result = await fetchWorkerPool();
    expect(result).toEqual({
      workers: [],
      total_workers: 0,
      active_workers: 0,
      total_capacity: 0,
      total_active_tasks: 0,
    });
  });

  it('throws descriptive error on HTTP failure', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({ error: 'worker discovery failed' }),
    });
    globalThis.fetch = fetchMock;

    await expect(fetchWorkerPool()).rejects.toThrow('worker discovery failed');
  });

  it('throws when network fetch rejects', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('Network offline'));
    globalThis.fetch = fetchMock;

    await expect(fetchWorkerPool()).rejects.toThrow('Network error fetching worker pool: Network offline');
  });
});
