// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import type { WorkerInfo, WorkerPoolResponse, WorkerStatus } from '../types/worker';

export interface FetchWorkerPoolOptions {
  baseUrl?: string;
  customFetch?: typeof fetch;
}

/**
 * Normalizes worker status to 'ready' | 'busy' | 'offline'.
 */
export function normalizeWorkerStatus(rawStatus?: string, activeTasks = 0): WorkerStatus {
  if (!rawStatus) {
    return activeTasks > 0 ? 'busy' : 'ready';
  }
  const s = rawStatus.toLowerCase().trim();
  if (s === 'busy' || s === 'running') return 'busy';
  if (s === 'offline' || s === 'dead' || s === 'disconnected') return 'offline';
  return 'ready';
}

/**
 * Queries GET /api/v1/workers to retrieve cluster workers and telemetry.
 */
export async function fetchWorkerPool(
  options?: FetchWorkerPoolOptions
): Promise<WorkerPoolResponse> {
  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = options?.baseUrl ? options.baseUrl.replace(/\/+$/, '') : '';
  const url = `${baseUrl}/api/v1/workers`;

  let response: Response;
  try {
    response = await fetchFn(url, {
      headers: {
        Accept: 'application/json',
      },
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`Network error fetching worker pool: ${msg}`);
  }

  if (!response.ok) {
    let errMsg = `Failed to fetch worker pool (status ${response.status})`;
    try {
      const data = await response.json();
      if (data && typeof data === 'object' && 'error' in data && typeof data.error === 'string') {
        errMsg = data.error;
      }
    } catch {
      // Ignore JSON parse error on non-ok responses
    }
    throw new Error(errMsg);
  }

  let data: Record<string, unknown> | null = null;
  try {
    data = await response.json();
  } catch {
    data = null;
  }

  const rawWorkers: WorkerInfo[] = (data && Array.isArray(data.workers))
    ? (data.workers as WorkerInfo[])
    : Array.isArray(data)
    ? (data as unknown as WorkerInfo[])
    : [];

  const workers: WorkerInfo[] = rawWorkers.map((w) => ({
    worker_id: w.worker_id,
    status: normalizeWorkerStatus(w.status, w.active_tasks),
    active_tasks: Number(w.active_tasks) || 0,
    max_concurrency: Number(w.max_concurrency) || 1,
    uptime_seconds: Number(w.uptime_seconds) || 0,
    last_heartbeat: w.last_heartbeat,
    timestamp: w.timestamp,
  }));

  const totalWorkers = typeof data?.total_workers === 'number' ? data.total_workers : workers.length;
  const activeWorkers = typeof data?.active_workers === 'number'
    ? data.active_workers
    : workers.filter((w) => w.status !== 'offline').length;
  const totalCapacity = typeof data?.total_capacity === 'number'
    ? data.total_capacity
    : workers.reduce((acc, w) => acc + (w.max_concurrency || 0), 0);
  const totalActiveTasks = typeof data?.total_active_tasks === 'number'
    ? data.total_active_tasks
    : workers.reduce((acc, w) => acc + (w.active_tasks || 0), 0);

  return {
    workers,
    total_workers: totalWorkers,
    active_workers: activeWorkers,
    total_capacity: totalCapacity,
    total_active_tasks: totalActiveTasks,
  };
}
