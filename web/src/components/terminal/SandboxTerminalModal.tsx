// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useEffect, useRef, useState, useCallback } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { useFactoryFlowStore } from '../../store';

export type TerminalConnectionStatus = 'connecting' | 'online' | 'offline' | 'error';

export interface SandboxTerminalModalProps {
  isOpen?: boolean;
  onClose?: () => void;
  runId?: string;
  wsUrl?: string;
  onClear?: () => void;
  onFit?: () => void;
  onSendInput?: (data: string) => void;
}

/**
 * Resolves the WebSocket URL for the target sandbox terminal.
 */
export function buildTerminalWebSocketUrl(runId: string, wsUrlOverride?: string): string {
  if (wsUrlOverride) {
    return wsUrlOverride;
  }
  if (typeof window === 'undefined') {
    return `ws://localhost:8080/api/v1/sandboxes/${encodeURIComponent(runId)}/terminal`;
  }
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const host = window.location.host;

  // In Vite dev mode (ports 5173 / 3000), redirect to default backend port 8080
  if (host.includes(':5173') || host.includes(':3000')) {
    const hostname = window.location.hostname || 'localhost';
    return `${protocol}//${hostname}:8080/api/v1/sandboxes/${encodeURIComponent(runId)}/terminal`;
  }
  return `${protocol}//${host}/api/v1/sandboxes/${encodeURIComponent(runId)}/terminal`;
}

export const SandboxTerminalModal: React.FC<SandboxTerminalModalProps> = ({
  isOpen,
  onClose,
  runId: initialRunId,
  wsUrl: wsUrlOverride,
  onClear,
  onFit,
  onSendInput,
}) => {
  const store = useFactoryFlowStore();
  const storeOpen = typeof window === 'undefined' ? useFactoryFlowStore.getState().showTerminalModal : store.showTerminalModal;
  const isModalOpen = isOpen !== undefined ? isOpen : storeOpen;

  const defaultRun = initialRunId || store.activeRunId || 'default-run';
  const [selectedRunId, setSelectedRunId] = useState<string>(defaultRun);
  const [runInput, setRunInput] = useState<string>(defaultRun);
  const [connectionStatus, setConnectionStatus] = useState<TerminalConnectionStatus>('connecting');
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const terminalRef = useRef<HTMLDivElement>(null);
  const xtermInstance = useRef<Terminal | null>(null);
  const fitAddonInstance = useRef<FitAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);

  const handleClose = useCallback(() => {
    if (onClose) {
      onClose();
    } else {
      useFactoryFlowStore.getState().setShowTerminalModal(false);
    }
  }, [onClose]);

  // Clean terminal screen
  const handleClear = useCallback(() => {
    if (xtermInstance.current) {
      xtermInstance.current.clear();
    }
    if (onClear) {
      onClear();
    }
  }, [onClear]);

  // Refit terminal dimensions
  const handleFit = useCallback(() => {
    if (fitAddonInstance.current) {
      try {
        fitAddonInstance.current.fit();
      } catch (err) {
        // Ignored if dimensions not rendered yet
      }
    }
    if (onFit) {
      onFit();
    }
  }, [onFit]);

  // Connect WebSocket and initialize xterm
  const connectWebSocket = useCallback((targetRunId: string) => {
    if (typeof window === 'undefined') return;

    if (socketRef.current) {
      try {
        socketRef.current.close();
      } catch (e) {
        // Ignore close errors
      }
      socketRef.current = null;
    }

    setConnectionStatus('connecting');
    setErrorMessage(null);

    const wsUrl = buildTerminalWebSocketUrl(targetRunId, wsUrlOverride);

    try {
      const ws = new WebSocket(wsUrl);
      socketRef.current = ws;

      ws.onopen = () => {
        setConnectionStatus('online');
        setErrorMessage(null);
      };

      ws.onmessage = (event) => {
        if (xtermInstance.current && typeof event.data === 'string') {
          xtermInstance.current.write(event.data);
        }
      };

      ws.onerror = () => {
        setConnectionStatus('error');
        setErrorMessage('WebSocket connection error');
      };

      ws.onclose = () => {
        setConnectionStatus('offline');
      };
    } catch (err: any) {
      setConnectionStatus('error');
      setErrorMessage(err?.message || 'Failed to establish WebSocket');
    }
  }, [wsUrlOverride]);

  // Initialize xterm instance when modal opens
  useEffect(() => {
    if (!isModalOpen || typeof window === 'undefined') {
      return;
    }

    const term = new Terminal({
      theme: {
        background: '#0f172a',
        foreground: '#e2e8f0',
        cursor: '#22c55e',
        cursorAccent: '#0f172a',
        selectionBackground: '#334155',
        black: '#0f172a',
        red: '#ef4444',
        green: '#22c55e',
        yellow: '#eab308',
        blue: '#3b82f6',
        magenta: '#a855f7',
        cyan: '#06b6d4',
        white: '#f8fafc',
        brightBlack: '#64748b',
        brightRed: '#f87171',
        brightGreen: '#4ade80',
        brightYellow: '#fde047',
        brightBlue: '#60a5fa',
        brightMagenta: '#c084fc',
        brightCyan: '#22d3ee',
        brightWhite: '#ffffff',
      },
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
      fontSize: 13,
      lineHeight: 1.25,
      cursorBlink: true,
      convertEol: true,
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);

    xtermInstance.current = term;
    fitAddonInstance.current = fitAddon;

    if (terminalRef.current) {
      term.open(terminalRef.current);
      try {
        fitAddon.fit();
      } catch (e) {
        // Ignored
      }
    }

    // Bidirectional input: send typed data to WebSocket
    const onDataDisposable = term.onData((data) => {
      if (onSendInput) {
        onSendInput(data);
      }
      if (socketRef.current && socketRef.current.readyState === WebSocket.OPEN) {
        socketRef.current.send(data);
      }
    });

    // Auto fit on window resize
    const handleResize = () => {
      try {
        fitAddon.fit();
      } catch (e) {
        // Ignored
      }
    };
    window.addEventListener('resize', handleResize);

    // Connect WebSocket
    connectWebSocket(selectedRunId);

    return () => {
      window.removeEventListener('resize', handleResize);
      onDataDisposable.dispose();
      term.dispose();
      xtermInstance.current = null;
      fitAddonInstance.current = null;
      if (socketRef.current) {
        try {
          socketRef.current.close();
        } catch (e) {
          // Ignored
        }
        socketRef.current = null;
      }
    };
  }, [isModalOpen, selectedRunId, connectWebSocket, onSendInput]);

  if (!isModalOpen) {
    return null;
  }

  const handleRunSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = runInput.trim();
    if (trimmed && trimmed !== selectedRunId) {
      setSelectedRunId(trimmed);
      if (xtermInstance.current) {
        xtermInstance.current.clear();
      }
    }
  };

  return (
    <div className="sandbox-terminal-modal-backdrop" data-testid="terminal-modal-backdrop">
      <div className="sandbox-terminal-modal-content" role="dialog" aria-modal="true" aria-labelledby="terminal-title">
        {/* Toolbar Header */}
        <div className="sandbox-terminal-toolbar">
          <div className="sandbox-terminal-title-group">
            <span className="sandbox-terminal-icon">💻</span>
            <div>
              <h3 id="terminal-title" className="sandbox-terminal-title">
                Sandbox Terminal
              </h3>
              <span className="sandbox-terminal-run-badge">
                Sandbox: <strong>{selectedRunId}</strong>
              </span>
            </div>
          </div>

          {/* Connection status indicator */}
          <div className={`sandbox-terminal-status-badge status-${connectionStatus}`} title={`Status: ${connectionStatus}`}>
            <span className={`terminal-status-dot dot-${connectionStatus}`} />
            <span className="terminal-status-text">
              {connectionStatus === 'online'
                ? 'Online'
                : connectionStatus === 'connecting'
                ? 'Connecting...'
                : connectionStatus === 'error'
                ? 'Error'
                : 'Offline'}
            </span>
          </div>

          {/* Run selector form */}
          <form className="sandbox-terminal-run-form" onSubmit={handleRunSubmit}>
            <input
              type="text"
              className="sandbox-terminal-run-input"
              value={runInput}
              onChange={(e) => setRunInput(e.target.value)}
              placeholder="Run / Sandbox ID"
              aria-label="Target Sandbox ID"
            />
            <button
              type="submit"
              className="sandbox-terminal-run-btn"
              disabled={!runInput.trim() || runInput.trim() === selectedRunId}
              title="Switch active sandbox"
            >
              Switch
            </button>
          </form>

          {/* Action buttons */}
          <div className="sandbox-terminal-actions">
            {connectionStatus !== 'online' && (
              <button
                type="button"
                className="terminal-btn terminal-btn-reconnect"
                onClick={() => connectWebSocket(selectedRunId)}
                title="Reconnect terminal WebSocket"
              >
                Reconnect
              </button>
            )}
            <button
              type="button"
              className="terminal-btn terminal-btn-clear"
              onClick={handleClear}
              title="Clear terminal buffer"
            >
              Limpiar
            </button>
            <button
              type="button"
              className="terminal-btn terminal-btn-fit"
              onClick={handleFit}
              title="Fit terminal size to window"
            >
              Ajustar
            </button>
            <button
              type="button"
              className="terminal-btn terminal-btn-close"
              onClick={handleClose}
              title="Close terminal"
              aria-label="Close Terminal"
            >
              ✕ Cerrar
            </button>
          </div>
        </div>

        {/* Error message banner */}
        {errorMessage && (
          <div className="sandbox-terminal-error-bar">
            <span>⚠️ {errorMessage}</span>
          </div>
        )}

        {/* Terminal viewport */}
        <div
          ref={terminalRef}
          className="sandbox-terminal-container"
          data-testid="terminal-container"
        />
      </div>
    </div>
  );
};

export const SandboxTerminalButton: React.FC<{
  className?: string;
  label?: string;
}> = ({ className = '', label = 'Terminal' }) => {
  const store = useFactoryFlowStore();
  const isOpen = typeof window === 'undefined' ? useFactoryFlowStore.getState().showTerminalModal : store.showTerminalModal;

  return (
    <button
      type="button"
      className={`sandbox-terminal-btn ${className} ${isOpen ? 'active' : ''}`}
      onClick={() => useFactoryFlowStore.getState().setShowTerminalModal(!isOpen)}
      title="Open Sandbox Interactive Terminal"
      aria-label="Toggle Sandbox Terminal"
    >
      <span className="sandbox-terminal-btn-icon">💻</span>
      <span className="sandbox-terminal-btn-label">{label}</span>
    </button>
  );
};
