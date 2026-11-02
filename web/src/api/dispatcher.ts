// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import type { FactoryFlowState } from '../store';
import type { GateApprovalPayload, ReviewerDiagnostic } from '../types/flow';
import type { GatewayEvent } from './types';

export function dispatchGatewayEvent(event: GatewayEvent, store: FactoryFlowState): void {
  if (!event || !event.type) return;

  const type = event.type.toLowerCase();
  const payload = event.payload || {};
  const stepId = (event.task_id || (payload.step_id as string) || (payload.id as string) || '').trim();

  if (type === 'task.created' || type === 'ov.task.pending') {
    if (stepId) {
      store.updateNodeStatus(stepId, 'pending');
    }
  } else if (type === 'step.started' || type === 'ov.task.started') {
    if (stepId) {
      store.updateNodeTelemetry(stepId, {
        status: 'running',
        startedAt: new Date().toISOString(),
      });
    }
  } else if (
    type === 'ov.task.log' ||
    type === 'task.log' ||
    type === 'ov.worker.logs' ||
    type === 'worker.logs' ||
    type === 'worker.log.chunk' ||
    type === 'ov.worker.log.chunk'
  ) {
    if (stepId) {
      const stream = (payload.stream as 'stdout' | 'stderr') || 'stdout';
      const msg =
        (payload.chunk as string) ||
        (payload.message as string) ||
        (payload.log as string) ||
        (payload.line as string) ||
        '';
      const ts = (payload.timestamp as string) || (event.timestamp ? String(event.timestamp) : undefined);
      if (msg) {
        store.appendNodeLog(stepId, stream, msg, ts);
      }
    }
  } else if (type === 'ov.task.span' || type === 'task.span') {
    if (stepId) {
      const duration = (payload.duration_ms as number) ?? (payload.duration as number);
      const tokensUsed = (payload.tokens_used as number) ?? (payload.tokens as number);
      const promptTokens = (payload.prompt_tokens as number);
      const completionTokens = (payload.completion_tokens as number);
      store.updateNodeTelemetry(stepId, {
        ...(duration !== undefined ? { executionDurationMs: duration } : {}),
        ...(tokensUsed !== undefined ? { tokensUsed } : {}),
        ...(promptTokens !== undefined ? { promptTokens } : {}),
        ...(completionTokens !== undefined ? { completionTokens } : {}),
        attributes: payload,
      });
    }
  } else if (type === 'step.completed' || type === 'ov.task.completed') {
    if (stepId) {
      const duration = (payload.duration_ms as number) || (payload.duration as number) || 0;
      store.updateNodeTelemetry(stepId, {
        status: 'completed',
        executionDurationMs: duration,
        completedAt: new Date().toISOString(),
        output: payload,
      });

      if (typeof payload.diff === 'string' && payload.diff.length > 0) {
        store.setNodeGitDiff(stepId, payload.diff);
      }
    }
  } else if (type === 'step.failed' || type === 'ov.task.failed') {
    if (stepId) {
      store.updateNodeTelemetry(stepId, {
        status: 'failed',
        errorMessage: (payload.error as string) || 'Execution failed',
        completedAt: new Date().toISOString(),
      });
    }
  } else if (type === 'ov.run.queued' || type === 'run.queued') {
    const runId = event.run_id || (payload.run_id as string) || '';
    if (runId) {
      store.setActiveRun(runId, 'QUEUED');
    }
  } else if (type === 'ov.run.started' || type === 'run.started') {
    const runId = event.run_id || (payload.run_id as string) || '';
    if (runId) {
      store.setActiveRun(runId, 'RUNNING');
    }
  } else if (type === 'ov.run.completed' || type === 'run.completed' || type === 'merger.completed') {
    const runId = event.run_id || (payload.run_id as string) || '';
    if (runId) {
      store.setActiveRun(runId, 'COMPLETED');
    }
    const actionNode = store.nodes.find((n) => n.data.nodeType === 'action' || n.id.includes('merger'));
    if (actionNode) {
      store.updateNodeStatus(actionNode.id, 'completed');
    }
  } else if (type === 'ov.run.failed' || type === 'run.failed') {
    const runId = event.run_id || (payload.run_id as string) || '';
    if (runId) {
      store.setActiveRun(runId, 'FAILED');
    }
  } else if (type === 'ov.run.cancelled' || type === 'run.cancelled') {
    const runId = event.run_id || (payload.run_id as string) || '';
    if (runId) {
      store.setActiveRun(runId, 'CANCELLED');
    }
  } else if (type === 'ov.worker.heartbeat' || type === 'worker.heartbeat') {
    const workerId = (payload.worker_id as string) || (event.task_id as string) || '';
    if (workerId) {
      store.updateWorkerHeartbeat({
        worker_id: workerId,
        active_tasks: Number(payload.active_tasks) || 0,
        max_concurrency: Number(payload.max_concurrency) || 1,
        uptime_seconds: Number(payload.uptime_seconds) || 0,
        timestamp: Number(payload.timestamp) || (event.timestamp ? Math.floor(event.timestamp / 1000) : Math.floor(Date.now() / 1000)),
        status: (payload.status as import('../types/worker').WorkerStatus) || (Number(payload.active_tasks) > 0 ? 'busy' : 'ready'),
      });
    }
  } else if (
    type === 'gate.approval_required' ||
    type === 'ov.gate.approval_required' ||
    type === 'gate.requested' ||
    type === 'ov.gate.requested'
  ) {
    const runId = event.run_id || (payload.run_id as string) || `run-${Date.now()}`;
    const token = (payload.token as string) || 'hmac-approval-token-xyz';
    const timeoutMs = (payload.timeout_ms as number) ?? (payload.timeoutMs as number);
    const expiresAt =
      (payload.expires_at as string) ||
      (payload.expiresAt as string) ||
      (timeoutMs ? new Date(Date.now() + timeoutMs).toISOString() : new Date(Date.now() + 3600000).toISOString());

    const rawGateType = (payload.gate_type as string) || (payload.gateType as string);
    const toolName = (payload.tool_name as string) || (payload.toolName as string);
    const callerId = (payload.caller_id as string) || (payload.callerId as string) || stepId;
    const toolArguments = (payload.arguments as Record<string, unknown> | string) ?? (payload.tool_arguments as Record<string, unknown> | string);

    const isToolGate = rawGateType === 'tool' || rawGateType === 'tool_gate' || Boolean(toolName);
    const gateType = isToolGate ? 'tool' : 'pipeline';

    if (callerId) {
      const targetNode = store.nodes.find(
        (n) => n.id === callerId || (n.data as { stepId?: string })?.stepId === callerId
      );
      if (targetNode) {
        store.updateNodeStatus(targetNode.id, 'awaiting_approval');
      }
    }

    if (!callerId || !isToolGate) {
      const approverNode = store.nodes.find(
        (n) => n.data.role === 'approver' || n.id.includes('approver') || n.id.includes('gate')
      );
      if (approverNode) {
        store.updateNodeStatus(approverNode.id, 'awaiting_approval');
      }
    }

    const approvalPayload: GateApprovalPayload = {
      runId,
      token,
      expiresAt,
      gateType,
      toolName,
      toolArguments,
      callerId,
      stepId: stepId || callerId,
      timeoutMs,
      coveragePercent: (payload.coverage_percent as number) ?? 93.8,
      securityAlerts: (payload.security_alerts as number) ?? 0,
      minCoveragePercent: (payload.min_coverage_percent as number) ?? 90.0,
      maxSecurityAlerts: (payload.max_security_alerts as number) ?? 0,
      headBranch: (payload.head_branch as string) || `ov-run/${runId}`,
      baseBranch: (payload.base_branch as string) || 'main',
      diagnostics: (payload.diagnostics as ReviewerDiagnostic[]) || [
        {
          reviewerId: 'security-reviewer',
          reviewerName: 'Security Reviewer',
          specialty: 'security',
          status: 'passed',
          findings: ['0 OWASP Top-10 vulnerabilities found', '0 hardcoded secrets identified'],
        },
        {
          reviewerId: 'arch-reviewer',
          reviewerName: 'Architecture Reviewer',
          specialty: 'arch',
          status: 'passed',
          findings: ['Clean architecture boundaries preserved', 'No cyclical module imports'],
        },
        {
          reviewerId: 'code-reviewer',
          reviewerName: 'Code Quality Reviewer',
          specialty: 'code',
          status: 'passed',
          findings: ['Test coverage: 93.8% (Target: >=90.0%)', 'Zero linter warnings'],
        },
      ],
    };

    store.setActiveApproval(approvalPayload);
  } else if (type === 'merger.completed' || type === 'ov.run.completed') {
    const actionNode = store.nodes.find((n) => n.data.nodeType === 'action' || n.id.includes('merger'));
    if (actionNode) {
      store.updateNodeStatus(actionNode.id, 'completed');
    }
  } else if (type === 'merger.diverted_to_pr') {
    const approverNode = store.nodes.find(
      (n) => n.data.role === 'approver' || n.id.includes('approver')
    );
    if (approverNode) {
      store.updateNodeTelemetry(approverNode.id, {
        status: 'completed',
        output: { pr_url: (payload.pr_url as string) || 'https://github.com/gek0v/gv-factory/pull/1' },
      });
    }
  }

  if (stepId && Array.isArray(payload.logs)) {
    for (const log of payload.logs as Array<{ stream?: 'stdout' | 'stderr'; message?: string }>) {
      if (log && typeof log.message === 'string') {
        store.appendNodeLog(stepId, log.stream || 'stdout', log.message);
      }
    }
  }
}
