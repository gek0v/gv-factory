// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

export interface TemplateMetadata {
  name: string;
  version: string;
  description: string;
  labels?: Record<string, string>;
}

export interface StepConfig {
  id: string;
  name: string;
  plugin: string;
  parallel?: boolean;
  depends_on?: string[];
  config?: Record<string, unknown>;
}

export interface GatesConfig {
  min_coverage_percent: number;
  max_security_alerts: number;
  require_approval: boolean;
}

export interface FactorySpec {
  entrypoint: string;
  steps: StepConfig[];
  gates: GatesConfig;
}

export interface FactoryTemplate {
  apiVersion: string;
  kind: string;
  metadata: TemplateMetadata;
  spec: FactorySpec;
}

export interface ValidationResult {
  isValid: boolean;
  errors: string[];
  warnings: string[];
}
