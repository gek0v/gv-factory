// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect } from 'vitest';
import { formatUptime, getWorkerStatusBadgeClass } from './WorkerDashboard';

describe('formatUptime pure helper', () => {
  it('formats uptime seconds into readable strings', () => {
    expect(formatUptime(undefined)).toBe('0s');
    expect(formatUptime(-1)).toBe('0s');
    expect(formatUptime(0)).toBe('0s');
    expect(formatUptime(45)).toBe('45s');
    expect(formatUptime(90)).toBe('1m 30s');
    expect(formatUptime(3600)).toBe('1h 0m');
    expect(formatUptime(7325)).toBe('2h 2m');
  });
});

describe('getWorkerStatusBadgeClass pure helper', () => {
  it('maps WorkerStatus to CSS classes', () => {
    expect(getWorkerStatusBadgeClass('ready')).toBe('worker-status-ready');
    expect(getWorkerStatusBadgeClass('busy')).toBe('worker-status-busy');
    expect(getWorkerStatusBadgeClass('offline')).toBe('worker-status-offline');
  });
});
