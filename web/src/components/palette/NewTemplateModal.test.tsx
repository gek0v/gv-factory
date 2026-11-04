// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { NewTemplateModal } from './NewTemplateModal';

describe('NewTemplateModal Component (Obsidian Glass)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders null when isOpen is false', () => {
    const html = renderToStaticMarkup(
      <NewTemplateModal isOpen={false} onClose={vi.fn()} />
    );
    expect(html).toBe('');
  });

  it('renders Obsidian Glass modal when isOpen is true', () => {
    const html = renderToStaticMarkup(
      <NewTemplateModal isOpen={true} onClose={vi.fn()} />
    );

    // Backdrop & Modal structure
    expect(html).toContain('obsidian-glass-backdrop');
    expect(html).toContain('obsidian-glass-modal');
    expect(html).toContain('role="dialog"');
    expect(html).toContain('aria-modal="true"');

    // Header & Title
    expect(html).toContain('Create Agent Template');
    expect(html).toContain('Design and persist a reusable autonomous agent persona in Obsidian Glass');
    expect(html).toContain('aria-label="Close modal"');

    // Form inputs
    expect(html).toContain('Template Name');
    expect(html).toContain('Template ID');
    expect(html).toContain('Description');
    expect(html).toContain('Persona Role');
    expect(html).toContain('LLM Model');
    expect(html).toContain('Reasoning / Thinking Effort');
    expect(html).toContain('Skills');
    expect(html).toContain('Tools &amp; MCPs');
    expect(html).toContain('System Prompt Persona');

    // LLM Models & Thinking options
    expect(html).toContain('claude-3-7-sonnet');
    expect(html).toContain('DeepSeek R1');
    expect(html).toContain('thinking-btn-off');
    expect(html).toContain('thinking-btn-high');

    // Buttons
    expect(html).toContain('Cancel');
    expect(html).toContain('Create Template');
  });

  it('contains expected role options', () => {
    const html = renderToStaticMarkup(
      <NewTemplateModal isOpen={true} onClose={vi.fn()} />
    );

    expect(html).toContain('builder_tdd');
    expect(html).toContain('spec_writer');
    expect(html).toContain('security_reviewer');
    expect(html).toContain('architecture_reviewer');
    expect(html).toContain('approver_gate');
    expect(html).toContain('bug_hunter');
    expect(html).toContain('refactor_specialist');
  });
});
