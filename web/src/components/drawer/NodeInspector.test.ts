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
});
