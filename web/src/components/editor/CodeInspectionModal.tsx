// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useState, useEffect, useCallback, useMemo } from 'react';
import Editor from '@monaco-editor/react';
import {
  listSandboxFiles,
  getSandboxFileContent,
  updateSandboxFileContent,
  type SandboxFileInfo,
} from '../../api/factory';

export interface CodeInspectionModalProps {
  isOpen: boolean;
  onClose: () => void;
  runId: string;
  initialFile?: string;
  files?: Array<{ path: string; size?: number; modified?: boolean } | string>;
  onSaveSuccess?: (diff: string, filePath: string) => void;
}

/**
 * Automatically detects the Monaco language identifier based on file extension and filename.
 */
export function detectLanguage(filePath: string): string {
  if (!filePath) return 'plaintext';
  const lower = filePath.toLowerCase();
  const ext = lower.split('.').pop() || '';

  switch (ext) {
    case 'go':
      return 'go';
    case 'ts':
    case 'tsx':
      return 'typescript';
    case 'js':
    case 'jsx':
    case 'mjs':
    case 'cjs':
      return 'javascript';
    case 'json':
      return 'json';
    case 'md':
    case 'markdown':
      return 'markdown';
    case 'sql':
      return 'sql';
    case 'yaml':
    case 'yml':
      return 'yaml';
    case 'html':
    case 'htm':
      return 'html';
    case 'css':
    case 'scss':
      return 'css';
    case 'sh':
    case 'bash':
      return 'shell';
    case 'py':
      return 'python';
    case 'rs':
      return 'rust';
    case 'toml':
      return 'ini';
    case 'xml':
      return 'xml';
    default:
      if (lower.endsWith('dockerfile')) return 'dockerfile';
      if (lower.endsWith('go.mod') || lower.endsWith('go.sum')) return 'go';
      if (lower.endsWith('makefile')) return 'makefile';
      return 'plaintext';
  }
}

export function formatFileSize(bytes?: number): string {
  if (bytes === undefined || bytes === null || isNaN(bytes)) return '';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export const CodeInspectionModal: React.FC<CodeInspectionModalProps> = ({
  isOpen,
  onClose,
  runId,
  initialFile,
  files: propFiles,
  onSaveSuccess,
}) => {
  // Normalize initial files from props
  const normalizedPropFiles = useMemo<SandboxFileInfo[]>(() => {
    if (!propFiles || propFiles.length === 0) return [];
    return propFiles.map((f) => {
      if (typeof f === 'string') {
        return { path: f, size: 0, modified: true };
      }
      return {
        path: f.path,
        size: f.size || 0,
        modified: f.modified ?? true,
      };
    });
  }, [propFiles]);

  const initialTarget = initialFile || normalizedPropFiles[0]?.path || '';

  const [fileList, setFileList] = useState<SandboxFileInfo[]>(() => normalizedPropFiles);
  const [activeFile, setActiveFile] = useState<string>(() => initialTarget);
  const [content, setContent] = useState<string>('');
  const [originalContent, setOriginalContent] = useState<string>('');
  const [searchFilter, setSearchFilter] = useState<string>('');
  const [isLoadingFiles, setIsLoadingFiles] = useState<boolean>(false);
  const [isLoadingContent, setIsLoadingContent] = useState<boolean>(false);
  const [isSaving, setIsSaving] = useState<boolean>(false);
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(null);
  const [openTabs, setOpenTabs] = useState<string[]>(() => (initialTarget ? [initialTarget] : []));

  const isDirty = content !== originalContent;
  const currentLanguage = useMemo(() => detectLanguage(activeFile), [activeFile]);

  // Load sandbox file listing
  const fetchFiles = useCallback(async () => {
    if (!runId) return;
    setIsLoadingFiles(true);
    try {
      const serverFiles = await listSandboxFiles(runId);
      if (serverFiles && serverFiles.length > 0) {
        setFileList(serverFiles);
      } else if (normalizedPropFiles.length > 0) {
        setFileList(normalizedPropFiles);
      }
    } catch {
      // Fallback to prop files if remote list is unavailable
      if (normalizedPropFiles.length > 0) {
        setFileList(normalizedPropFiles);
      }
    } finally {
      setIsLoadingFiles(false);
    }
  }, [runId, normalizedPropFiles]);

  // Initial load when modal opens
  useEffect(() => {
    if (isOpen) {
      setFeedback(null);
      fetchFiles();

      const target = initialFile || (normalizedPropFiles[0]?.path ?? '');
      if (target) {
        setActiveFile(target);
        setOpenTabs((prev) => (prev.includes(target) ? prev : [...prev, target]));
      }
    } else {
      setActiveFile('');
      setContent('');
      setOriginalContent('');
      setOpenTabs([]);
      setFeedback(null);
    }
  }, [isOpen, initialFile, fetchFiles, normalizedPropFiles]);

  // Fetch file content when active file changes
  useEffect(() => {
    if (!isOpen || !activeFile || !runId) return;

    let isMounted = true;
    setIsLoadingContent(true);
    setFeedback(null);

    getSandboxFileContent(runId, activeFile)
      .then((res) => {
        if (isMounted) {
          setContent(res.content);
          setOriginalContent(res.content);
        }
      })
      .catch((err) => {
        if (isMounted) {
          const msg = err instanceof Error ? err.message : String(err);
          setFeedback({
            type: 'error',
            message: `Error al leer archivo: ${msg}`,
          });
          setContent('');
          setOriginalContent('');
        }
      })
      .finally(() => {
        if (isMounted) {
          setIsLoadingContent(false);
        }
      });

    return () => {
      isMounted = false;
    };
  }, [isOpen, activeFile, runId]);

  // Select a file
  const handleSelectFile = (path: string) => {
    if (path === activeFile) return;
    setActiveFile(path);
    setOpenTabs((prev) => (prev.includes(path) ? prev : [...prev, path]));
  };

  // Close tab
  const handleCloseTab = (e: React.MouseEvent, path: string) => {
    e.stopPropagation();
    const remaining = openTabs.filter((t) => t !== path);
    setOpenTabs(remaining);
    if (activeFile === path) {
      if (remaining.length > 0) {
        setActiveFile(remaining[remaining.length - 1]);
      } else {
        setActiveFile('');
        setContent('');
        setOriginalContent('');
      }
    }
  };

  // Save changes handler
  const handleSaveChanges = async () => {
    if (!runId || !activeFile || isSaving) return;

    setIsSaving(true);
    setFeedback(null);

    try {
      const res = await updateSandboxFileContent(runId, activeFile, content);
      setOriginalContent(content);
      setFeedback({
        type: 'success',
        message: '✓ Cambios guardados con éxito. Git diff recalculado en el sandbox.',
      });

      // Mark file as modified locally in sidebar
      setFileList((prev) =>
        prev.map((f) => (f.path === activeFile ? { ...f, modified: true } : f))
      );

      if (onSaveSuccess && res.diff) {
        onSaveSuccess(res.diff, activeFile);
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setFeedback({
        type: 'error',
        message: `Error al guardar cambios: ${msg}`,
      });
    } finally {
      setIsSaving(false);
    }
  };

  // Keyboard shortcut Ctrl+S / Cmd+S
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (!isOpen) return;
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
        e.preventDefault();
        handleSaveChanges();
      }
      if (e.key === 'Escape' && !isDirty) {
        onClose();
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, isDirty, handleSaveChanges, onClose]);

  if (!isOpen) {
    return null;
  }

  // Filtered files for sidebar
  const filteredFiles = fileList.filter((f) =>
    f.path.toLowerCase().includes(searchFilter.toLowerCase().trim())
  );

  return (
    <div
      className="code-inspection-modal-overlay"
      role="dialog"
      aria-modal="true"
      aria-labelledby="code-inspector-title"
    >
      <div className="code-inspection-modal-container">
        {/* Header */}
        <header className="code-inspection-header">
          <div className="code-inspection-title-group">
            <span className="code-inspection-icon">🔍</span>
            <div className="code-inspection-titles">
              <h2 id="code-inspector-title" className="code-inspection-title">
                Monaco Code Inspector &amp; Hot Edit
              </h2>
              <span className="code-inspection-subtitle">
                Sandbox: <code>{runId}</code> • Auto-language detection • Instant Git Amend
              </span>
            </div>
          </div>

          <div className="code-inspection-header-actions">
            {isDirty && (
              <span className="dirty-indicator" title="Cambios sin guardar">
                ● Modificado
              </span>
            )}

            <button
              type="button"
              className="btn-save-changes"
              disabled={isSaving || !activeFile}
              onClick={handleSaveChanges}
              title="Guardar cambios y recalcular git diff (Ctrl+S)"
              aria-label="Guardar Cambios"
            >
              {isSaving ? (
                <>
                  <span className="spinner-mini" /> Guardando...
                </>
              ) : (
                <>💾 Guardar Cambios</>
              )}
            </button>

            <button
              type="button"
              className="btn-close-inspector"
              onClick={onClose}
              title="Cerrar modal (Esc)"
              aria-label="Cerrar Inspector"
            >
              ✕
            </button>
          </div>
        </header>

        {/* Feedback visual banner */}
        {feedback && (
          <div
            className={`code-inspection-feedback-banner feedback-${feedback.type}`}
            role="alert"
          >
            <span className="feedback-icon">
              {feedback.type === 'success' ? '✓' : '⚠️'}
            </span>
            <span className="feedback-message">{feedback.message}</span>
            <button
              type="button"
              className="feedback-dismiss-btn"
              onClick={() => setFeedback(null)}
              aria-label="Cerrar mensaje"
            >
              ✕
            </button>
          </div>
        )}

        {/* Modal Main Area */}
        <div className="code-inspection-body">
          {/* Sidebar */}
          <aside className="code-inspection-sidebar" aria-label="Archivos del Workspace">
            <div className="sidebar-search-box">
              <input
                type="text"
                placeholder="Filtrar archivos..."
                value={searchFilter}
                onChange={(e) => setSearchFilter(e.target.value)}
                className="sidebar-search-input"
                aria-label="Filtrar archivos del sandbox"
              />
              <button
                type="button"
                className="sidebar-refresh-btn"
                onClick={fetchFiles}
                title="Refrescar lista de archivos"
                aria-label="Refrescar archivos"
              >
                ↻
              </button>
            </div>

            <div className="sidebar-file-list" role="list">
              {isLoadingFiles ? (
                <div className="sidebar-loading">Cargando archivos...</div>
              ) : filteredFiles.length === 0 ? (
                <div className="sidebar-empty">No se encontraron archivos</div>
              ) : (
                filteredFiles.map((file) => {
                  const isSelected = file.path === activeFile;
                  return (
                    <button
                      key={file.path}
                      type="button"
                      className={`sidebar-file-item ${isSelected ? 'active' : ''} ${
                        file.modified ? 'file-modified' : ''
                      }`}
                      onClick={() => handleSelectFile(file.path)}
                      title={file.path}
                    >
                      <span className="file-icon">
                        {file.path.endsWith('.go')
                          ? '🐹'
                          : file.path.endsWith('.json')
                          ? '📋'
                          : file.path.endsWith('.md')
                          ? '📝'
                          : file.path.endsWith('.sql')
                          ? '🗄️'
                          : '📄'}
                      </span>
                      <span className="file-name">{file.path}</span>
                      {file.size > 0 && (
                        <span className="file-size-tag">{formatFileSize(file.size)}</span>
                      )}
                      {file.modified && (
                        <span className="modified-pill" title="Archivo modificado">
                          MOD
                        </span>
                      )}
                    </button>
                  );
                })
              )}
            </div>
          </aside>

          {/* Main Editor Center */}
          <main className="code-inspection-editor-pane">
            {/* Tabs */}
            {openTabs.length > 0 && (
              <div className="editor-tabs-bar" role="tablist">
                {openTabs.map((tabPath) => {
                  const isActive = tabPath === activeFile;
                  return (
                    <div
                      key={tabPath}
                      role="tab"
                      aria-selected={isActive}
                      className={`editor-tab ${isActive ? 'tab-active' : ''}`}
                      onClick={() => handleSelectFile(tabPath)}
                    >
                      <span className="tab-label">{tabPath.split('/').pop()}</span>
                      {isActive && isDirty && <span className="tab-dirty-dot">●</span>}
                      <button
                        type="button"
                        className="tab-close-btn"
                        onClick={(e) => handleCloseTab(e, tabPath)}
                        title="Cerrar pestaña"
                      >
                        ✕
                      </button>
                    </div>
                  );
                })}
              </div>
            )}

            {/* Editor Area */}
            <div className="monaco-editor-wrapper">
              {isLoadingContent ? (
                <div className="editor-loading-state">
                  <span className="spinner-large" />
                  <span>Cargando contenido de {activeFile}...</span>
                </div>
              ) : !activeFile ? (
                <div className="editor-empty-state">
                  <span>Selecciona un archivo del sandbox para inspeccionar y editar</span>
                </div>
              ) : (
                <Editor
                  height="100%"
                  theme="vs-dark"
                  language={currentLanguage}
                  value={content}
                  onChange={(val) => setContent(val || '')}
                  options={{
                    minimap: { enabled: true },
                    wordWrap: 'on',
                    fontSize: 13,
                    fontFamily: "'JetBrains Mono', 'Fira Code', Consolas, monospace",
                    lineNumbers: 'on',
                    scrollBeyondLastLine: false,
                    automaticLayout: true,
                    tabSize: 2,
                    renderWhitespace: 'selection',
                    bracketPairColorization: { enabled: true },
                  }}
                />
              )}
            </div>

            {/* Footer Status Bar */}
            <footer className="editor-status-bar">
              <div className="status-bar-left">
                <span className="status-pill status-lang">
                  Lenguaje: <strong>{currentLanguage}</strong>
                </span>
                {activeFile && (
                  <span className="status-pill status-path">
                    Ruta: <code>{activeFile}</code>
                  </span>
                )}
              </div>
              <div className="status-bar-right">
                <span className="status-hint">Atajo: [Ctrl+S] para guardar cambios</span>
              </div>
            </footer>
          </main>
        </div>
      </div>
    </div>
  );
};
