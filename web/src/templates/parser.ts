// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { load } from 'js-yaml';
import { MarkerType } from '@xyflow/react';
import type { FactoryFlowNode, FactoryFlowEdge, FactoryNodeData, AgentRole, ReviewerSpecialty, TriggerSubtype } from '../types/flow';
import type { FactoryTemplate, StepConfig } from './types';
import { validateFactoryTemplate } from './validator';

export interface ParseResult {
  template: FactoryTemplate;
  nodes: FactoryFlowNode[];
  edges: FactoryFlowEdge[];
  isValid: boolean;
  errors: string[];
  warnings: string[];
}

export function parseTemplateInput(input: string | FactoryTemplate): FactoryTemplate {
  if (typeof input === 'object' && input !== null) {
    return input;
  }

  const trimmed = input.trim();
  if (trimmed.startsWith('{')) {
    return JSON.parse(trimmed) as FactoryTemplate;
  }
  return load(trimmed) as FactoryTemplate;
}

export function templateToCanvas(input: string | FactoryTemplate): ParseResult {
  const template = parseTemplateInput(input);
  const validation = validateFactoryTemplate(template);

  const steps = template.spec?.steps || [];
  const entrypoint = template.spec?.entrypoint || (steps[0]?.id ?? '');

  // Calculate topological depths
  const depths = new Map<string, number>();
  if (entrypoint) {
    depths.set(entrypoint, 0);
  }

  let changed = true;
  for (let iter = 0; iter <= steps.length && changed; iter++) {
    changed = false;
    for (const step of steps) {
      if (step.id === entrypoint) continue;
      let maxDep = -1;
      for (const dep of step.depends_on || []) {
        if (depths.has(dep)) {
          maxDep = Math.max(maxDep, depths.get(dep)!);
        }
      }
      if (maxDep >= 0) {
        const newDepth = maxDep + 1;
        if (depths.get(step.id) !== newDepth) {
          depths.set(step.id, newDepth);
          changed = true;
        }
      }
    }
  }

  // Bucket steps by depth
  const depthBuckets = new Map<number, StepConfig[]>();
  for (const step of steps) {
    const d = depths.get(step.id) ?? 0;
    if (!depthBuckets.has(d)) {
      depthBuckets.set(d, []);
    }
    depthBuckets.get(d)!.push(step);
  }

  const sortedDepths = Array.from(depthBuckets.keys()).sort((a, b) => a - b);
  const nodes: FactoryFlowNode[] = [];
  const edges: FactoryFlowEdge[] = [];

  for (const d of sortedDepths) {
    const bucket = depthBuckets.get(d)!;
    bucket.sort((a, b) => a.id.localeCompare(b.id));

    bucket.forEach((step, idx) => {
      const { nodeType, role, reviewerSpecialty, subtype, model } = inferNodeProperties(step, entrypoint);

      const xPos = d * 280 + 80;
      const yPos = idx * 140 + 80;

      const sandboxFromConfig = step.config?.sandbox as import('../types/flow').NodeSandboxConfig | undefined;

      const rawLlm = step.config?.llm as Record<string, unknown> | undefined;
      const parsedLlmConfig: import('../types/flow').AgentLLMConfig | undefined = rawLlm
        ? {
            provider: (rawLlm.provider as string) || 'anthropic',
            model: (rawLlm.model as string) || model || 'claude-3-5-sonnet',
            temperature: typeof rawLlm.temperature === 'number' ? rawLlm.temperature : undefined,
            maxIterations:
              typeof rawLlm.max_iterations === 'number'
                ? rawLlm.max_iterations
                : typeof rawLlm.maxIterations === 'number'
                  ? rawLlm.maxIterations
                  : undefined,
            authorizedTools: Array.isArray(rawLlm.authorized_tools)
              ? (rawLlm.authorized_tools as string[])
              : Array.isArray(rawLlm.authorizedTools)
                ? (rawLlm.authorizedTools as string[])
                : undefined,
          }
        : undefined;

      const effectiveModel = parsedLlmConfig?.model || model || 'claude-3-5-sonnet';

      const nodeData: FactoryNodeData = {
        label: step.name || step.id,
        description: `Plugin: ${step.plugin}`,
        status: 'pending',
        nodeType,
        subtype: subtype as TriggerSubtype,
        role: role as AgentRole,
        reviewerSpecialty: reviewerSpecialty as ReviewerSpecialty,
        model: effectiveModel,
        pluginTag: step.plugin,
        parallel: step.parallel,
        config: step.config ? { ...step.config } : {},
        ...(sandboxFromConfig ? { sandboxConfig: sandboxFromConfig } : {}),
        ...(parsedLlmConfig ? { llmConfig: parsedLlmConfig } : {}),
      } as FactoryNodeData;

      nodes.push({
        id: step.id,
        type: nodeType,
        position: { x: xPos, y: yPos },
        data: nodeData,
      });

      for (const dep of step.depends_on || []) {
        edges.push({
          id: `e-${dep}-${step.id}`,
          source: dep,
          target: step.id,
          animated: false,
          markerEnd: { type: MarkerType.ArrowClosed },
        });
      }
    });
  }

  return {
    template,
    nodes,
    edges,
    isValid: validation.isValid,
    errors: validation.errors,
    warnings: validation.warnings,
  };
}

function inferNodeProperties(step: StepConfig, entrypoint: string) {
  const p = `${step.plugin || ''} ${step.id}`.toLowerCase();

  if (step.id === entrypoint || p.includes('ingestion') || p.includes('trigger')) {
    return {
      nodeType: 'trigger' as const,
      subtype: 'webhook' as const,
      model: undefined,
      role: undefined,
      reviewerSpecialty: undefined,
    };
  }

  if (p.includes('reviewer')) {
    let specialty: ReviewerSpecialty = 'code';
    if (p.includes('security')) specialty = 'security';
    else if (p.includes('arch')) specialty = 'arch';
    else if (p.includes('uiux') || p.includes('ui-ux')) specialty = 'ui';

    return {
      nodeType: 'agent' as const,
      role: 'reviewer' as const,
      reviewerSpecialty: specialty,
      model: 'claude-3-5-sonnet',
      subtype: undefined,
    };
  }

  if (p.includes('approver') || p.includes('gate')) {
    return {
      nodeType: 'agent' as const,
      role: 'approver' as const,
      model: 'claude-3-5-sonnet',
      reviewerSpecialty: undefined,
      subtype: undefined,
    };
  }

  if (p.includes('builder') || p.includes('patch')) {
    return {
      nodeType: 'agent' as const,
      role: 'builder_tdd' as const,
      model: 'claude-3-5-sonnet',
      reviewerSpecialty: undefined,
      subtype: undefined,
    };
  }

  return {
    nodeType: 'agent' as const,
    role: 'spec_writer' as const,
    model: 'claude-3-5-sonnet',
    reviewerSpecialty: undefined,
    subtype: undefined,
  };
}
