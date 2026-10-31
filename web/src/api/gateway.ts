// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { useFactoryFlowStore } from '../store';
import type { GatewayEvent, GatewayClientConfig } from './types';
import { dispatchGatewayEvent } from './dispatcher';

export class GatewayClient {
  private url: string;
  private initialReconnectMs: number;
  private maxReconnectMs: number;
  private retryCount = 0;
  private eventSource: EventSource | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private isExplicitlyClosed = false;

  constructor(config?: GatewayClientConfig) {
    this.url = config?.url || '/events';
    this.initialReconnectMs = config?.initialReconnectMs || 1000;
    this.maxReconnectMs = config?.maxReconnectMs || 30000;
  }

  public connect(): void {
    if (this.eventSource) {
      this.disconnect();
    }
    this.isExplicitlyClosed = false;

    const store = useFactoryFlowStore.getState();
    store.setGatewayStatus('connecting');

    try {
      this.eventSource = new EventSource(this.url);

      this.eventSource.onopen = () => {
        this.retryCount = 0;
        store.setGatewayStatus('connected');
      };

      this.eventSource.onmessage = (event) => {
        try {
          const raw = JSON.parse(event.data);
          this.handleEvent(raw);
        } catch (err) {
          console.warn('[GatewayClient] Failed to parse SSE event data:', err);
        }
      };

      this.eventSource.onerror = () => {
        if (this.isExplicitlyClosed) return;
        this.handleDisconnect();
      };
    } catch (err) {
      console.warn('[GatewayClient] Error initializing EventSource:', err);
      this.handleDisconnect();
    }
  }

  public disconnect(): void {
    this.isExplicitlyClosed = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.eventSource) {
      this.eventSource.close();
      this.eventSource = null;
    }
    useFactoryFlowStore.getState().setGatewayStatus('disconnected');
  }

  public getRetryCount(): number {
    return this.retryCount;
  }

  public computeNextDelay(): number {
    return Math.min(
      this.initialReconnectMs * Math.pow(2, this.retryCount),
      this.maxReconnectMs
    );
  }

  private handleDisconnect(): void {
    if (this.eventSource) {
      this.eventSource.close();
      this.eventSource = null;
    }

    const store = useFactoryFlowStore.getState();
    store.setGatewayStatus('reconnecting');

    const delay = this.computeNextDelay();
    this.retryCount++;

    this.reconnectTimer = setTimeout(() => {
      if (!this.isExplicitlyClosed) {
        this.connect();
      }
    }, delay);
  }

  public handleEvent(event: GatewayEvent): void {
    dispatchGatewayEvent(event, useFactoryFlowStore.getState());
  }
}

export const defaultGatewayClient = new GatewayClient();
