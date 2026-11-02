// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import type { Node, Edge } from '@xyflow/react';
import type { FactoryNodeData, FactoryFlowNode, FactoryFlowEdge } from '../types/flow';
import type { FactoryTemplate } from '../templates/types';
import {
  canvasToTemplate,
  exportTemplateToJson,
  exportTemplateToYaml,
  type ExportOptions,
} from '../templates/exporter';
import { validateFactoryTemplate } from '../templates/validator';
import { defaultGatewayClient } from './gateway';

/**
 * Serialized representation of a canvas graph matching the backend CanvasGraph struct.
 */
export interface CanvasNodeDTO {
  id: string;
  type: string;
  position: {
    x: number;
    y: number;
  };
  data: {
    label: string;
    description?: string;
    status: string;
    nodeType: string;
    role?: string;
    reviewerSpecialty?: string;
    subtype?: string;
    model?: string;
    pluginTag?: string;
    parallel?: boolean;
    config?: Record<string, unknown>;
    stepId?: string;
    [key: string]: unknown;
  };
}

export interface CanvasEdgeDTO {
  id: string;
  source: string;
  target: string;
  markerEnd?: unknown;
}

export interface CanvasGraphDTO {
  nodes: CanvasNodeDTO[];
  edges: CanvasEdgeDTO[];
  metadata: {
    name: string;
    version: string;
    description: string;
    labels?: Record<string, string>;
  };
  gates: {
    min_coverage_percent: number;
    max_security_alerts: number;
    require_approval: boolean;
  };
}

export interface RunSubmitRequest {
  run_id?: string;
  template?: FactoryTemplate;
  canvas_graph?: CanvasGraphDTO;
  repo_path?: string;
  branch?: string;
  base_branch?: string;
  priority?: string;
  max_memory_mb?: number;
}

export interface RunSubmitResponse {
  success?: boolean;
  run_id: string;
  state: 'QUEUED' | 'RUNNING' | 'COMPLETED' | 'FAILED' | 'CANCELLED';
  template_name?: string;
  branch?: string;
  queued_at?: string;
  message?: string;
}

export interface RunCancelResponse {
  success?: boolean;
  run_id: string;
  cancelled?: boolean;
  state?: string;
  message?: string;
}

export interface CanvasValidationResult {
  isValid: boolean;
  errors: string[];
  warnings: string[];
  template?: FactoryTemplate;
}

/**
 * Serializes nodes and edges to standard FactoryTemplate.
 */
export function serializeCanvasToTemplate(
  nodes: Node<FactoryNodeData>[] | FactoryFlowNode[],
  edges: Edge[] | FactoryFlowEdge[],
  options?: ExportOptions
): FactoryTemplate {
  return canvasToTemplate(nodes, edges, options);
}

/**
 * Validates the current canvas graph structure and schema.
 */
export const validateCanvasGraph = (
  nodes: Node<FactoryNodeData>[] | FactoryFlowNode[],
  edges: Edge[] | FactoryFlowEdge[]
): CanvasValidationResult => {
  const errors: string[] = [];
  const warnings: string[] = [];

  if (!nodes || nodes.length === 0) {
    errors.push('Canvas is empty: at least one node is required to build a pipeline.');
    return { isValid: false, errors, warnings };
  }

  // Validate plugin tags
  for (const node of nodes) {
    const pluginTag = (node.data as { pluginTag?: string })?.pluginTag;
    if (pluginTag && !pluginTag.startsWith('extism://')) {
      errors.push(`Node "${node.id}" has invalid plugin tag "${pluginTag}": must begin with "extism://"`);
    }
  }

  // Check for graph cycles via DFS
  const adj = new Map<string, string[]>();
  for (const edge of edges) {
    if (!adj.has(edge.source)) {
      adj.set(edge.source, []);
    }
    adj.get(edge.source)!.push(edge.target);
  }

  const visited = new Set<string>();
  const recStack = new Set<string>();
  let hasCycle = false;

  function dfs(nodeId: string): boolean {
    visited.add(nodeId);
    recStack.add(nodeId);

    const neighbors = adj.get(nodeId) || [];
    for (const neighbor of neighbors) {
      if (!visited.has(neighbor)) {
        if (dfs(neighbor)) return true;
      } else if (recStack.has(neighbor)) {
        return true;
      }
    }

    recStack.delete(nodeId);
    return false;
  }

  for (const node of nodes) {
    if (!visited.has(node.id)) {
      if (dfs(node.id)) {
        hasCycle = true;
        break;
      }
    }
  }

  if (hasCycle) {
    errors.push('Cyclic dependency detected: pipeline DAG must be strictly acyclic.');
  }

  // Build and validate template
  const template = canvasToTemplate(nodes, edges);
  const tplValidation = validateFactoryTemplate(template);

  errors.push(...tplValidation.errors);
  warnings.push(...tplValidation.warnings);

  return {
    isValid: errors.length === 0,
    errors,
    warnings,
    template,
  };
};

/**
 * Validates a pipeline defined by canvas nodes and edges.
 * Alias for validateCanvasGraph to conform to standard validation API naming.
 */
export const validatePipeline = validateCanvasGraph;

/**
 * Serializes React Flow nodes and edges into a CanvasGraphDTO compatible with ov-server.
 */
export function serializeCanvasGraph(
  nodes: Node<FactoryNodeData>[] | FactoryFlowNode[],
  edges: Edge[] | FactoryFlowEdge[],
  metadataOptions?: Partial<CanvasGraphDTO['metadata']>,
  gatesOptions?: Partial<CanvasGraphDTO['gates']>
): CanvasGraphDTO {
  const serializedNodes: CanvasNodeDTO[] = nodes.map((n) => {
    const data = n.data || ({} as FactoryNodeData);
    return {
      id: n.id,
      type: n.type || data.nodeType || 'agent',
      position: {
        x: n.position.x,
        y: n.position.y,
      },
      data: {
        label: data.label || n.id,
        description: data.description,
        status: data.status || 'pending',
        nodeType: data.nodeType || 'agent',
        role: 'role' in data ? (data as { role?: string }).role : undefined,
        reviewerSpecialty:
          'reviewerSpecialty' in data
            ? (data as { reviewerSpecialty?: string }).reviewerSpecialty
            : undefined,
        subtype: 'subtype' in data ? (data as { subtype?: string }).subtype : undefined,
        model: 'model' in data ? (data as { model?: string }).model : undefined,
        pluginTag: 'pluginTag' in data ? (data as { pluginTag?: string }).pluginTag : undefined,
        parallel: (data as { parallel?: boolean }).parallel,
        config: (data as { config?: Record<string, unknown> }).config,
        stepId: n.id,
      },
    };
  });

  const serializedEdges: CanvasEdgeDTO[] = edges.map((e) => ({
    id: e.id,
    source: e.source,
    target: e.target,
    markerEnd: e.markerEnd,
  }));

  return {
    nodes: serializedNodes,
    edges: serializedEdges,
    metadata: {
      name: metadataOptions?.name || 'canvas-exported-factory',
      version: metadataOptions?.version || '0.1.0',
      description: metadataOptions?.description || 'Exported from interactive DAG canvas',
      ...(metadataOptions?.labels ? { labels: metadataOptions.labels } : {}),
    },
    gates: {
      min_coverage_percent: gatesOptions?.min_coverage_percent ?? 90.0,
      max_security_alerts: gatesOptions?.max_security_alerts ?? 0,
      require_approval: gatesOptions?.require_approval ?? true,
    },
  };
}

/**
 * Submits a pipeline run to ov-server at POST /api/v1/runs.
 * If live execution is started, automatically connects SSE if not yet connected.
 */
export async function submitPipelineRun(
  nodes: Node<FactoryNodeData>[] | FactoryFlowNode[],
  edges: Edge[] | FactoryFlowEdge[],
  options?: {
    baseUrl?: string;
    runId?: string;
    repoPath?: string;
    branch?: string;
    baseBranch?: string;
    priority?: string;
    autoConnectSSE?: boolean;
  },
  customFetch?: typeof fetch
): Promise<RunSubmitResponse> {
  const validation = validateCanvasGraph(nodes, edges);
  if (!validation.isValid) {
    throw new Error(`Canvas validation failed: ${validation.errors.join('; ')}`);
  }

  const fetchFn = customFetch || globalThis.fetch;
  const baseUrl = options?.baseUrl ?? '';
  const url = `${baseUrl}/api/v1/runs`;

  const canvasGraph = serializeCanvasGraph(nodes, edges);
  const template = validation.template || canvasToTemplate(nodes, edges);

  const payload: RunSubmitRequest = {
    run_id: options?.runId,
    template,
    canvas_graph: canvasGraph,
    repo_path: options?.repoPath,
    branch: options?.branch,
    base_branch: options?.baseBranch,
    priority: options?.priority,
  };

  const response = await fetchFn(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
    },
    body: JSON.stringify(payload),
  });

  if (!response.ok) {
    let errorDetail = `HTTP ${response.status} ${response.statusText}`;
    try {
      const errJson = await response.json();
      if (errJson?.error) {
        errorDetail = errJson.error;
      } else if (errJson?.message) {
        errorDetail = errJson.message;
      }
    } catch {
      // ignore parsing error
    }
    throw new Error(errorDetail);
  }

  const result: RunSubmitResponse = await response.json();

  if (options?.autoConnectSSE !== false) {
    defaultGatewayClient.connect();
  }

  return result;
}

/**
 * Cancels an active or queued pipeline run at POST /api/v1/runs/{id}/cancel.
 */
export async function cancelPipelineRun(
  runId: string,
  optionsOrFetch?: { baseUrl?: string } | typeof fetch,
  customFetch?: typeof fetch
): Promise<RunCancelResponse> {
  if (!runId || !runId.trim()) {
    throw new Error('runId is required to cancel a pipeline run');
  }

  let baseUrl = '';
  let fetchFn = globalThis.fetch;

  if (typeof optionsOrFetch === 'function') {
    fetchFn = optionsOrFetch;
  } else if (optionsOrFetch && typeof optionsOrFetch === 'object') {
    baseUrl = optionsOrFetch.baseUrl ?? '';
    if (customFetch) {
      fetchFn = customFetch;
    }
  }

  const url = `${baseUrl}/api/v1/runs/${encodeURIComponent(runId.trim())}/cancel`;

  const response = await fetchFn(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
    },
  });

  if (!response.ok) {
    let errorDetail = `HTTP ${response.status} ${response.statusText}`;
    try {
      const errJson = await response.json();
      if (errJson?.error) {
        errorDetail = errJson.error;
      } else if (errJson?.message) {
        errorDetail = errJson.message;
      }
    } catch {
      // ignore
    }
    throw new Error(errorDetail);
  }

  const result: RunCancelResponse = await response.json();
  return result;
}

/**
 * Exports the current canvas nodes and edges as a FactoryTemplate formatted in JSON or YAML.
 */
export function exportPipelineTemplate(
  nodes: Node<FactoryNodeData>[] | FactoryFlowNode[],
  edges: Edge[] | FactoryFlowEdge[],
  format: 'json' | 'yaml' = 'json'
): string {
  const template = canvasToTemplate(nodes, edges);
  if (format === 'yaml') {
    return exportTemplateToYaml(template);
  }
  return exportTemplateToJson(template);
}
