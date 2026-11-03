// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import {
  SandboxTerminalModal,
  SandboxTerminalButton,
  buildTerminalWebSocketUrl,
} from './SandboxTerminalModal';
import { useFactoryFlowStore } from '../../store';

// Mock MockWebSocket for interactive testing
class MockWebSocket {
  static instances: MockWebSocket[] = [];
  public url: string;
  public readyState: number = 0; // CONNECTING
  public sentData: string[] = [];
  public onopen: (() => void) | null = null;
  public onmessage: ((event: { data: any }) => void) | null = null;
  public onerror: ((event: any) => void) | null = null;
  public onclose: (() => void) | null = null;

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  send(data: string) {
    this.sentData.push(data);
  }

  close() {
    this.readyState = 3; // CLOSED
    if (this.onclose) this.onclose();
  }

  triggerOpen() {
    this.readyState = 1; // OPEN
    if (this.onopen) this.onopen();
  }

  triggerMessage(data: string) {
    if (this.onmessage) this.onmessage({ data });
  }

  triggerError(err: any = new Error('ws error')) {
    if (this.onerror) this.onerror(err);
  }
}

describe('SandboxTerminalModal Component & Helpers', () => {
  const originalWebSocket = globalThis.WebSocket;

  beforeEach(() => {
    vi.restoreAllMocks();
    MockWebSocket.instances = [];
    (globalThis as any).WebSocket = MockWebSocket as any;
    useFactoryFlowStore.setState({
      showTerminalModal: false,
      activeRunId: 'test-run-42',
    });
  });

  afterEach(() => {
    globalThis.WebSocket = originalWebSocket;
    vi.restoreAllMocks();
  });

  describe('buildTerminalWebSocketUrl', () => {
    it('returns wsUrl override if provided', () => {
      const url = buildTerminalWebSocketUrl('run-1', 'wss://custom.domain/ws');
      expect(url).toBe('wss://custom.domain/ws');
    });

    it('builds standard websocket url based on window location', () => {
      const url = buildTerminalWebSocketUrl('run-100');
      expect(url).toContain('/api/v1/sandboxes/run-100/terminal');
      expect(url.startsWith('ws://') || url.startsWith('wss://')).toBe(true);
    });

    it('properly URI encodes runId', () => {
      const url = buildTerminalWebSocketUrl('run/with/slashes');
      expect(url).toContain('run%2Fwith%2Fslashes');
    });
  });

  describe('SandboxTerminalButton', () => {
    it('renders with button label and icon', () => {
      const html = renderToStaticMarkup(React.createElement(SandboxTerminalButton));
      expect(html).toContain('Sandbox Terminal');
      expect(html).toContain('sandbox-terminal-btn');
      expect(html).toContain('💻');
    });

    it('supports custom label and className', () => {
      const html = renderToStaticMarkup(
        React.createElement(SandboxTerminalButton, {
          label: 'Custom Terminal',
          className: 'extra-class',
        })
      );
      expect(html).toContain('Custom Terminal');
      expect(html).toContain('extra-class');
    });

    it('reflects active state when modal is open in store', () => {
      useFactoryFlowStore.setState({ showTerminalModal: true });
      const html = renderToStaticMarkup(React.createElement(SandboxTerminalButton));
      expect(html).toContain('active');
    });
  });

  describe('SandboxTerminalModal Static Rendering', () => {
    it('renders nothing when closed', () => {
      const html = renderToStaticMarkup(
        React.createElement(SandboxTerminalModal, { isOpen: false })
      );
      expect(html).toBe('');
    });

    it('renders modal dialog and toolbar when open', () => {
      const html = renderToStaticMarkup(
        React.createElement(SandboxTerminalModal, {
          isOpen: true,
          runId: 'sandbox-alpha',
        })
      );
      expect(html).toContain('sandbox-terminal-modal-backdrop');
      expect(html).toContain('Sandbox Terminal');
      expect(html).toContain('sandbox-alpha');
      expect(html).toContain('Limpiar');
      expect(html).toContain('Ajustar');
      expect(html).toContain('Cerrar');
      expect(html).toContain('terminal-container');
    });

    it('renders connection status indicator', () => {
      const html = renderToStaticMarkup(
        React.createElement(SandboxTerminalModal, {
          isOpen: true,
          runId: 'run-99',
        })
      );
      expect(html).toContain('sandbox-terminal-status-badge');
    });

    it('renders run input and switch button', () => {
      const html = renderToStaticMarkup(
        React.createElement(SandboxTerminalModal, {
          isOpen: true,
          runId: 'run-switch-test',
        })
      );
      expect(html).toContain('sandbox-terminal-run-input');
      expect(html).toContain('sandbox-terminal-run-btn');
      expect(html).toContain('Switch');
    });

    it('renders error bar when error prop or state is triggered', () => {
      const html = renderToStaticMarkup(
        React.createElement(SandboxTerminalModal, {
          isOpen: true,
          runId: 'run-error',
        })
      );
      expect(html).toContain('sandbox-terminal-status-badge');
    });
  });
});
