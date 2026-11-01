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

  it('handles ov.worker.logs and worker.logs with multiline chunks', () => {
    const targetNode = useFactoryFlowStore.getState().nodes[0];
    expect(targetNode).toBeTruthy();

    const workerLogEvent: GatewayEvent = {
      type: 'ov.worker.logs',
      task_id: targetNode.id,
      payload: {
        run_id: 'run-101',
        stream: 'stdout',
        chunk: '\x1b[32m[PASS]\x1b[0m test_worker_pool\n\x1b[31m[FAIL]\x1b[0m test_timeout\n',
        timestamp: '2026-11-02T10:05:00Z',
      },
    };

    dispatchGatewayEvent(workerLogEvent, useFactoryFlowStore.getState());

    const node = useFactoryFlowStore.getState().nodes.find((n) => n.id === targetNode.id);
    expect(node?.data.logs?.length).toBe(2);
    expect(node?.data.logs?.[0].message).toBe('\x1b[32m[PASS]\x1b[0m test_worker_pool');
    expect(node?.data.logs?.[0].stream).toBe('stdout');
    expect(node?.data.logs?.[1].message).toBe('\x1b[31m[FAIL]\x1b[0m test_timeout');
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

  it('handles gate.approval_required for MCP agent tool gates and flags caller node', () => {
    const store = useFactoryFlowStore.getState();
    const builderNode = store.nodes.find((n) => n.data.role === 'builder_tdd') || store.nodes[1];
    expect(builderNode).toBeTruthy();

    const toolGateEvent: GatewayEvent = {
      type: 'gate.approval_required',
      run_id: 'run-mcp-42',
      payload: {
        token: 'hmac-tool-sig-abc',
        gate_type: 'tool',
        tool_name: 'bash_exec',
        arguments: { command: 'rm -rf /tmp/target && make build' },
        caller_id: builderNode.id,
        timeout_ms: 45000,
      },
    };

    dispatchGatewayEvent(toolGateEvent, store);

    const activeApproval = useFactoryFlowStore.getState().activeApproval;
    expect(activeApproval).toBeTruthy();
    expect(activeApproval?.gateType).toBe('tool');
    expect(activeApproval?.toolName).toBe('bash_exec');
    expect(activeApproval?.callerId).toBe(builderNode.id);
    expect(activeApproval?.token).toBe('hmac-tool-sig-abc');
    expect(activeApproval?.timeoutMs).toBe(45000);

    const updatedNode = useFactoryFlowStore.getState().nodes.find((n) => n.id === builderNode.id);
    expect(updatedNode?.data.status).toBe('awaiting_approval');
  });

  it('handles ov.worker.heartbeat events and updates workerPool in store', () => {
    const store = useFactoryFlowStore.getState();

    const heartbeatEvent: GatewayEvent = {
      type: 'ov.worker.heartbeat',
      payload: {
        worker_id: 'worker-node-alpha',
        active_tasks: 2,
        max_concurrency: 4,
        uptime_seconds: 3600,
        timestamp: 1761998500,
        status: 'busy',
      },
    };

    dispatchGatewayEvent(heartbeatEvent, store);

    const pool = useFactoryFlowStore.getState().workerPool;
    expect(pool).toBeTruthy();
    expect(pool?.total_workers).toBe(1);
    expect(pool?.active_workers).toBe(1);
    expect(pool?.total_capacity).toBe(4);
    expect(pool?.total_active_tasks).toBe(2);
    expect(pool?.workers[0].worker_id).toBe('worker-node-alpha');
    expect(pool?.workers[0].status).toBe('busy');
  });
});

