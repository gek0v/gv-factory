// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { useFactoryFlowStore } from '../store';
import {
  searchBrain,
  getBrainStats,
  syncADRs,
  indexRepo,
  type BrainCategory,
  type BrainSearchResult,
  type BrainStats,
  type ADRRule,
  type ADRSyncResponse,
  type IndexResultResponse,
} from '../api/brain';
import {
  BrainIcon,
  SearchIcon,
  ShieldIcon,
  ZapIcon,
  RefreshCwIcon,
  XIcon,
  CheckCircleIcon,
  AlertCircleIcon,
  SlidersIcon,
  CodeIcon,
  LayersIcon,
} from './icons/Icons';

export type BrainExplorerTab = 'search' | 'rules' | 'stats';

export interface BrainExplorerModalProps {
  isOpen?: boolean;
  onClose?: () => void;
  initialTab?: BrainExplorerTab;
  initialResults?: BrainSearchResult[];
  initialRules?: ADRRule[];
  initialStats?: BrainStats;
}

export function getSeverityBadgeClass(severity: string): string {
  const s = severity.toUpperCase().trim();
  switch (s) {
    case 'ERROR':
    case 'CRITICAL':
      return 'badge-severity-error';
    case 'WARN':
    case 'WARNING':
      return 'badge-severity-warn';
    case 'INFO':
    default:
      return 'badge-severity-info';
  }
}

export function formatScore(score: number): string {
  if (isNaN(score) || score <= 0) return '0%';
  const pct = Math.min(100, Math.round(score * 100));
  return `${pct}%`;
}

export function formatCategory(cat: string): string {
  switch (cat.toLowerCase().trim()) {
    case 'knowledge':
      return 'Code Knowledge';
    case 'rules':
      return 'ADR Rule';
    case 'episodes':
      return 'Task Episode';
    case 'all':
      return 'All';
    default:
      return cat;
  }
}

// Built-in fallback ADR rules from documentation in case backend hasn't synced yet
const DEFAULT_FALLBACK_RULES: ADRRule[] = [
  {
    rule_code: 'ADR-001',
    title: 'PostgreSQL 16 with pgvector Extension for Long-Term Memory',
    severity: 'ERROR',
    category: 'database',
    description: 'All episodic memory, vector similarity queries, and knowledge chunks must use PostgreSQL 16 with pgvector extension.',
    source_adr: 'docs/adr/001-pgvector-memory.md',
  },
  {
    rule_code: 'ADR-002',
    title: 'Tree-sitter Chunker and AST Symbol Extraction',
    severity: 'WARN',
    category: 'indexing',
    description: 'Codebase files must be indexed structurally via Tree-sitter AST nodes to retain symbol declarations and semantic context.',
    source_adr: 'docs/adr/002-tree-sitter-ast.md',
  },
  {
    rule_code: 'ADR-003',
    title: 'Reciprocal Rank Fusion Hybrid Search (α-weighted)',
    severity: 'INFO',
    category: 'search',
    description: 'Codebase search must combine vector similarity (HNSW cosine distance) with lexical trigram match using α parameter.',
    source_adr: 'docs/adr/003-hybrid-rrf-search.md',
  },
  {
    rule_code: 'ADR-004',
    title: 'Git Worktree Ephemeral Isolation for Sandbox Workers',
    severity: 'ERROR',
    category: 'sandbox',
    description: 'Tasks mutating code must run exclusively in isolated ephemeral Git Worktrees and never mutate main branch directly.',
    source_adr: 'docs/adr/004-worktree-isolation.md',
  },
];

export const BrainExplorerButton: React.FC = () => {
  const { setShowBrainExplorer } = useFactoryFlowStore();

  return (
    <button
      type="button"
      className="brain-explorer-btn"
      onClick={() => setShowBrainExplorer(true)}
      title="Open Codebase Brain & Memory Explorer"
      aria-label="Brain Explorer"
    >
      <BrainIcon size={14} className="brain-btn-icon" />
      <span className="brain-btn-text">Brain</span>
    </button>
  );
};

export const BrainExplorerModal: React.FC<BrainExplorerModalProps> = ({
  isOpen,
  onClose,
  initialTab = 'search',
  initialResults,
  initialRules,
  initialStats,
}) => {
  const store = useFactoryFlowStore();
  const storeOpen = typeof window === 'undefined' ? useFactoryFlowStore.getState().showBrainExplorer : store.showBrainExplorer;
  const isModalOpen = isOpen !== undefined ? isOpen : storeOpen;
  const closeModal = onClose || (() => useFactoryFlowStore.getState().setShowBrainExplorer(false));

  const [activeTab, setActiveTab] = useState<BrainExplorerTab>(initialTab);

  // Search Tab State
  const [searchQuery, setSearchQuery] = useState('');
  const [searchCategory, setSearchCategory] = useState<BrainCategory>('all');
  const [alpha, setAlpha] = useState(0.7);
  const [searchResults, setSearchResults] = useState<BrainSearchResult[]>(initialResults || []);
  const [isSearching, setIsSearching] = useState(false);
  const [searchError, setSearchError] = useState<string | null>(null);

  // Rules Tab State
  const [rules, setRules] = useState<ADRRule[]>(initialRules || DEFAULT_FALLBACK_RULES);
  const [ruleFilterText, setRuleFilterText] = useState('');
  const [severityFilter, setSeverityFilter] = useState<'ALL' | 'ERROR' | 'WARN' | 'INFO'>('ALL');

  // Stats & Sync Tab State
  const [stats, setStats] = useState<BrainStats | null>(initialStats || null);
  const [isLoadingStats, setIsLoadingStats] = useState(false);
  const [statsError, setStatsError] = useState<string | null>(null);
  const [adrPath, setAdrPath] = useState('docs/adr');
  const [isSyncingADRs, setIsSyncingADRs] = useState(false);
  const [syncResponse, setSyncResponse] = useState<ADRSyncResponse | null>(null);
  const [syncError, setSyncError] = useState<string | null>(null);
  const [isIndexing, setIsIndexing] = useState(false);
  const [indexResponse, setIndexResponse] = useState<IndexResultResponse | null>(null);

  // Close on Escape key
  useEffect(() => {
    if (!isModalOpen) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        closeModal();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isModalOpen, closeModal]);

  // Load stats when stats tab opens
  const fetchStats = useCallback(async () => {
    setIsLoadingStats(true);
    setStatsError(null);
    try {
      const data = await getBrainStats();
      setStats(data);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setStatsError(msg);
    } finally {
      setIsLoadingStats(false);
    }
  }, []);

  useEffect(() => {
    if (isModalOpen && activeTab === 'stats' && !initialStats) {
      fetchStats();
    }
  }, [isModalOpen, activeTab, fetchStats, initialStats]);

  // Execute Search
  const handleSearch = useCallback(
    async (overrideQuery?: string) => {
      const q = (overrideQuery !== undefined ? overrideQuery : searchQuery).trim();
      if (!q) {
        if (!initialResults) setSearchResults([]);
        return;
      }
      setIsSearching(true);
      setSearchError(null);
      try {
        const results = await searchBrain({
          query: q,
          category: searchCategory,
          alpha,
          limit: 20,
        });
        setSearchResults(results);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        setSearchError(msg);
      } finally {
        setIsSearching(false);
      }
    },
    [searchQuery, searchCategory, alpha, initialResults]
  );

  // Sync ADRs action
  const handleSyncADRs = useCallback(async () => {
    setIsSyncingADRs(true);
    setSyncError(null);
    try {
      const res = await syncADRs(adrPath.trim() || undefined);
      setSyncResponse(res);
      if (res.rules && res.rules.length > 0) {
        setRules(res.rules);
      }
      fetchStats();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setSyncError(msg);
    } finally {
      setIsSyncingADRs(false);
    }
  }, [adrPath, fetchStats]);

  // Quick Index Repo action
  const handleIndexRepo = useCallback(async () => {
    setIsIndexing(true);
    try {
      const res = await indexRepo({ path: '.', dry_run: true });
      setIndexResponse(res);
      fetchStats();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setSyncError(msg);
    } finally {
      setIsIndexing(false);
    }
  }, [fetchStats]);

  // Filtered Rules
  const filteredRules = useMemo(() => {
    return rules.filter((r) => {
      if (severityFilter !== 'ALL' && r.severity.toUpperCase() !== severityFilter) {
        return false;
      }
      if (ruleFilterText.trim()) {
        const text = ruleFilterText.toLowerCase();
        const matchesCode = r.rule_code.toLowerCase().includes(text);
        const matchesTitle = r.title.toLowerCase().includes(text);
        const matchesDesc = r.description.toLowerCase().includes(text);
        const matchesCat = (r.category || '').toLowerCase().includes(text);
        return matchesCode || matchesTitle || matchesDesc || matchesCat;
      }
      return true;
    });
  }, [rules, severityFilter, ruleFilterText]);

  if (!isModalOpen) {
    return null;
  }

  return (
    <div
      className="brain-modal-backdrop"
      onClick={(e) => {
        if (e.target === e.currentTarget) closeModal();
      }}
    >
      <div
        className="brain-modal-content"
        role="dialog"
        aria-modal="true"
        aria-labelledby="brain-modal-title"
      >
        {/* Modal Header */}
        <div className="brain-modal-header">
          <div className="brain-header-left">
            <BrainIcon size={22} className="brain-header-icon" />
            <div>
              <h2 id="brain-modal-title" className="brain-modal-title">
                Codebase Brain &amp; Memory Explorer
              </h2>
              <p className="brain-modal-subtitle">
                pgvector semantic memory, AST symbol knowledge, and ADR architectural rules
              </p>
            </div>
          </div>
          <div className="brain-header-right">
            <button
              type="button"
              className="brain-close-btn"
              onClick={closeModal}
              aria-label="Close modal"
              title="Close (Esc)"
            >
              <XIcon size={18} />
            </button>
          </div>
        </div>

        {/* Tab Navigation */}
        <div className="brain-tabs-nav" role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={activeTab === 'search'}
            className={`brain-tab-btn ${activeTab === 'search' ? 'active' : ''}`}
            onClick={() => setActiveTab('search')}
          >
            <SearchIcon size={14} />
            <span>Search / Knowledge</span>
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={activeTab === 'rules'}
            className={`brain-tab-btn ${activeTab === 'rules' ? 'active' : ''}`}
            onClick={() => setActiveTab('rules')}
          >
            <ShieldIcon size={14} />
            <span>Architectural Rules</span>
            <span className="brain-tab-count">{rules.length}</span>
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={activeTab === 'stats'}
            className={`brain-tab-btn ${activeTab === 'stats' ? 'active' : ''}`}
            onClick={() => setActiveTab('stats')}
          >
            <ZapIcon size={14} />
            <span>Stats &amp; Sync</span>
          </button>
        </div>

        {/* Modal Body: Active Tab Content */}
        <div className="brain-modal-body">
          {/* TAB 1: Search / Knowledge */}
          {activeTab === 'search' && (
            <div className="brain-search-pane">
              <form
                className="brain-search-bar-row"
                onSubmit={(e) => {
                  e.preventDefault();
                  handleSearch();
                }}
              >
                <div className="brain-input-wrapper">
                  <SearchIcon size={16} className="brain-input-icon" />
                  <input
                    type="search"
                    className="brain-search-input"
                    placeholder="Search codebase knowledge, ADR rules, episodic memory..."
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                  />
                </div>
                <button
                  type="submit"
                  className="brain-search-action-btn"
                  disabled={isSearching}
                >
                  {isSearching ? 'Searching...' : 'Search'}
                </button>
              </form>

              {/* Filters Row */}
              <div className="brain-search-controls-row">
                <div className="brain-category-pills" role="radiogroup" aria-label="Search Category">
                  <button
                    type="button"
                    className={`category-pill ${searchCategory === 'all' ? 'active' : ''}`}
                    onClick={() => setSearchCategory('all')}
                  >
                    All Categories
                  </button>
                  <button
                    type="button"
                    className={`category-pill ${searchCategory === 'knowledge' ? 'active' : ''}`}
                    onClick={() => setSearchCategory('knowledge')}
                  >
                    Knowledge Chunks
                  </button>
                  <button
                    type="button"
                    className={`category-pill ${searchCategory === 'rules' ? 'active' : ''}`}
                    onClick={() => setSearchCategory('rules')}
                  >
                    ADR Rules
                  </button>
                  <button
                    type="button"
                    className={`category-pill ${searchCategory === 'episodes' ? 'active' : ''}`}
                    onClick={() => setSearchCategory('episodes')}
                  >
                    Past Episodes
                  </button>
                </div>

                {/* Alpha Slider */}
                <div className="brain-alpha-slider-container" title="Hybrid Search Balance: 0.0 (Pure Lexical) to 1.0 (Pure Semantic)">
                  <SlidersIcon size={14} className="slider-icon" />
                  <span className="slider-label">
                    Hybrid Balance (α): <strong>{alpha.toFixed(2)}</strong>
                  </span>
                  <input
                    type="range"
                    min="0"
                    max="1"
                    step="0.05"
                    value={alpha}
                    onChange={(e) => setAlpha(parseFloat(e.target.value))}
                    className="brain-alpha-slider"
                  />
                  <span className="slider-hint">
                    {alpha < 0.4 ? 'Lexical' : alpha > 0.6 ? 'Semantic' : 'Hybrid'}
                  </span>
                </div>
              </div>

              {/* Error Message */}
              {searchError && (
                <div className="brain-alert brain-alert-error">
                  <AlertCircleIcon size={16} />
                  <span>{searchError}</span>
                </div>
              )}

              {/* Search Results List */}
              <div className="brain-results-container">
                {isSearching ? (
                  <div className="brain-empty-state">
                    <RefreshCwIcon size={24} className="spin-icon" />
                    <p>Executing hybrid reciprocal rank fusion query...</p>
                  </div>
                ) : searchResults.length > 0 ? (
                  <div className="brain-results-list">
                    {searchResults.map((item, idx) => (
                      <div key={idx} className="brain-result-card">
                        <div className="result-card-header">
                          <div className="result-card-title-group">
                            <span className={`result-category-badge category-${item.category}`}>
                              {formatCategory(item.category)}
                            </span>
                            {item.title && <h4 className="result-card-title">{item.title}</h4>}
                          </div>
                          <span className="result-score-badge" title={`Relevance score: ${item.score}`}>
                            {formatScore(item.score)} match
                          </span>
                        </div>

                        {(item.file_path || item.symbol_name) && (
                          <div className="result-meta-row">
                            {item.file_path && (
                              <span className="result-filepath" title={item.file_path}>
                                <CodeIcon size={13} />
                                {item.file_path}
                              </span>
                            )}
                            {item.symbol_name && (
                              <span className="result-symbol">
                                <LayersIcon size={13} />
                                <strong>{item.symbol_name}</strong>
                                {item.symbol_type && <span className="symbol-type">({item.symbol_type})</span>}
                              </span>
                            )}
                          </div>
                        )}

                        <div className="result-summary-box">
                          <code>{item.summary}</code>
                        </div>

                        {(item.vector_score !== undefined || item.lexical_score !== undefined) && (
                          <div className="result-score-breakdown">
                            {item.vector_score !== undefined && (
                              <span>vec: {(item.vector_score).toFixed(3)}</span>
                            )}
                            {item.lexical_score !== undefined && (
                              <span>lex: {(item.lexical_score).toFixed(3)}</span>
                            )}
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="brain-empty-state">
                    <BrainIcon size={32} className="empty-brain-icon" />
                    <p>No knowledge chunks or ADR rules found matching your query.</p>
                    <span className="empty-subtitle">
                      Enter a search keyword above or click &quot;Sync ADRs&quot; in the Stats tab to index documentation.
                    </span>
                  </div>
                )}
              </div>
            </div>
          )}

          {/* TAB 2: Architectural Rules */}
          {activeTab === 'rules' && (
            <div className="brain-rules-pane">
              <div className="brain-rules-filter-row">
                <div className="brain-input-wrapper">
                  <SearchIcon size={14} className="brain-input-icon" />
                  <input
                    type="search"
                    className="brain-rule-filter-input"
                    placeholder="Filter rules by code, title, or description..."
                    value={ruleFilterText}
                    onChange={(e) => setRuleFilterText(e.target.value)}
                  />
                </div>
                <div className="brain-severity-select-group">
                  <label htmlFor="severity-filter" className="severity-filter-label">
                    Severity:
                  </label>
                  <select
                    id="severity-filter"
                    className="brain-severity-select"
                    value={severityFilter}
                    onChange={(e) => setSeverityFilter(e.target.value as any)}
                  >
                    <option value="ALL">All Severities</option>
                    <option value="ERROR">ERROR</option>
                    <option value="WARN">WARN</option>
                    <option value="INFO">INFO</option>
                  </select>
                </div>
              </div>

              <div className="brain-rules-list">
                {filteredRules.map((rule, idx) => (
                  <div key={rule.rule_code || idx} className="brain-rule-card">
                    <div className="rule-card-header">
                      <div className="rule-code-badge">{rule.rule_code}</div>
                      <h4 className="rule-title">{rule.title}</h4>
                      <span className={`rule-severity-badge ${getSeverityBadgeClass(rule.severity)}`}>
                        {rule.severity.toUpperCase()}
                      </span>
                    </div>

                    <p className="rule-description">{rule.description}</p>

                    {rule.rationale && (
                      <div className="rule-rationale">
                        <strong>Rationale:</strong> {rule.rationale}
                      </div>
                    )}

                    <div className="rule-footer">
                      {rule.category && (
                        <span className="rule-category-tag">Category: {rule.category}</span>
                      )}
                      {rule.source_adr && (
                        <span className="rule-source-adr">Source: {rule.source_adr}</span>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* TAB 3: Stats & Sync */}
          {activeTab === 'stats' && (
            <div className="brain-stats-pane">
              {statsError && (
                <div className="brain-alert brain-alert-error">
                  <AlertCircleIcon size={16} />
                  <span>{statsError}</span>
                </div>
              )}

              {/* Metric Cards Grid */}
              <div className="brain-stats-grid">
                <div className="brain-stat-card">
                  <div className="stat-card-icon-wrap icon-knowledge">
                    <CodeIcon size={20} />
                  </div>
                  <div className="stat-card-content">
                    <span className="stat-card-value">
                      {isLoadingStats ? '...' : (stats?.indexed_knowledge ?? 0)}
                    </span>
                    <span className="stat-card-label">Indexed Knowledge Chunks</span>
                  </div>
                </div>

                <div className="brain-stat-card">
                  <div className="stat-card-icon-wrap icon-rules">
                    <ShieldIcon size={20} />
                  </div>
                  <div className="stat-card-content">
                    <span className="stat-card-value">
                      {isLoadingStats ? '...' : (stats?.active_rules ?? rules.length)}
                    </span>
                    <span className="stat-card-label">Active Architectural Rules</span>
                  </div>
                </div>

                <div className="brain-stat-card">
                  <div className="stat-card-icon-wrap icon-episodes">
                    <ZapIcon size={20} />
                  </div>
                  <div className="stat-card-content">
                    <span className="stat-card-value">
                      {isLoadingStats ? '...' : (stats?.task_episodes ?? 0)}
                    </span>
                    <span className="stat-card-label">Task Episodes</span>
                  </div>
                </div>
              </div>

              {/* Sync Actions Section */}
              <div className="brain-sync-actions-card">
                <h4 className="sync-section-title">
                  <RefreshCwIcon size={16} />
                  ADR Rules Synchronization
                </h4>
                <p className="sync-section-desc">
                  Scan markdown architectural decision records and synchronize rules to the vector database.
                </p>

                <div className="sync-input-row">
                  <input
                    type="text"
                    className="sync-path-input"
                    placeholder="ADR Directory (e.g. docs/adr)"
                    value={adrPath}
                    onChange={(e) => setAdrPath(e.target.value)}
                  />
                  <button
                    type="button"
                    className="sync-action-btn"
                    onClick={handleSyncADRs}
                    disabled={isSyncingADRs}
                  >
                    {isSyncingADRs ? 'Syncing ADRs...' : 'Sync ADRs'}
                  </button>
                  <button
                    type="button"
                    className="sync-secondary-btn"
                    onClick={handleIndexRepo}
                    disabled={isIndexing}
                    title="Simulate AST symbol indexing on repository files"
                  >
                    {isIndexing ? 'Indexing...' : 'Index Repo (Dry Run)'}
                  </button>
                  <button
                    type="button"
                    className="sync-refresh-btn"
                    onClick={fetchStats}
                    disabled={isLoadingStats}
                    title="Refresh memory statistics"
                  >
                    <RefreshCwIcon size={14} className={isLoadingStats ? 'spin-icon' : ''} />
                  </button>
                </div>

                {syncError && (
                  <div className="brain-alert brain-alert-error">
                    <AlertCircleIcon size={16} />
                    <span>{syncError}</span>
                  </div>
                )}

                {syncResponse && (
                  <div className="brain-alert brain-alert-success">
                    <CheckCircleIcon size={16} />
                    <div>
                      <strong>ADR Sync Completed:</strong> Scanned {syncResponse.total_files} files
                      ({syncResponse.created} created, {syncResponse.updated} updated, {syncResponse.skipped} skipped, {syncResponse.obsolete} obsolete).
                    </div>
                  </div>
                )}

                {indexResponse && (
                  <div className="brain-alert brain-alert-success">
                    <CheckCircleIcon size={16} />
                    <div>
                      <strong>Repo Index Completed:</strong> Scanned {indexResponse.total_files_scanned} files, extracted {indexResponse.extracted_symbols} symbols, skipped {indexResponse.skipped_unchanged} unchanged files.
                    </div>
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
