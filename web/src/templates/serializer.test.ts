// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect } from 'vitest';
import { templateToCanvas } from './parser';
import { canvasToTemplate, exportTemplateToYaml, exportTemplateToJson } from './exporter';
import { validateFactoryTemplate } from './validator';
import { bugfixExpressTemplate, featureCompleteTemplate, refactorDocTemplate } from './catalog';
import type { FactoryTemplate } from './types';

describe('Factory Templates & Canvas Serializer', () => {
  it('parses canonical templates into deterministic canvas graphs', () => {
    const templates = [bugfixExpressTemplate, featureCompleteTemplate, refactorDocTemplate];

    for (const tpl of templates) {
      const result = templateToCanvas(tpl);
      expect(result.isValid).toBe(true);
      expect(result.errors.length).toBe(0);
      expect(result.nodes.length).toBe(tpl.spec.steps.length);

      // Verify entrypoint is positioned at x = 80
      const entryNode = result.nodes.find((n) => n.id === tpl.spec.entrypoint);
      expect(entryNode).toBeDefined();
      expect(entryNode?.position.x).toBe(80);
    }
  });

  it('performs lossless roundtrip serialization: template -> canvas -> template', () => {
    const original = featureCompleteTemplate;

    const { nodes, edges } = templateToCanvas(original);
    const reconstructed = canvasToTemplate(nodes, edges, {
      metadata: original.metadata,
      gates: original.spec.gates,
    });

    expect(reconstructed.metadata.name).toBe(original.metadata.name);
    expect(reconstructed.spec.entrypoint).toBe(original.spec.entrypoint);
    expect(reconstructed.spec.gates.min_coverage_percent).toBe(original.spec.gates.min_coverage_percent);
    expect(reconstructed.spec.steps.length).toBe(original.spec.steps.length);

    // Verify step configurations and parallel flags
    const secReviewer = reconstructed.spec.steps.find((s) => s.id === 'security-reviewer');
    expect(secReviewer?.parallel).toBe(true);
    expect(secReviewer?.depends_on).toEqual(['builder-tdd']);
    expect(secReviewer?.config).toEqual({ clean_context: true });

    // Validate reconstructed template
    const validation = validateFactoryTemplate(reconstructed);
    expect(validation.isValid).toBe(true);
  });

  it('exports template to valid YAML and JSON formats', () => {
    const yamlOutput = exportTemplateToYaml(bugfixExpressTemplate);
    expect(yamlOutput).toContain('bugfix-express');
    expect(yamlOutput).toContain('min_coverage_percent: 85');

    const jsonOutput = exportTemplateToJson(bugfixExpressTemplate);
    const parsed = JSON.parse(jsonOutput);
    expect(parsed.metadata.name).toBe('bugfix-express');
    expect(parsed.spec.gates.require_approval).toBe(false);
  });

  it('detects cycle in template dependencies', () => {
    const cyclicTpl: FactoryTemplate = {
      apiVersion: 'factory.ov.io/v1alpha1',
      kind: 'FactoryTemplate',
      metadata: { name: 'cyclic', version: '0.1.0', description: 'test' },
      spec: {
        entrypoint: 'a',
        steps: [
          { id: 'a', name: 'A', plugin: 'extism://plugins/a.wasm', depends_on: ['b'] },
          { id: 'b', name: 'B', plugin: 'extism://plugins/b.wasm', depends_on: ['a'] },
        ],
        gates: { min_coverage_percent: 90, max_security_alerts: 0, require_approval: true },
      },
    };

    const validation = validateFactoryTemplate(cyclicTpl);
    expect(validation.isValid).toBe(false);
    expect(validation.errors.some((e) => e.includes('Cycle detected'))).toBe(true);
  });

  it('detects missing entrypoint and disconnected entrypoint', () => {
    const missingEntry: FactoryTemplate = {
      apiVersion: 'factory.ov.io/v1alpha1',
      kind: 'FactoryTemplate',
      metadata: { name: 'bad-entry', version: '0.1.0', description: 'test' },
      spec: {
        entrypoint: 'does-not-exist',
        steps: [{ id: 'a', name: 'A', plugin: 'extism://plugins/a.wasm' }],
        gates: { min_coverage_percent: 90, max_security_alerts: 0, require_approval: true },
      },
    };

    const validation = validateFactoryTemplate(missingEntry);
    expect(validation.isValid).toBe(false);
    expect(validation.errors.some((e) => e.includes('does not exist'))).toBe(true);

    const disconnected: FactoryTemplate = {
      apiVersion: 'factory.ov.io/v1alpha1',
      kind: 'FactoryTemplate',
      metadata: { name: 'disconnected', version: '0.1.0', description: 'test' },
      spec: {
        entrypoint: 'a',
        steps: [
          { id: 'a', name: 'A', plugin: 'extism://plugins/a.wasm' },
          { id: 'b', name: 'B', plugin: 'extism://plugins/b.wasm' },
        ],
        gates: { min_coverage_percent: 90, max_security_alerts: 0, require_approval: true },
      },
    };

    const validationDisc = validateFactoryTemplate(disconnected);
    expect(validationDisc.isValid).toBe(false);
    expect(validationDisc.errors.some((e) => e.includes('does not connect'))).toBe(true);
  });

  it('detects invalid plugin URI and gate thresholds', () => {
    const invalidProps: FactoryTemplate = {
      apiVersion: 'factory.ov.io/v1alpha1',
      kind: 'FactoryTemplate',
      metadata: { name: 'invalid-props', version: '0.1.0', description: 'test' },
      spec: {
        entrypoint: 'a',
        steps: [{ id: 'a', name: 'A', plugin: 'http://example.com/plugin.wasm' }],
        gates: { min_coverage_percent: 150, max_security_alerts: -2, require_approval: true },
      },
    };

    const validation = validateFactoryTemplate(invalidProps);
    expect(validation.isValid).toBe(false);
    expect(validation.errors.some((e) => e.includes('extism://'))).toBe(true);
    expect(validation.errors.some((e) => e.includes('min_coverage_percent'))).toBe(true);
    expect(validation.errors.some((e) => e.includes('max_security_alerts'))).toBe(true);
  });
});
