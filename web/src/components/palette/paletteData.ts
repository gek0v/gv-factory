// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import type { FactoryNodeData, FactoryNodeType } from '../../types/flow';

export interface PaletteItem {
  type: FactoryNodeType;
  label: string;
  description: string;
  iconName: string;
  data: Partial<FactoryNodeData>;
}

export const PALETTE_GROUPS: { name: string; items: PaletteItem[] }[] = [
  {
    name: 'Triggers (Inbound)',
    items: [
      {
        type: 'trigger',
        label: 'Webhook Trigger',
        description: 'HTTP POST webhook payload listener',
        iconName: 'webhook',
        data: {
          nodeType: 'trigger',
          subtype: 'webhook',
          label: 'Inbound Webhook',
          description: 'Receives external JSON payload',
          eventSource: '/api/v1/webhooks/inbound',
          status: 'pending',
        },
      },
      {
        type: 'trigger',
        label: 'GitHub Issue',
        description: 'New issue or label assigned trigger',
        iconName: 'github',
        data: {
          nodeType: 'trigger',
          subtype: 'github_issue',
          label: 'GitHub Issue Event',
          description: 'Triggers on label ready-for-agent',
          eventSource: 'repo:gek0v/gv-factory',
          status: 'pending',
        },
      },
      {
        type: 'trigger',
        label: 'Chat Listener',
        description: 'Slack/Discord human dispatch event',
        iconName: 'chat',
        data: {
          nodeType: 'trigger',
          subtype: 'chat_listener',
          label: 'Chat Command Listener',
          description: 'Listens for /factory run command',
          eventSource: '#factory-ops',
          status: 'pending',
        },
      },
      {
        type: 'trigger',
        label: 'Cron Schedule',
        description: 'Time-based automated periodic execution',
        iconName: 'clock',
        data: {
          nodeType: 'trigger',
          subtype: 'cron',
          label: 'Periodic Sync Cron',
          description: 'Triggers on recurring timetable',
          cronSchedule: '0 */4 * * *',
          status: 'pending',
        },
      },
    ],
  },
  {
    name: 'Agents (Autonomous)',
    items: [
      {
        type: 'agent',
        label: 'Spec Writer',
        description: 'SDD & TDD requirements generator',
        iconName: 'brain',
        data: {
          nodeType: 'agent',
          role: 'spec_writer',
          label: 'Spec Writer (SDD & TDD)',
          description: 'Drafts technical specification and test requirements',
          model: 'claude-3-5-sonnet',
          pluginTag: 'extism://plugins/sdd-gen.wasm',
          status: 'pending',
        },
      },
      {
        type: 'agent',
        label: 'Builder TDD',
        description: 'Red-Green-Refactor code generator in sandbox',
        iconName: 'code',
        data: {
          nodeType: 'agent',
          role: 'builder_tdd',
          label: 'Builder TDD (Sandbox)',
          description: 'Isolated rootless container with git worktree',
          model: 'claude-3-5-sonnet',
          pluginTag: 'extism://plugins/tdd-runner.wasm',
          sandboxEnabled: true,
          status: 'pending',
        },
      },
      {
        type: 'agent',
        label: 'Reviewer (Security)',
        description: 'AST & SAST vulnerability auditing',
        iconName: 'shield',
        data: {
          nodeType: 'agent',
          role: 'reviewer',
          reviewerSpecialty: 'security',
          label: 'Security Reviewer',
          description: 'AST vulnerability analysis and secret scanning',
          model: 'gpt-4o',
          pluginTag: 'extism://plugins/sec-audit.wasm',
          status: 'pending',
        },
      },
      {
        type: 'agent',
        label: 'Approver Gate',
        description: 'Human-in-the-loop decision gate',
        iconName: 'check-circle',
        data: {
          nodeType: 'agent',
          role: 'approver',
          label: 'Approver & Gate',
          description: 'Cryptographic HMAC approval check before merge',
          model: 'human-gate',
          status: 'awaiting_approval',
        },
      },
    ],
  },
  {
    name: 'Logic & Branching',
    items: [
      {
        type: 'logic',
        label: 'If / Else Branch',
        description: 'Binary condition splitting',
        iconName: 'branch',
        data: {
          nodeType: 'logic',
          subtype: 'if_else',
          label: 'Conditional Branch',
          description: 'Routes based on boolean evaluation',
          condition: 'review.score >= 0.9 && security.alerts == 0',
          status: 'pending',
        },
      },
      {
        type: 'logic',
        label: 'Threshold Evaluator',
        description: 'Checks quality and coverage thresholds',
        iconName: 'sliders',
        data: {
          nodeType: 'logic',
          subtype: 'threshold',
          label: 'Code Quality Gate',
          description: 'Verifies test coverage >= 90%',
          thresholdScore: 90,
          status: 'pending',
        },
      },
      {
        type: 'logic',
        label: 'LLM Router',
        description: 'Multi-intent dynamic branching',
        iconName: 'bot',
        data: {
          nodeType: 'logic',
          subtype: 'llm_router',
          label: 'Dynamic Intent Router',
          description: 'Routes workflow based on task classification',
          routes: ['Bugfix Path', 'Feature Path', 'Refactor Path'],
          status: 'pending',
        },
      },
    ],
  },
  {
    name: 'Actions (Mutations)',
    items: [
      {
        type: 'action',
        label: 'Open Pull Request',
        description: 'Creates GitHub PR with summary and tests',
        iconName: 'pr',
        data: {
          nodeType: 'action',
          subtype: 'open_pr',
          label: 'Create Pull Request',
          description: 'Opens PR to main via Merge Queue',
          target: 'origin/main',
          branchName: 'ov-run/feature-auto',
          status: 'pending',
        },
      },
      {
        type: 'action',
        label: 'Git Commit',
        description: 'Applies conventional commit to worktree',
        iconName: 'commit',
        data: {
          nodeType: 'action',
          subtype: 'git_commit',
          label: 'Git Commit Worktree',
          description: 'Commits verified changes with SPDX headers',
          status: 'pending',
        },
      },
      {
        type: 'action',
        label: 'Slack Notification',
        description: 'Broadcasts pipeline completion alert',
        iconName: 'send',
        data: {
          nodeType: 'action',
          subtype: 'slack_notification',
          label: 'Notify Slack Ops',
          description: 'Sends execution telemetry to #deployments',
          target: '#eng-releases',
          status: 'pending',
        },
      },
    ],
  },
];

