// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  searchBrain,
  getBrainStats,
  syncADRs,
  indexRepo,
  type BrainSearchParams,
  type BrainSearchResult,
  type BrainStats,
  type ADRSyncResponse,
  type IndexResultResponse,
} from './brain';

describe('Brain API Client (web/src/api/brain.ts)', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  describe('searchBrain', () => {
    it('sends POST /api/v1/brain/search with default query payload', async () => {
      const mockResults: BrainSearchResult[] = [
        {
          category: 'knowledge',
          file_path: 'internal/brain/stats.go',
          symbol_name: 'GetStats',
          symbol_type: 'function',
          title: 'Stats summary',
          summary: 'Returns counts of indexed knowledge, rules, and task episodes',
          score: 0.92,
          vector_score: 0.88,
          lexical_score: 0.95,
        },
      ];

      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => mockResults,
      });
      globalThis.fetch = fetchMock;

      const params: BrainSearchParams = { query: 'GetStats' };
      const results = await searchBrain(params);

      expect(fetchMock).toHaveBeenCalledWith('/api/v1/brain/search', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Accept: 'application/json',
        },
        body: JSON.stringify({ query: 'GetStats' }),
      });
      expect(results).toEqual(mockResults);
    });

    it('includes category, limit, alpha, and handles custom baseUrl', async () => {
      const mockResults: BrainSearchResult[] = [
        {
          category: 'rules',
          title: 'ADR-001 Architecture Rule',
          summary: 'All nodes must declare inputs and outputs',
          score: 0.85,
        },
      ];

      const customFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => mockResults,
      });

      const params: BrainSearchParams = {
        query: 'ADR rule',
        category: 'rules',
        limit: 10,
        alpha: 0.65,
        baseUrl: 'http://localhost:8080/',
        customFetch,
      };

      const results = await searchBrain(params);

      expect(customFetch).toHaveBeenCalledWith('http://localhost:8080/api/v1/brain/search', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Accept: 'application/json',
        },
        body: JSON.stringify({
          query: 'ADR rule',
          category: 'rules',
          limit: 10,
          alpha: 0.65,
        }),
      });
      expect(results).toEqual(mockResults);
    });

    it('returns empty array when response body is not an array', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ results: null }),
      });
      globalThis.fetch = fetchMock;

      const results = await searchBrain({ query: 'empty' });
      expect(results).toEqual([]);
    });

    it('throws network error when fetch fails', async () => {
      const fetchMock = vi.fn().mockRejectedValue(new Error('Connection refused'));
      globalThis.fetch = fetchMock;

      await expect(searchBrain({ query: 'test' })).rejects.toThrow(
        'Network error searching brain: Connection refused'
      );
    });

    it('throws server error message when response is not ok', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
        json: async () => ({ error: 'query is required' }),
      });
      globalThis.fetch = fetchMock;

      await expect(searchBrain({ query: '' })).rejects.toThrow('query is required');
    });

    it('throws status error fallback when json parse fails on non-ok response', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        json: async () => {
          throw new Error('invalid json');
        },
      });
      globalThis.fetch = fetchMock;

      await expect(searchBrain({ query: 'crash' })).rejects.toThrow(
        'Failed to search brain (status 500)'
      );
    });
  });

  describe('getBrainStats', () => {
    it('sends GET /api/v1/brain/stats and returns stats', async () => {
      const mockStats: BrainStats = {
        indexed_knowledge: 42,
        active_rules: 15,
        task_episodes: 7,
      };

      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => mockStats,
      });
      globalThis.fetch = fetchMock;

      const stats = await getBrainStats();

      expect(fetchMock).toHaveBeenCalledWith('/api/v1/brain/stats', {
        method: 'GET',
        headers: { Accept: 'application/json' },
      });
      expect(stats).toEqual(mockStats);
    });

    it('supports baseUrl and customFetch options', async () => {
      const mockStats: BrainStats = {
        indexed_knowledge: 10,
        active_rules: 5,
        task_episodes: 2,
      };

      const customFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => mockStats,
      });

      const stats = await getBrainStats({
        baseUrl: 'http://127.0.0.1:9000///',
        customFetch,
      });

      expect(customFetch).toHaveBeenCalledWith('http://127.0.0.1:9000/api/v1/brain/stats', {
        method: 'GET',
        headers: { Accept: 'application/json' },
      });
      expect(stats).toEqual(mockStats);
    });

    it('handles non-ok response and throws server error', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: false,
        status: 503,
        json: async () => ({ error: 'brain service is not available' }),
      });
      globalThis.fetch = fetchMock;

      await expect(getBrainStats()).rejects.toThrow('brain service is not available');
    });

    it('handles network error', async () => {
      const fetchMock = vi.fn().mockRejectedValue(new Error('Network offline'));
      globalThis.fetch = fetchMock;

      await expect(getBrainStats()).rejects.toThrow(
        'Network error fetching brain stats: Network offline'
      );
    });

    it('returns zeroed stats when json returns invalid object', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => null,
      });
      globalThis.fetch = fetchMock;

      const stats = await getBrainStats();
      expect(stats).toEqual({
        indexed_knowledge: 0,
        active_rules: 0,
        task_episodes: 0,
      });
    });
  });

  describe('syncADRs', () => {
    it('sends POST /api/v1/brain/sync/adrs with optional path', async () => {
      const mockSyncResult: ADRSyncResponse = {
        total_files: 5,
        created: 2,
        updated: 3,
        skipped: 0,
        obsolete: 0,
        rules: [
          {
            rule_code: 'ADR-001',
            title: 'Use PostgreSQL and pgvector',
            severity: 'ERROR',
            category: 'architecture',
            description: 'All episodic memory and vector search must use pgvector',
          },
        ],
      };

      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => mockSyncResult,
      });
      globalThis.fetch = fetchMock;

      const result = await syncADRs('docs/adr');

      expect(fetchMock).toHaveBeenCalledWith('/api/v1/brain/sync/adrs', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Accept: 'application/json',
        },
        body: JSON.stringify({ path: 'docs/adr' }),
      });
      expect(result).toEqual(mockSyncResult);
    });

    it('sends empty body when path is omitted', async () => {
      const mockSyncResult: ADRSyncResponse = {
        total_files: 0,
        created: 0,
        updated: 0,
        skipped: 0,
        obsolete: 0,
      };

      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => mockSyncResult,
      });
      globalThis.fetch = fetchMock;

      const result = await syncADRs();
      expect(fetchMock).toHaveBeenCalledWith('/api/v1/brain/sync/adrs', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Accept: 'application/json',
        },
        body: JSON.stringify({}),
      });
      expect(result).toEqual(mockSyncResult);
    });

    it('handles network and status errors in syncADRs', async () => {
      const fetchMock = vi.fn().mockRejectedValue(new Error('DNS failure'));
      globalThis.fetch = fetchMock;

      await expect(syncADRs('docs/adr')).rejects.toThrow(
        'Network error syncing ADRs: DNS failure'
      );

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        json: async () => ({ error: 'failed to read ADR directory' }),
      });

      await expect(syncADRs('docs/adr')).rejects.toThrow('failed to read ADR directory');
    });
  });

  describe('indexRepo', () => {
    it('sends POST /api/v1/brain/index with path, exclusions, dry_run', async () => {
      const mockIndexResult: IndexResultResponse = {
        total_files_scanned: 120,
        indexed_files: 45,
        extracted_symbols: 350,
        upserted_symbols: 350,
        skipped_unchanged: 75,
        purged_stale_records: 0,
      };

      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => mockIndexResult,
      });
      globalThis.fetch = fetchMock;

      const result = await indexRepo({
        path: '.',
        exclusions: ['node_modules', 'dist'],
        dry_run: true,
      });

      expect(fetchMock).toHaveBeenCalledWith('/api/v1/brain/index', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Accept: 'application/json',
        },
        body: JSON.stringify({
          path: '.',
          exclusions: ['node_modules', 'dist'],
          dry_run: true,
        }),
      });
      expect(result).toEqual(mockIndexResult);
    });

    it('handles network and status errors in indexRepo', async () => {
      const fetchMock = vi.fn().mockRejectedValue(new Error('Timeout'));
      globalThis.fetch = fetchMock;

      await expect(indexRepo({ path: '.' })).rejects.toThrow(
        'Network error indexing repository: Timeout'
      );

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
        json: async () => ({ error: 'invalid repo path' }),
      });

      await expect(indexRepo({ path: 'invalid' })).rejects.toThrow('invalid repo path');
    });
  });
});
