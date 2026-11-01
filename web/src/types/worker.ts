// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

export type RunStepStatus =
  | 'PENDING'
  | 'RUNNING'
  | 'COMPLETED'
  | 'FAILED'
  | 'SKIPPED';

export interface RunStep {
  id: string;
  run_id: string;
  step_name: string;
  status: RunStepStatus;
  duration_ms: number;
  error_message?: string;
  output?: Record<string, unknown>;
  created_at: string;
}

export type WorkerStatus = 'ready' | 'busy' | 'offline';

export interface WorkerInfo {
  worker_id: string;
  status: WorkerStatus;
  active_tasks: number;
  max_concurrency: number;
  uptime_seconds: number;
  last_heartbeat?: string;
  timestamp?: number;
}

export interface WorkerPoolResponse {
  workers: WorkerInfo[];
  total_workers: number;
  active_workers: number;
  total_capacity: number;
  total_active_tasks: number;
}

export interface WorkerHeartbeatEvent {
  worker_id: string;
  active_tasks: number;
  max_concurrency: number;
  uptime_seconds: number;
  timestamp: number;
  status?: WorkerStatus;
}
