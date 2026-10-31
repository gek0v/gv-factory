// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package schema

// FactoryTemplate defines the structure of a multi-agent software factory pipeline.
type FactoryTemplate struct {
	APIVersion string       `json:"apiVersion" yaml:"apiVersion"`
	Kind       string       `json:"kind" yaml:"kind"`
	Metadata   Metadata     `json:"metadata" yaml:"metadata"`
	Spec       FactorySpec  `json:"spec" yaml:"spec"`
}

// Metadata holds naming and versioning info for the template.
type Metadata struct {
	Name        string            `json:"name" yaml:"name"`
	Version     string            `json:"version" yaml:"version"`
	Description string            `json:"description" yaml:"description"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// FactorySpec describes pipeline steps, reviewers, thresholds, and routing gates.
type FactorySpec struct {
	Entrypoint string       `json:"entrypoint" yaml:"entrypoint"`
	Steps      []StepConfig `json:"steps" yaml:"steps"`
	Gates      GatesConfig  `json:"gates" yaml:"gates"`
}

// StepConfig defines execution details of an individual agent/plugin node.
type StepConfig struct {
	ID        string            `json:"id" yaml:"id"`
	Name      string            `json:"name" yaml:"name"`
	Plugin    string            `json:"plugin" yaml:"plugin"`
	Parallel  bool              `json:"parallel,omitempty" yaml:"parallel,omitempty"`
	DependsOn []string          `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	Config    map[string]any    `json:"config,omitempty" yaml:"config,omitempty"`
}

// GatesConfig configures criteria for human review versus automated fast-track to main.
type GatesConfig struct {
	MinCoveragePercent float64 `json:"min_coverage_percent" yaml:"min_coverage_percent"`
	MaxSecurityAlerts  int     `json:"max_security_alerts" yaml:"max_security_alerts"`
	RequireApproval    bool    `json:"require_approval" yaml:"require_approval"`
}
