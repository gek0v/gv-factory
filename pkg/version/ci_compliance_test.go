// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package version

import (
	"bufio"
	"encoding/json"
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

// TestDockerfileWebPnpmPinning valida que Dockerfile.web no utilice pnpm@latest
// y que web/package.json defina packageManager fijado a pnpm v9.
func TestDockerfileWebPnpmPinning(t *testing.T) {
	// 1. Validar build/package/Dockerfile.web
	dockerfilePath := filepath.Join("..", "..", "build", "package", "Dockerfile.web")
	dockerfileBytes, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("error leyendo Dockerfile.web en %q: %v", dockerfilePath, err)
	}
	dockerfileContent := string(dockerfileBytes)

	if strings.Contains(dockerfileContent, "pnpm@latest") {
		t.Errorf("Dockerfile.web no debe utilizar 'pnpm@latest'; causa rotura con lockfile v9.0")
	}

	pnpm9Regex := regexp.MustCompile(`pnpm@9(?:\.[0-9]+(?:\.[0-9]+)?)?`)
	if !pnpm9Regex.MatchString(dockerfileContent) {
		t.Errorf("Dockerfile.web debe fijar explícitamente una versión pnpm v9 (ej. pnpm@9.15.4 o pnpm@9)")
	}

	// 2. Validar web/package.json
	pkgJSONPath := filepath.Join("..", "..", "web", "package.json")
	pkgJSONBytes, err := os.ReadFile(pkgJSONPath)
	if err != nil {
		t.Fatalf("error leyendo web/package.json en %q: %v", pkgJSONPath, err)
	}

	var pkgMeta struct {
		PackageManager string `json:"packageManager"`
	}
	if err := json.Unmarshal(pkgJSONBytes, &pkgMeta); err != nil {
		t.Fatalf("error deserializando web/package.json: %v", err)
	}

	if pkgMeta.PackageManager == "" {
		t.Errorf("web/package.json debe definir el campo 'packageManager'")
	} else if !strings.HasPrefix(pkgMeta.PackageManager, "pnpm@9") {
		t.Errorf("web/package.json 'packageManager' esperado iniciando con 'pnpm@9', obtenido: %q", pkgMeta.PackageManager)
	}
}

// TestReleaseWorkflowAttestProvenanceResilience valida que el paso de atestación SLSA
// en .github/workflows/release.yml contenga 'continue-on-error: true' para soportar
// repositorios privados no-Enterprise.
func TestReleaseWorkflowAttestProvenanceResilience(t *testing.T) {
	workflowPath := filepath.Join("..", "..", ".github", "workflows", "release.yml")
	workflowBytes, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("error leyendo release.yml en %q: %v", workflowPath, err)
	}
	content := string(workflowBytes)

	if !strings.Contains(content, "actions/attest-build-provenance@v2") {
		t.Fatalf("release.yml no referencia actions/attest-build-provenance@v2")
	}

	// Verificar que el bloque de attest-build-provenance tenga continue-on-error: true
	scanner := bufio.NewScanner(strings.NewReader(content))
	inAttestStep := false
	hasContinueOnError := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "- name:") && strings.Contains(line, "Attest Build Provenance") {
			inAttestStep = true
			continue
		}

		if inAttestStep {
			if strings.HasPrefix(trimmed, "- name:") {
				// Inicio del siguiente paso
				break
			}
			if strings.HasPrefix(trimmed, "continue-on-error:") && strings.Contains(trimmed, "true") {
				hasContinueOnError = true
			}
		}
	}

	if !hasContinueOnError {
		t.Errorf("release.yml: el paso 'Attest Build Provenance (SLSA Level 3)' debe tener 'continue-on-error: true' para evitar abortos en repositorios de usuario")
	}
}

// TestOpenCodeMultiAgentConfiguration valida que opencode.jsonc configure worktree.directory
// y subagent_depth >= 2, y que los agentes/comandos de orquestación paralela existan con SPDX.
func TestOpenCodeMultiAgentConfiguration(t *testing.T) {
	// 1. Validar opencode.jsonc
	configPath := filepath.Join("..", "..", "opencode.jsonc")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("error leyendo opencode.jsonc en %q: %v", configPath, err)
	}
	content := string(configBytes)

	if !strings.Contains(content, `"worktree"`) || !strings.Contains(content, `".worktrees"`) {
		t.Errorf("opencode.jsonc debe configurar 'worktree.directory: \".worktrees\"'")
	}
	depthRegex := regexp.MustCompile(`"subagent_depth":\s*([2-9]|\d{2,})`)
	if !depthRegex.MatchString(content) {
		t.Errorf("opencode.jsonc debe habilitar 'subagent_depth' >= 2 para permitir orquestación anidada")
	}

	// REUSE-IgnoreStart
	spdxRequired := "SPDX-License-Identifier" + ": " + "Apache-2.0"
	// REUSE-IgnoreEnd

	// 2. Validar que .opencode/agents/issue-orchestrator.md exista y tenga SPDX
	agentPath := filepath.Join("..", "..", ".opencode", "agents", "issue-orchestrator.md")
	agentBytes, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("error leyendo .opencode/agents/issue-orchestrator.md: %v", err)
	}
	agentContent := string(agentBytes)
	if !strings.Contains(agentContent, spdxRequired) {
		t.Errorf("issue-orchestrator.md debe tener cabecera SPDX Apache-2.0")
	}
	if !strings.Contains(agentContent, "issue-orchestrator") && !strings.Contains(agentContent, "Issue Orchestrator") {
		t.Errorf("issue-orchestrator.md no define la identidad del agente Issue Orchestrator")
	}

	// 3. Validar comandos de orquestación
	commands := []string{
		filepath.Join("..", "..", ".opencode", "commands", "work-on-issue.md"),
		filepath.Join("..", "..", ".opencode", "commands", "work-on-issues.md"),
	}

	for _, cmdPath := range commands {
		cmdBytes, err := os.ReadFile(cmdPath)
		if err != nil {
			t.Fatalf("error leyendo comando en %q: %v", cmdPath, err)
		}
		cmdContent := string(cmdBytes)
		if !strings.Contains(cmdContent, spdxRequired) {
			t.Errorf("%s debe contener cabecera SPDX Apache-2.0", cmdPath)
		}
		if !strings.Contains(cmdContent, "issue-orchestrator") {
			t.Errorf("%s debe delegar en 'issue-orchestrator'", cmdPath)
		}
	}
}
