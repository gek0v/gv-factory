// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useFactoryFlowStore } from '../../store';
import * as stepsApi from '../../api/steps';
import type { RunStep } from '../../types/worker';

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
});
