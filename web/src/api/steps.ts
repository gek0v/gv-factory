// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import type { RunStep } from '../types/worker';

export interface FetchRunStepsOptions {
  baseUrl?: string;
  customFetch?: typeof fetch;
}

/**
 * Queries GET /api/v1/runs/{id}/steps to retrieve all executed/in-flight steps for a run.
 */
export async function fetchRunSteps(
  runId: string,
  options?: FetchRunStepsOptions
): Promise<RunStep[]> {
  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = options?.baseUrl ? options.baseUrl.replace(/\/+$/, '') : '';
  const url = `${baseUrl}/api/v1/runs/${encodeURIComponent(runId)}/steps`;

  let response: Response;
  try {
    response = await fetchFn(url, {
      headers: {
        Accept: 'application/json',
      },
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`Network error fetching steps: ${msg}`);
  }

  if (response.status === 404) {
    throw new Error(`Run not found: ${runId}`);
  }

  if (!response.ok) {
    let errMsg = `Failed to fetch steps for run ${runId} (status ${response.status})`;
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

  try {
    const steps = await response.json();
    if (!Array.isArray(steps)) {
      return [];
    }
    return steps;
  } catch {
    return [];
  }
}
