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

export type FactoryNodeType = 'trigger' | 'agent' | 'logic' | 'action' | 'decision_gate';

export type DecisionGateStatus =
  | 'pending_approval'
  | 'approved'
  | 'rejected'
  | 'timed_out';

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

export type SandboxRuntime = 'auto' | 'host' | 'oci' | 'microvm';
export type SandboxNetworkPolicy = 'none' | 'bridge' | 'host';
export type SandboxSecurityLevel = 'standard' | 'strict';

export type LLMProvider = 'anthropic' | 'openai' | 'ollama' | 'local';

export interface AgentLLMConfig {
  provider: LLMProvider | string;
  model: string;
  temperature?: number;          // Rango 0.0 - 2.0 (default: 0.2)
  maxIterations?: number;        // Rango 1 - 50 (default: 10)
  authorizedTools?: string[];    // e.g. ['bash_exec', 'git_read', 'file_writer', 'repo_search']
}

export interface NodeSandboxConfig {
  runtime?: SandboxRuntime;
  cpuLimit?: string;       // e.g. "0.5", "1.0", "2.0", "4.0"
  memoryLimit?: string;    // e.g. "256m", "512m", "1024m", "2048m"
  pidsLimit?: number;      // e.g. 128, 256, 512, 1024
  networkPolicy?: SandboxNetworkPolicy;
  securityLevel?: SandboxSecurityLevel;
}

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
  sandboxEnabled?: boolean;
  sandboxConfig?: NodeSandboxConfig;
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
  llmConfig?: AgentLLMConfig;
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

export interface DecisionGateNodeData extends BaseNodeData {
  nodeType: 'decision_gate';
  gateType?: GateType | 'pipeline_gate' | 'tool_gate';
  gateStatus?: DecisionGateStatus;
  thresholdScore?: number;
  maxSecurityAlerts?: number;
  currentCoverage?: number;
  criticalFindingsCount?: number;
  expiresAt?: string;
  tokenPrefix?: string;
  toolName?: string;
  rejectionReason?: string;
  diagnostics?: ReviewerDiagnostic[];
}

export * from './worker';

export type FactoryNodeData =
  | TriggerNodeData
  | AgentNodeData
  | LogicNodeData
  | ActionNodeData
  | DecisionGateNodeData;

export type FactoryFlowNode = Node<FactoryNodeData>;
export type FactoryFlowEdge = Edge;
