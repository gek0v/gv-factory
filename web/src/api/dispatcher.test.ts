// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, beforeEach } from 'vitest';
import { useFactoryFlowStore } from '../store';
import { dispatchGatewayEvent } from './dispatcher';
import type { GatewayEvent } from './types';

describe('dispatchGatewayEvent for logs, spans, and run lifecycle', () => {
  beforeEach(() => {
    // Reset store to known state
    useFactoryFlowStore.getState().setActiveRun(null);
    const firstNode = useFactoryFlowStore.getState().nodes[0];
    if (firstNode) {
      useFactoryFlowStore.getState().clearNodeLogs(firstNode.id);
    }
  });

  it('handles ov.task.log and task.log appending to node logs', () => {
    const targetNode = useFactoryFlowStore.getState().nodes[0];
    expect(targetNode).toBeTruthy();

    const logEvent1: GatewayEvent = {
      type: 'ov.task.log',
      task_id: targetNode.id,
      timestamp: 1761998400000,
      payload: {
        stream: 'stdout',
        message: 'Compilation started via rustc WASM target',
      },
    };

    dispatchGatewayEvent(logEvent1, useFactoryFlowStore.getState());

    let node = useFactoryFlowStore.getState().nodes.find((n) => n.id === targetNode.id);
    expect(node?.data.logs?.length).toBe(1);
    expect(node?.data.logs?.[0].message).toBe('Compilation started via rustc WASM target');
    expect(node?.data.logs?.[0].stream).toBe('stdout');

    const logEvent2: GatewayEvent = {
      type: 'task.log',
      payload: {
        step_id: targetNode.id,
        stream: 'stderr',
        log: 'Compilation warning: unused variable',
        timestamp: '2026-11-01T12:00:01Z',
      },
    };

    dispatchGatewayEvent(logEvent2, useFactoryFlowStore.getState());

    node = useFactoryFlowStore.getState().nodes.find((n) => n.id === targetNode.id);
    expect(node?.data.logs?.length).toBe(2);
    expect(node?.data.logs?.[1].message).toBe('Compilation warning: unused variable');
    expect(node?.data.logs?.[1].stream).toBe('stderr');
  });

  it('handles ov.task.span and task.span updating node telemetry with duration and tokens', () => {
    const targetNode = useFactoryFlowStore.getState().nodes[0];
    expect(targetNode).toBeTruthy();

    const spanEvent: GatewayEvent = {
      type: 'ov.task.span',
      task_id: targetNode.id,
      payload: {
        duration_ms: 1250,
        tokens_used: 4200,
        prompt_tokens: 3000,
        completion_tokens: 1200,
        trace_id: 'trace-abc-123',
      },
    };

    dispatchGatewayEvent(spanEvent, useFactoryFlowStore.getState());

    const node = useFactoryFlowStore.getState().nodes.find((n) => n.id === targetNode.id);
    expect(node?.data.executionDurationMs).toBe(1250);
    expect(node?.data.tokensUsed).toBe(4200);
    expect(node?.data.promptTokens).toBe(3000);
    expect(node?.data.completionTokens).toBe(1200);
  });

  it('handles ov.run.* lifecycle events updating activeRun in store', () => {
    const store = useFactoryFlowStore.getState();

    // Queued
    dispatchGatewayEvent(
      { type: 'ov.run.queued', run_id: 'run-99', payload: {} },
      store
    );
    expect(useFactoryFlowStore.getState().activeRunId).toBe('run-99');
    expect(useFactoryFlowStore.getState().activeRunState).toBe('QUEUED');

    // Started
    dispatchGatewayEvent(
      { type: 'ov.run.started', run_id: 'run-99', payload: {} },
      store
    );
    expect(useFactoryFlowStore.getState().activeRunState).toBe('RUNNING');

    // Completed
    dispatchGatewayEvent(
      { type: 'ov.run.completed', run_id: 'run-99', payload: {} },
      store
    );
    expect(useFactoryFlowStore.getState().activeRunState).toBe('COMPLETED');

    // Failed
    dispatchGatewayEvent(
      { type: 'ov.run.failed', run_id: 'run-99', payload: {} },
      store
    );
    expect(useFactoryFlowStore.getState().activeRunState).toBe('FAILED');

    // Cancelled
    dispatchGatewayEvent(
      { type: 'ov.run.cancelled', run_id: 'run-99', payload: {} },
      store
    );
    expect(useFactoryFlowStore.getState().activeRunState).toBe('CANCELLED');
  });
});
