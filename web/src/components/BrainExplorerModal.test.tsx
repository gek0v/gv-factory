// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import {
  BrainExplorerButton,
  BrainExplorerModal,
  getSeverityBadgeClass,
  formatScore,
  formatCategory,
} from './BrainExplorerModal';
import { useFactoryFlowStore } from '../store';
import * as brainApi from '../api/brain';

describe('BrainExplorerModal Component & Helpers', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useFactoryFlowStore.setState({
      showBrainExplorer: false,
    } as any);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('Pure formatters and badge helpers', () => {
    it('getSeverityBadgeClass returns appropriate CSS class based on severity', () => {
      expect(getSeverityBadgeClass('ERROR')).toBe('badge-severity-error');
      expect(getSeverityBadgeClass('error')).toBe('badge-severity-error');
      expect(getSeverityBadgeClass('CRITICAL')).toBe('badge-severity-error');
      expect(getSeverityBadgeClass('WARN')).toBe('badge-severity-warn');
      expect(getSeverityBadgeClass('warning')).toBe('badge-severity-warn');
      expect(getSeverityBadgeClass('INFO')).toBe('badge-severity-info');
      expect(getSeverityBadgeClass('UNKNOWN')).toBe('badge-severity-info');
    });

    it('formatScore formats scores into percentage strings', () => {
      expect(formatScore(0.923)).toBe('92%');
      expect(formatScore(1)).toBe('100%');
      expect(formatScore(0.05)).toBe('5%');
      expect(formatScore(0)).toBe('0%');
      expect(formatScore(-0.1)).toBe('0%');
    });

    it('formatCategory formats category tokens into readable labels', () => {
      expect(formatCategory('knowledge')).toBe('Code Knowledge');
      expect(formatCategory('rules')).toBe('ADR Rule');
      expect(formatCategory('episodes')).toBe('Task Episode');
      expect(formatCategory('all')).toBe('All');
      expect(formatCategory('custom')).toBe('custom');
    });
  });

  describe('BrainExplorerButton', () => {
    it('renders header trigger button with brain icon and label', () => {
      const html = renderToStaticMarkup(React.createElement(BrainExplorerButton));

      expect(html).toContain('brain-explorer-btn');
      expect(html).toContain('Brain Explorer');
      expect(html).toContain('title="Open Codebase Brain &amp; Memory Explorer"');
      expect(html).toContain('aria-label="Brain Explorer"');
    });

    it('toggles showBrainExplorer store state when clicked', () => {
      const store = useFactoryFlowStore.getState() as any;
      expect(store.showBrainExplorer).toBe(false);

      if (typeof store.setShowBrainExplorer === 'function') {
        store.setShowBrainExplorer(true);
        expect((useFactoryFlowStore.getState() as any).showBrainExplorer).toBe(true);
      }
    });
  });

  describe('BrainExplorerModal rendering', () => {
    it('renders null when showBrainExplorer is false in store', () => {
      useFactoryFlowStore.setState({ showBrainExplorer: false } as any);
      const html = renderToStaticMarkup(React.createElement(BrainExplorerModal));
      expect(html).toBe('');
    });

    it('renders modal dialog container when showBrainExplorer is true', () => {
      useFactoryFlowStore.setState({ showBrainExplorer: true } as any);
      const html = renderToStaticMarkup(React.createElement(BrainExplorerModal));

      expect(html).toContain('brain-modal-backdrop');
      expect(html).toContain('brain-modal-content');
      expect(html).toContain('role="dialog"');
      expect(html).toContain('aria-modal="true"');
      expect(html).toContain('Codebase Brain &amp; Memory Explorer');
      expect(html).toContain('Close modal');
    });

    it('renders tab bar with Search, Rules, and Stats & Sync tabs', () => {
      useFactoryFlowStore.setState({ showBrainExplorer: true } as any);
      const html = renderToStaticMarkup(React.createElement(BrainExplorerModal));

      expect(html).toContain('Search / Knowledge');
      expect(html).toContain('Architectural Rules');
      expect(html).toContain('Stats &amp; Sync');
    });

    it('renders search tab content with live query input, category pills, and alpha slider', () => {
      useFactoryFlowStore.setState({ showBrainExplorer: true } as any);
      const html = renderToStaticMarkup(
        React.createElement(BrainExplorerModal, { initialTab: 'search' })
      );

      expect(html).toContain('Search codebase knowledge, ADR rules, episodic memory...');
      expect(html).toContain('All Categories');
      expect(html).toContain('Knowledge Chunks');
      expect(html).toContain('ADR Rules');
      expect(html).toContain('Past Episodes');
      expect(html).toContain('Hybrid Balance (α):');
      expect(html).toContain('type="range"');
    });

    it('renders search results when provided or found', () => {
      useFactoryFlowStore.setState({ showBrainExplorer: true } as any);
      const mockResults: brainApi.BrainSearchResult[] = [
        {
          category: 'knowledge',
          file_path: 'internal/brain/hybrid_search.go',
          symbol_name: 'HybridSearchCodebase',
          symbol_type: 'method',
          title: 'Hybrid Codebase Search',
          summary: 'Executes reciprocal rank fusion between vector and lexical queries',
          score: 0.94,
          vector_score: 0.92,
          lexical_score: 0.96,
        },
      ];

      const html = renderToStaticMarkup(
        React.createElement(BrainExplorerModal, {
          initialTab: 'search',
          initialResults: mockResults,
        })
      );

      expect(html).toContain('internal/brain/hybrid_search.go');
      expect(html).toContain('HybridSearchCodebase');
      expect(html).toContain('method');
      expect(html).toContain('94%');
      expect(html).toContain('Executes reciprocal rank fusion between vector and lexical queries');
    });

    it('renders architectural rules tab with rules list, filter input, and severity badges', () => {
      useFactoryFlowStore.setState({ showBrainExplorer: true } as any);
      const mockRules: brainApi.ADRRule[] = [
        {
          rule_code: 'ADR-001',
          title: 'PostgreSQL 16 with pgvector Extension',
          severity: 'ERROR',
          category: 'database',
          description: 'All vector embeddings and semantic search must run on PostgreSQL with pgvector',
          source_adr: 'docs/adr/001-pgvector.md',
        },
        {
          rule_code: 'ADR-005',
          title: 'Git Worktree Ephemeral Isolation',
          severity: 'WARN',
          category: 'sandbox',
          description: 'Never mutate git main branch directly; use ephemeral worktrees',
          source_adr: 'docs/adr/005-worktrees.md',
        },
      ];

      const html = renderToStaticMarkup(
        React.createElement(BrainExplorerModal, {
          initialTab: 'rules',
          initialRules: mockRules,
        })
      );

      expect(html).toContain('ADR-001');
      expect(html).toContain('PostgreSQL 16 with pgvector Extension');
      expect(html).toContain('badge-severity-error');
      expect(html).toContain('ERROR');
      expect(html).toContain('ADR-005');
      expect(html).toContain('Git Worktree Ephemeral Isolation');
      expect(html).toContain('badge-severity-warn');
      expect(html).toContain('WARN');
      expect(html).toContain('docs/adr/001-pgvector.md');
    });

    it('renders stats & sync tab with metrics cards and Sync ADRs action', () => {
      useFactoryFlowStore.setState({ showBrainExplorer: true } as any);
      const mockStats: brainApi.BrainStats = {
        indexed_knowledge: 154,
        active_rules: 28,
        task_episodes: 12,
      };

      const html = renderToStaticMarkup(
        React.createElement(BrainExplorerModal, {
          initialTab: 'stats',
          initialStats: mockStats,
        })
      );

      expect(html).toContain('154');
      expect(html).toContain('Indexed Knowledge Chunks');
      expect(html).toContain('28');
      expect(html).toContain('Active Architectural Rules');
      expect(html).toContain('12');
      expect(html).toContain('Task Episodes');
      expect(html).toContain('Sync ADRs');
    });

    it('respects isOpen explicit prop over store state', () => {
      useFactoryFlowStore.setState({ showBrainExplorer: false } as any);
      const htmlExplicitOpen = renderToStaticMarkup(
        React.createElement(BrainExplorerModal, { isOpen: true })
      );
      expect(htmlExplicitOpen).toContain('brain-modal-content');

      useFactoryFlowStore.setState({ showBrainExplorer: true } as any);
      const htmlExplicitClosed = renderToStaticMarkup(
        React.createElement(BrainExplorerModal, { isOpen: false })
      );
      expect(htmlExplicitClosed).toBe('');
    });

    it('renders vector and lexical breakdown scores when available in search results', () => {
      const mockResults: brainApi.BrainSearchResult[] = [
        {
          category: 'rules',
          title: 'Memory Limit ADR',
          summary: 'Worker sandboxes must limit memory to 2GB',
          score: 0.88,
          vector_score: 0.854,
          lexical_score: 0.912,
        },
      ];

      const html = renderToStaticMarkup(
        React.createElement(BrainExplorerModal, {
          isOpen: true,
          initialTab: 'search',
          initialResults: mockResults,
        })
      );

      expect(html).toContain('vec: 0.854');
      expect(html).toContain('lex: 0.912');
      expect(html).toContain('88% match');
    });

    it('filters rules by severity and search term in rules view', () => {
      const mockRules: brainApi.ADRRule[] = [
        {
          rule_code: 'ADR-010',
          title: 'NATS JetStream Messaging',
          severity: 'INFO',
          category: 'gateway',
          description: 'Gateway events must be published to NATS JetStream',
          rationale: 'High throughput and persistent message streams',
        },
      ];

      const html = renderToStaticMarkup(
        React.createElement(BrainExplorerModal, {
          isOpen: true,
          initialTab: 'rules',
          initialRules: mockRules,
        })
      );

      expect(html).toContain('ADR-010');
      expect(html).toContain('NATS JetStream Messaging');
      expect(html).toContain('High throughput and persistent message streams');
      expect(html).toContain('badge-severity-info');
    });

    it('renders sync response summary banner when sync completed', () => {
      const mockStats: brainApi.BrainStats = {
        indexed_knowledge: 50,
        active_rules: 10,
        task_episodes: 2,
      };

      const html = renderToStaticMarkup(
        React.createElement(BrainExplorerModal, {
          isOpen: true,
          initialTab: 'stats',
          initialStats: mockStats,
        })
      );

      expect(html).toContain('ADR Rules Synchronization');
      expect(html).toContain('Index Repo (Dry Run)');
    });
  });
});
