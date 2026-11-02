// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

export type BrainCategory = 'all' | 'knowledge' | 'rules' | 'episodes';

export interface BrainSearchParams {
  query: string;
  category?: BrainCategory;
  limit?: number;
  alpha?: number; // 0.0 (lexical) to 1.0 (semantic)
  baseUrl?: string;
  customFetch?: typeof fetch;
}

export interface BrainSearchResult {
  category: string;
  file_path?: string;
  symbol_name?: string;
  symbol_type?: string;
  title?: string;
  summary: string;
  score: number;
  vector_score?: number;
  lexical_score?: number;
}

export interface BrainRequestOptions {
  baseUrl?: string;
  customFetch?: typeof fetch;
}

export interface BrainStats {
  indexed_knowledge: number;
  active_rules: number;
  task_episodes: number;
}

export interface ADRRule {
  id?: string;
  rule_code: string;
  title: string;
  severity: 'ERROR' | 'WARN' | 'INFO' | string;
  category?: string;
  description: string;
  rationale?: string;
  remediation?: string;
  source_adr?: string;
  is_active?: boolean;
  created_at?: string;
  updated_at?: string;
}

export interface ADRSyncResponse {
  total_files: number;
  created: number;
  updated: number;
  skipped: number;
  obsolete: number;
  errors?: string[];
  rules?: ADRRule[];
  duration?: number;
}

export interface IndexRepoParams {
  path: string;
  exclusions?: string[];
  dry_run?: boolean;
}

export interface IndexResultResponse {
  total_files_scanned: number;
  indexed_files: number;
  extracted_symbols: number;
  upserted_symbols: number;
  skipped_unchanged: number;
  purged_stale_records: number;
  errors?: string[];
  duration?: number;
}

function normalizeBaseUrl(url?: string): string {
  return url ? url.replace(/\/+$/, '') : '';
}

async function extractErrorMessage(response: Response, defaultMessage: string): Promise<string> {
  try {
    const data = await response.json();
    if (data && typeof data === 'object' && 'error' in data && typeof data.error === 'string') {
      return data.error;
    }
  } catch {
    // Non-JSON or empty response body
  }
  return `${defaultMessage} (status ${response.status})`;
}

/**
 * Searches the Codebase Brain knowledge, rules, and episodic memory.
 * POST /api/v1/brain/search
 */
export async function searchBrain(params: BrainSearchParams): Promise<BrainSearchResult[]> {
  const fetchFn = params.customFetch || globalThis.fetch;
  const baseUrl = normalizeBaseUrl(params.baseUrl);
  const url = `${baseUrl}/api/v1/brain/search`;

  const payload: Record<string, unknown> = {
    query: params.query,
  };
  if (params.category !== undefined) {
    payload.category = params.category;
  }
  if (params.limit !== undefined) {
    payload.limit = params.limit;
  }
  if (params.alpha !== undefined) {
    payload.alpha = params.alpha;
  }

  let response: Response;
  try {
    response = await fetchFn(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
      },
      body: JSON.stringify(payload),
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`Network error searching brain: ${msg}`);
  }

  if (!response.ok) {
    const errMsg = await extractErrorMessage(response, 'Failed to search brain');
    throw new Error(errMsg);
  }

  try {
    const data = await response.json();
    return Array.isArray(data) ? data : [];
  } catch {
    return [];
  }
}

/**
 * Retrieves aggregate metrics of indexed knowledge chunks, active rules, and task episodes.
 * GET /api/v1/brain/stats
 */
export async function getBrainStats(options?: BrainRequestOptions): Promise<BrainStats> {
  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = normalizeBaseUrl(options?.baseUrl);
  const url = `${baseUrl}/api/v1/brain/stats`;

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
    throw new Error(`Network error fetching brain stats: ${msg}`);
  }

  if (!response.ok) {
    const errMsg = await extractErrorMessage(response, 'Failed to fetch brain stats');
    throw new Error(errMsg);
  }

  try {
    const data = await response.json();
    if (data && typeof data === 'object') {
      return {
        indexed_knowledge: typeof data.indexed_knowledge === 'number' ? data.indexed_knowledge : 0,
        active_rules: typeof data.active_rules === 'number' ? data.active_rules : 0,
        task_episodes: typeof data.task_episodes === 'number' ? data.task_episodes : 0,
      };
    }
    return {
      indexed_knowledge: 0,
      active_rules: 0,
      task_episodes: 0,
    };
  } catch {
    return {
      indexed_knowledge: 0,
      active_rules: 0,
      task_episodes: 0,
    };
  }
}

/**
 * Scans an ADR directory and synchronizes all architectural decision records to the rules store.
 * POST /api/v1/brain/sync/adrs
 */
export async function syncADRs(path?: string, options?: BrainRequestOptions): Promise<ADRSyncResponse> {
  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = normalizeBaseUrl(options?.baseUrl);
  const url = `${baseUrl}/api/v1/brain/sync/adrs`;

  const payload: Record<string, unknown> = {};
  if (path !== undefined) {
    payload.path = path;
  }

  let response: Response;
  try {
    response = await fetchFn(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
      },
      body: JSON.stringify(payload),
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`Network error syncing ADRs: ${msg}`);
  }

  if (!response.ok) {
    const errMsg = await extractErrorMessage(response, 'Failed to sync ADRs');
    throw new Error(errMsg);
  }

  try {
    const data = await response.json();
    return data as ADRSyncResponse;
  } catch {
    return {
      total_files: 0,
      created: 0,
      updated: 0,
      skipped: 0,
      obsolete: 0,
    };
  }
}

/**
 * Scans repository files to extract AST symbols and index chunks in codebase_knowledge.
 * POST /api/v1/brain/index
 */
export async function indexRepo(
  params: IndexRepoParams,
  options?: BrainRequestOptions
): Promise<IndexResultResponse> {
  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = normalizeBaseUrl(options?.baseUrl);
  const url = `${baseUrl}/api/v1/brain/index`;

  let response: Response;
  try {
    response = await fetchFn(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
      },
      body: JSON.stringify(params),
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`Network error indexing repository: ${msg}`);
  }

  if (!response.ok) {
    const errMsg = await extractErrorMessage(response, 'Failed to index repository');
    throw new Error(errMsg);
  }

  try {
    const data = await response.json();
    return data as IndexResultResponse;
  } catch {
    return {
      total_files_scanned: 0,
      indexed_files: 0,
      extracted_symbols: 0,
      upserted_symbols: 0,
      skipped_unchanged: 0,
      purged_stale_records: 0,
    };
  }
}
