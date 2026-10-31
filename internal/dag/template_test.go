// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gv-factory/gv-factory/pkg/schema"
)

func TestCanonicalTemplates_ParsingAndValidation(t *testing.T) {
	templates := []string{
		"bugfix-express.yaml",
		"feature-complete.yaml",
		"refactor-doc.yaml",
		"default-tdd-factory.yaml",
	}

	baseDir := filepath.Join("..", "..", "templates", "factories")

	for _, filename := range templates {
		t.Run(filename, func(t *testing.T) {
			path := filepath.Join(baseDir, filename)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read template file %s: %v", path, err)
			}

			tpl, err := ParseTemplateYAML(content)
			if err != nil {
				t.Fatalf("failed to parse template YAML %s: %v", filename, err)
			}

			if tpl.Metadata.Name == "" {
				t.Fatalf("expected non-empty template name in %s", filename)
			}

			issues, err := ValidateTemplate(tpl)
			if err != nil {
				t.Fatalf("expected template %s to be valid, got issues: %v, err: %v", filename, issues, err)
			}

			canvas, err := TemplateToCanvas(tpl)
			if err != nil {
				t.Fatalf("TemplateToCanvas failed for %s: %v", filename, err)
			}

			if len(canvas.Nodes) != len(tpl.Spec.Steps) {
				t.Fatalf("expected %d nodes in canvas, got %d", len(tpl.Spec.Steps), len(canvas.Nodes))
			}

			var entrypointNode *CanvasNode
			for i := range canvas.Nodes {
				if canvas.Nodes[i].ID == tpl.Spec.Entrypoint {
					entrypointNode = &canvas.Nodes[i]
					break
				}
			}
			if entrypointNode == nil {
				t.Fatalf("entrypoint node %q not found in canvas", tpl.Spec.Entrypoint)
			}
			if entrypointNode.Position.X != 80.0 {
				t.Fatalf("expected entrypoint node X=80, got %f", entrypointNode.Position.X)
			}
		})
	}
}

func TestTemplate_RoundtripConversion(t *testing.T) {
	tplYAML := `apiVersion: factory.ov.io/v1alpha1
kind: FactoryTemplate
metadata:
  name: roundtrip-test-pipeline
  version: 1.0.0
  description: Test pipeline for roundtrip serialization
spec:
  entrypoint: step-a
  steps:
    - id: step-a
      name: Step A
      plugin: extism://plugins/step-a.wasm
      config:
        mode: fast
    - id: step-b
      name: Step B
      plugin: extism://plugins/step-b.wasm
      parallel: true
      depends_on:
        - step-a
      config:
        workers: 4
    - id: step-c
      name: Step C
      plugin: extism://plugins/step-c.wasm
      depends_on:
        - step-b
  gates:
    min_coverage_percent: 88.5
    max_security_alerts: 0
    require_approval: true
`

	originalTpl, err := ParseTemplateYAML([]byte(tplYAML))
	if err != nil {
		t.Fatalf("failed to parse initial YAML: %v", err)
	}

	canvas, err := TemplateToCanvas(originalTpl)
	if err != nil {
		t.Fatalf("failed to convert template to canvas: %v", err)
	}

	reconstructedTpl, err := CanvasToTemplate(canvas)
	if err != nil {
		t.Fatalf("failed to convert canvas back to template: %v", err)
	}

	issues, err := ValidateTemplate(reconstructedTpl)
	if err != nil {
		t.Fatalf("reconstructed template validation failed: %v", issues)
	}

	if reconstructedTpl.Metadata.Name != originalTpl.Metadata.Name {
		t.Errorf("expected name %s, got %s", originalTpl.Metadata.Name, reconstructedTpl.Metadata.Name)
	}
	if reconstructedTpl.Spec.Entrypoint != originalTpl.Spec.Entrypoint {
		t.Errorf("expected entrypoint %s, got %s", originalTpl.Spec.Entrypoint, reconstructedTpl.Spec.Entrypoint)
	}
	if reconstructedTpl.Spec.Gates.MinCoveragePercent != 88.5 {
		t.Errorf("expected min coverage 88.5, got %f", reconstructedTpl.Spec.Gates.MinCoveragePercent)
	}
	if len(reconstructedTpl.Spec.Steps) != len(originalTpl.Spec.Steps) {
		t.Fatalf("step count mismatch: expected %d, got %d", len(originalTpl.Spec.Steps), len(reconstructedTpl.Spec.Steps))
	}

	exportedYAML, err := ExportTemplateYAML(reconstructedTpl)
	if err != nil {
		t.Fatalf("failed to export YAML: %v", err)
	}
	if len(exportedYAML) == 0 {
		t.Fatalf("exported YAML is empty")
	}

	exportedJSON, err := ExportTemplateJSON(reconstructedTpl)
	if err != nil {
		t.Fatalf("failed to export JSON: %v", err)
	}
	parsedJSONTpl, err := ParseTemplateJSON(exportedJSON)
	if err != nil {
		t.Fatalf("failed to parse exported JSON: %v", err)
	}
	if parsedJSONTpl.Metadata.Name != originalTpl.Metadata.Name {
		t.Fatalf("JSON roundtrip name mismatch")
	}
}

func TestTemplate_ValidationFailures(t *testing.T) {
	t.Run("cycle detected", func(t *testing.T) {
		tpl := &schema.FactoryTemplate{
			Metadata: schema.Metadata{Name: "cyclic"},
			Spec: schema.FactorySpec{
				Entrypoint: "a",
				Steps: []schema.StepConfig{
					{ID: "a", Name: "A", Plugin: "extism://plugins/a.wasm", DependsOn: []string{"b"}},
					{ID: "b", Name: "B", Plugin: "extism://plugins/b.wasm", DependsOn: []string{"a"}},
				},
				Gates: schema.GatesConfig{MinCoveragePercent: 90.0},
			},
		}
		issues, err := ValidateTemplate(tpl)
		if err == nil {
			t.Fatalf("expected error on cycle, got nil (issues: %v)", issues)
		}
	})

	t.Run("missing entrypoint", func(t *testing.T) {
		tpl := &schema.FactoryTemplate{
			Metadata: schema.Metadata{Name: "missing-entry"},
			Spec: schema.FactorySpec{
				Entrypoint: "non-existent",
				Steps: []schema.StepConfig{
					{ID: "a", Name: "A", Plugin: "extism://plugins/a.wasm"},
				},
			},
		}
		_, err := ValidateTemplate(tpl)
		if err == nil {
			t.Fatalf("expected error for non-existent entrypoint")
		}
	})

	t.Run("invalid plugin URI", func(t *testing.T) {
		tpl := &schema.FactoryTemplate{
			Metadata: schema.Metadata{Name: "bad-plugin"},
			Spec: schema.FactorySpec{
				Entrypoint: "a",
				Steps: []schema.StepConfig{
					{ID: "a", Name: "A", Plugin: "http://malicious.wasm"},
				},
			},
		}
		_, err := ValidateTemplate(tpl)
		if err == nil {
			t.Fatalf("expected error for non-conforming plugin URI")
		}
	})

	t.Run("invalid gates threshold", func(t *testing.T) {
		tpl := &schema.FactoryTemplate{
			Metadata: schema.Metadata{Name: "bad-gates"},
			Spec: schema.FactorySpec{
				Entrypoint: "a",
				Steps: []schema.StepConfig{
					{ID: "a", Name: "A", Plugin: "extism://plugins/a.wasm"},
				},
				Gates: schema.GatesConfig{MinCoveragePercent: 120.0, MaxSecurityAlerts: -1},
			},
		}
		_, err := ValidateTemplate(tpl)
		if err == nil {
			t.Fatalf("expected error for invalid coverage and security alerts")
		}
	})
}
