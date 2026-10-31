// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

export interface GatewayEvent {
  type: string;
  run_id?: string;
  task_id?: string;
  timestamp?: number;
  payload?: Record<string, unknown>;
}

export interface GatewayClientConfig {
  url?: string;
  initialReconnectMs?: number;
  maxReconnectMs?: number;
}
