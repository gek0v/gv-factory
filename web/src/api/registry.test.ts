// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  fetchSkills,
  fetchTools,
  fetchPlugins,
  fetchAgentTemplates,
  createAgentTemplate,
  deleteAgentTemplate,
  _resetLocalTemplates,
  DEFAULT_SKILLS,
  DEFAULT_TOOLS,
  DEFAULT_PLUGINS,
  DEFAULT_AGENT_TEMPLATES,
  type Skill,
  type Tool,
  type Plugin,
  type AgentTemplate,
} from './registry';

describe('Registry API Client (web/src/api/registry.ts)', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
    _resetLocalTemplates();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  describe('fetchSkills', () => {
    it('returns default skills when fetch fails with fallback enabled', async () => {
      globalThis.fetch = vi.fn().mockRejectedValue(new Error('Network error'));
      const skills = await fetchSkills();
      expect(skills).toEqual(DEFAULT_SKILLS);
      expect(skills.length).toBeGreaterThan(0);
      expect(skills.some((s) => s.id === 'tdd-workflow')).toBe(true);
    });

    it('fetches skills successfully from GET /api/v1/registry/skills', async () => {
      const mockSkills: Skill[] = [
        {
          id: 'custom-skill',
          name: 'Custom Skill',
          description: 'Special skill',
          category: 'Custom',
        },
      ];

      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => mockSkills,
      });

      const result = await fetchSkills({
        baseUrl: 'http://localhost:8080',
        customFetch: fetchMock as any,
      });

      expect(fetchMock).toHaveBeenCalledWith('http://localhost:8080/api/v1/registry/skills', {
        method: 'GET',
        headers: { Accept: 'application/json' },
      });
      expect(result).toEqual(mockSkills);
    });

    it('handles { skills: [...] } wrapped JSON response', async () => {
      const mockSkills: Skill[] = [
        {
          id: 'wrapped-skill',
          name: 'Wrapped Skill',
          description: 'Wrapped payload',
        },
      ];

      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ skills: mockSkills }),
      });

      const result = await fetchSkills({ customFetch: fetchMock as any });
      expect(result).toEqual(mockSkills);
    });

    it('throws error when fallbackToDefaults is false and request fails', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        json: async () => ({ error: 'Internal server error' }),
      });

      await expect(
        fetchSkills({ customFetch: fetchMock as any, fallbackToDefaults: false })
      ).rejects.toThrow('Failed to fetch skills (status 500)');
    });
  });

  describe('fetchTools', () => {
    it('returns default tools when fetch fails with fallback enabled', async () => {
      globalThis.fetch = vi.fn().mockRejectedValue(new Error('Connection refused'));
      const tools = await fetchTools();
      expect(tools).toEqual(DEFAULT_TOOLS);
      expect(tools.some((t) => t.id === 'bash_exec')).toBe(true);
      expect(tools.some((t) => t.id === 'mcp_postgres')).toBe(true);
    });

    it('fetches tools successfully from GET /api/v1/registry/tools', async () => {
      const mockTools: Tool[] = [
        {
          id: 'custom_mcp',
          name: 'custom_mcp',
          description: 'Custom MCP server tool',
          category: 'mcp',
          riskLevel: 'sensitive',
        },
      ];

      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => mockTools,
      });

      const result = await fetchTools({
        baseUrl: 'http://localhost:8080',
        customFetch: fetchMock as any,
      });

      expect(fetchMock).toHaveBeenCalledWith('http://localhost:8080/api/v1/registry/tools', {
        method: 'GET',
        headers: { Accept: 'application/json' },
      });
      expect(result).toEqual(mockTools);
    });

    it('throws error when fallbackToDefaults is false and request fails', async () => {
      const fetchMock = vi.fn().mockRejectedValue(new Error('DNS resolution failed'));
      await expect(
        fetchTools({ customFetch: fetchMock as any, fallbackToDefaults: false })
      ).rejects.toThrow('DNS resolution failed');
    });
  });

  describe('fetchPlugins', () => {
    it('returns default plugins when fetch fails with fallback enabled', async () => {
      globalThis.fetch = vi.fn().mockRejectedValue(new Error('Network error'));
      const plugins = await fetchPlugins();
      expect(plugins).toEqual(DEFAULT_PLUGINS);
      expect(plugins.some((p) => p.uri === 'extism://plugins/sdd-gen.wasm')).toBe(true);
    });

    it('fetches plugins successfully from GET /api/v1/registry/plugins', async () => {
      const mockPlugins: Plugin[] = [
        {
          id: 'extism-custom',
          name: 'Custom WASM Plugin',
          description: 'Special Extism plugin',
          uri: 'extism://plugins/custom.wasm',
        },
      ];

      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ plugins: mockPlugins }),
      });

      const result = await fetchPlugins({ customFetch: fetchMock as any });
      expect(result).toEqual(mockPlugins);
    });

    it('throws error when fallbackToDefaults is false and request fails', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
      });

      await expect(
        fetchPlugins({ customFetch: fetchMock as any, fallbackToDefaults: false })
      ).rejects.toThrow('Failed to fetch plugins (status 404)');
    });
  });

  describe('fetchAgentTemplates and createAgentTemplate', () => {
    it('returns default templates initially', async () => {
      globalThis.fetch = vi.fn().mockRejectedValue(new Error('Offline'));
      const templates = await fetchAgentTemplates();
      expect(templates.length).toBeGreaterThanOrEqual(DEFAULT_AGENT_TEMPLATES.length);
      expect(templates.some((t) => t.id === 'tpl-spec-writer')).toBe(true);
      expect(templates.some((t) => t.id === 'tpl-builder-tdd')).toBe(true);
    });

    it('creates agent template locally and includes it in future fetchAgentTemplates', async () => {
      globalThis.fetch = vi.fn().mockRejectedValue(new Error('Offline dev mode'));

      const newTpl = await createAgentTemplate({
        name: 'Fullstack Refactor Specialist',
        description: 'Refactors legacy microservices with AST safety',
        role: 'builder_tdd',
        model: 'deepseek-r1',
        prompt: 'Refactor cleanly without functional regression',
        skills: ['ast-analysis', 'tdd-workflow'],
        tools: ['bash_exec', 'file_writer', 'repo_search'],
        plugins: ['extism://plugins/tdd-runner.wasm'],
        thinking: 'max',
      });

      expect(newTpl.id).toBeDefined();
      expect(newTpl.name).toBe('Fullstack Refactor Specialist');
      expect(newTpl.model).toBe('deepseek-r1');
      expect(newTpl.thinking).toBe('max');

      // Subsequent fetch should include this newly created template
      const allTemplates = await fetchAgentTemplates();
      const found = allTemplates.find((t) => t.id === newTpl.id);
      expect(found).toBeDefined();
      expect(found?.name).toBe('Fullstack Refactor Specialist');
    });

    it('creates agent template via POST /api/v1/registry/templates when remote server responds ok', async () => {
      const serverCreated: AgentTemplate = {
        id: 'tpl-remote-99',
        name: 'Cloud Security Sentinel',
        description: 'Monitors CVEs in real-time',
        role: 'reviewer',
        model: 'claude-3-5-sonnet',
        skills: ['security-audit'],
        tools: ['ast_grep'],
        thinking: 'high',
        createdAt: '2026-03-30T10:00:00Z',
      };

      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 201,
        json: async () => ({ template: serverCreated }),
      });

      const result = await createAgentTemplate(
        {
          name: 'Cloud Security Sentinel',
          description: 'Monitors CVEs in real-time',
          role: 'reviewer',
          model: 'claude-3-5-sonnet',
        },
        { baseUrl: 'http://localhost:8080', customFetch: fetchMock as any }
      );

      expect(fetchMock).toHaveBeenCalledWith(
        'http://localhost:8080/api/v1/registry/templates',
        expect.objectContaining({
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Accept: 'application/json',
          },
        })
      );
      expect(result).toEqual(serverCreated);
    });

    it('rejects template creation if name is empty', async () => {
      await expect(
        createAgentTemplate({
          name: '',
          description: 'No name',
          model: 'gpt-4o',
        })
      ).rejects.toThrow('Template name is required');
    });

    it('deletes agent template locally', async () => {
      globalThis.fetch = vi.fn().mockRejectedValue(new Error('Offline'));
      await createAgentTemplate({
        id: 'tpl-to-delete',
        name: 'Temporary Agent',
        description: 'Will be deleted',
        model: 'claude-3-5-sonnet',
      });

      let templates = await fetchAgentTemplates();
      expect(templates.some((t) => t.id === 'tpl-to-delete')).toBe(true);

      const deleted = await deleteAgentTemplate('tpl-to-delete');
      expect(deleted).toBe(true);

      templates = await fetchAgentTemplates();
      expect(templates.some((t) => t.id === 'tpl-to-delete')).toBe(false);
    });

    it('deletes agent template via DELETE /api/v1/agent-templates/:id when remote server responds ok', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ success: true, id: 'tpl-remote-delete' }),
      });

      const result = await deleteAgentTemplate('tpl-remote-delete', {
        baseUrl: 'http://localhost:8080',
        customFetch: fetchMock as any,
      });

      expect(fetchMock).toHaveBeenCalledWith(
        'http://localhost:8080/api/v1/agent-templates/tpl-remote-delete',
        expect.objectContaining({
          method: 'DELETE',
          headers: {
            Accept: 'application/json',
          },
        })
      );
      expect(result).toBe(true);
    });

    it('rejects deletion if id is empty', async () => {
      await expect(deleteAgentTemplate('')).rejects.toThrow('Template id is required');
      await expect(deleteAgentTemplate('   ')).rejects.toThrow('Template id is required');
    });
  });
});
