// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect } from 'vitest';
import {
  formatDuration,
  getStatusBadgeClass,
  getSandboxBadgeClass,
  getSandboxBadgeLabel,
  extractStepSandboxTelemetry,
} from './StepExecutionViewer';
import type { RunStep } from '../../types/worker';

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

describe('getSandboxBadgeClass pure helper', () => {
  it('maps sandbox modes to corresponding CSS styling classes', () => {
    expect(getSandboxBadgeClass('oci')).toBe('step-sandbox-oci');
    expect(getSandboxBadgeClass('microvm')).toBe('step-sandbox-microvm');
    expect(getSandboxBadgeClass('host')).toBe('step-sandbox-host');
    expect(getSandboxBadgeClass('auto')).toBe('step-sandbox-auto');
    expect(getSandboxBadgeClass(undefined)).toBe('step-sandbox-none');
    expect(getSandboxBadgeClass('unknown-mode')).toBe('step-sandbox-none');
  });
});

describe('getSandboxBadgeLabel pure helper', () => {
  it('returns human-readable labels for sandbox runtimes', () => {
    expect(getSandboxBadgeLabel('oci')).toBe('OCI Container');
    expect(getSandboxBadgeLabel('microvm')).toBe('MicroVM');
    expect(getSandboxBadgeLabel('host')).toBe('Host In-Process');
    expect(getSandboxBadgeLabel('auto')).toBe('Auto Sandbox');
    expect(getSandboxBadgeLabel(undefined)).toBe('');
  });
});

describe('extractStepSandboxTelemetry helper', () => {
  it('extracts top-level and output telemetry correctly for OCI container step', () => {
    const ociStep: RunStep = {
      id: 'step-oci-1',
      run_id: 'run-1',
      step_name: 'builder_tdd',
      status: 'COMPLETED',
      duration_ms: 3200,
      created_at: '2026-11-01T10:00:00Z',
      sandbox_mode: 'oci',
      container_id: 'c7f93a1290bd847291a0c8b67e543210fe9876543210abcedf0123456789abcd',
      cgroups_enforced: true,
      runtime_engine: 'podman',
      resource_limits: {
        cpu: '2.0',
        memory: '1024m',
        pids: 256,
      },
      output: {
        network_policy: 'none',
        security_level: 'strict',
      },
    };

    const telemetry = extractStepSandboxTelemetry(ociStep);
    expect(telemetry).not.toBeNull();
    expect(telemetry?.sandbox_mode).toBe('oci');
    expect(telemetry?.container_id).toBe('c7f93a1290bd847291a0c8b67e543210fe9876543210abcedf0123456789abcd');
    expect(telemetry?.cgroups_enforced).toBe(true);
    expect(telemetry?.runtime_engine).toBe('podman');
    expect(telemetry?.resource_limits?.cpu).toBe('2.0');
    expect(telemetry?.resource_limits?.memory).toBe('1024m');
    expect(telemetry?.resource_limits?.pids).toBe(256);
    expect(telemetry?.network_policy).toBe('none');
    expect(telemetry?.security_level).toBe('strict');
  });

  it('extracts telemetry embedded entirely in step output', () => {
    const outputStep: RunStep = {
      id: 'step-out-1',
      run_id: 'run-1',
      step_name: 'wasm_plugin',
      status: 'COMPLETED',
      duration_ms: 450,
      created_at: '2026-11-01T10:05:00Z',
      output: {
        sandbox_mode: 'microvm',
        container_id: 'vm-isolated-8410',
        cgroups_enforced: true,
        runtime_engine: 'cloud-hypervisor',
        resource_limits: {
          cpu: '1.0',
          memory: '512m',
        },
        network_policy: 'bridge',
      },
    };

    const telemetry = extractStepSandboxTelemetry(outputStep);
    expect(telemetry).not.toBeNull();
    expect(telemetry?.sandbox_mode).toBe('microvm');
    expect(telemetry?.container_id).toBe('vm-isolated-8410');
    expect(telemetry?.runtime_engine).toBe('cloud-hypervisor');
    expect(telemetry?.cgroups_enforced).toBe(true);
    expect(telemetry?.resource_limits?.cpu).toBe('1.0');
    expect(telemetry?.network_policy).toBe('bridge');
  });

  it('returns null when step has no sandbox or security telemetry', () => {
    const plainStep: RunStep = {
      id: 'step-plain',
      run_id: 'run-1',
      step_name: 'git_checkout',
      status: 'COMPLETED',
      duration_ms: 120,
      created_at: '2026-11-01T09:59:00Z',
      output: { commit: 'abcdef' },
    };

    const telemetry = extractStepSandboxTelemetry(plainStep);
    expect(telemetry).toBeNull();
  });
});
