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

func TestCoreWorkloadDeployments(t *testing.T) {
	requiredFiles := []struct {
		path     string
		patterns []string
	}{
		{
			path: filepath.Join(".", "templates", "server-deployment.yaml"),
			patterns: []string{"kind: Deployment", "livenessProbe", "/healthz", ".Values.server.service.port", "emptyDir"},
		},
		{
			path: filepath.Join(".", "templates", "server-service.yaml"),
			patterns: []string{"kind: Service", ".Values.server.service.port", "targetPort: http"},
		},
		{
			path: filepath.Join(".", "templates", "worker-deployment.yaml"),
			patterns: []string{"kind: Deployment", "livenessProbe", ".Values.worker.service.port", ".Values.worker.persistence.mountPath"},
		},
		{
			path: filepath.Join(".", "templates", "worker-service.yaml"),
			patterns: []string{"kind: Service", ".Values.worker.service.port"},
		},
		{
			path: filepath.Join(".", "templates", "web-deployment.yaml"),
			patterns: []string{"kind: Deployment", ".Values.web.service.port", "/var/cache/nginx"},
		},
		{
			path: filepath.Join(".", "templates", "web-service.yaml"),
			patterns: []string{"kind: Service", ".Values.web.service.port"},
		},
	}

	for _, req := range requiredFiles {
		data, err := os.ReadFile(req.path)
		if err != nil {
			t.Fatalf("Failed to read %s: %v", req.path, err)
		}
		content := string(data)
		for _, pat := range req.patterns {
			if !strings.Contains(content, pat) {
				t.Errorf("File %s missing expected pattern %q", req.path, pat)
			}
		}
	}
}

func TestScalingAndResilienceManifests(t *testing.T) {
	requiredFiles := []struct {
		path     string
		patterns []string
	}{
		{
			path:     filepath.Join(".", "templates", "pvc.yaml"),
			patterns: []string{"kind: PersistentVolumeClaim", "accessModes:", "storage:"},
		},
		{
			path:     filepath.Join(".", "templates", "server-hpa.yaml"),
			patterns: []string{"apiVersion: autoscaling/v2", "kind: HorizontalPodAutoscaler", "scaleTargetRef:", "server"},
		},
		{
			path:     filepath.Join(".", "templates", "worker-hpa.yaml"),
			patterns: []string{"apiVersion: autoscaling/v2", "kind: HorizontalPodAutoscaler", "scaleTargetRef:", "worker"},
		},
		{
			path:     filepath.Join(".", "templates", "pdb.yaml"),
			patterns: []string{"kind: PodDisruptionBudget", "minAvailable:"},
		},
	}

	for _, req := range requiredFiles {
		data, err := os.ReadFile(req.path)
		if err != nil {
			t.Fatalf("Failed to read %s: %v", req.path, err)
		}
		content := string(data)
		for _, pat := range req.patterns {
			if !strings.Contains(content, pat) {
				t.Errorf("File %s missing expected pattern %q", req.path, pat)
			}
		}
	}
}

func TestPersistenceAndMessagingManifests(t *testing.T) {
	requiredFiles := []struct {
		path     string
		patterns []string
	}{
		{
			path:     filepath.Join(".", "templates", "postgres-statefulset.yaml"),
			patterns: []string{"kind: StatefulSet", "volumeClaimTemplates", "POSTGRES_DB"},
		},
		{
			path:     filepath.Join(".", "templates", "postgres-service.yaml"),
			patterns: []string{"kind: Service", ".Values.postgres.service.port"},
		},
		{
			path:     filepath.Join(".", "templates", "nats-statefulset.yaml"),
			patterns: []string{"kind: StatefulSet", "volumeClaimTemplates", "clientPort"},
		},
		{
			path:     filepath.Join(".", "templates", "nats-service.yaml"),
			patterns: []string{"kind: Service", "clientPort"},
		},
		{
			path:     filepath.Join(".", "templates", "hatchet-deployment.yaml"),
			patterns: []string{"kind: Deployment", "hatchet"},
		},
		{
			path:     filepath.Join(".", "templates", "hatchet-service.yaml"),
			patterns: []string{"kind: Service", ".Values.hatchet.service.port"},
		},
	}

	for _, req := range requiredFiles {
		data, err := os.ReadFile(req.path)
		if err != nil {
			t.Fatalf("Failed to read %s: %v", req.path, err)
		}
		content := string(data)
		for _, pat := range req.patterns {
			if !strings.Contains(content, pat) {
				t.Errorf("File %s missing expected pattern %q", req.path, pat)
			}
		}
	}
}

func TestIngressAndNetworkPolicyManifests(t *testing.T) {
	requiredFiles := []struct {
		path     string
		patterns []string
	}{
		{
			path:     filepath.Join(".", "templates", "ingress.yaml"),
			patterns: []string{"kind: Ingress", "ingress.annotations", "paths:", "path: {{ .path }}"},
		},
		{
			path:     filepath.Join(".", "values.yaml"),
			patterns: []string{"proxy-buffering", "path: /api", "path: /events", "path: /"},
		},
		{
			path:     filepath.Join(".", "templates", "networkpolicy.yaml"),
			patterns: []string{"kind: NetworkPolicy", "policyTypes:", "- Ingress", "- Egress", "postgres", "nats"},
		},
	}

	for _, req := range requiredFiles {
		data, err := os.ReadFile(req.path)
		if err != nil {
			t.Fatalf("Failed to read %s: %v", req.path, err)
		}
		content := string(data)
		for _, pat := range req.patterns {
			if !strings.Contains(content, pat) {
				t.Errorf("File %s missing expected pattern %q", req.path, pat)
			}
		}
	}

	// Verify Makefile integration
	makefilePath := filepath.Join("..", "..", "..", "Makefile")
	makeData, err := os.ReadFile(makefilePath)
	if err != nil {
		t.Fatalf("Failed to read Makefile: %v", err)
	}
	makeContent := string(makeData)
	if !strings.Contains(makeContent, "helm-lint:") {
		t.Errorf("Makefile missing 'helm-lint:' target")
	}
	if !strings.Contains(makeContent, "helm-template:") {
		t.Errorf("Makefile missing 'helm-template:' target")
	}
}





