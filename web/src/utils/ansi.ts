// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

export interface AnsiSpan {
  text: string;
  classes: string[];
  isMatch?: boolean;
}

// Regex to match all ANSI escape codes:
// 1. CSI codes: \x1b\[[0-9;]*[a-zA-Z]
// 2. OSC codes: \x1b\][^\x07\x1b]*(?:\x07|\x1b\\)
// 3. Simple escapes: \x1b[@-Z\\-_]
const STRIP_ANSI_REGEX =
  /(?:\x1b|\u001b)(?:\[[0-9;]*[a-zA-Z]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[@-Z\\-_])/g;

// Regex matching specifically CSI escape sequences
const CSI_REGEX = /(?:\x1b|\u001b)\[([0-9;]*)([a-zA-Z])/g;

const FG_COLOR_MAP: Record<number, string> = {
  30: 'ansi-fg-black',
  31: 'ansi-fg-red',
  32: 'ansi-fg-green',
  33: 'ansi-fg-yellow',
  34: 'ansi-fg-blue',
  35: 'ansi-fg-magenta',
  36: 'ansi-fg-cyan',
  37: 'ansi-fg-white',
  90: 'ansi-fg-bright-black',
  91: 'ansi-fg-bright-red',
  92: 'ansi-fg-bright-green',
  93: 'ansi-fg-bright-yellow',
  94: 'ansi-fg-bright-blue',
  95: 'ansi-fg-bright-magenta',
  96: 'ansi-fg-bright-cyan',
  97: 'ansi-fg-bright-white',
};

const BG_COLOR_MAP: Record<number, string> = {
  40: 'ansi-bg-black',
  41: 'ansi-bg-red',
  42: 'ansi-bg-green',
  43: 'ansi-bg-yellow',
  44: 'ansi-bg-blue',
  45: 'ansi-bg-magenta',
  46: 'ansi-bg-cyan',
  47: 'ansi-bg-white',
  100: 'ansi-bg-bright-black',
  101: 'ansi-bg-bright-red',
  102: 'ansi-bg-bright-green',
  103: 'ansi-bg-bright-yellow',
  104: 'ansi-bg-bright-blue',
  105: 'ansi-bg-bright-magenta',
  106: 'ansi-bg-bright-cyan',
  107: 'ansi-bg-bright-white',
};

interface AnsiStyleState {
  bold: boolean;
  dim: boolean;
  italic: boolean;
  underline: boolean;
  fgColor: string | null;
  bgColor: string | null;
}

function createDefaultState(): AnsiStyleState {
  return {
    bold: false,
    dim: false,
    italic: false,
    underline: false,
    fgColor: null,
    bgColor: null,
  };
}

function stateToClasses(state: AnsiStyleState): string[] {
  const classes: string[] = [];
  if (state.bold) classes.push('ansi-bold');
  if (state.dim) classes.push('ansi-dim');
  if (state.italic) classes.push('ansi-italic');
  if (state.underline) classes.push('ansi-underline');
  if (state.fgColor) classes.push(state.fgColor);
  if (state.bgColor) classes.push(state.bgColor);
  return classes;
}

/**
 * Strips all ANSI escape sequences (CSI, OSC, etc.) from a given string.
 */
export function stripAnsi(text: string): string {
  if (!text) return '';
  return text.replace(STRIP_ANSI_REGEX, '');
}

/**
 * Parses ANSI escape sequences into styled text spans.
 */
export function parseAnsi(text: string): AnsiSpan[] {
  if (!text) return [];

  // If text does not contain escape characters, return directly
  if (!text.includes('\x1b') && !text.includes('\u001b')) {
    return [{ text, classes: [] }];
  }

  const spans: AnsiSpan[] = [];
  let state = createDefaultState();
  let lastIndex = 0;

  // Reset regex state
  CSI_REGEX.lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = CSI_REGEX.exec(text)) !== null) {
    const matchStart = match.index;
    const matchEnd = CSI_REGEX.lastIndex;

    // Check if there was non-escape text between lastIndex and matchStart
    if (matchStart > lastIndex) {
      const rawChunk = text.slice(lastIndex, matchStart);
      const cleanChunk = stripAnsi(rawChunk);
      if (cleanChunk.length > 0) {
        spans.push({
          text: cleanChunk,
          classes: stateToClasses(state),
        });
      }
    }

    lastIndex = matchEnd;

    const command = match[2];
    const rawCodes = match[1];

    // We only process SGR commands ('m')
    if (command === 'm') {
      const codes = rawCodes.length === 0 ? [0] : rawCodes.split(';').map(Number);

      for (const code of codes) {
        if (code === 0) {
          state = createDefaultState();
        } else if (code === 1) {
          state.bold = true;
        } else if (code === 2) {
          state.dim = true;
        } else if (code === 3) {
          state.italic = true;
        } else if (code === 4) {
          state.underline = true;
        } else if (code === 22) {
          state.bold = false;
          state.dim = false;
        } else if (code === 23) {
          state.italic = false;
        } else if (code === 24) {
          state.underline = false;
        } else if (code === 39) {
          state.fgColor = null;
        } else if (code === 49) {
          state.bgColor = null;
        } else if (FG_COLOR_MAP[code]) {
          state.fgColor = FG_COLOR_MAP[code];
        } else if (BG_COLOR_MAP[code]) {
          state.bgColor = BG_COLOR_MAP[code];
        }
      }
    }
  }

  // Trailing text after the last escape sequence
  if (lastIndex < text.length) {
    const rawChunk = text.slice(lastIndex);
    const cleanChunk = stripAnsi(rawChunk);
    if (cleanChunk.length > 0) {
      spans.push({
        text: cleanChunk,
        classes: stateToClasses(state),
      });
    }
  }

  return spans;
}

function escapeRegExp(str: string): string {
  return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/**
 * Highlights matches of a query string across an array of AnsiSpans.
 */
export function highlightText(spans: AnsiSpan[], query: string): AnsiSpan[] {
  const trimmed = query.trim();
  if (!trimmed) {
    return spans;
  }

  const escaped = escapeRegExp(trimmed);
  const regex = new RegExp(`(${escaped})`, 'gi');
  const result: AnsiSpan[] = [];

  for (const span of spans) {
    if (!span.text) continue;

    const parts = span.text.split(regex);
    for (const part of parts) {
      if (!part) continue;

      if (part.toLowerCase() === trimmed.toLowerCase()) {
        result.push({
          text: part,
          classes: [...span.classes, 'log-search-match'],
          isMatch: true,
        });
      } else {
        result.push({
          text: part,
          classes: [...span.classes],
        });
      }
    }
  }

  return result;
}
