// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseADRContent_MADRStandard(t *testing.T) {
	content := `# ADR 0001: Microkernel Extism WASM para Nodos y Agentes

* **Estado:** Aceptado
* **Fecha:** 2026-10-31
* **Decisores:** Equipo de Arquitectura de gv-factory

## Contexto
En una fábrica de software autónoma multi-agente, diferentes nodos ejecutan tareas heterogéneas.

## Decisión
Adoptar **Extism** como microkernel para todos los nodos funcionales del pipeline:
1. Todos los agentes se compilan como WASM.

## Consecuencias
* **Positivas:** Aislamiento de memoria por diseño.
* **Negativas:** Limitaciones de memoria inicial.
`

	doc, err := ParseADRContent("0001-microkernel-extism-wasm.md", content)
	if err != nil {
		t.Fatalf("unexpected error parsing ADR: %v", err)
	}

	if doc.RuleCode != "ADR-0001" {
		t.Errorf("expected RuleCode ADR-0001, got %q", doc.RuleCode)
	}
	if doc.Number != 1 {
		t.Errorf("expected Number 1, got %d", doc.Number)
	}
	if doc.Title != "Microkernel Extism WASM para Nodos y Agentes" {
		t.Errorf("unexpected Title: %q", doc.Title)
	}
	if doc.Status != ADRStatusAccepted {
		t.Errorf("expected Status %q, got %q", ADRStatusAccepted, doc.Status)
	}
	if doc.Severity != "ERROR" {
		t.Errorf("expected Severity ERROR, got %q", doc.Severity)
	}
	if doc.Date != "2026-10-31" {
		t.Errorf("expected Date 2026-10-31, got %q", doc.Date)
	}
	if doc.Deciders != "Equipo de Arquitectura de gv-factory" {
		t.Errorf("unexpected Deciders: %q", doc.Deciders)
	}
	if doc.Context == "" || doc.Decision == "" || doc.Consequences == "" {
		t.Errorf("expected non-empty sections, got context=%q, decision=%q, consequences=%q", doc.Context, doc.Decision, doc.Consequences)
	}
	if doc.Description == "" {
		t.Error("expected non-empty Description")
	}
}

func TestParseADRContent_NumberedFormat(t *testing.T) {
	content := `# 11. Inspección Granular de Steps y Dashboard de Clúster de Workers en Canvas Web

- **Estado:** Aceptado
- **Fecha:** 2026-11-01
- **Decisores:** Equipo Core gv-factory, Agente Spec-Writer
- **Consulta:** Issue #92

---

## Contexto y Planteamiento del Problema
La plataforma gv-factory coordina la ejecución de fábricas.

## Decisión de Diseño
Se aprueba la implementación nativa en web/.

## Consecuencias
Trazabilidad y depuración inmediata.
`

	doc, err := ParseADRContent("0011-step-inspection-worker-cluster-dashboard.md", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.RuleCode != "ADR-0011" {
		t.Errorf("expected RuleCode ADR-0011, got %q", doc.RuleCode)
	}
	if doc.Number != 11 {
		t.Errorf("expected Number 11, got %d", doc.Number)
	}
	if doc.Title != "Inspección Granular de Steps y Dashboard de Clúster de Workers en Canvas Web" {
		t.Errorf("unexpected Title: %q", doc.Title)
	}
	if doc.Status != ADRStatusAccepted {
		t.Errorf("expected Status Accepted, got %q", doc.Status)
	}
	if doc.Severity != "ERROR" {
		t.Errorf("expected Severity ERROR, got %q", doc.Severity)
	}
}

func TestParseADRContent_StatusVariations(t *testing.T) {
	tests := []struct {
		name         string
		statusLine   string
		expectedStat ADRStatus
		expectedSev  string
	}{
		{"Accepted EN", "* **Status:** Accepted", ADRStatusAccepted, "ERROR"},
		{"Aceptado ES", "* **Estado:** Aceptado", ADRStatusAccepted, "ERROR"},
		{"Aprobado ES", "- **Estado:** Aprobado", ADRStatusAccepted, "ERROR"},
		{"Proposed EN", "* **Status:** Proposed", ADRStatusProposed, "INFO"},
		{"Propuesto ES", "- **Estado:** Propuesto", ADRStatusProposed, "INFO"},
		{"Draft EN", "* **Status:** Draft", ADRStatusProposed, "INFO"},
		{"Superseded EN", "* **Status:** Superseded by ADR-0015", ADRStatusSuperseded, "INFO"},
		{"Superado ES", "- **Estado:** Superado", ADRStatusSuperseded, "INFO"},
		{"Deprecated EN", "* **Status:** Deprecated", ADRStatusDeprecated, "INFO"},
		{"Obsoleto ES", "* **Estado:** Obsoleto", ADRStatusDeprecated, "INFO"},
		{"Rejected EN", "* **Status:** Rejected", ADRStatusRejected, "INFO"},
		{"Rechazado ES", "* **Estado:** Rechazado", ADRStatusRejected, "INFO"},
		{"Unknown", "* **Status:** Under Investigation", ADRStatusUnknown, "INFO"},
		{"Missing status", "", ADRStatusUnknown, "INFO"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content := fmt.Sprintf("# ADR 0005: Sample Architecture\n\n%s\n\n## Context\nSome context.\n## Decision\nSome decision.\n", tc.statusLine)
			doc, err := ParseADRContent("0005-sample.md", content)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if doc.Status != tc.expectedStat {
				t.Errorf("expected status %q, got %q", tc.expectedStat, doc.Status)
			}
			if doc.Severity != tc.expectedSev {
				t.Errorf("expected severity %q, got %q", tc.expectedSev, doc.Severity)
			}
		})
	}
}

func TestParseADRContent_FilenameFallbackAndSPDX(t *testing.T) {
	t.Run("SPDX comments before H1", func(t *testing.T) {
		content := `// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

# ADR 0002: Orquestación Híbrida Hatchet

* **Estado:** Aceptado
`
		doc, err := ParseADRContent("0002-orquestacion-hibrida-hatchet.md", content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if doc.RuleCode != "ADR-0002" {
			t.Errorf("expected ADR-0002, got %q", doc.RuleCode)
		}
		if doc.Title != "Orquestación Híbrida Hatchet" {
			t.Errorf("expected title 'Orquestación Híbrida Hatchet', got %q", doc.Title)
		}
	})

	t.Run("H1 without number uses filename prefix", func(t *testing.T) {
		content := `# Global Distributed Telemetry

* **Estado:** Aceptado

## Context
Context info.
`
		doc, err := ParseADRContent("0009-telemetry.md", content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if doc.RuleCode != "ADR-0009" {
			t.Errorf("expected RuleCode ADR-0009 from filename, got %q", doc.RuleCode)
		}
		if doc.Title != "Global Distributed Telemetry" {
			t.Errorf("unexpected Title: %q", doc.Title)
		}
	})

	t.Run("Filename non-numeric slug", func(t *testing.T) {
		content := `# Security Isolation Guardrails

* **Estado:** Aceptado

## Context
Security context.
`
		doc, err := ParseADRContent("sec-guardrails.md", content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if doc.RuleCode != "ADR-SEC-GUARDRAILS" {
			t.Errorf("expected RuleCode ADR-SEC-GUARDRAILS, got %q", doc.RuleCode)
		}
	})
}

func TestParseADRContent_EdgeCases(t *testing.T) {
	t.Run("empty content returns error", func(t *testing.T) {
		_, err := ParseADRContent("empty.md", "   \n\t  ")
		if err == nil {
			t.Fatal("expected error on empty content, got nil")
		}
	})

	t.Run("missing H1 header returns error", func(t *testing.T) {
		content := `Only paragraph text without headers.
* **Estado:** Aceptado
`
		_, err := ParseADRContent("invalid.md", content)
		if err == nil {
			t.Fatal("expected error on missing H1 header, got nil")
		}
	})

	t.Run("extremely long title truncated for VARCHAR(255)", func(t *testing.T) {
		longTitle := make([]byte, 300)
		for i := range longTitle {
			longTitle[i] = 'A'
		}
		content := fmt.Sprintf("# ADR 0099: %s\n\n* **Estado:** Aceptado\n\n## Context\nTest", string(longTitle))
		doc, err := ParseADRContent("0099-long.md", content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(doc.Title) > 255 {
			t.Errorf("expected Title length <= 255, got %d", len(doc.Title))
		}
	})

	t.Run("content without explicit sections synthesizes description", func(t *testing.T) {
		content := `# ADR 0004: Simple Policy

* **Estado:** Aceptado

This is general text that applies to the entire architecture rule.
It explains the rationale in a unified narrative format.
`
		doc, err := ParseADRContent("0004-simple.md", content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if doc.Description == "" {
			t.Error("expected non-empty synthesized description")
		}
		if doc.Severity != "ERROR" {
			t.Errorf("expected ERROR severity, got %q", doc.Severity)
		}
	})
}

func TestParseADRFile_RealADRs(t *testing.T) {
	adrDir := filepath.Join("..", "..", "docs", "adr")
	entries, err := os.ReadDir(adrDir)
	if err != nil {
		t.Fatalf("could not read docs/adr directory: %v", err)
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" || strings.EqualFold(entry.Name(), "readme.md") {
			continue
		}
		path := filepath.Join(adrDir, entry.Name())
		doc, err := ParseADRFile(path)
		if err != nil {
			t.Fatalf("failed to parse real ADR %s: %v", entry.Name(), err)
		}
		if doc.RuleCode == "" {
			t.Errorf("empty RuleCode for %s", entry.Name())
		}
		if doc.Title == "" {
			t.Errorf("empty Title for %s", entry.Name())
		}
		if doc.Description == "" {
			t.Errorf("empty Description for %s", entry.Name())
		}
		if doc.Severity == "" {
			t.Errorf("empty Severity for %s", entry.Name())
		}
		count++
	}

	if count < 10 {
		t.Errorf("expected to parse at least 10 real ADRs, parsed %d", count)
	}
}
