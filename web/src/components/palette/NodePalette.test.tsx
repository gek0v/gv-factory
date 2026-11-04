// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { NodePalette } from './NodePalette';

describe('NodePalette Component', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders Agent Templates section with "+ New" button', () => {
    const html = renderToStaticMarkup(<NodePalette />);

    // Section title
    expect(html).toContain('Agent Templates');

    // "+ New" button for custom agent template designer
    expect(html).toContain('palette-btn-new-template');
    expect(html).toContain('+ New');
    expect(html).toContain('data-testid="btn-new-template"');
    expect(html).toContain('title="Design new custom agent template"');
  });

  it('renders default agent templates in palette', () => {
    const html = renderToStaticMarkup(<NodePalette />);

    // Check presence of some default templates
    expect(html).toContain('Spec Writer (SDD Architect)');
    expect(html).toContain('Builder TDD (Autonomous Coder)');
    expect(html).toContain('Security &amp; SAST Reviewer');
    expect(html).toContain('data-testid="palette-template-tpl-spec-writer"');
  });

  it('renders standard component groups', () => {
    const html = renderToStaticMarkup(<NodePalette />);

    expect(html).toContain('Triggers (Inbound)');
    expect(html).toContain('Agents (Autonomous)');
    expect(html).toContain('Logic &amp; Governance');
    expect(html).toContain('Actions (Mutations)');
  });
});
