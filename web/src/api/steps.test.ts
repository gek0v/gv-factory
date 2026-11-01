// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fetchRunSteps } from './steps';
import type { RunStep } from '../types/worker';

describe('fetchRunSteps API client', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('fetches run steps successfully returning RunStep[]', async () => {
    const mockSteps: RunStep[] = [
      {
        id: 'step-1',
        run_id: 'run-101',
        step_name: 'spec_writer',
        status: 'COMPLETED',
        duration_ms: 450,
        output: { spec: 'done' },
        created_at: '2026-11-01T12:00:00Z',
      },
      {
        id: 'step-2',
        run_id: 'run-101',
        step_name: 'builder_tdd',
        status: 'RUNNING',
        duration_ms: 120,
        created_at: '2026-11-01T12:01:00Z',
      },
    ];

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => mockSteps,
    });
    globalThis.fetch = fetchMock;

    const result = await fetchRunSteps('run-101');
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/runs/run-101/steps', {
      headers: { Accept: 'application/json' },
    });
    expect(result).toEqual(mockSteps);
  });

  it('honors custom baseUrl option', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => [],
    });
    globalThis.fetch = fetchMock;

    await fetchRunSteps('run-42', { baseUrl: 'http://localhost:8080' });
    expect(fetchMock).toHaveBeenCalledWith('http://localhost:8080/api/v1/runs/run-42/steps', {
      headers: { Accept: 'application/json' },
    });
  });

  it('returns empty array when backend returns empty list or null', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => null,
    });
    globalThis.fetch = fetchMock;

    const result = await fetchRunSteps('run-empty');
    expect(result).toEqual([]);
  });

  it('throws descriptive error on 404 Not Found', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 404,
      json: async () => ({ error: 'run "run-nonexistent" not found' }),
    });
    globalThis.fetch = fetchMock;

    await expect(fetchRunSteps('run-nonexistent')).rejects.toThrow('Run not found: run-nonexistent');
  });

  it('throws descriptive error on 500 Internal Server Error with backend message', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({ error: 'database disk full' }),
    });
    globalThis.fetch = fetchMock;

    await expect(fetchRunSteps('run-err')).rejects.toThrow('database disk full');
  });

  it('throws when network fetch rejects', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('Connection refused'));
    globalThis.fetch = fetchMock;

    await expect(fetchRunSteps('run-fail')).rejects.toThrow('Network error fetching steps: Connection refused');
  });
});
