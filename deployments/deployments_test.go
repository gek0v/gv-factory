// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package deployments_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type ComposeConfig struct {
	Version  string                     `yaml:"version"`
	Services map[string]ComposeService `yaml:"services"`
	Volumes  map[string]interface{}     `yaml:"volumes"`
}

type ComposeService struct {
	Image         string                 `yaml:"image"`
	ContainerName string                 `yaml:"container_name"`
	Restart       string                 `yaml:"restart"`
	Build         *ComposeBuild          `yaml:"build"`
	Environment   map[string]string      `yaml:"environment"`
	Ports         []string               `yaml:"ports"`
	Volumes       []string               `yaml:"volumes"`
	DependsOn     map[string]interface{} `yaml:"depends_on"`
	Healthcheck   *ComposeHealthcheck    `yaml:"healthcheck"`
}

type ComposeBuild struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

type ComposeHealthcheck struct {
	Test        []string `yaml:"test"`
	Interval    string   `yaml:"interval"`
	Timeout     string   `yaml:"timeout"`
	Retries     int      `yaml:"retries"`
	StartPeriod string   `yaml:"start_period"`
}

func loadComposeConfig(t *testing.T) ComposeConfig {
	t.Helper()
	composePath := filepath.Join(".", "docker-compose.yml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", composePath, err)
	}

	var config ComposeConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse %s: %v", composePath, err)
	}
	return config
}

func TestDockerComposeStructureAndServices(t *testing.T) {
	config := loadComposeConfig(t)

	requiredServices := []string{"postgres", "ov-server", "ov-worker", "web"}
	for _, svcName := range requiredServices {
		if _, exists := config.Services[svcName]; !exists {
			t.Errorf("docker-compose.yml missing required service %q", svcName)
		}
	}

	requiredVolumes := []string{"pgdata", "natsdata", "sandboxes"}
	for _, volName := range requiredVolumes {
		if _, exists := config.Volumes[volName]; !exists {
			t.Errorf("docker-compose.yml missing required volume %q", volName)
		}
	}
}

func TestPostgresServiceConfiguration(t *testing.T) {
	config := loadComposeConfig(t)
	pg, exists := config.Services["postgres"]
	if !exists {
		t.Fatalf("postgres service not found in docker-compose.yml")
	}

	if !strings.Contains(pg.Image, "pgvector/pgvector:pg16") {
		t.Errorf("expected postgres image 'pgvector/pgvector:pg16', got %q", pg.Image)
	}

	hasDataVol := false
	hasInitSchema := false
	for _, vol := range pg.Volumes {
		if strings.HasPrefix(vol, "pgdata:") {
			hasDataVol = true
		}
		if strings.Contains(vol, "000001_init_pgvector.up.sql") {
			hasInitSchema = true
		}
	}

	if !hasDataVol {
		t.Errorf("postgres service missing 'pgdata' persistence volume mount")
	}
	if !hasInitSchema {
		t.Errorf("postgres service missing auto-mounted init schema 000001_init_pgvector.up.sql")
	}

	// Verify the mounted SQL initialization file actually exists on disk
	sqlPath := filepath.Join("..", "migrations", "brain", "000001_init_pgvector.up.sql")
	if _, err := os.Stat(sqlPath); os.IsNotExist(err) {
		t.Fatalf("mounted initialization schema does not exist at %s", sqlPath)
	}

	if pg.Healthcheck == nil {
		t.Fatalf("postgres service missing healthcheck configuration")
	}
	testCmd := strings.Join(pg.Healthcheck.Test, " ")
	if !strings.Contains(testCmd, "pg_isready") {
		t.Errorf("postgres healthcheck test must use pg_isready, got %q", testCmd)
	}
}

func TestOvServerServiceConfiguration(t *testing.T) {
	config := loadComposeConfig(t)
	srv, exists := config.Services["ov-server"]
	if !exists {
		t.Fatalf("ov-server service not found in docker-compose.yml")
	}

	if srv.Build == nil {
		t.Fatalf("ov-server service missing build configuration")
	}
	expectedDockerfile := "deployments/Dockerfile.server"
	if srv.Build.Dockerfile != expectedDockerfile {
		t.Errorf("expected ov-server dockerfile %q, got %q", expectedDockerfile, srv.Build.Dockerfile)
	}

	// Verify Dockerfile exists on disk
	dockerfilePath := filepath.Join("..", srv.Build.Dockerfile)
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		t.Fatalf("ov-server Dockerfile does not exist at %s", dockerfilePath)
	}

	hasPort8080 := false
	for _, port := range srv.Ports {
		if strings.Contains(port, "8080") {
			hasPort8080 = true
			break
		}
	}
	if !hasPort8080 {
		t.Errorf("ov-server service must expose port 8080, got %v", srv.Ports)
	}

	if srv.DependsOn == nil {
		t.Fatalf("ov-server service missing depends_on configuration")
	}
	if _, pgDep := srv.DependsOn["postgres"]; !pgDep {
		t.Errorf("ov-server must depend on postgres service")
	}

	if srv.Healthcheck == nil {
		t.Fatalf("ov-server missing healthcheck configuration")
	}
	testCmd := strings.Join(srv.Healthcheck.Test, " ")
	if !strings.Contains(testCmd, "/healthz") {
		t.Errorf("ov-server healthcheck must probe /healthz, got %q", testCmd)
	}

	if srv.Environment["DATABASE_URL"] == "" {
		t.Errorf("ov-server missing DATABASE_URL environment configuration")
	}
}

func TestOvWorkerServiceConfiguration(t *testing.T) {
	config := loadComposeConfig(t)
	worker, exists := config.Services["ov-worker"]
	if !exists {
		t.Fatalf("ov-worker service not found in docker-compose.yml")
	}

	if worker.Build == nil {
		t.Fatalf("ov-worker service missing build configuration")
	}
	expectedDockerfile := "deployments/Dockerfile.worker"
	if worker.Build.Dockerfile != expectedDockerfile {
		t.Errorf("expected ov-worker dockerfile %q, got %q", expectedDockerfile, worker.Build.Dockerfile)
	}

	dockerfilePath := filepath.Join("..", worker.Build.Dockerfile)
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		t.Fatalf("ov-worker Dockerfile does not exist at %s", dockerfilePath)
	}

	hasPort8081 := false
	for _, port := range worker.Ports {
		if strings.Contains(port, "8081") {
			hasPort8081 = true
			break
		}
	}
	if !hasPort8081 {
		t.Errorf("ov-worker service must expose health port 8081, got %v", worker.Ports)
	}

	if worker.Healthcheck == nil {
		t.Fatalf("ov-worker missing healthcheck configuration")
	}
	testCmd := strings.Join(worker.Healthcheck.Test, " ")
	if !strings.Contains(testCmd, "/healthz") {
		t.Errorf("ov-worker healthcheck must probe /healthz, got %q", testCmd)
	}

	hasSandboxVol := false
	for _, vol := range worker.Volumes {
		if strings.Contains(vol, "/tmp/ov-sandboxes") {
			hasSandboxVol = true
			break
		}
	}
	if !hasSandboxVol {
		t.Errorf("ov-worker missing volume mount for /tmp/ov-sandboxes")
	}
}

func TestWebServiceConfiguration(t *testing.T) {
	config := loadComposeConfig(t)
	web, exists := config.Services["web"]
	if !exists {
		t.Fatalf("web service not found in docker-compose.yml")
	}

	if web.Build == nil {
		t.Fatalf("web service missing build configuration")
	}
	expectedDockerfile := "web/Dockerfile"
	if web.Build.Dockerfile != expectedDockerfile {
		t.Errorf("expected web dockerfile %q, got %q", expectedDockerfile, web.Build.Dockerfile)
	}

	dockerfilePath := filepath.Join("..", web.Build.Dockerfile)
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		t.Fatalf("web Dockerfile does not exist at %s", dockerfilePath)
	}

	hasPort3000 := false
	for _, port := range web.Ports {
		if strings.Contains(port, "3000") || strings.Contains(port, "8080") {
			hasPort3000 = true
			break
		}
	}
	if !hasPort3000 {
		t.Errorf("web service must map external port to 8080, got %v", web.Ports)
	}

	if web.DependsOn == nil {
		t.Fatalf("web service missing depends_on configuration")
	}
	if _, srvDep := web.DependsOn["ov-server"]; !srvDep {
		t.Errorf("web service must depend on ov-server")
	}

	if web.Healthcheck == nil {
		t.Fatalf("web service missing healthcheck configuration")
	}
}

func TestDockerfileServerSpecification(t *testing.T) {
	path := filepath.Join(".", "Dockerfile.server")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	content := string(data)

	// Validate multi-stage build structure
	if !strings.Contains(content, "FROM golang:1.24-alpine AS builder") {
		t.Errorf("Dockerfile.server builder stage must use golang:1.24-alpine")
	}
	if !strings.Contains(content, "FROM alpine:3.21") {
		t.Errorf("Dockerfile.server runtime stage must use alpine:3.21")
	}
	if !strings.Contains(content, "CGO_ENABLED=0") && !strings.Contains(content, "CGO_ENABLED=1") {
		t.Errorf("Dockerfile.server must set CGO_ENABLED")
	}
	if !strings.Contains(content, "-ldflags=\"-s -w\"") {
		t.Errorf("Dockerfile.server must strip debug symbols with -ldflags=\"-s -w\"")
	}

	// Hardened unprivileged execution
	if !strings.Contains(content, "10001") {
		t.Errorf("Dockerfile.server must run as unprivileged user (UID 10001)")
	}
	if !strings.Contains(content, "USER 10001:10001") {
		t.Errorf("Dockerfile.server must switch to USER 10001:10001")
	}

	// Exposed port & Healthcheck
	if !strings.Contains(content, "EXPOSE 8080") {
		t.Errorf("Dockerfile.server must EXPOSE 8080")
	}
	if !strings.Contains(content, "HEALTHCHECK") || !strings.Contains(content, "/healthz") {
		t.Errorf("Dockerfile.server must define HEALTHCHECK probing /healthz")
	}
	if !strings.Contains(content, `ENTRYPOINT ["/usr/local/bin/ov-server"]`) {
		t.Errorf("Dockerfile.server must define ENTRYPOINT for ov-server")
	}
}

func TestDockerfileWorkerSpecification(t *testing.T) {
	path := filepath.Join(".", "Dockerfile.worker")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	content := string(data)

	if !strings.Contains(content, "FROM golang:1.24-alpine AS builder") {
		t.Errorf("Dockerfile.worker builder stage must use golang:1.24-alpine")
	}
	if !strings.Contains(content, "FROM alpine:3.21") {
		t.Errorf("Dockerfile.worker runtime stage must use alpine:3.21")
	}

	// Ensure git and build toolchains are available
	if !strings.Contains(content, "git") {
		t.Errorf("Dockerfile.worker must install git for worktree operations")
	}
	if !strings.Contains(content, "10001") {
		t.Errorf("Dockerfile.worker must run as unprivileged user (UID 10001)")
	}
	if !strings.Contains(content, "USER 10001:10001") {
		t.Errorf("Dockerfile.worker must switch to USER 10001:10001")
	}

	if !strings.Contains(content, "EXPOSE 8081") {
		t.Errorf("Dockerfile.worker must EXPOSE 8081")
	}
	if !strings.Contains(content, "HEALTHCHECK") || !strings.Contains(content, "/healthz") {
		t.Errorf("Dockerfile.worker must define HEALTHCHECK probing /healthz")
	}
	if !strings.Contains(content, `ENTRYPOINT ["/usr/local/bin/ov-worker"]`) {
		t.Errorf("Dockerfile.worker must define ENTRYPOINT for ov-worker")
	}
}

func TestWebDockerfileSpecification(t *testing.T) {
	path := filepath.Join("..", "web", "Dockerfile")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	content := string(data)

	if !strings.Contains(content, "FROM node:22-alpine AS builder") {
		t.Errorf("web/Dockerfile builder stage must use node:22-alpine")
	}
	pnpm9Regex := regexp.MustCompile(`pnpm@9(?:\.[0-9]+(?:\.[0-9]+)?)?`)
	if !pnpm9Regex.MatchString(content) {
		t.Errorf("web/Dockerfile must pin pnpm v9")
	}
	if !strings.Contains(content, "pnpm build") {
		t.Errorf("web/Dockerfile must run 'pnpm build'")
	}

	if !strings.Contains(content, "FROM nginx:alpine") && !strings.Contains(content, "FROM nginx:alpine-slim") {
		t.Errorf("web/Dockerfile runtime stage must use nginx:alpine or nginx:alpine-slim")
	}
	if !strings.Contains(content, "web/nginx.conf") {
		t.Errorf("web/Dockerfile must copy web/nginx.conf")
	}
	if !strings.Contains(content, "USER 10001:10001") {
		t.Errorf("web/Dockerfile must run as unprivileged user")
	}
	if !strings.Contains(content, "EXPOSE 8080") {
		t.Errorf("web/Dockerfile must EXPOSE 8080")
	}
	if !strings.Contains(content, "HEALTHCHECK") || !strings.Contains(content, "/healthz") {
		t.Errorf("web/Dockerfile must define HEALTHCHECK probing /healthz")
	}
}

func TestWebNginxConfSpecification(t *testing.T) {
	path := filepath.Join("..", "web", "nginx.conf")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	content := string(data)

	// Gzip compression check
	if !strings.Contains(content, "gzip on;") {
		t.Errorf("web/nginx.conf must enable gzip compression")
	}

	// Reverse proxy for /api/
	if !strings.Contains(content, "location /api/") || !strings.Contains(content, "proxy_pass http://ov-server:8080;") {
		t.Errorf("web/nginx.conf must proxy /api/ to http://ov-server:8080")
	}

	// SSE proxying for /events
	if !strings.Contains(content, "location /events") {
		t.Errorf("web/nginx.conf must configure location /events for SSE")
	}
	if !strings.Contains(content, "proxy_buffering off;") {
		t.Errorf("web/nginx.conf must disable proxy buffering for SSE stream")
	}

	// SPA fallback routing
	if !strings.Contains(content, "try_files $uri $uri/ /index.html;") {
		t.Errorf("web/nginx.conf must configure SPA fallback routing to /index.html")
	}

	// Unprivileged listen port 8080 & healthz probe
	if !strings.Contains(content, "listen 8080;") {
		t.Errorf("web/nginx.conf must listen on unprivileged port 8080")
	}
	if !strings.Contains(content, "location /healthz") {
		t.Errorf("web/nginx.conf must include /healthz probe endpoint")
	}
}

func TestQuickstartScriptSpecification(t *testing.T) {
	path := filepath.Join("..", "scripts", "quickstart.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	content := string(data)

	if !strings.Contains(content, "command -v docker") {
		t.Errorf("quickstart.sh must check for docker binary")
	}
	if !strings.Contains(content, "docker info") {
		t.Errorf("quickstart.sh must verify docker daemon accessibility")
	}
	if !strings.Contains(content, "docker compose") {
		t.Errorf("quickstart.sh must verify docker compose support")
	}
	if !strings.Contains(content, "AUTH_SECRET_KEY") {
		t.Errorf("quickstart.sh must manage AUTH_SECRET_KEY generation")
	}
	if !strings.Contains(content, "PORTS_TO_CHECK") && !strings.Contains(content, "is_port_in_use") {
		t.Errorf("quickstart.sh must perform port checking")
	}
	if !strings.Contains(content, "up -d") {
		t.Errorf("quickstart.sh must launch stack with 'up -d'")
	}
}

func TestSPDXHeadersCompliance(t *testing.T) {
	files := []string{
		filepath.Join(".", "docker-compose.yml"),
		filepath.Join(".", "Dockerfile.server"),
		filepath.Join(".", "Dockerfile.worker"),
		filepath.Join(".", "deployments_test.go"),
		filepath.Join("..", "web", "Dockerfile"),
		filepath.Join("..", "web", "nginx.conf"),
		filepath.Join("..", "scripts", "quickstart.sh"),
	}

	// REUSE-IgnoreStart
	expectedLicense := "SPDX-License-Identifier: Apache-2.0"
	expectedCopyright := "SPDX-FileCopyrightText: 2026 gv-factory contributors"
	// REUSE-IgnoreEnd

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("failed to read %s: %v", file, err)
		}
		content := string(data)
		if !strings.Contains(content, expectedLicense) {
			t.Errorf("file %s is missing SPDX license identifier %q", file, expectedLicense)
		}
		if !strings.Contains(content, expectedCopyright) {
			t.Errorf("file %s is missing SPDX copyright header %q", file, expectedCopyright)
		}
	}
}
