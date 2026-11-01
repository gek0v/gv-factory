// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package version

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestREUSEComplianceDep5AndJsonc(t *testing.T) {
	// 1. Validar .reuse/dep5
	dep5Path := filepath.Join("..", "..", ".reuse", "dep5")
	dep5Content, err := os.ReadFile(dep5Path)
	if err != nil {
		t.Fatalf("error leyendo .reuse/dep5: %v", err)
	}

	if !strings.Contains(string(dep5Content), "*.jsonc") || !strings.Contains(string(dep5Content), "**/*.jsonc") {
		t.Errorf(".reuse/dep5 debe contener soporte explícito para '*.jsonc' y '**/*.jsonc'")
	}

	// 2. Validar cabeceras en opencode.jsonc
	jsoncPath := filepath.Join("..", "..", "opencode.jsonc")
	jsoncContent, err := os.ReadFile(jsoncPath)
	if err != nil {
		t.Fatalf("error leyendo opencode.jsonc: %v", err)
	}

	// REUSE-IgnoreStart
	spdxIdent := "SPDX-License-Identifier" + ": " + "Apache-2.0"
	spdxCopy := "SPDX-FileCopyrightText" + ": 2026 gv-factory contributors"
	// REUSE-IgnoreEnd

	strJsonc := string(jsoncContent)
	if !strings.Contains(strJsonc, spdxIdent) {
		t.Errorf("opencode.jsonc no posee la etiqueta de identificador Apache-2.0")
	}
	if !strings.Contains(strJsonc, spdxCopy) {
		t.Errorf("opencode.jsonc no posee la etiqueta de copyright")
	}
}

func TestDocumentationSPDXFormatting(t *testing.T) {
	plansDir := filepath.Join("..", "..", "docs", "superpowers", "plans")
	entries, err := os.ReadDir(plansDir)
	if err != nil {
		t.Fatalf("error leyendo directorio de planes: %v", err)
	}

	// Regex que detecta etiquetas SPDX con comilla simple o backtick en línea inline
	// REUSE-IgnoreStart
	inlineSpdxRegex := regexp.MustCompile("`[^`]*" + "SPDX-License-Identifier:" + "[^`]+`")
	// REUSE-IgnoreEnd

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(plansDir, entry.Name())
		file, err := os.Open(path)
		if err != nil {
			t.Fatalf("error abriendo %s: %v", path, err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if inlineSpdxRegex.MatchString(line) {
				t.Errorf("%s:%d contiene tag SPDX envuelto en inline code backtick provocando parsing error en REUSE: %q", entry.Name(), lineNum, line)
			}
		}
	}
}

func TestDependencyVulnerabilitiesGoMod(t *testing.T) {
	goModPath := filepath.Join("..", "..", "go.mod")
	content, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("error leyendo go.mod: %v", err)
	}

	re := regexp.MustCompile(`golang\.org/x/text\s+v([0-9]+\.[0-9]+\.[0-9]+)`)
	matches := re.FindStringSubmatch(string(content))
	if len(matches) < 2 {
		t.Fatalf("no se encontró dependencia golang.org/x/text en go.mod")
	}

	version := matches[1]
	// Verificar que no sea v0.21.0 y sea >= v0.34.0 (versión no vulnerable a GO-2026-6629 compatible con Go 1.24)
	if version == "0.21.0" || strings.Compare(version, "0.34.0") < 0 {
		t.Errorf("golang.org/x/text en versión vulnerable %s (GO-2026-6629). Se requiere >= v0.34.0", version)
	}
}
