// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import {
  CodeInspectionModal,
  detectLanguage,
  formatFileSize,
} from './CodeInspectionModal';
import * as factoryApi from '../../api/factory';

// Mock Monaco Editor for server-side / JSDOM environment
vi.mock('@monaco-editor/react', () => {
  return {
    default: ({ value, theme, language, options }: any) => (
      <div
        data-testid="mock-monaco-editor"
        className="mock-monaco-editor"
        data-theme={theme}
        data-language={language}
        data-font-size={options?.fontSize}
        data-word-wrap={options?.wordWrap}
        data-minimap={options?.minimap?.enabled ? 'enabled' : 'disabled'}
      >
        <textarea
          data-testid="monaco-textarea"
          defaultValue={value}
          readOnly
        />
      </div>
    ),
  };
});

describe('detectLanguage helper', () => {
  it('detects Golang files correctly', () => {
    expect(detectLanguage('main.go')).toBe('go');
    expect(detectLanguage('pkg/calculator/math.go')).toBe('go');
    expect(detectLanguage('go.mod')).toBe('go');
    expect(detectLanguage('go.sum')).toBe('go');
  });

  it('detects TypeScript and JavaScript correctly', () => {
    expect(detectLanguage('src/app.ts')).toBe('typescript');
    expect(detectLanguage('src/components/Modal.tsx')).toBe('typescript');
    expect(detectLanguage('server.js')).toBe('javascript');
    expect(detectLanguage('component.jsx')).toBe('javascript');
  });

  it('detects JSON, Markdown, and SQL correctly', () => {
    expect(detectLanguage('package.json')).toBe('json');
    expect(detectLanguage('README.md')).toBe('markdown');
    expect(detectLanguage('docs/architecture.markdown')).toBe('markdown');
    expect(detectLanguage('migrations/001_init.sql')).toBe('sql');
  });

  it('detects YAML, Dockerfile and shell scripts', () => {
    expect(detectLanguage('docker-compose.yaml')).toBe('yaml');
    expect(detectLanguage('values.yml')).toBe('yaml');
    expect(detectLanguage('Dockerfile')).toBe('dockerfile');
    expect(detectLanguage('deploy.sh')).toBe('shell');
    expect(detectLanguage('script.bash')).toBe('shell');
  });

  it('falls back to plaintext for unknown extensions', () => {
    expect(detectLanguage('unknown.xyz')).toBe('plaintext');
    expect(detectLanguage('notes.unknown')).toBe('plaintext');
    expect(detectLanguage('')).toBe('plaintext');
  });
});

describe('formatFileSize helper', () => {
  it('formats bytes, KB, and MB accurately', () => {
    expect(formatFileSize(500)).toBe('500 B');
    expect(formatFileSize(2048)).toBe('2.0 KB');
    expect(formatFileSize(5 * 1024 * 1024)).toBe('5.0 MB');
    expect(formatFileSize(undefined)).toBe('');
    expect(formatFileSize(NaN)).toBe('');
  });
});

describe('CodeInspectionModal Rendering', () => {
  const mockFiles: factoryApi.SandboxFileInfo[] = [
    { path: 'main.go', size: 1024, modified: true },
    { path: 'pkg/calc.go', size: 512, modified: false },
    { path: 'README.md', size: 256, modified: false },
  ];

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders null when isOpen is false', () => {
    const html = renderToStaticMarkup(
      React.createElement(CodeInspectionModal, {
        isOpen: false,
        onClose: vi.fn(),
        runId: 'run-test-1',
      })
    );
    expect(html).toBe('');
  });

  it('renders modal dialog container when isOpen is true', () => {
    const html = renderToStaticMarkup(
      React.createElement(CodeInspectionModal, {
        isOpen: true,
        onClose: vi.fn(),
        runId: 'run-test-123',
        initialFile: 'main.go',
        files: mockFiles,
      })
    );

    expect(html).toContain('code-inspection-modal-overlay');
    expect(html).toContain('code-inspection-modal-container');
    expect(html).toContain('Monaco Code Inspector &amp; Hot Edit');
    expect(html).toContain('run-test-123');
    expect(html).toContain('Guardar Cambios');
    expect(html).toContain('Cerrar Inspector');
  });

  it('renders file list in sidebar with modified badges and sizes', () => {
    const html = renderToStaticMarkup(
      React.createElement(CodeInspectionModal, {
        isOpen: true,
        onClose: vi.fn(),
        runId: 'run-test-123',
        initialFile: 'main.go',
        files: mockFiles,
      })
    );

    expect(html).toContain('main.go');
    expect(html).toContain('pkg/calc.go');
    expect(html).toContain('README.md');
    expect(html).toContain('MOD');
    expect(html).toContain('1.0 KB');
    expect(html).toContain('512 B');
  });

  it('renders Monaco editor with vs-dark theme, font size 13, and word wrap', () => {
    const html = renderToStaticMarkup(
      React.createElement(CodeInspectionModal, {
        isOpen: true,
        onClose: vi.fn(),
        runId: 'run-test-123',
        initialFile: 'main.go',
        files: mockFiles,
      })
    );

    expect(html).toContain('data-theme="vs-dark"');
    expect(html).toContain('data-language="go"');
    expect(html).toContain('data-font-size="13"');
    expect(html).toContain('data-word-wrap="on"');
    expect(html).toContain('data-minimap="enabled"');
    expect(html).toContain('Atajo: [Ctrl+S] para guardar cambios');
  });

  it('renders active file tab and status bar for typescript file', () => {
    const tsFiles: factoryApi.SandboxFileInfo[] = [
      { path: 'src/handler.ts', size: 2048, modified: true },
    ];
    const html = renderToStaticMarkup(
      React.createElement(CodeInspectionModal, {
        isOpen: true,
        onClose: vi.fn(),
        runId: 'run-ts-test',
        initialFile: 'src/handler.ts',
        files: tsFiles,
      })
    );

    expect(html).toContain('data-language="typescript"');
    expect(html).toContain('handler.ts');
    expect(html).toContain('src/handler.ts');
  });

  it('supports string array in files prop', () => {
    const stringFiles = ['cmd/main.go', 'internal/engine.go'];
    const html = renderToStaticMarkup(
      React.createElement(CodeInspectionModal, {
        isOpen: true,
        onClose: vi.fn(),
        runId: 'run-str-files',
        files: stringFiles,
      })
    );

    expect(html).toContain('cmd/main.go');
    expect(html).toContain('internal/engine.go');
  });

  it('renders empty editor state when no file is selected', () => {
    const html = renderToStaticMarkup(
      React.createElement(CodeInspectionModal, {
        isOpen: true,
        onClose: vi.fn(),
        runId: 'run-empty',
        files: [],
      })
    );

    expect(html).toContain('Selecciona un archivo del sandbox para inspeccionar y editar');
    expect(html).toContain('No se encontraron archivos');
  });
});
