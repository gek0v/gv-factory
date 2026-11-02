// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect } from 'vitest';
import { initialNodes, initialEdges } from './initialFlow';
import { PALETTE_GROUPS } from './components/palette/paletteData';
import { validateCanvasGraph, validatePipeline } from './api/runs';
import { canvasToTemplate } from './templates/exporter';
import { validateFactoryTemplate } from './templates/validator';

describe('initialFlow and palette extism URI conformity', () => {
  it('validates initialNodes and initialEdges through validateCanvasGraph and validatePipeline without errors', () => {
    const resultGraph = validateCanvasGraph(initialNodes, initialEdges);
    expect(resultGraph.isValid).toBe(true);
    expect(resultGraph.errors).toEqual([]);

    const resultPipeline = validatePipeline(initialNodes, initialEdges);
    expect(resultPipeline.isValid).toBe(true);
    expect(resultPipeline.errors).toEqual([]);
  });

  it('validates all agent plugin tags in initialNodes conform to extism:// standard', () => {
    const agentNodes = initialNodes.filter((n) => n.data.nodeType === 'agent' && n.data.pluginTag);
    expect(agentNodes.length).toBeGreaterThan(0);

    for (const node of agentNodes) {
      expect(node.data.pluginTag).toMatch(/^extism:\/\/plugins\/[a-z0-9_-]+\.wasm$/);
    }
  });

  it('validates exported template from initialFlow passes validateFactoryTemplate', () => {
    const template = canvasToTemplate(initialNodes, initialEdges);
    const result = validateFactoryTemplate(template);
    expect(result.isValid).toBe(true);
    expect(result.errors).toEqual([]);
  });

  it('enforces all palette agent items with pluginTag conform to extism:// URI standard', () => {
    const allItems = PALETTE_GROUPS.flatMap((g) => g.items);
    const itemsWithPlugin = allItems.filter((i) => i.data?.pluginTag);
    expect(itemsWithPlugin.length).toBeGreaterThan(0);

    for (const item of itemsWithPlugin) {
      expect(item.data.pluginTag).toMatch(/^extism:\/\/plugins\/[a-z0-9_-]+\.wasm$/);
    }
  });
});
