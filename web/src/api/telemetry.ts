// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import type { RunCostSummary, CostsOverviewResponse, BudgetGuardrail } from '../types/telemetry';

export interface FetchTelemetryOptions {
  baseUrl?: string;
  customFetch?: typeof fetch;
}

/**
 * Fetches all tracked runs and aggregate costs from GET /api/v1/telemetry/costs.
 */
export async function fetchCostsOverview(
  options?: FetchTelemetryOptions
): Promise<CostsOverviewResponse> {
  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = options?.baseUrl ? options.baseUrl.replace(/\/+$/, '') : '';
  const url = `${baseUrl}/api/v1/telemetry/costs`;

  let response: Response;
  try {
    response = await fetchFn(url, {
      headers: {
        Accept: 'application/json',
      },
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`Network error fetching telemetry costs overview: ${msg}`);
  }

  if (!response.ok) {
    let errMsg = `Failed to fetch telemetry costs (status ${response.status})`;
    try {
      const data = await response.json();
      if (data && typeof data === 'object' && 'error' in data && typeof data.error === 'string') {
        errMsg = data.error;
      }
    } catch {
      // Keep default error message
    }
    throw new Error(errMsg);
  }

  return response.json();
}

/**
 * Fetches telemetry cost and token usage for a specific run ID from GET /api/v1/telemetry/costs/:run_id.
 */
export async function fetchRunCost(
  runId: string,
  options?: FetchTelemetryOptions
): Promise<RunCostSummary> {
  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = options?.baseUrl ? options.baseUrl.replace(/\/+$/, '') : '';
  const cleanId = encodeURIComponent(runId.trim());
  const url = `${baseUrl}/api/v1/telemetry/costs/${cleanId}`;

  let response: Response;
  try {
    response = await fetchFn(url, {
      headers: {
        Accept: 'application/json',
      },
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`Network error fetching cost for run "${runId}": ${msg}`);
  }

  if (!response.ok) {
    let errMsg = `Failed to fetch cost for run "${runId}" (status ${response.status})`;
    try {
      const data = await response.json();
      if (data && typeof data === 'object' && 'error' in data && typeof data.error === 'string') {
        errMsg = data.error;
      }
    } catch {
      // Keep default error message
    }
    throw new Error(errMsg);
  }

  return response.json();
}

/**
 * Updates budget guardrail for a specific run via POST /api/v1/telemetry/costs/:run_id/guardrail.
 */
export async function updateRunGuardrail(
  runId: string,
  guardrail: BudgetGuardrail,
  options?: FetchTelemetryOptions
): Promise<RunCostSummary> {
  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = options?.baseUrl ? options.baseUrl.replace(/\/+$/, '') : '';
  const cleanId = encodeURIComponent(runId.trim());
  const url = `${baseUrl}/api/v1/telemetry/costs/${cleanId}/guardrail`;

  let response: Response;
  try {
    response = await fetchFn(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
      },
      body: JSON.stringify(guardrail),
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`Network error updating guardrail for run "${runId}": ${msg}`);
  }

  if (!response.ok) {
    let errMsg = `Failed to update guardrail for run "${runId}" (status ${response.status})`;
    try {
      const data = await response.json();
      if (data && typeof data === 'object' && 'error' in data && typeof data.error === 'string') {
        errMsg = data.error;
      }
    } catch {
      // Keep default error message
    }
    throw new Error(errMsg);
  }

  return response.json();
}
