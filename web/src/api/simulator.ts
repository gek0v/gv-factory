// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { useFactoryFlowStore } from '../store';
import { dispatchGatewayEvent } from './dispatcher';

const SAMPLE_BUILDER_DIFF = `diff --git a/internal/service.go b/internal/service.go
index 1a2b3c4..5d6e7f8 100644
--- a/internal/service.go
+++ b/internal/service.go
@@ -10,6 +10,14 @@ func ExecuteTask(ctx context.Context, task Task) error {
+	// Red-Green-Refactor: implemented automated rollback logic
+	if task.Payload == nil {
+		return fmt.Errorf("task payload cannot be nil")
+	}
+	log.Printf("Executing task %s in ephemeral sandbox", task.ID)
+	return nil
 }`;

export async function simulatePipelineRun(): Promise<void> {
  const store = useFactoryFlowStore.getState();
  const nodes = store.nodes;
  if (nodes.length === 0) return;

  // Reset statuses to pending
  nodes.forEach((n) => store.updateNodeStatus(n.id, 'pending'));
  store.setGatewayStatus('connected');

  const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

  // Find steps
  const triggerNode = nodes.find((n) => n.data.nodeType === 'trigger') || nodes[0];
  const builderNode = nodes.find((n) => n.data.role === 'builder_tdd' || n.id.includes('builder'));
  const reviewerNodes = nodes.filter((n) => n.data.role === 'reviewer' || n.id.includes('reviewer'));
  const approverNode = nodes.find((n) => n.data.role === 'approver' || n.id.includes('approver') || n.id.includes('gate'));
  const actionNode = nodes.find((n) => n.data.nodeType === 'action' || n.id.includes('merger'));

  // 1. Trigger
  if (triggerNode) {
    dispatchGatewayEvent({ type: 'step.started', task_id: triggerNode.id }, store);
    await sleep(400);
    dispatchGatewayEvent(
      {
        type: 'step.completed',
        task_id: triggerNode.id,
        payload: {
          duration_ms: 320,
          logs: [{ stream: 'stdout', message: 'Inbound GitHub Issue webhook received & verified' }],
        },
      },
      store
    );
  }

  // 2. Builder TDD
  if (builderNode) {
    await sleep(300);
    dispatchGatewayEvent({ type: 'step.started', task_id: builderNode.id }, store);
    dispatchGatewayEvent(
      {
        type: 'step.started',
        task_id: builderNode.id,
        payload: {
          logs: [
            { stream: 'stdout', message: 'Cloned ephemeral Git worktree: /tmp/ov-worktrees/run-101' },
            { stream: 'stdout', message: 'TDD Loop 1 (Red): writing failing test suite' },
            { stream: 'stdout', message: 'TDD Loop 2 (Green): generating minimum viable patch' },
            { stream: 'stdout', message: 'TDD Loop 3 (Refactor): optimizing AST structures' },
          ],
        },
      },
      store
    );
    await sleep(600);
    dispatchGatewayEvent(
      {
        type: 'step.completed',
        task_id: builderNode.id,
        payload: {
          duration_ms: 1850,
          diff: SAMPLE_BUILDER_DIFF,
        },
      },
      store
    );
  }

  // 3. Parallel Reviewers
  if (reviewerNodes.length > 0) {
    await sleep(300);
    reviewerNodes.forEach((rev) => {
      dispatchGatewayEvent({ type: 'step.started', task_id: rev.id }, store);
    });
    await sleep(700);
    reviewerNodes.forEach((rev) => {
      dispatchGatewayEvent(
        {
          type: 'step.completed',
          task_id: rev.id,
          payload: {
            duration_ms: 680,
            logs: [{ stream: 'stdout', message: `Review completed with zero blocking findings` }],
          },
        },
        store
      );
    });
  }

  // 4. Gate Approval Required
  if (approverNode) {
    await sleep(400);
    dispatchGatewayEvent(
      {
        type: 'gate.approval_required',
        run_id: 'run-simulated-live-99',
        payload: {
          token: 'hmac-simulated-token-998877',
          coverage_percent: 94.6,
          security_alerts: 0,
        },
      },
      store
    );
  }

  // 5. Action Node if existing
  if (actionNode && !approverNode) {
    await sleep(400);
    dispatchGatewayEvent({ type: 'merger.completed', task_id: actionNode.id }, store);
  }
}
