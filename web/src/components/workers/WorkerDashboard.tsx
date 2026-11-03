// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useEffect, useCallback } from 'react';
import { useFactoryFlowStore } from '../../store';
import { fetchWorkerPool } from '../../api/workers';
import type { WorkerStatus } from '../../types/worker';
import { ServerIcon, RefreshCwIcon, XIcon } from '../icons/Icons';

export function formatUptime(uptimeSeconds?: number): string {
  if (uptimeSeconds === undefined || uptimeSeconds === null || isNaN(uptimeSeconds) || uptimeSeconds < 0) {
    return '0s';
  }
  if (uptimeSeconds < 60) {
    return `${Math.floor(uptimeSeconds)}s`;
  }
  const minutes = Math.floor(uptimeSeconds / 60);
  if (minutes < 60) {
    return `${minutes}m ${Math.floor(uptimeSeconds % 60)}s`;
  }
  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  return `${hours}h ${remainingMinutes}m`;
}

export function getWorkerStatusBadgeClass(status: WorkerStatus): string {
  switch (status) {
    case 'ready':
      return 'worker-status-ready';
    case 'busy':
      return 'worker-status-busy';
    case 'offline':
    default:
      return 'worker-status-offline';
  }
}

export const WorkerClusterHealthButton: React.FC = () => {
  const { workerPool, setShowWorkerDashboard } = useFactoryFlowStore();

  const totalWorkers = workerPool?.total_workers ?? 0;
  const activeWorkers = workerPool?.active_workers ?? 0;
  const isHealthy = activeWorkers > 0 && activeWorkers === totalWorkers;
  const hasOffline = totalWorkers > 0 && activeWorkers < totalWorkers;

  return (
    <button
      type="button"
      className="cluster-health-btn"
      onClick={() => setShowWorkerDashboard(true)}
      title="Open Worker Cluster Health Dashboard"
      aria-label="Worker Cluster Monitor"
    >
      <ServerIcon size={14} className="cluster-health-icon" />
      <span className="cluster-health-text">
        {totalWorkers > 0 ? `${activeWorkers}/${totalWorkers}` : 'Workers'}
      </span>
      <span
        className={`cluster-health-dot ${
          isHealthy ? 'dot-healthy' : hasOffline ? 'dot-warning' : 'dot-standby'
        }`}
      />
    </button>
  );
};

export const WorkerDashboardModal: React.FC = () => {
  const { showWorkerDashboard, setShowWorkerDashboard, workerPool, setWorkerPool } =
    useFactoryFlowStore();

  const loadWorkers = useCallback(async () => {
    try {
      const data = await fetchWorkerPool();
      setWorkerPool(data);
    } catch (err) {
      console.warn('[WorkerDashboard] Polling error:', err);
    }
  }, [setWorkerPool]);

  // Initial load and periodic polling every 5s while dashboard modal is open
  useEffect(() => {
    if (!showWorkerDashboard) return;

    loadWorkers();
    const interval = setInterval(loadWorkers, 5000);
    return () => clearInterval(interval);
  }, [showWorkerDashboard, loadWorkers]);

  if (!showWorkerDashboard) {
    return null;
  }

  const totalWorkers = workerPool?.total_workers ?? 0;
  const activeWorkers = workerPool?.active_workers ?? 0;
  const totalCapacity = workerPool?.total_capacity ?? 0;
  const totalActiveTasks = workerPool?.total_active_tasks ?? 0;
  const workers = workerPool?.workers ?? [];

  return (
    <div
      className="worker-modal-backdrop"
      onClick={() => setShowWorkerDashboard(false)}
      role="dialog"
      aria-modal="true"
      aria-label="Worker Cluster Monitor"
    >
      <div className="worker-modal-content" onClick={(e) => e.stopPropagation()}>
        {/* Header */}
        <div className="worker-modal-header">
          <div className="worker-header-left">
            <ServerIcon size={18} className="worker-title-icon" />
            <div>
              <h3 className="worker-modal-title">Distributed Worker Cluster</h3>
              <p className="worker-modal-subtitle">
                Real-time NATS task queue worker pool, capacity &amp; telemetry
              </p>
            </div>
          </div>
          <div className="worker-header-right">
            <button
              type="button"
              className="worker-refresh-btn"
              onClick={loadWorkers}
              title="Refresh Worker Pool"
            >
              <RefreshCwIcon size={14} />
              <span>Refresh</span>
            </button>
            <button
              type="button"
              className="worker-close-btn"
              onClick={() => setShowWorkerDashboard(false)}
              title="Close Dashboard"
            >
              <XIcon size={16} />
            </button>
          </div>
        </div>

        {/* Global KPI Metrics Cards */}
        <div className="worker-kpi-grid">
          <div className="worker-kpi-card">
            <div className="kpi-label">Total Workers</div>
            <div className="kpi-value">{totalWorkers}</div>
            <div className="kpi-meta">Discovered nodes</div>
          </div>

          <div className="worker-kpi-card">
            <div className="kpi-label">Active Workers</div>
            <div className="kpi-value text-success">{activeWorkers}</div>
            <div className="kpi-meta">Ready or processing</div>
          </div>

          <div className="worker-kpi-card">
            <div className="kpi-label">Total Capacity</div>
            <div className="kpi-value">{totalCapacity}</div>
            <div className="kpi-meta">Max concurrent slots</div>
          </div>

          <div className="worker-kpi-card">
            <div className="kpi-label">Active Tasks</div>
            <div className="kpi-value text-accent">{totalActiveTasks}</div>
            <div className="kpi-meta">In-flight step workloads</div>
          </div>
        </div>

        {/* Workers Table */}
        <div className="worker-table-container">
          <table className="worker-table">
            <thead>
              <tr>
                <th>Worker ID</th>
                <th>Status</th>
                <th>Utilization</th>
                <th>Uptime</th>
                <th>Last Heartbeat</th>
              </tr>
            </thead>
            <tbody>
              {workers.length === 0 ? (
                <tr>
                  <td colSpan={5} className="worker-table-empty">
                    No distributed workers registered in the cluster yet.
                  </td>
                </tr>
              ) : (
                workers.map((w) => {
                  const maxSlots = w.max_concurrency > 0 ? w.max_concurrency : 1;
                  const ratio = Math.min(w.active_tasks / maxSlots, 1);
                  const percent = Math.round(ratio * 100);

                  return (
                    <tr key={w.worker_id} className="worker-row">
                      <td className="worker-id-col">
                        <span className="worker-id-badge mono">{w.worker_id}</span>
                      </td>
                      <td>
                        <span className={`worker-status-badge ${getWorkerStatusBadgeClass(w.status)}`}>
                          {w.status.toUpperCase()}
                        </span>
                      </td>
                      <td className="worker-utilization-col">
                        <div className="utilization-bar-wrapper">
                          <div className="utilization-bar-bg">
                            <div
                              className={`utilization-bar-fill ${
                                ratio >= 1 ? 'fill-full' : ratio > 0.5 ? 'fill-busy' : 'fill-normal'
                              }`}
                              style={{ width: `${percent}%` }}
                            />
                          </div>
                          <span className="utilization-text">
                            {w.active_tasks}/{w.max_concurrency} ({percent}%)
                          </span>
                        </div>
                      </td>
                      <td className="worker-uptime-col">{formatUptime(w.uptime_seconds)}</td>
                      <td className="worker-heartbeat-col">
                        {w.last_heartbeat ? new Date(w.last_heartbeat).toLocaleTimeString() : 'Just now'}
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
};
