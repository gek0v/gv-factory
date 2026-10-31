# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 gv-factory contributors

.PHONY: all build test lint dev clean web-install web-build

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

clean:
	@echo "==> Cleaning built artifacts..."
	@rm -rf $(BIN_DIR)

web-install:
	@echo "==> Installing frontend dependencies..."
	@cd web && pnpm install

web-build:
	@echo "==> Building frontend SPA..."
	@cd web && pnpm build
