// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gv-factory/gv-factory/internal/dag"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

// TemplateSummary holds metadata for displaying templates in CLI.
type TemplateSummary struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Entrypoint  string `json:"entrypoint"`
	StepsCount  int    `json:"steps_count"`
	Description string `json:"description"`
}

// BuiltinTemplateCatalog maps canonical names to their embedded YAML definition.
var BuiltinTemplateCatalog = map[string]string{
	"bugfix-express": `apiVersion: factory.ov.io/v1alpha1
kind: FactoryTemplate
metadata:
  name: bugfix-express
  version: 0.1.0
  description: Lightweight pipeline for log error remediation with automated fast-track to main upon passing tests.
spec:
  entrypoint: error-log-ingestion
  steps:
    - id: error-log-ingestion
      name: Error Log Ingestion
      plugin: extism://plugins/ingestion.wasm
    - id: bug-localizer
      name: Bug Localizer (Trace & AST)
      plugin: extism://plugins/spec-writer.wasm
      depends_on: ["error-log-ingestion"]
    - id: builder-tdd
      name: Fast TDD Patch Runner
      plugin: extism://plugins/builder-tdd.wasm
      depends_on: ["bug-localizer"]
    - id: code-verifier
      name: Automated Test & Lint Verifier
      plugin: extism://plugins/reviewer-code.wasm
      depends_on: ["builder-tdd"]
    - id: fast-track-merger
      name: Fast-Track Merge Gate
      plugin: extism://plugins/approver.wasm
      depends_on: ["code-verifier"]
`,
	"feature-complete": `apiVersion: factory.ov.io/v1alpha1
kind: FactoryTemplate
metadata:
  name: feature-complete
  version: 0.1.0
  description: Comprehensive pipeline with SDD spec-writer, builder-tdd, 4 parallel reviewers, and mandatory human approval gate.
spec:
  entrypoint: ingestion
  steps:
    - id: ingestion
      name: Issue Ingestion & Context Hydration
      plugin: extism://plugins/ingestion.wasm
    - id: spec-writer
      name: Spec Writer (SDD Engine)
      plugin: extism://plugins/spec-writer.wasm
      depends_on: ["ingestion"]
    - id: builder-tdd
      name: Builder Agent (TDD Red-Green-Refactor)
      plugin: extism://plugins/builder-tdd.wasm
      depends_on: ["spec-writer"]
    - id: reviewer-code
      name: Code Quality Reviewer
      plugin: extism://plugins/reviewer-code.wasm
      depends_on: ["builder-tdd"]
    - id: reviewer-security
      name: Security Auditor
      plugin: extism://plugins/reviewer-security.wasm
      depends_on: ["builder-tdd"]
    - id: approver
      name: Approver Consensus Gate
      plugin: extism://plugins/approver.wasm
      depends_on: ["reviewer-code", "reviewer-security"]
`,
	"refactor-doc": `apiVersion: factory.ov.io/v1alpha1
kind: FactoryTemplate
metadata:
  name: refactor-doc
  version: 0.1.0
  description: Non-destructive pipeline focusing on AST complexity analysis, documentation sync, and zero business logic alteration.
spec:
  entrypoint: refactor-trigger
  steps:
    - id: refactor-trigger
      name: Refactoring Scope Ingestion
      plugin: extism://plugins/ingestion.wasm
    - id: ast-analyzer
      name: AST Structural Analyzer
      plugin: extism://plugins/spec-writer.wasm
      depends_on: ["refactor-trigger"]
    - id: doc-generator
      name: Documentation Generator
      plugin: extism://plugins/builder-tdd.wasm
      depends_on: ["ast-analyzer"]
    - id: reviewer-doc
      name: Documentation Verifier
      plugin: extism://plugins/reviewer-code.wasm
      depends_on: ["doc-generator"]
    - id: doc-merger
      name: Fast-Track Doc Merger
      plugin: extism://plugins/approver.wasm
      depends_on: ["reviewer-doc"]
`,
}

// ListTemplates returns a list of all known built-in template summaries.
func ListTemplates() []TemplateSummary {
	var list []TemplateSummary
	for name, yamlData := range BuiltinTemplateCatalog {
		tpl, err := dag.ParseTemplateYAML([]byte(yamlData))
		if err == nil && tpl != nil {
			list = append(list, TemplateSummary{
				Name:        name,
				Version:     tpl.Metadata.Version,
				Entrypoint:  tpl.Spec.Entrypoint,
				StepsCount:  len(tpl.Spec.Steps),
				Description: tpl.Metadata.Description,
			})
		}
	}
	return list
}

// LoadTemplate resolves and parses a factory template from file path or catalog name.
func LoadTemplate(identifier string) (*schema.FactoryTemplate, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		identifier = "bugfix-express"
	}

	data, err := readTemplateData(identifier)
	if err != nil {
		return nil, err
	}

	tpl, err := dag.ParseTemplateYAML(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template %q: %w", identifier, err)
	}

	issues, err := dag.ValidateTemplate(tpl)
	if err != nil {
		return nil, fmt.Errorf("invalid template %q: %w (issues: %s)", identifier, err, strings.Join(issues, "; "))
	}

	return tpl, nil
}

func readTemplateData(identifier string) ([]byte, error) {
	// 1. Direct file path check
	if _, err := os.Stat(identifier); err == nil {
		return os.ReadFile(identifier)
	}

	// 2. Relative check in templates/factories/
	factoriesPath := filepath.Join("templates", "factories", identifier)
	if !strings.HasSuffix(factoriesPath, ".yaml") && !strings.HasSuffix(factoriesPath, ".yml") {
		factoriesPath += ".yaml"
	}
	if _, err := os.Stat(factoriesPath); err == nil {
		return os.ReadFile(factoriesPath)
	}

	// 3. Builtin Catalog lookup by name
	normalized := strings.TrimSuffix(identifier, ".yaml")
	normalized = strings.TrimSuffix(normalized, ".yml")
	normalized = filepath.Base(normalized)

	if yamlStr, ok := BuiltinTemplateCatalog[normalized]; ok {
		return []byte(yamlStr), nil
	}

	return nil, fmt.Errorf("template %q not found on disk or builtin catalog", identifier)
}
