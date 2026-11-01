// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect } from 'vitest';
import { parseAnsi, stripAnsi, highlightText } from './ansi';

describe('ansi utils', () => {
  describe('stripAnsi', () => {
    it('returns empty string if input is empty', () => {
      expect(stripAnsi('')).toBe('');
    });

    it('returns original string if there are no ansi escape sequences', () => {
      expect(stripAnsi('hello world')).toBe('hello world');
    });

    it('strips standard ansi color codes', () => {
      const input = '\x1b[31mError:\x1b[0m build failed';
      expect(stripAnsi(input)).toBe('Error: build failed');
    });

    it('strips compound and bright color codes', () => {
      const input = '\x1b[1;92;44m[SUCCESS]\x1b[0m all tests passed';
      expect(stripAnsi(input)).toBe('[SUCCESS] all tests passed');
    });

    it('strips unsupported CSI cursor codes and OSC window title codes safely', () => {
      const input = '\x1b[2K\x1b[1A\x1b]0;My Title\x07Progress: 50%';
      expect(stripAnsi(input)).toBe('Progress: 50%');
    });

    it('preserves multi-byte UTF-8 characters including accents and emojis', () => {
      const input = '\x1b[33m⚡ Compilación finalizada con éxito: café y piña 🍍\x1b[0m';
      expect(stripAnsi(input)).toBe('⚡ Compilación finalizada con éxito: café y piña 🍍');
    });
  });

  describe('parseAnsi', () => {
    it('parses plain text without ansi codes as a single span', () => {
      const spans = parseAnsi('Plain text message');
      expect(spans).toHaveLength(1);
      expect(spans[0]).toEqual({
        text: 'Plain text message',
        classes: [],
      });
    });

    it('parses standard foreground 16 colors (standard 30-37 and bright 90-97)', () => {
      const spans = parseAnsi('\x1b[31mRed\x1b[32mGreen\x1b[94mBrightBlue\x1b[0m');
      expect(spans).toHaveLength(3);
      expect(spans[0]).toEqual({ text: 'Red', classes: ['ansi-fg-red'] });
      expect(spans[1]).toEqual({ text: 'Green', classes: ['ansi-fg-green'] });
      expect(spans[2]).toEqual({ text: 'BrightBlue', classes: ['ansi-fg-bright-blue'] });
    });

    it('parses standard background colors (standard 40-47 and bright 100-107)', () => {
      const spans = parseAnsi('\x1b[41mRedBg\x1b[102mBrightGreenBg\x1b[0m');
      expect(spans).toHaveLength(2);
      expect(spans[0]).toEqual({ text: 'RedBg', classes: ['ansi-bg-red'] });
      expect(spans[1]).toEqual({ text: 'BrightGreenBg', classes: ['ansi-bg-bright-green'] });
    });

    it('parses text styles (bold, dim, italic, underline)', () => {
      const spans = parseAnsi('\x1b[1mBold\x1b[2mDim\x1b[3mItalic\x1b[4mUnderline\x1b[0m');
      expect(spans).toHaveLength(4);
      expect(spans[0].classes).toContain('ansi-bold');
      expect(spans[1].classes).toContain('ansi-dim');
      expect(spans[2].classes).toContain('ansi-italic');
      expect(spans[3].classes).toContain('ansi-underline');
    });

    it('parses compound parameters in a single escape sequence (e.g. \x1b[1;31;42m)', () => {
      const spans = parseAnsi('\x1b[1;31;42mStyled Text\x1b[0m');
      expect(spans).toHaveLength(1);
      expect(spans[0]).toEqual({
        text: 'Styled Text',
        classes: ['ansi-bold', 'ansi-fg-red', 'ansi-bg-green'],
      });
    });

    it('supports selective resets (22: not bold/dim, 23: not italic, 24: not underline, 39: default fg, 49: default bg)', () => {
      const spans = parseAnsi('\x1b[1;31mBoldRed\x1b[22mNormalRed\x1b[39mNormalDefault\x1b[0m');
      expect(spans).toHaveLength(3);
      expect(spans[0].classes).toEqual(['ansi-bold', 'ansi-fg-red']);
      expect(spans[1].classes).toEqual(['ansi-fg-red']);
      expect(spans[2].classes).toEqual([]);
    });

    it('ignores cursor movement and non-SGR sequences without failing', () => {
      const spans = parseAnsi('\x1b[2K\x1b[32mClean\x1b[0m');
      expect(spans).toHaveLength(1);
      expect(spans[0]).toEqual({ text: 'Clean', classes: ['ansi-fg-green'] });
    });

    it('handles unclosed ansi sequences gracefully without spilling over', () => {
      const spans = parseAnsi('\x1b[35mMagenta without reset');
      expect(spans).toHaveLength(1);
      expect(spans[0]).toEqual({ text: 'Magenta without reset', classes: ['ansi-fg-magenta'] });
    });
  });

  describe('highlightText', () => {
    it('returns original spans if query is empty or only whitespace', () => {
      const spans = [{ text: 'Hello World', classes: ['ansi-bold'] }];
      expect(highlightText(spans, '')).toEqual(spans);
      expect(highlightText(spans, '   ')).toEqual(spans);
    });

    it('splits text into matched and non-matched sub-spans with highlight class', () => {
      const spans = [{ text: 'Error in compiling main.go: Error 404', classes: ['ansi-fg-red'] }];
      const highlighted = highlightText(spans, 'Error');
      expect(highlighted).toHaveLength(4);
      expect(highlighted[0]).toEqual({ text: 'Error', classes: ['ansi-fg-red', 'log-search-match'], isMatch: true });
      expect(highlighted[1]).toEqual({ text: ' in compiling main.go: ', classes: ['ansi-fg-red'] });
      expect(highlighted[2]).toEqual({ text: 'Error', classes: ['ansi-fg-red', 'log-search-match'], isMatch: true });
      expect(highlighted[3]).toEqual({ text: ' 404', classes: ['ansi-fg-red'] });
    });

    it('performs case-insensitive matching by default', () => {
      const spans = [{ text: 'INFO info Info', classes: [] }];
      const highlighted = highlightText(spans, 'info');
      expect(highlighted).toHaveLength(5);
      expect(highlighted[0].isMatch).toBe(true);
      expect(highlighted[0].text).toBe('INFO');
      expect(highlighted[2].isMatch).toBe(true);
      expect(highlighted[2].text).toBe('info');
      expect(highlighted[4].isMatch).toBe(true);
      expect(highlighted[4].text).toBe('Info');
    });

    it('escapes special regex characters in search query', () => {
      const spans = [{ text: 'Pointer to [obj] is *ptr', classes: [] }];
      const highlighted = highlightText(spans, '[obj]');
      expect(highlighted).toHaveLength(3);
      expect(highlighted[0]).toEqual({ text: 'Pointer to ', classes: [] });
      expect(highlighted[1]).toEqual({ text: '[obj]', classes: ['log-search-match'], isMatch: true });
      expect(highlighted[2]).toEqual({ text: ' is *ptr', classes: [] });
    });
  });
});
