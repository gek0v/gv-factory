// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestGoModVersionAlignment(t *testing.T) {
	goModPath := filepath.Join("..", "..", "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("failed to read go.mod at %q: %v", goModPath, err)
	}

	re := regexp.MustCompile(`(?m)^go\s+([0-9]+\.[0-9]+(?:\.[0-9]+)?)`)
	matches := re.FindSubmatch(data)
	if len(matches) < 2 {
		t.Fatalf("no 'go' version directive found in go.mod")
	}

	version := string(matches[1])
	expectedVersion := "1.24.0"
	if version != expectedVersion {
		t.Errorf("go.mod specifies Go version %q; expected %q to prevent toolchain auto-download failure in container builds", version, expectedVersion)
	}
}

func TestWebPackageJSONOnlyBuiltDependencies(t *testing.T) {
	pkgJSONPath := filepath.Join("..", "..", "web", "package.json")
	data, err := os.ReadFile(pkgJSONPath)
	if err != nil {
		t.Fatalf("failed to read package.json at %q: %v", pkgJSONPath, err)
	}

	var pkg struct {
		PNPM struct {
			OnlyBuiltDependencies []string `json:"onlyBuiltDependencies"`
		} `json:"pnpm"`
	}

	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatalf("failed to unmarshal web/package.json: %v", err)
	}

	hasEsbuild := false
	for _, dep := range pkg.PNPM.OnlyBuiltDependencies {
		if dep == "esbuild" {
			hasEsbuild = true
			break
		}
	}

	if !hasEsbuild {
		t.Errorf("web/package.json is missing 'pnpm.onlyBuiltDependencies' containing 'esbuild'; required for pnpm v12+ container builds")
	}
}

func TestDockerfilesBuilderVersionConsistency(t *testing.T) {
	dockerfiles := []string{
		filepath.Join("..", "..", "build", "package", "Dockerfile.server"),
		filepath.Join("..", "..", "build", "package", "Dockerfile.worker"),
	}

	for _, df := range dockerfiles {
		content, err := os.ReadFile(df)
		if err != nil {
			t.Fatalf("failed to read %s: %v", df, err)
		}

		if !strings.Contains(string(content), "golang:1.24-alpine") {
			t.Errorf("%s does not reference golang:1.24-alpine builder image", df)
		}
	}
}
