// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import type { AgentRole } from '../types/flow';

export interface Skill {
  id: string;
  name: string;
  description: string;
  category?: string;
  tags?: string[];
  enabled?: boolean;
}

export interface Tool {
  id: string;
  name: string;
  description: string;
  category?: 'mcp' | 'builtin' | 'custom' | string;
  riskLevel?: 'standard' | 'sensitive' | 'high' | 'critical' | string;
  enabled?: boolean;
}

export interface Plugin {
  id: string;
  name: string;
  description: string;
  version?: string;
  uri?: string;
  entrypoint?: string;
  author?: string;
}

export interface AgentTemplate {
  id: string;
  name: string;
  description: string;
  role?: AgentRole | string;
  model: string;
  prompt?: string;
  system_prompt?: string;
  skills?: string[];
  tools?: string[];
  plugins?: string[];
  thinking?: string | boolean;
  temperature?: number;
  maxIterations?: number;
  createdAt?: string;
}

export interface RegistryRequestOptions {
  baseUrl?: string;
  customFetch?: typeof fetch;
  fallbackToDefaults?: boolean;
}

export const DEFAULT_SKILLS: Skill[] = [
  {
    id: 'sdd-architecture',
    name: 'SDD Architecture',
    description: 'System Design Document drafting and architectural contract decomposition',
    category: 'Architecture',
    tags: ['design', 'spec', 'contracts'],
  },
  {
    id: 'tdd-workflow',
    name: 'TDD Workflow',
    description: 'Red-Green-Refactor test first methodology and unit coverage verification',
    category: 'Testing',
    tags: ['tdd', 'unit-tests', 'refactor'],
  },
  {
    id: 'ast-analysis',
    name: 'AST Code Analysis',
    description: 'Tree-sitter structural AST pattern matching and semantic parsing',
    category: 'Analysis',
    tags: ['tree-sitter', 'ast-grep', 'static-analysis'],
  },
  {
    id: 'security-audit',
    name: 'Security Audit',
    description: 'SAST vulnerability scanning, secret detection, and CVE inspection',
    category: 'Security',
    tags: ['cve', 'secrets', 'sast'],
  },
  {
    id: 'git-guardrails',
    name: 'Git Guardrails',
    description: 'Safe worktree branch management, atomic commits, and merge verification',
    category: 'DevOps',
    tags: ['git', 'worktree', 'merge-tree'],
  },
  {
    id: 'code-review-standards',
    name: 'Code Review Standards',
    description: 'Automated linter and idiom compliance based on project ADRs',
    category: 'Quality',
    tags: ['linter', 'adr', 'style'],
  },
  {
    id: 'perf-profiler',
    name: 'Performance Profiling',
    description: 'Memory allocation tracing and CPU bottleneck detection',
    category: 'Optimization',
    tags: ['profiler', 'benchmarks', 'latency'],
  },
];

export const DEFAULT_TOOLS: Tool[] = [
  {
    id: 'bash_exec',
    name: 'bash_exec',
    description: 'Sandboxed shell command execution inside rootless container',
    category: 'builtin',
    riskLevel: 'sensitive',
  },
  {
    id: 'git_read',
    name: 'git_read',
    description: 'Git status, log, and worktree diff inspection',
    category: 'builtin',
    riskLevel: 'standard',
  },
  {
    id: 'file_writer',
    name: 'file_writer',
    description: 'Atomic file creation and in-place code editing in worktree',
    category: 'builtin',
    riskLevel: 'sensitive',
  },
  {
    id: 'repo_search',
    name: 'repo_search',
    description: 'Fast symbol indexing and ripgrep code queries',
    category: 'builtin',
    riskLevel: 'standard',
  },
  {
    id: 'ast_grep',
    name: 'ast_grep',
    description: 'Structural pattern search via ast-grep engine',
    category: 'builtin',
    riskLevel: 'standard',
  },
  {
    id: 'test_runner',
    name: 'test_runner',
    description: 'Executes vitest / go test suites with coverage instrumentation',
    category: 'builtin',
    riskLevel: 'standard',
  },
  {
    id: 'mcp_postgres',
    name: 'mcp_postgres',
    description: 'PostgreSQL schema reflection and migration inspector (MCP)',
    category: 'mcp',
    riskLevel: 'sensitive',
  },
  {
    id: 'mcp_github',
    name: 'mcp_github',
    description: 'GitHub API client for PR creation, reviews, and issue triage (MCP)',
    category: 'mcp',
    riskLevel: 'high',
  },
  {
    id: 'mcp_snyk',
    name: 'mcp_snyk',
    description: 'Snyk software composition analysis and dependency audit (MCP)',
    category: 'mcp',
    riskLevel: 'standard',
  },
];

export const DEFAULT_PLUGINS: Plugin[] = [
  {
    id: 'extism-sdd-gen',
    name: 'SDD Specification Generator',
    description: 'Extism WASM plugin generating formal SDD documents from issues',
    version: '1.2.0',
    uri: 'extism://plugins/sdd-gen.wasm',
    entrypoint: 'generate_spec',
    author: 'gv-factory core',
  },
  {
    id: 'extism-tdd-runner',
    name: 'TDD Test Runner Harness',
    description: 'Automated test suite execution and diff verification plugin',
    version: '2.0.1',
    uri: 'extism://plugins/tdd-runner.wasm',
    entrypoint: 'run_tests',
    author: 'gv-factory core',
  },
  {
    id: 'extism-security-audit',
    name: 'Security SAST Scanner',
    description: 'AST-based static security vulnerability analyzer plugin',
    version: '1.4.0',
    uri: 'extism://plugins/security-audit.wasm',
    entrypoint: 'audit_worktree',
    author: 'gv-factory security',
  },
  {
    id: 'extism-coverage-gate',
    name: 'Coverage Gate Evaluator',
    description: 'Verifies minimum unit test coverage thresholds before merge',
    version: '1.1.0',
    uri: 'extism://plugins/coverage-gate.wasm',
    entrypoint: 'evaluate_coverage',
    author: 'gv-factory quality',
  },
];

export const DEFAULT_AGENT_TEMPLATES: AgentTemplate[] = [
  {
    id: 'tpl-spec-writer',
    name: 'Spec Writer (SDD Architect)',
    description: 'Generates rigorous technical specifications and test contracts from requirements',
    role: 'spec_writer',
    model: 'claude-3-5-sonnet',
    prompt: 'You are an autonomous Specification Writer. Analyze incoming requirements, decompose them into architectural components, and output clear SDD specs and test criteria.',
    skills: ['sdd-architecture', 'code-review-standards'],
    tools: ['git_read', 'repo_search', 'ast_grep'],
    plugins: ['extism://plugins/sdd-gen.wasm'],
    thinking: 'high',
    temperature: 0.2,
    maxIterations: 10,
    createdAt: '2026-01-15T00:00:00Z',
  },
  {
    id: 'tpl-builder-tdd',
    name: 'Builder TDD (Autonomous Coder)',
    description: 'Full autonomous code generation following strict Red-Green-Refactor cycles in sandbox',
    role: 'builder_tdd',
    model: 'claude-3-5-sonnet',
    prompt: 'You are an autonomous TDD Builder. Write tests first, implement the minimal code to satisfy them, verify zero regressions, and refactor clean code.',
    skills: ['tdd-workflow', 'git-guardrails', 'ast-analysis'],
    tools: ['bash_exec', 'git_read', 'file_writer', 'repo_search', 'test_runner'],
    plugins: ['extism://plugins/tdd-runner.wasm'],
    thinking: 'max',
    temperature: 0.15,
    maxIterations: 20,
    createdAt: '2026-01-15T00:00:00Z',
  },
  {
    id: 'tpl-reviewer-security',
    name: 'Security & SAST Reviewer',
    description: 'Deep AST vulnerability analysis, secret scanning, and CVE prevention before merge',
    role: 'reviewer',
    model: 'claude-3-5-sonnet',
    prompt: 'You are a Security Reviewer. Perform deep AST static analysis, identify potential CVEs or secret leaks, and reject hazardous code patterns.',
    skills: ['security-audit', 'ast-analysis', 'code-review-standards'],
    tools: ['git_read', 'ast_grep', 'repo_search', 'mcp_snyk'],
    plugins: ['extism://plugins/security-audit.wasm'],
    thinking: 'high',
    temperature: 0.1,
    maxIterations: 12,
    createdAt: '2026-01-15T00:00:00Z',
  },
  {
    id: 'tpl-approver-gatekeeper',
    name: 'Gatekeeper & Release Approver',
    description: 'Reviews coverage metrics, linter outputs, and approves or routes to human decision gate',
    role: 'approver',
    model: 'claude-3-5-sonnet',
    prompt: 'You are an autonomous Gatekeeper. Evaluate coverage percentages, security diagnostics, and confirm PR readiness.',
    skills: ['code-review-standards', 'perf-profiler'],
    tools: ['git_read', 'test_runner', 'mcp_github'],
    plugins: ['extism://plugins/coverage-gate.wasm'],
    thinking: 'medium',
    temperature: 0.1,
    maxIterations: 8,
    createdAt: '2026-01-15T00:00:00Z',
  },
];

// In-memory templates store for runtime template creation in frontend session
let localCustomTemplates: AgentTemplate[] = [];

/**
 * Reset local custom templates (primarily for testing)
 */
export function _resetLocalTemplates(): void {
  localCustomTemplates = [];
}

/**
 * Fetch skills from GET /api/v1/registry/skills with graceful fallback
 */
export async function fetchSkills(options?: RegistryRequestOptions): Promise<Skill[]> {
  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = options?.baseUrl ? options.baseUrl.replace(/\/+$/, '') : '';
  const url = `${baseUrl}/api/v1/registry/skills`;

  try {
    const res = await fetchFn(url, {
      method: 'GET',
      headers: { Accept: 'application/json' },
    });

    if (res.ok) {
      const data = await res.json();
      if (Array.isArray(data)) return data as Skill[];
      if (data && Array.isArray(data.skills)) return data.skills as Skill[];
    } else if (options?.fallbackToDefaults === false) {
      throw new Error(`Failed to fetch skills (status ${res.status})`);
    }
  } catch (err) {
    if (options?.fallbackToDefaults === false) {
      throw err;
    }
  }

  return [...DEFAULT_SKILLS];
}

/**
 * Fetch tools and MCPs from GET /api/v1/registry/tools with graceful fallback
 */
export async function fetchTools(options?: RegistryRequestOptions): Promise<Tool[]> {
  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = options?.baseUrl ? options.baseUrl.replace(/\/+$/, '') : '';
  const url = `${baseUrl}/api/v1/registry/tools`;

  try {
    const res = await fetchFn(url, {
      method: 'GET',
      headers: { Accept: 'application/json' },
    });

    if (res.ok) {
      const data = await res.json();
      if (Array.isArray(data)) return data as Tool[];
      if (data && Array.isArray(data.tools)) return data.tools as Tool[];
    } else if (options?.fallbackToDefaults === false) {
      throw new Error(`Failed to fetch tools (status ${res.status})`);
    }
  } catch (err) {
    if (options?.fallbackToDefaults === false) {
      throw err;
    }
  }

  return [...DEFAULT_TOOLS];
}

/**
 * Fetch plugins from GET /api/v1/registry/plugins with graceful fallback
 */
export async function fetchPlugins(options?: RegistryRequestOptions): Promise<Plugin[]> {
  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = options?.baseUrl ? options.baseUrl.replace(/\/+$/, '') : '';
  const url = `${baseUrl}/api/v1/registry/plugins`;

  try {
    const res = await fetchFn(url, {
      method: 'GET',
      headers: { Accept: 'application/json' },
    });

    if (res.ok) {
      const data = await res.json();
      if (Array.isArray(data)) return data as Plugin[];
      if (data && Array.isArray(data.plugins)) return data.plugins as Plugin[];
    } else if (options?.fallbackToDefaults === false) {
      throw new Error(`Failed to fetch plugins (status ${res.status})`);
    }
  } catch (err) {
    if (options?.fallbackToDefaults === false) {
      throw err;
    }
  }

  return [...DEFAULT_PLUGINS];
}

/**
 * Fetch agent templates from GET /api/v1/registry/templates with fallback to catalog + user created templates
 */
export async function fetchAgentTemplates(options?: RegistryRequestOptions): Promise<AgentTemplate[]> {
  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = options?.baseUrl ? options.baseUrl.replace(/\/+$/, '') : '';
  const url = `${baseUrl}/api/v1/registry/templates`;

  try {
    const res = await fetchFn(url, {
      method: 'GET',
      headers: { Accept: 'application/json' },
    });

    if (res.ok) {
      const data = await res.json();
      const serverTemplates = Array.isArray(data)
        ? (data as AgentTemplate[])
        : Array.isArray(data?.templates)
        ? (data.templates as AgentTemplate[])
        : [];
      // Combine server templates with any local templates created in this session
      const combined = [...serverTemplates];
      for (const lt of localCustomTemplates) {
        if (!combined.some((t) => t.id === lt.id)) {
          combined.push(lt);
        }
      }
      return combined;
    } else if (options?.fallbackToDefaults === false) {
      throw new Error(`Failed to fetch agent templates (status ${res.status})`);
    }
  } catch (err) {
    if (options?.fallbackToDefaults === false) {
      throw err;
    }
  }

  return [...DEFAULT_AGENT_TEMPLATES, ...localCustomTemplates];
}

/**
 * Create a new agent template via POST /api/v1/registry/templates
 */
export async function createAgentTemplate(
  template: Omit<AgentTemplate, 'id'> | AgentTemplate,
  options?: RegistryRequestOptions
): Promise<AgentTemplate> {
  if (!template.name || template.name.trim().length === 0) {
    throw new Error('Template name is required');
  }

  const generatedId = (template as AgentTemplate).id || `tpl-custom-${Date.now()}`;
  const newTemplate: AgentTemplate = {
    ...template,
    id: generatedId,
    createdAt: (template as AgentTemplate).createdAt || new Date().toISOString(),
  };

  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = options?.baseUrl ? options.baseUrl.replace(/\/+$/, '') : '';
  const url = `${baseUrl}/api/v1/registry/templates`;

  try {
    const res = await fetchFn(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
      },
      body: JSON.stringify(newTemplate),
    });

    if (res.ok) {
      const saved = await res.json();
      const result = (saved?.template || saved) as AgentTemplate;
      localCustomTemplates = [result, ...localCustomTemplates.filter((t) => t.id !== result.id)];
      return result;
    } else if (options?.fallbackToDefaults === false) {
      throw new Error(`Failed to create agent template (status ${res.status})`);
    }
  } catch (err) {
    if (options?.fallbackToDefaults === false) {
      throw err;
    }
  }

  // Graceful fallback: persist to local session store
  localCustomTemplates = [newTemplate, ...localCustomTemplates.filter((t) => t.id !== newTemplate.id)];
  return newTemplate;
}

/**
 * Delete an agent template via DELETE /api/v1/agent-templates/{id} or /api/v1/registry/templates/{id}
 */
export async function deleteAgentTemplate(
  id: string,
  options?: RegistryRequestOptions
): Promise<boolean> {
  if (!id || id.trim().length === 0) {
    throw new Error('Template id is required');
  }

  const cleanId = id.trim();
  const fetchFn = options?.customFetch || globalThis.fetch;
  const baseUrl = options?.baseUrl ? options.baseUrl.replace(/\/+$/, '') : '';
  const url = `${baseUrl}/api/v1/agent-templates/${encodeURIComponent(cleanId)}`;

  try {
    const res = await fetchFn(url, {
      method: 'DELETE',
      headers: {
        Accept: 'application/json',
      },
    });

    if (res.ok) {
      localCustomTemplates = localCustomTemplates.filter((t) => t.id !== cleanId);
      return true;
    } else if (res.status === 404) {
      // Also try alternative endpoint /api/v1/registry/templates/{id} if needed
      const altUrl = `${baseUrl}/api/v1/registry/templates/${encodeURIComponent(cleanId)}`;
      try {
        const altRes = await fetchFn(altUrl, {
          method: 'DELETE',
          headers: { Accept: 'application/json' },
        });
        if (altRes.ok) {
          localCustomTemplates = localCustomTemplates.filter((t) => t.id !== cleanId);
          return true;
        }
      } catch {
        // ignore
      }
      if (options?.fallbackToDefaults === false) {
        throw new Error(`Failed to delete agent template (status ${res.status})`);
      }
    } else if (options?.fallbackToDefaults === false) {
      throw new Error(`Failed to delete agent template (status ${res.status})`);
    }
  } catch (err) {
    if (options?.fallbackToDefaults === false) {
      throw err;
    }
  }

  // Graceful fallback: remove from local session store
  localCustomTemplates = localCustomTemplates.filter((t) => t.id !== cleanId);
  return true;
}

/**
 * Reset local custom templates cache (primarily for test isolation)
 */
export function _resetLocalCustomTemplates(): void {
  localCustomTemplates = [];
}
