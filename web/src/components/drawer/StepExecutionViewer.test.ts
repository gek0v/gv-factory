// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect } from 'vitest';
import { formatDuration, getStatusBadgeClass } from './StepExecutionViewer';

describe('formatDuration pure helper', () => {
  it('formats milliseconds, seconds, and minutes correctly', () => {
    expect(formatDuration(undefined)).toBe('—');
    expect(formatDuration(null as unknown as number)).toBe('—');
    expect(formatDuration(85)).toBe('85ms');
    expect(formatDuration(999)).toBe('999ms');
    expect(formatDuration(1400)).toBe('1.4s');
    expect(formatDuration(12500)).toBe('12.5s');
    expect(formatDuration(60000)).toBe('1m 0s');
    expect(formatDuration(130000)).toBe('2m 10s');
  });
});

describe('getStatusBadgeClass pure helper', () => {
  it('maps RunStepStatus to CSS classes', () => {
    expect(getStatusBadgeClass('COMPLETED')).toBe('step-badge-completed');
    expect(getStatusBadgeClass('RUNNING')).toBe('step-badge-running');
    expect(getStatusBadgeClass('FAILED')).toBe('step-badge-failed');
    expect(getStatusBadgeClass('SKIPPED')).toBe('step-badge-skipped');
    expect(getStatusBadgeClass('PENDING')).toBe('step-badge-pending');
  });
});
