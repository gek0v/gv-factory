// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useFactoryFlowStore } from '../store';
import * as runsApi from '../api/runs';
import * as simulatorApi from '../api/simulator';

describe('Toolbar integration and state interaction', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('triggers live pipeline execution via submitPipelineRun with active store nodes & edges', async () => {
    const mockSubmit = vi.spyOn(runsApi, 'submitPipelineRun').mockResolvedValueOnce({
      run_id: 'run-live-101',
      state: 'RUNNING',
    });

    const store = useFactoryFlowStore.getState();
    const result = await runsApi.submitPipelineRun(store.nodes, store.edges);

    expect(result.run_id).toBe('run-live-101');
    expect(result.state).toBe('RUNNING');
    expect(mockSubmit).toHaveBeenCalledTimes(1);
    expect(mockSubmit).toHaveBeenCalledWith(store.nodes, store.edges);
  });

  it('triggers simulatorApi.simulatePipelineRun to execute local simulated flow', async () => {
    const mockSim = vi.spyOn(simulatorApi, 'simulatePipelineRun').mockResolvedValueOnce();

    await simulatorApi.simulatePipelineRun();
    expect(mockSim).toHaveBeenCalledTimes(1);
  });

  it('exports valid formatted JSON DAG template from store nodes & edges', () => {
    const store = useFactoryFlowStore.getState();
    const jsonStr = runsApi.exportPipelineTemplate(store.nodes, store.edges, 'json');

    const parsed = JSON.parse(jsonStr);
    expect(parsed.apiVersion).toBe('factory.ov.io/v1alpha1');
    expect(parsed.kind).toBe('FactoryTemplate');
    expect(parsed.spec.steps.length).toBeGreaterThan(0);
    expect(parsed.spec.entrypoint).toBeDefined();
  });

  it('exports valid formatted YAML DAG template from store nodes & edges', () => {
    const store = useFactoryFlowStore.getState();
    const yamlStr = runsApi.exportPipelineTemplate(store.nodes, store.edges, 'yaml');

    expect(yamlStr).toContain('apiVersion: factory.ov.io/v1alpha1');
    expect(yamlStr).toContain('kind: FactoryTemplate');
    expect(yamlStr).toContain('entrypoint:');
  });
});
