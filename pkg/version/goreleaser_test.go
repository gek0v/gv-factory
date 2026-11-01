// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package version

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGoReleaserV2Schema(t *testing.T) {
	// Locate repository root from pkg/version directory.
	rootPath := filepath.Join("..", "..", ".goreleaser.yaml")
	data, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatalf("failed to read .goreleaser.yaml at %q: %v", rootPath, err)
	}

	var config map[string]any
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to unmarshal .goreleaser.yaml: %v", err)
	}

	// 1. Root level checks: homebrew_casks must NOT exist, brews MUST exist.
	if _, ok := config["homebrew_casks"]; ok {
		t.Errorf("found deprecated 'homebrew_casks' at root; must be replaced with 'brews'")
	}

	rawBrews, ok := config["brews"]
	if !ok {
		t.Fatalf("missing required 'brews' section at root")
	}

	brews, ok := rawBrews.([]any)
	if !ok || len(brews) == 0 {
		t.Fatalf("'brews' must be a non-empty slice")
	}

	for i, b := range brews {
		brewMap, ok := b.(map[string]any)
		if !ok {
			t.Fatalf("brew[%d] must be a map", i)
		}
		if _, ok := brewMap["ids"]; !ok {
			t.Errorf("brew[%d] missing 'ids' field", i)
		}
		for _, requiredKey := range []string{"repository", "homepage", "description", "license"} {
			if _, ok := brewMap[requiredKey]; !ok {
				t.Errorf("brew[%d] missing required field %q", i, requiredKey)
			}
		}
	}

	// 2. Archives checks: must use 'builds' instead of 'ids', 'format' instead of 'formats'.
	rawArchives, ok := config["archives"]
	if !ok {
		t.Fatalf("missing 'archives' section at root")
	}

	archives, ok := rawArchives.([]any)
	if !ok || len(archives) == 0 {
		t.Fatalf("'archives' must be a non-empty slice")
	}

	for i, a := range archives {
		archMap, ok := a.(map[string]any)
		if !ok {
			t.Fatalf("archive[%d] must be a map", i)
		}

		if _, hasIDs := archMap["ids"]; hasIDs {
			t.Errorf("archive[%d] has deprecated 'ids' field; must use 'builds'", i)
		}

		if _, hasBuilds := archMap["builds"]; !hasBuilds {
			t.Errorf("archive[%d] missing required 'builds' field", i)
		}

		if _, hasFormats := archMap["formats"]; hasFormats {
			t.Errorf("archive[%d] has deprecated 'formats' field; must use 'format'", i)
		}

		if formatVal, hasFormat := archMap["format"]; !hasFormat {
			t.Errorf("archive[%d] missing required 'format' field", i)
		} else if _, isStr := formatVal.(string); !isStr {
			t.Errorf("archive[%d] 'format' must be a string, got %T", i, formatVal)
		}

		if rawOverrides, ok := archMap["format_overrides"]; ok {
			overrides, ok := rawOverrides.([]any)
			if !ok {
				t.Errorf("archive[%d] 'format_overrides' must be a slice", i)
				continue
			}
			for j, o := range overrides {
				ovMap, ok := o.(map[string]any)
				if !ok {
					t.Errorf("archive[%d] format_overrides[%d] must be a map", i, j)
					continue
				}
				if _, hasFmts := ovMap["formats"]; hasFmts {
					t.Errorf("archive[%d] format_overrides[%d] has deprecated 'formats' field; must use 'format'", i, j)
				}
				if fmtVal, hasFmt := ovMap["format"]; !hasFmt {
					t.Errorf("archive[%d] format_overrides[%d] missing required 'format' field", i, j)
				} else if _, isStr := fmtVal.(string); !isStr {
					t.Errorf("archive[%d] format_overrides[%d] 'format' must be a string", i, j)
				}
			}
		}
	}
}
