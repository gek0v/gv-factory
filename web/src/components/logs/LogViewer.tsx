// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useState, useRef, useEffect, useMemo, useCallback } from 'react';
import type { NodeLogEntry } from '../../types/flow';
import { parseAnsi, stripAnsi, highlightText, type AnsiSpan } from '../../utils/ansi';
import {
  SearchIcon,
  CopyIcon,
  DownloadIcon,
  TrashIcon,
  ArrowDownIcon,
  XIcon,
} from '../icons/Icons';

export type LogStreamFilter = 'all' | 'stdout' | 'stderr';

export interface LogViewerProps {
  logs?: NodeLogEntry[];
  onClear?: () => void;
}

export function isScrolledToBottom(
  scrollTop: number,
  clientHeight: number,
  scrollHeight: number,
  threshold = 40
): boolean {
  return scrollHeight - (scrollTop + clientHeight) <= threshold;
}

export function filterLogs(
  logs: NodeLogEntry[],
  streamFilter: LogStreamFilter,
  searchQuery: string
): NodeLogEntry[] {
  const query = searchQuery.trim().toLowerCase();

  return logs.filter((entry) => {
    if (streamFilter !== 'all' && entry.stream !== streamFilter) {
      return false;
    }
    if (query.length > 0) {
      const cleanMessage = stripAnsi(entry.message).toLowerCase();
      if (!cleanMessage.includes(query)) {
        return false;
      }
    }
    return true;
  });
}

export function countStreamLogs(logs: NodeLogEntry[]): {
  all: number;
  stdout: number;
  stderr: number;
} {
  let stdout = 0;
  let stderr = 0;
  for (const log of logs) {
    if (log.stream === 'stdout') stdout++;
    else if (log.stream === 'stderr') stderr++;
  }
  return {
    all: logs.length,
    stdout,
    stderr,
  };
}

export function countSearchMatches(logs: NodeLogEntry[], searchQuery: string): number {
  const query = searchQuery.trim().toLowerCase();
  if (!query) return 0;

  let total = 0;
  for (const log of logs) {
    const clean = stripAnsi(log.message).toLowerCase();
    let pos = 0;
    while ((pos = clean.indexOf(query, pos)) !== -1) {
      total++;
      pos += query.length;
    }
  }
  return total;
}

export function formatLogsAsPlainText(logs: NodeLogEntry[]): string {
  return logs
    .map((log) => {
      const time = log.timestamp ? `[${log.timestamp}] ` : '';
      const streamTag = `[${log.stream.toUpperCase()}] `;
      const cleanMsg = stripAnsi(log.message);
      return `${time}${streamTag}${cleanMsg}`;
    })
    .join('\n');
}

export const LogViewer: React.FC<LogViewerProps> = ({ logs = [], onClear }) => {
  const [streamFilter, setStreamFilter] = useState<LogStreamFilter>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [isAutoScrollActive, setIsAutoScrollActive] = useState(true);
  const [unreadCount, setUnreadCount] = useState(0);
  const [copyFeedback, setCopyFeedback] = useState<string | null>(null);

  const consoleRef = useRef<HTMLDivElement>(null);
  const prevLogsLengthRef = useRef(logs.length);

  const streamCounts = useMemo(() => countStreamLogs(logs), [logs]);
  const filteredLogs = useMemo(
    () => filterLogs(logs, streamFilter, searchQuery),
    [logs, streamFilter, searchQuery]
  );
  const searchMatchCount = useMemo(
    () => countSearchMatches(filteredLogs, searchQuery),
    [filteredLogs, searchQuery]
  );

  const scrollToBottom = useCallback((smooth = true) => {
    if (consoleRef.current) {
      consoleRef.current.scrollTo({
        top: consoleRef.current.scrollHeight,
        behavior: smooth ? 'smooth' : 'auto',
      });
    }
    setIsAutoScrollActive(true);
    setUnreadCount(0);
  }, []);

  // Handle incoming logs: auto-scroll or accumulate unread count
  useEffect(() => {
    const diff = logs.length - prevLogsLengthRef.current;
    prevLogsLengthRef.current = logs.length;

    if (diff > 0) {
      if (isAutoScrollActive) {
        // Need a slight tick for DOM render
        requestAnimationFrame(() => {
          if (consoleRef.current) {
            consoleRef.current.scrollTop = consoleRef.current.scrollHeight;
          }
        });
      } else {
        setUnreadCount((prev) => prev + diff);
      }
    }
  }, [logs.length, isAutoScrollActive]);

  const handleScroll = () => {
    if (!consoleRef.current) return;
    const { scrollTop, clientHeight, scrollHeight } = consoleRef.current;
    const atBottom = isScrolledToBottom(scrollTop, clientHeight, scrollHeight, 40);

    if (atBottom) {
      if (!isAutoScrollActive) {
        setIsAutoScrollActive(true);
        setUnreadCount(0);
      }
    } else {
      if (isAutoScrollActive) {
        setIsAutoScrollActive(false);
      }
    }
  };

  const handleCopy = async () => {
    const text = formatLogsAsPlainText(filteredLogs);
    try {
      await navigator.clipboard.writeText(text);
      setCopyFeedback('Copied!');
      setTimeout(() => setCopyFeedback(null), 2000);
    } catch {
      setCopyFeedback('Failed to copy');
      setTimeout(() => setCopyFeedback(null), 2000);
    }
  };

  const handleDownload = () => {
    const text = formatLogsAsPlainText(filteredLogs);
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `pipeline-logs-${Date.now()}.log`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
  };

  if (logs.length === 0) {
    return (
      <div className="logs-empty-state">
        <p>No stdout/stderr telemetry received yet.</p>
        <span className="logs-empty-hint">
          Logs will stream in real-time when the WASM plugin or worker executes.
        </span>
      </div>
    );
  }

  return (
    <div className="log-viewer-container">
      {/* Top Controls Toolbar */}
      <div className="log-viewer-toolbar">
        {/* Stream Filter Tabs */}
        <div className="log-stream-tabs" role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={streamFilter === 'all'}
            className={`log-tab-btn ${streamFilter === 'all' ? 'active' : ''}`}
            onClick={() => setStreamFilter('all')}
          >
            All <span className="log-tab-count">({streamCounts.all})</span>
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={streamFilter === 'stdout'}
            className={`log-tab-btn stream-stdout-tab ${streamFilter === 'stdout' ? 'active' : ''}`}
            onClick={() => setStreamFilter('stdout')}
          >
            stdout <span className="log-tab-count">({streamCounts.stdout})</span>
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={streamFilter === 'stderr'}
            className={`log-tab-btn stream-stderr-tab ${streamFilter === 'stderr' ? 'active' : ''}`}
            onClick={() => setStreamFilter('stderr')}
          >
            stderr <span className="log-tab-count">({streamCounts.stderr})</span>
          </button>
        </div>

        {/* Utility Actions */}
        <div className="log-viewer-actions">
          <button
            type="button"
            className="log-action-btn"
            onClick={handleCopy}
            title="Copy logs to clipboard"
          >
            <CopyIcon size={13} />
            <span>{copyFeedback || 'Copy'}</span>
          </button>
          <button
            type="button"
            className="log-action-btn"
            onClick={handleDownload}
            title="Download logs file"
          >
            <DownloadIcon size={13} />
            <span>Download</span>
          </button>
          {onClear && (
            <button
              type="button"
              className="log-action-btn log-clear-action-btn"
              onClick={onClear}
              title="Clear logs"
            >
              <TrashIcon size={13} />
              <span>Clear</span>
            </button>
          )}
        </div>
      </div>

      {/* Search Bar */}
      <div className="log-search-row">
        <div className="log-search-input-wrapper">
          <SearchIcon size={13} className="log-search-icon" />
          <input
            type="text"
            className="log-search-input"
            placeholder="Search logs (regex/text)..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
          />
          {searchQuery && (
            <button
              type="button"
              className="log-search-clear-btn"
              onClick={() => setSearchQuery('')}
              title="Clear search"
            >
              <XIcon size={12} />
            </button>
          )}
        </div>
        {searchQuery.trim() && (
          <span className="log-match-counter">
            {searchMatchCount} {searchMatchCount === 1 ? 'match' : 'matches'}
          </span>
        )}
      </div>

      {/* Terminal Console */}
      <div
        className="log-stream-console log-terminal"
        ref={consoleRef}
        onScroll={handleScroll}
        tabIndex={0}
      >
        {filteredLogs.length === 0 ? (
          <div className="logs-no-filter-match">
            <span>No log lines matched the filter or search query.</span>
          </div>
        ) : (
          filteredLogs.map((entry, idx) => {
            const rawSpans = parseAnsi(entry.message);
            const styledSpans = searchQuery.trim()
              ? highlightText(rawSpans, searchQuery)
              : rawSpans;

            const timeStr = entry.timestamp ? entry.timestamp.slice(11, 23) : '';

            return (
              <div key={idx} className={`log-entry log-${entry.stream}`}>
                <span className="log-idx">{idx + 1}</span>
                {timeStr && <span className="log-timestamp">{timeStr}</span>}
                <span className={`log-stream-tag stream-${entry.stream}`}>
                  {entry.stream.toUpperCase()}
                </span>
                <span className="log-message">
                  {styledSpans.map((span: AnsiSpan, sIdx: number) => (
                    <span
                      key={sIdx}
                      className={span.classes.length > 0 ? span.classes.join(' ') : undefined}
                    >
                      {span.text}
                    </span>
                  ))}
                </span>
              </div>
            );
          })
        )}
      </div>

      {/* Floating Jump to Bottom Button */}
      {!isAutoScrollActive && (
        <button
          type="button"
          className="log-jump-bottom-btn"
          onClick={() => scrollToBottom(true)}
        >
          <ArrowDownIcon size={13} />
          <span>Auto-scroll paused — Jump to bottom</span>
          {unreadCount > 0 && <span className="log-unread-badge">+{unreadCount}</span>}
        </button>
      )}
    </div>
  );
};
