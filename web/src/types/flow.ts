// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import type { Node, Edge } from '@xyflow/react';

export type ExecutionStatus =
  | 'pending'
  | 'running'
  | 'completed'
  | 'failed'
  | 'awaiting_approval';

export type RunLifecycleState =
  | 'QUEUED'
  | 'RUNNING'
  | 'COMPLETED'
  | 'FAILED'
  | 'CANCELLED';

export type FactoryNodeType = 'trigger' | 'agent' | 'logic' | 'action';

export type TriggerSubtype = 'webhook' | 'github_issue' | 'chat_listener' | 'cron';

export type AgentRole = 'spec_writer' | 'builder_tdd' | 'reviewer' | 'approver';

export type ReviewerSpecialty = 'security' | 'arch' | 'ui' | 'code';

export type LogicSubtype = 'if_else' | 'threshold' | 'llm_router';

export type ActionSubtype =
  | 'git_commit'
  | 'git_push'
  | 'open_pr'
  | 'slack_notification'
  | 'discord_notification';

export interface NodeLogEntry {
  timestamp: string;
  stream: 'stdout' | 'stderr';
  message: string;
}

export interface ReviewerDiagnostic {
  reviewerId: string;
  reviewerName: string;
  specialty: ReviewerSpecialty;
  status: 'passed' | 'failed' | 'warning';
  findings: string[];
}

export type GateType = 'pipeline' | 'tool';

export type ToolRiskLevel = 'critical' | 'high' | 'sensitive' | 'standard';

export interface GateApprovalPayload {
  runId: string;
  token: string;
  expiresAt: string;
  gateType?: GateType | 'pipeline_gate' | 'tool_gate';
  coveragePercent?: number;
  securityAlerts?: number;
  minCoveragePercent?: number;
  maxSecurityAlerts?: number;
  diagnostics?: ReviewerDiagnostic[];
  headBranch?: string;
  baseBranch?: string;
  toolName?: string;
  toolArguments?: Record<string, unknown> | string;
  callerId?: string;
  stepId?: string;
  timeoutMs?: number;
  rejectionReason?: string;
}

export type GatewayConnectionStatus = 'disconnected' | 'connecting' | 'connected' | 'reconnecting';

export interface NodeTelemetryUpdate {
  status?: ExecutionStatus;
  executionDurationMs?: number;
  startedAt?: string;
  completedAt?: string;
  tokensUsed?: number;
  promptTokens?: number;
  completionTokens?: number;
  attributes?: Record<string, unknown>;
  output?: Record<string, unknown>;
  errorMessage?: string;
}

export interface BaseNodeData {
  label: string;
  description?: string;
  status: ExecutionStatus;
  nodeType: FactoryNodeType;
  executionDurationMs?: number;
  tokensUsed?: number;
  promptTokens?: number;
  completionTokens?: number;
  startedAt?: string;
  completedAt?: string;
  logs?: NodeLogEntry[];
  gitDiff?: string;
  output?: Record<string, unknown>;
  errorMessage?: string;
  [key: string]: unknown;
}

export interface TriggerNodeData extends BaseNodeData {
  nodeType: 'trigger';
  subtype: TriggerSubtype;
  eventSource?: string;
  cronSchedule?: string;
}

export interface AgentNodeData extends BaseNodeData {
  nodeType: 'agent';
  role: AgentRole;
  reviewerSpecialty?: ReviewerSpecialty;
  model: string;
  pluginTag?: string;
  sandboxEnabled?: boolean;
}

export interface LogicNodeData extends BaseNodeData {
  nodeType: 'logic';
  subtype: LogicSubtype;
  condition?: string;
  thresholdScore?: number;
  routes?: string[];
}

export interface ActionNodeData extends BaseNodeData {
  nodeType: 'action';
  subtype: ActionSubtype;
  target?: string;
  branchName?: string;
}

export type FactoryNodeData =
  | TriggerNodeData
  | AgentNodeData
  | LogicNodeData
  | ActionNodeData;

export type FactoryFlowNode = Node<FactoryNodeData>;
export type FactoryFlowEdge = Edge;
