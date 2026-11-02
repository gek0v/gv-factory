// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect } from 'vitest';
import type { NodeLogEntry } from '../../types/flow';
import {
  isScrolledToBottom,
  filterLogs,
  countStreamLogs,
  countSearchMatches,
  formatLogsAsPlainText,
} from './LogViewer';

describe('LogViewer helper utilities', () => {
  const sampleLogs: NodeLogEntry[] = [
    { timestamp: '2026-11-02T10:00:00Z', stream: 'stdout', message: 'Starting process' },
    { timestamp: '2026-11-02T10:00:01Z', stream: 'stdout', message: '\x1b[32mBuild succeeded\x1b[0m' },
    { timestamp: '2026-11-02T10:00:02Z', stream: 'stderr', message: '\x1b[31mWarning: deprecated API\x1b[0m' },
    { timestamp: '2026-11-02T10:00:03Z', stream: 'stderr', message: 'Second stderr line' },
  ];

  describe('isScrolledToBottom', () => {
    it('returns true when scrollTop + clientHeight is near or at scrollHeight', () => {
      // scrollHeight = 1000, clientHeight = 400, scrollTop = 600 -> exactly at bottom
      expect(isScrolledToBottom(600, 400, 1000, 40)).toBe(true);
      // scrollTop = 570 -> 570 + 400 = 970 (difference 30 <= 40 threshold)
      expect(isScrolledToBottom(570, 400, 1000, 40)).toBe(true);
    });

    it('returns false when user scrolled up past the threshold', () => {
      // scrollTop = 500 -> 500 + 400 = 900 (difference 100 > 40 threshold)
      expect(isScrolledToBottom(500, 400, 1000, 40)).toBe(false);
    });
  });

  describe('filterLogs', () => {
    it('returns all logs when stream is "all" and search is empty', () => {
      const filtered = filterLogs(sampleLogs, 'all', '');
      expect(filtered).toHaveLength(4);
    });

    it('filters strictly by stdout or stderr stream', () => {
      const stdoutOnly = filterLogs(sampleLogs, 'stdout', '');
      expect(stdoutOnly).toHaveLength(2);
      expect(stdoutOnly.every((l) => l.stream === 'stdout')).toBe(true);

      const stderrOnly = filterLogs(sampleLogs, 'stderr', '');
      expect(stderrOnly).toHaveLength(2);
      expect(stderrOnly.every((l) => l.stream === 'stderr')).toBe(true);
    });

    it('filters by search query case-insensitively while ignoring ANSI color codes', () => {
      const filtered = filterLogs(sampleLogs, 'all', 'build');
      expect(filtered).toHaveLength(1);
      expect(filtered[0].message).toContain('Build succeeded');

      const warningFiltered = filterLogs(sampleLogs, 'all', 'WARNING');
      expect(warningFiltered).toHaveLength(1);
      expect(warningFiltered[0].stream).toBe('stderr');
    });

    it('combines stream filter and search query correctly', () => {
      // 'line' is in 'Second stderr line'
      const stdoutMatches = filterLogs(sampleLogs, 'stdout', 'line');
      expect(stdoutMatches).toHaveLength(0);

      const stderrMatches = filterLogs(sampleLogs, 'stderr', 'line');
      expect(stderrMatches).toHaveLength(1);
    });
  });

  describe('countStreamLogs', () => {
    it('returns correct counts for all, stdout, and stderr', () => {
      const counts = countStreamLogs(sampleLogs);
      expect(counts).toEqual({
        all: 4,
        stdout: 2,
        stderr: 2,
      });
    });

    it('returns zero counts for empty logs list', () => {
      expect(countStreamLogs([])).toEqual({
        all: 0,
        stdout: 0,
        stderr: 0,
      });
    });
  });

  describe('countSearchMatches', () => {
    it('counts total occurrences of query in logs, ignoring ANSI codes', () => {
      const logs: NodeLogEntry[] = [
        { timestamp: 'ts', stream: 'stdout', message: 'test one test two' },
        { timestamp: 'ts', stream: 'stderr', message: '\x1b[31mtest three\x1b[0m' },
      ];
      expect(countSearchMatches(logs, 'test')).toBe(3);
    });

    it('returns 0 when search query is empty', () => {
      expect(countSearchMatches(sampleLogs, '')).toBe(0);
      expect(countSearchMatches(sampleLogs, '   ')).toBe(0);
    });
  });

  describe('formatLogsAsPlainText', () => {
    it('formats logs stripping ANSI codes with timestamps and stream tags', () => {
      const text = formatLogsAsPlainText(sampleLogs);
      expect(text).not.toContain('\x1b');
      expect(text).toContain('[STDOUT] Starting process');
      expect(text).toContain('[STDOUT] Build succeeded');
      expect(text).toContain('[STDERR] Warning: deprecated API');
    });
  });

  describe('parseThoughtBlocks helper', () => {
    it('detects and parses <thought> blocks in logs', async () => {
      const { parseThoughtBlocks } = await import('./LogViewer');

      const message = 'Prefix text\n<thought>\nI need to check repo structure first.\n</thought>\nSuffix text';
      const parts = parseThoughtBlocks(message);

      expect(parts).toHaveLength(3);
      expect(parts[0]).toEqual({ type: 'text', content: 'Prefix text\n' });
      expect(parts[1]).toEqual({ type: 'thought', content: 'I need to check repo structure first.' });
      expect(parts[2]).toEqual({ type: 'text', content: '\nSuffix text' });
    });

    it('detects <thinking> blocks as well', async () => {
      const { parseThoughtBlocks } = await import('./LogViewer');

      const message = '<thinking>Deliberating step</thinking>Done.';
      const parts = parseThoughtBlocks(message);

      expect(parts).toHaveLength(2);
      expect(parts[0]).toEqual({ type: 'thought', content: 'Deliberating step' });
      expect(parts[1]).toEqual({ type: 'text', content: 'Done.' });
    });

    it('returns raw text unchanged if no thought tags present', async () => {
      const { parseThoughtBlocks } = await import('./LogViewer');

      const message = 'Standard raw log message without thoughts';
      const parts = parseThoughtBlocks(message);

      expect(parts).toHaveLength(1);
      expect(parts[0]).toEqual({ type: 'text', content: message });
    });
  });
});
