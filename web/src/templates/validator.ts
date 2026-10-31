// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import type { FactoryTemplate, ValidationResult } from './types';

export function validateFactoryTemplate(template: FactoryTemplate): ValidationResult {
  const errors: string[] = [];
  const warnings: string[] = [];

  if (!template) {
    return {
      isValid: false,
      errors: ['Template definition is null or empty'],
      warnings: [],
    };
  }

  if (!template.metadata?.name) {
    errors.push('Template metadata.name is required');
  }

  const steps = template.spec?.steps || [];
  if (steps.length === 0) {
    errors.push('Template must contain at least one step in spec.steps');
    return { isValid: false, errors, warnings };
  }

  const stepIds = new Set<string>();
  const stepMap = new Map<string, (typeof steps)[0]>();

  for (const step of steps) {
    if (!step.id) {
      errors.push('Step ID cannot be empty');
      continue;
    }
    if (stepIds.has(step.id)) {
      errors.push(`Duplicate step ID "${step.id}"`);
    }
    stepIds.add(step.id);
    stepMap.set(step.id, step);

    // Validate Plugin URI conformity to PDK (extism://)
    if (!step.plugin) {
      errors.push(`Step "${step.id}" is missing required plugin URI`);
    } else if (!step.plugin.startsWith('extism://')) {
      errors.push(
        `Step "${step.id}" plugin URI "${step.plugin}" does not conform to PDK standard (must begin with "extism://")`
      );
    }
  }

  const entrypoint = template.spec?.entrypoint;
  if (!entrypoint) {
    errors.push('spec.entrypoint cannot be empty');
  } else if (!stepIds.has(entrypoint)) {
    errors.push(`Entrypoint "${entrypoint}" does not exist in template steps`);
  }

  // Build adjacency list
  const adj = new Map<string, string[]>();
  for (const id of stepIds) {
    adj.set(id, []);
  }

  for (const step of steps) {
    for (const dep of step.depends_on || []) {
      if (!stepIds.has(dep)) {
        errors.push(`Step "${step.id}" depends on non-existent step "${dep}"`);
      } else {
        adj.get(dep)?.push(step.id);
      }
    }
  }

  // Entrypoint connectivity
  if (steps.length > 1 && entrypoint && stepIds.has(entrypoint)) {
    const outgoing = adj.get(entrypoint) || [];
    if (outgoing.length === 0) {
      errors.push(`Entrypoint "${entrypoint}" does not connect to any subsequent steps`);
    }
  }

  // Cycle detection (DFS with 3 colors)
  const visited = new Map<string, number>(); // 0: unvisited, 1: visiting, 2: visited
  let hasCycle = false;

  function dfs(nodeId: string, path: string[]) {
    visited.set(nodeId, 1);
    const neighbors = adj.get(nodeId) || [];

    for (const next of neighbors) {
      if (visited.get(next) === 1) {
        hasCycle = true;
        errors.push(`Cycle detected in graph: ${[...path, nodeId, next].join(' -> ')}`);
        return;
      }
      if (!visited.get(next)) {
        dfs(next, [...path, nodeId]);
      }
    }
    visited.set(nodeId, 2);
  }

  for (const id of stepIds) {
    if (!visited.get(id)) {
      dfs(id, []);
    }
  }

  // Reachability check
  if (entrypoint && stepIds.has(entrypoint) && !hasCycle) {
    const reachable = new Set<string>();
    const queue = [entrypoint];
    reachable.add(entrypoint);

    while (queue.length > 0) {
      const curr = queue.shift()!;
      for (const next of adj.get(curr) || []) {
        if (!reachable.has(next)) {
          reachable.add(next);
          queue.push(next);
        }
      }
    }

    for (const id of stepIds) {
      if (!reachable.has(id)) {
        errors.push(`Isolated step node "${id}" is unreachable from entrypoint "${entrypoint}"`);
      }
    }
  }

  // Gates validation
  const gates = template.spec?.gates;
  if (gates) {
    if (gates.min_coverage_percent < 0 || gates.min_coverage_percent > 100) {
      errors.push(
        `Gates min_coverage_percent must be between 0.0 and 100.0 (got ${gates.min_coverage_percent})`
      );
    }
    if (gates.max_security_alerts < 0) {
      errors.push(`Gates max_security_alerts cannot be negative (got ${gates.max_security_alerts})`);
    }
    if (!gates.require_approval && gates.max_security_alerts > 0) {
      warnings.push(
        'Automated fast-track to main is active without mandatory approval while allowing security alerts'
      );
    }
  } else {
    warnings.push('Template spec does not define gates criteria');
  }

  return {
    isValid: errors.length === 0,
    errors,
    warnings,
  };
}
