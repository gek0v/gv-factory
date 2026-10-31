// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import type { Node, Edge } from '@xyflow/react';

export type ExecutionStatus =
  | 'pending'
  | 'running'
  | 'completed'
  | 'failed'
  | 'awaiting_approval';

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

export interface BaseNodeData {
  label: string;
  description?: string;
  status: ExecutionStatus;
  nodeType: FactoryNodeType;
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
