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

func TestValuesSchemaAndConfiguration(t *testing.T) {
	valuesPath := filepath.Join(".", "values.yaml")
	data, err := os.ReadFile(valuesPath)
	if err != nil {
		t.Fatalf("Failed to read values.yaml: %v", err)
	}

	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		t.Fatalf("Failed to unmarshal values.yaml: %v", err)
	}

	requiredSections := []string{"global", "server", "worker", "web", "postgres", "nats", "ingress"}
	for _, section := range requiredSections {
		if _, ok := values[section]; !ok {
			t.Errorf("values.yaml is missing required section %q", section)
		}
	}

	// Verify server security context defaults
	server, ok := values["server"].(map[string]interface{})
	if !ok {
		t.Fatalf("server section is not a map")
	}
	secCtx, ok := server["securityContext"].(map[string]interface{})
	if !ok {
		t.Fatalf("server.securityContext is not a map")
	}
	if secCtx["runAsNonRoot"] != true {
		t.Errorf("Expected server.securityContext.runAsNonRoot == true, got %v", secCtx["runAsNonRoot"])
	}
	if secCtx["readOnlyRootFilesystem"] != true {
		t.Errorf("Expected server.securityContext.readOnlyRootFilesystem == true, got %v", secCtx["readOnlyRootFilesystem"])
	}
	if secCtx["runAsUser"] != 10001 {
		t.Errorf("Expected server.securityContext.runAsUser == 10001, got %v", secCtx["runAsUser"])
	}

	// Verify template files exist
	configmapPath := filepath.Join(".", "templates", "configmap.yaml")
	if _, err := os.Stat(configmapPath); err != nil {
		t.Errorf("configmap.yaml does not exist: %v", err)
	}

	secretPath := filepath.Join(".", "templates", "secret.yaml")
	if _, err := os.Stat(secretPath); err != nil {
		t.Errorf("secret.yaml does not exist: %v", err)
	}
}

