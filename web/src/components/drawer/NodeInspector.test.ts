// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { useFactoryFlowStore } from '../../store';
import * as stepsApi from '../../api/steps';
import type { RunStep } from '../../types/worker';
import type { AgentNodeData } from '../../types/flow';
import { NodeInspector } from './NodeInspector';
import { DEFAULT_AGENT_TEMPLATES, createAgentTemplate } from '../../api/registry';

describe('NodeInspector Steps Integration', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('queries fetchRunSteps when activeRunId is present', async () => {
    const mockSteps: RunStep[] = [
      {
        id: 'step-01',
        run_id: 'run-insp-1',
        step_name: 'spec_writer',
        status: 'COMPLETED',
        duration_ms: 1200,
        output: { spec_approved: true },
        created_at: '2026-11-01T12:00:00Z',
      },
      {
        id: 'step-02',
        run_id: 'run-insp-1',
        step_name: 'builder_tdd',
        status: 'FAILED',
        duration_ms: 2500,
        error_message: 'Compiler panic: syntax error in main.go',
        created_at: '2026-11-01T12:01:00Z',
      },
    ];

    const fetchStepsSpy = vi.spyOn(stepsApi, 'fetchRunSteps').mockResolvedValue(mockSteps);

    useFactoryFlowStore.getState().setActiveRun('run-insp-1', 'RUNNING');
    expect(useFactoryFlowStore.getState().activeRunId).toBe('run-insp-1');

    const result = await stepsApi.fetchRunSteps('run-insp-1');
    expect(fetchStepsSpy).toHaveBeenCalledWith('run-insp-1');
    expect(result).toHaveLength(2);
    expect(result[0].status).toBe('COMPLETED');
    expect(result[1].status).toBe('FAILED');
    expect(result[1].error_message).toContain('Compiler panic');
  });

  it('selects and deselects node properly in store for inspector drawer visibility', () => {
    const store = useFactoryFlowStore.getState();
    const firstNode = store.nodes[0];

    store.setSelectedNodeId(firstNode.id);
    expect(useFactoryFlowStore.getState().selectedNodeId).toBe(firstNode.id);

    store.setSelectedNodeId(null);
    expect(useFactoryFlowStore.getState().selectedNodeId).toBeNull();
  });

  it('updates node sandboxConfig with security policies and resource limits', () => {
    const store = useFactoryFlowStore.getState();
    const targetNode = store.nodes[0];

    // Initial check: sandboxConfig should be undefined or empty
    expect(targetNode.data.sandboxConfig).toBeUndefined();

    // Mutate sandbox config with strict OCI isolation policies
    store.updateNodeData(targetNode.id, {
      sandboxConfig: {
        runtime: 'oci',
        cpuLimit: '2.0',
        memoryLimit: '1024m',
        pidsLimit: 256,
        networkPolicy: 'none',
        securityLevel: 'strict',
      },
    });

    const updatedNode = useFactoryFlowStore.getState().nodes.find((n) => n.id === targetNode.id);
    expect(updatedNode?.data.sandboxConfig).toBeDefined();
    expect(updatedNode?.data.sandboxConfig?.runtime).toBe('oci');
    expect(updatedNode?.data.sandboxConfig?.cpuLimit).toBe('2.0');
    expect(updatedNode?.data.sandboxConfig?.memoryLimit).toBe('1024m');
    expect(updatedNode?.data.sandboxConfig?.pidsLimit).toBe(256);
    expect(updatedNode?.data.sandboxConfig?.networkPolicy).toBe('none');
    expect(updatedNode?.data.sandboxConfig?.securityLevel).toBe('strict');

    // Partial update to switch security posture to standard and network to bridge
    store.updateNodeData(targetNode.id, {
      sandboxConfig: {
        ...updatedNode?.data.sandboxConfig,
        securityLevel: 'standard',
        networkPolicy: 'bridge',
      },
    });

    const patchedNode = useFactoryFlowStore.getState().nodes.find((n) => n.id === targetNode.id);
    expect(patchedNode?.data.sandboxConfig?.runtime).toBe('oci');
    expect(patchedNode?.data.sandboxConfig?.securityLevel).toBe('standard');
    expect(patchedNode?.data.sandboxConfig?.networkPolicy).toBe('bridge');
    expect(patchedNode?.data.sandboxConfig?.pidsLimit).toBe(256);
  });

  it('updates node llmConfig and model when edited in inspector', () => {
    const store = useFactoryFlowStore.getState();
    const agentNode = store.nodes.find((n) => n.data.nodeType === 'agent');
    expect(agentNode).toBeDefined();

    if (!agentNode) return;

    // Mutate LLM config
    store.updateNodeData(agentNode.id, {
      model: 'deepseek-r1',
      llmConfig: {
        provider: 'ollama',
        model: 'deepseek-r1',
        temperature: 0.7,
        maxIterations: 20,
        authorizedTools: ['bash_exec', 'file_writer'],
      },
    });

    const updated = useFactoryFlowStore.getState().nodes.find((n) => n.id === agentNode.id);
    expect(updated?.data.model).toBe('deepseek-r1');
    const llmCfg = updated?.data.llmConfig as import('../../types/flow').AgentLLMConfig;
    expect(llmCfg?.provider).toBe('ollama');
    expect(llmCfg?.temperature).toBe(0.7);
    expect(llmCfg?.maxIterations).toBe(20);
    expect(llmCfg?.authorizedTools).toEqual(['bash_exec', 'file_writer']);
  });

  it('updates decision_gate thresholds and diagnostics in store', () => {
    const store = useFactoryFlowStore.getState();
    const gateNode = store.nodes.find((n) => n.data.nodeType === 'decision_gate');
    expect(gateNode).toBeDefined();

    if (gateNode) {
      store.updateNodeData(gateNode.id, {
        thresholdScore: 95,
        maxSecurityAlerts: 1,
        diagnostics: [
          {
            reviewerId: 'sec-1',
            reviewerName: 'Security Reviewer',
            specialty: 'security',
            status: 'passed',
            findings: ['0 vulnerabilities found in worktree'],
          },
        ],
      });

      const updatedGate = useFactoryFlowStore.getState().nodes.find((n) => n.id === gateNode.id);
      expect((updatedGate?.data as any).thresholdScore).toBe(95);
      expect((updatedGate?.data as any).maxSecurityAlerts).toBe(1);
      expect((updatedGate?.data as any).diagnostics).toHaveLength(1);
    }
  });

  describe('Agent Template, Skills, Tools, Plugins & Thinking Designer', () => {
    it('auto-fills agent node skills, tools, model, prompt, and thinking when template is applied', () => {
      const store = useFactoryFlowStore.getState();
      const agentNode = store.nodes.find((n) => n.data.nodeType === 'agent');
      expect(agentNode).toBeDefined();
      if (!agentNode) return;

      const tpl = DEFAULT_AGENT_TEMPLATES.find((t) => t.id === 'tpl-builder-tdd')!;
      expect(tpl).toBeDefined();

      store.updateNodeData(agentNode.id, {
        templateId: tpl.id,
        model: tpl.model,
        skills: [...(tpl.skills || [])],
        tools: [...(tpl.tools || [])],
        plugins: [...(tpl.plugins || [])],
        thinking: tpl.thinking || 'off',
        prompt: tpl.prompt || '',
        llmConfig: {
          provider: 'anthropic',
          model: tpl.model,
          temperature: tpl.temperature ?? 0.2,
          maxIterations: tpl.maxIterations ?? 10,
          authorizedTools: [...(tpl.tools || [])],
        },
      });

      const updated = useFactoryFlowStore.getState().nodes.find((n) => n.id === agentNode.id);
      const data = updated?.data as AgentNodeData;

      expect(data.templateId).toBe('tpl-builder-tdd');
      expect(data.model).toBe('claude-3-5-sonnet');
      expect(data.skills).toContain('tdd-workflow');
      expect(data.skills).toContain('git-guardrails');
      expect(data.tools).toContain('bash_exec');
      expect(data.tools).toContain('test_runner');
      expect(data.plugins).toContain('extism://plugins/tdd-runner.wasm');
      expect(data.thinking).toBe('max');
      expect(data.prompt).toContain('autonomous TDD Builder');
      expect(data.llmConfig?.authorizedTools).toEqual(tpl.tools);
    });

    it('allows dynamic addition and removal of skills from pool', () => {
      const store = useFactoryFlowStore.getState();
      const agentNode = store.nodes.find((n) => n.data.nodeType === 'agent')!;

      // Add skills
      store.updateNodeData(agentNode.id, {
        skills: ['ast-analysis', 'security-audit'],
      });

      let updated = useFactoryFlowStore.getState().nodes.find((n) => n.id === agentNode.id);
      expect((updated?.data as AgentNodeData).skills).toEqual(['ast-analysis', 'security-audit']);

      // Append another skill
      const current = (updated?.data as AgentNodeData).skills || [];
      store.updateNodeData(agentNode.id, {
        skills: [...current, 'code-review-standards'],
      });

      updated = useFactoryFlowStore.getState().nodes.find((n) => n.id === agentNode.id);
      expect((updated?.data as AgentNodeData).skills).toHaveLength(3);

      // Remove one skill
      store.updateNodeData(agentNode.id, {
        skills: (updated?.data as AgentNodeData).skills?.filter((s) => s !== 'security-audit'),
      });

      updated = useFactoryFlowStore.getState().nodes.find((n) => n.id === agentNode.id);
      expect((updated?.data as AgentNodeData).skills).toEqual(['ast-analysis', 'code-review-standards']);
    });

    it('allows dynamic addition and removal of tools and authorized MCPs', () => {
      const store = useFactoryFlowStore.getState();
      const agentNode = store.nodes.find((n) => n.data.nodeType === 'agent')!;

      // Attach MCP postgres and bash
      store.updateNodeData(agentNode.id, {
        tools: ['bash_exec', 'mcp_postgres'],
        llmConfig: {
          provider: 'anthropic',
          model: 'claude-3-5-sonnet',
          authorizedTools: ['bash_exec', 'mcp_postgres'],
        },
      });

      let updated = useFactoryFlowStore.getState().nodes.find((n) => n.id === agentNode.id);
      expect((updated?.data as AgentNodeData).tools).toContain('mcp_postgres');
      expect((updated?.data as AgentNodeData).llmConfig?.authorizedTools).toContain('mcp_postgres');

      // Add mcp_github
      const newTools = [...((updated?.data as AgentNodeData).tools || []), 'mcp_github'];
      store.updateNodeData(agentNode.id, {
        tools: newTools,
        llmConfig: {
          ...((updated?.data as AgentNodeData).llmConfig || {}),
          model: 'claude-3-5-sonnet',
          provider: 'anthropic',
          authorizedTools: newTools,
        },
      });

      updated = useFactoryFlowStore.getState().nodes.find((n) => n.id === agentNode.id);
      expect((updated?.data as AgentNodeData).tools).toEqual(['bash_exec', 'mcp_postgres', 'mcp_github']);

      // Remove bash_exec
      const filtered = ((updated?.data as AgentNodeData).tools || []).filter((t) => t !== 'bash_exec');
      store.updateNodeData(agentNode.id, {
        tools: filtered,
        llmConfig: {
          ...((updated?.data as AgentNodeData).llmConfig || {}),
          model: 'claude-3-5-sonnet',
          provider: 'anthropic',
          authorizedTools: filtered,
        },
      });

      updated = useFactoryFlowStore.getState().nodes.find((n) => n.id === agentNode.id);
      expect((updated?.data as AgentNodeData).tools).toEqual(['mcp_postgres', 'mcp_github']);
    });

    it('allows switching thinking levels among off, minimal, low, medium, high, max', () => {
      const store = useFactoryFlowStore.getState();
      const agentNode = store.nodes.find((n) => n.data.nodeType === 'agent')!;

      const levels = ['off', 'minimal', 'low', 'medium', 'high', 'max'] as const;
      for (const level of levels) {
        store.updateNodeData(agentNode.id, { thinking: level });
        const updated = useFactoryFlowStore.getState().nodes.find((n) => n.id === agentNode.id);
        expect((updated?.data as AgentNodeData).thinking).toBe(level);
      }
    });

    it('renders Agent Template selector, skills, tools, plugins, and thinking controls in markup', () => {
      const currentNodes = useFactoryFlowStore.getState().nodes;
      const agentNode = currentNodes.find((n) => n.data.nodeType === 'agent')!;
      expect(agentNode).toBeDefined();

      useFactoryFlowStore.getState().setSelectedNodeId(agentNode.id);
      useFactoryFlowStore.getState().updateNodeData(agentNode.id, {
        templateId: 'tpl-spec-writer',
        skills: ['sdd-architecture'],
        tools: ['repo_search', 'git_read'],
        plugins: ['extism://plugins/sdd-gen.wasm'],
        thinking: 'high',
        prompt: 'You are an SDD Architect',
      });

      const html = renderToStaticMarkup(React.createElement(NodeInspector));

      expect(html).toContain('Agent Template');
      expect(html).toContain('agent-template-select');
      expect(html).toContain('Skills Pool');
      expect(html).toContain('Authorized Tools &amp; MCPs');
      expect(html).toContain('Plugins &amp; WASM Extensions');
      expect(html).toContain('Thinking Level (CoT Budget)');
      expect(html).toContain('Save Agent as New Template');
      expect(html).toContain('You are an SDD Architect');
    });

    it('creates and saves new agent template via createAgentTemplate', async () => {
      const created = await createAgentTemplate({
        name: 'Autonomous Refactoring Specialist',
        description: 'AST-driven code modernization',
        role: 'builder_tdd',
        model: 'deepseek-r1',
        skills: ['ast-analysis'],
        tools: ['bash_exec', 'file_writer'],
        thinking: 'high',
      });

      expect(created.id).toBeDefined();
      expect(created.name).toBe('Autonomous Refactoring Specialist');

      const store = useFactoryFlowStore.getState();
      const agentNode = store.nodes.find((n) => n.data.nodeType === 'agent')!;
      store.updateNodeData(agentNode.id, { templateId: created.id });

      const updated = useFactoryFlowStore.getState().nodes.find((n) => n.id === agentNode.id);
      expect((updated?.data as AgentNodeData).templateId).toBe(created.id);
    });
  });
});
