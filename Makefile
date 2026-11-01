# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 gv-factory contributors

.PHONY: all build test lint dev clean web-install web-build compose-up compose-down compose-check helm-lint helm-template release-check release-snapshot

BIN_DIR := bin

all: build

build:
	@echo "==> Building backend binaries..."
	@go build -o $(BIN_DIR)/ov-server ./cmd/ov-server
	@go build -o $(BIN_DIR)/ov-worker ./cmd/ov-worker
	@go build -o $(BIN_DIR)/ov-cli ./cmd/ov-cli

test:
	@echo "==> Running backend tests..."
	@go test -v -race ./...

lint:
	@echo "==> Running golangci-lint..."
	@golangci-lint run ./... || true

dev:
	@echo "==> Starting local infrastructure with Docker Compose..."
	@docker compose -f deployments/docker-compose.yml up -d

compose-up:
	@echo "==> Starting full OV-Factory stack..."
	@docker compose -f deployments/docker-compose.yml up -d --build

compose-down:
	@echo "==> Stopping OV-Factory stack..."
	@docker compose -f deployments/docker-compose.yml down -v

compose-check:
	@echo "==> Checking OV-Factory stack status and health..."
	@docker compose -f deployments/docker-compose.yml ps

clean:
	@echo "==> Cleaning built artifacts..."
	@rm -rf $(BIN_DIR)

web-install:
	@echo "==> Installing frontend dependencies..."
	@cd web && pnpm install

web-build:
	@echo "==> Building frontend SPA..."
	@cd web && pnpm build

helm-lint:
	@echo "==> Linting Helm chart..."
	@which helm > /dev/null 2>&1 && helm lint deployments/helm/ov-factory || go test -v ./deployments/helm/ov-factory/...

helm-template:
	@echo "==> Rendering Helm templates..."
	@which helm > /dev/null 2>&1 && helm template ov-factory deployments/helm/ov-factory || go test -v ./deployments/helm/ov-factory/...

release-check:
	@echo "==> Validating GoReleaser configuration..."
	@go run github.com/goreleaser/goreleaser/v2@latest check

release-snapshot:
	@echo "==> Building GoReleaser snapshot release..."
	@go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean --skip=sign,publish

