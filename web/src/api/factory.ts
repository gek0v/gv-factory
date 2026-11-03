// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

export type PipelineStage =
  | 'idle'
  | 'spec_generated'
  | 'building'
  | 'reviewing'
  | 'completed'
  | 'gate_waiting'
  | 'failed';

export interface FactoryRunRequest {
  title: string;
  description?: string;
  model?: string;
  labels?: string[] | string;
  constraints?: string[] | string;
  template_key?: string;
  templateKey?: string;
  metadata?: Record<string, unknown>;
}

export interface FactoryRunResponse {
  run_id: string;
  runId?: string;
  status: string;
  stage?: PipelineStage;
  message?: string;
  approval_token?: string;
  approvalToken?: string;
  created_at?: string;
}

export interface FactoryRunOutcome {
  run_id: string;
  runId?: string;
  status: string;
  stage: PipelineStage;
  title?: string;
  description?: string;
  approval_token?: string;
  approvalToken?: string;
  error?: string;
  logs?: string[];
  results?: Record<string, unknown>;
  completed_at?: string;
}

export interface FactoryRequestOptions {
  baseUrl?: string;
  customFetch?: typeof fetch;
}

function normalizeBaseUrl(url?: string): string {
  return url ? url.replace(/\/+$/, '') : '';
}

async function extractErrorMessage(response: Response, defaultMessage: string): Promise<string> {
  try {
    const data = await response.json();
    if (data && typeof data === 'object') {
      if ('error' in data && typeof data.error === 'string') {
        return data.error;
      }
      if ('message' in data && typeof data.message === 'string') {
        return data.message;
      }
    }
  } catch {
    // Non-JSON or empty response body
  }
  return `${defaultMessage} (status ${response.status})`;
}

/**
 * Dispatches an autonomous factory pipeline run via POST /api/v1/factory/run
 */
export async function dispatchFactoryRun(
  req: FactoryRunRequest,
  options?: FactoryRequestOptions
): Promise<FactoryRunResponse> {
  if (!req || typeof req.title !== 'string' || req.title.trim().length === 0) {
    throw new Error('Task title is required');
  }

  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = normalizeBaseUrl(options?.baseUrl);
  const url = `${baseUrl}/api/v1/factory/run`;

  let response: Response;
  try {
    response = await fetchFn(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
      },
      body: JSON.stringify(req),
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`Network error dispatching factory run: ${msg}`);
  }

  if (!response.ok) {
    const errMsg = await extractErrorMessage(response, 'Failed to dispatch factory run');
    throw new Error(errMsg);
  }

  let data: unknown;
  try {
    data = await response.json();
  } catch {
    throw new Error('Invalid JSON response from server');
  }

  if (!data || typeof data !== 'object' || !('run_id' in data || 'runId' in data)) {
    throw new Error('Server response missing valid run_id');
  }

  const parsed = data as FactoryRunResponse;
  if (!parsed.run_id && parsed.runId) {
    parsed.run_id = parsed.runId;
  }

  return parsed;
}

/**
 * Retrieves the current execution status and outcome of a factory run via GET /api/v1/factory/runs/:runId
 */
export async function getFactoryRun(
  runId: string,
  options?: FactoryRequestOptions
): Promise<FactoryRunOutcome> {
  if (!runId || typeof runId !== 'string' || runId.trim().length === 0) {
    throw new Error('Run ID is required');
  }

  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = normalizeBaseUrl(options?.baseUrl);
  const url = `${baseUrl}/api/v1/factory/runs/${encodeURIComponent(runId.trim())}`;

  let response: Response;
  try {
    response = await fetchFn(url, {
      method: 'GET',
      headers: {
        Accept: 'application/json',
      },
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`Network error retrieving factory run: ${msg}`);
  }

  if (!response.ok) {
    const errMsg = await extractErrorMessage(response, 'Failed to retrieve factory run');
    throw new Error(errMsg);
  }

  let data: unknown;
  try {
    data = await response.json();
  } catch {
    throw new Error('Invalid JSON response from server');
  }

  if (!data || typeof data !== 'object' || !('run_id' in data || 'runId' in data)) {
    throw new Error('Server response missing valid run_id');
  }

  const parsed = data as FactoryRunOutcome;
  if (!parsed.run_id && parsed.runId) {
    parsed.run_id = parsed.runId;
  }

  return parsed;
}

export interface SandboxFileInfo {
  path: string;
  size: number;
  modified: boolean;
  mod_time?: string;
}

export interface FileContentResponse {
  path: string;
  content: string;
  size: number;
}

export interface UpdateFileContentResponse {
  success: boolean;
  path: string;
  diff: string;
  files_modified?: string[];
  message?: string;
}

/**
 * Retrieves the list of files in the sandbox workspace for a factory run via GET /api/v1/factory/runs/:runId/files
 */
export async function listSandboxFiles(
  runId: string,
  options?: FactoryRequestOptions
): Promise<SandboxFileInfo[]> {
  if (!runId || typeof runId !== 'string' || runId.trim().length === 0) {
    throw new Error('Run ID is required');
  }

  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = normalizeBaseUrl(options?.baseUrl);
  const url = `${baseUrl}/api/v1/factory/runs/${encodeURIComponent(runId.trim())}/files`;

  let response: Response;
  try {
    response = await fetchFn(url, {
      method: 'GET',
      headers: {
        Accept: 'application/json',
      },
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`Network error listing sandbox files: ${msg}`);
  }

  if (!response.ok) {
    const errMsg = await extractErrorMessage(response, 'Failed to list sandbox files');
    throw new Error(errMsg);
  }

  try {
    const data = await response.json();
    return Array.isArray(data) ? data : (data.files || []);
  } catch {
    throw new Error('Invalid JSON response from server');
  }
}

/**
 * Retrieves the content of a file within the sandbox via GET /api/v1/factory/runs/:runId/files/content?path=...
 */
export async function getSandboxFileContent(
  runId: string,
  filePath: string,
  options?: FactoryRequestOptions
): Promise<FileContentResponse> {
  if (!runId || typeof runId !== 'string' || runId.trim().length === 0) {
    throw new Error('Run ID is required');
  }
  if (!filePath || typeof filePath !== 'string' || filePath.trim().length === 0) {
    throw new Error('File path is required');
  }

  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = normalizeBaseUrl(options?.baseUrl);
  const url = `${baseUrl}/api/v1/factory/runs/${encodeURIComponent(runId.trim())}/files/content?path=${encodeURIComponent(filePath.trim())}`;

  let response: Response;
  try {
    response = await fetchFn(url, {
      method: 'GET',
      headers: {
        Accept: 'application/json',
      },
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`Network error retrieving file content: ${msg}`);
  }

  if (!response.ok) {
    const errMsg = await extractErrorMessage(response, 'Failed to retrieve file content');
    throw new Error(errMsg);
  }

  try {
    return await response.json();
  } catch {
    throw new Error('Invalid JSON response from server');
  }
}

/**
 * Updates the content of a sandbox file and recalculates git diff via PUT /api/v1/factory/runs/:runId/files/content
 */
export async function updateSandboxFileContent(
  runId: string,
  filePath: string,
  content: string,
  options?: FactoryRequestOptions
): Promise<UpdateFileContentResponse> {
  if (!runId || typeof runId !== 'string' || runId.trim().length === 0) {
    throw new Error('Run ID is required');
  }
  if (!filePath || typeof filePath !== 'string' || filePath.trim().length === 0) {
    throw new Error('File path is required');
  }

  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = normalizeBaseUrl(options?.baseUrl);
  const url = `${baseUrl}/api/v1/factory/runs/${encodeURIComponent(runId.trim())}/files/content`;

  let response: Response;
  try {
    response = await fetchFn(url, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
      },
      body: JSON.stringify({
        path: filePath.trim(),
        content,
      }),
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`Network error updating sandbox file: ${msg}`);
  }

  if (!response.ok) {
    const errMsg = await extractErrorMessage(response, 'Failed to update sandbox file');
    throw new Error(errMsg);
  }

  try {
    return await response.json();
  } catch {
    throw new Error('Invalid JSON response from server');
  }
}
