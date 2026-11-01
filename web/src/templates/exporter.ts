// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { dump } from 'js-yaml';
import type { FactoryFlowNode, FactoryFlowEdge } from '../types/flow';
import type { FactoryTemplate, StepConfig, TemplateMetadata, GatesConfig } from './types';

export interface ExportOptions {
  metadata?: Partial<TemplateMetadata>;
  gates?: Partial<GatesConfig>;
  entrypoint?: string;
}

export function canvasToTemplate(
  nodes: FactoryFlowNode[],
  edges: FactoryFlowEdge[],
  options?: ExportOptions
): FactoryTemplate {
  // Map incoming dependencies for each target node
  const incoming = new Map<string, string[]>();
  for (const edge of edges) {
    if (!incoming.has(edge.target)) {
      incoming.set(edge.target, []);
    }
    incoming.get(edge.target)!.push(edge.source);
  }

  // Sort nodes deterministically by X position then Y position
  const sortedNodes = [...nodes].sort((a, b) => {
    if (a.position.x !== b.position.x) {
      return a.position.x - b.position.x;
    }
    return a.position.y - b.position.y;
  });

  let entrypoint = options?.entrypoint;
  const steps: StepConfig[] = [];

  for (const node of sortedNodes) {
    const deps = incoming.get(node.id) || [];
    deps.sort((a, b) => a.localeCompare(b));

    if (!entrypoint && deps.length === 0) {
      entrypoint = node.id;
    }

    const data = node.data;
    const pluginTag = (data.pluginTag as string) || `extism://plugins/${node.id}.wasm`;

    const stepConfig: Record<string, unknown> = {
      ...(data.config && typeof data.config === 'object' ? (data.config as Record<string, unknown>) : {}),
      ...(data.sandboxConfig ? { sandbox: data.sandboxConfig } : {}),
    };

    const step: StepConfig = {
      id: node.id,
      name: data.label || node.id,
      plugin: pluginTag,
      ...(deps.length > 0 ? { depends_on: deps } : {}),
      ...(data.parallel ? { parallel: true } : {}),
      ...(Object.keys(stepConfig).length > 0 ? { config: stepConfig } : {}),
    };

    steps.push(step);
  }

  if (!entrypoint && steps.length > 0) {
    entrypoint = steps[0].id;
  }

  const defaultMeta: TemplateMetadata = {
    name: options?.metadata?.name || 'canvas-exported-factory',
    version: options?.metadata?.version || '0.1.0',
    description: options?.metadata?.description || 'Exported from interactive DAG canvas',
    ...(options?.metadata?.labels ? { labels: options.metadata.labels } : {}),
  };

  const defaultGates: GatesConfig = {
    min_coverage_percent: options?.gates?.min_coverage_percent ?? 90.0,
    max_security_alerts: options?.gates?.max_security_alerts ?? 0,
    require_approval: options?.gates?.require_approval ?? true,
  };

  return {
    apiVersion: 'factory.ov.io/v1alpha1',
    kind: 'FactoryTemplate',
    metadata: defaultMeta,
    spec: {
      entrypoint: entrypoint || 'ingestion',
      steps,
      gates: defaultGates,
    },
  };
}

export function exportTemplateToYaml(template: FactoryTemplate): string {
  return dump(template, {
    indent: 2,
    lineWidth: 120,
    noRefs: true,
  });
}

export function exportTemplateToJson(template: FactoryTemplate): string {
  return JSON.stringify(template, null, 2);
}

