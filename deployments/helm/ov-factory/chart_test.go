// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package helm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type ChartMetadata struct {
	APIVersion  string `yaml:"apiVersion"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Type        string `yaml:"type"`
	Version     string `yaml:"version"`
	AppVersion  string `yaml:"appVersion"`
}

func TestChartMetadataAndHelpers(t *testing.T) {
	chartPath := filepath.Join(".", "Chart.yaml")
	data, err := os.ReadFile(chartPath)
	if err != nil {
		t.Fatalf("Failed to read Chart.yaml: %v", err)
	}

	var meta ChartMetadata
	if err := yaml.Unmarshal(data, &meta); err != nil {
		t.Fatalf("Failed to unmarshal Chart.yaml: %v", err)
	}

	if meta.APIVersion != "v2" {
		t.Errorf("Expected apiVersion 'v2', got %q", meta.APIVersion)
	}
	if meta.Name != "ov-factory" {
		t.Errorf("Expected name 'ov-factory', got %q", meta.Name)
	}
	if meta.Version != "0.1.0" {
		t.Errorf("Expected version '0.1.0', got %q", meta.Version)
	}
	if meta.AppVersion != "1.0.0" {
		t.Errorf("Expected appVersion '1.0.0', got %q", meta.AppVersion)
	}

	helpersPath := filepath.Join(".", "templates", "_helpers.tpl")
	helperData, err := os.ReadFile(helpersPath)
	if err != nil {
		t.Fatalf("Failed to read _helpers.tpl: %v", err)
	}
	helpersStr := string(helperData)
	for _, expectedMacro := range []string{"ov-factory.name", "ov-factory.fullname", "ov-factory.labels", "ov-factory.selectorLabels"} {
		if !strings.Contains(helpersStr, expectedMacro) {
			t.Errorf("Expected _helpers.tpl to contain definition of %q", expectedMacro)
		}
	}
}
