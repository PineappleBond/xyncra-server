# =============================================================================
# Xyncra Server — Makefile
# =============================================================================
#
# Targets:
#   make              Build server + client binaries
#   make build        Same as above
#   make test         Run unit tests only (no Redis/Docker required)
#   make test-e2e     Run server E2E tests (Redis on port 16379 required)
#   make test-cli-e2e Run CLI E2E tests (Docker E2E environment required)
#   make test-all     Run all tests (unit + e2e + cli-e2e)
#   make clean        Remove build artifacts (bin/, dist/, *.test, coverage, runtime DBs, logs)
#   make fmt          Format Go source code
#   make vet          Run Go static analysis
#   make tidy         Tidy go.mod dependencies
#   make release      Cross-compile for linux/darwin/windows x amd64/arm64
#
# Docker targets:
#   make docker-build       Build Docker image
#   make docker-up          Start production Docker environment
#   make docker-down        Stop production Docker environment
#   make docker-e2e-up      Start E2E Docker environment (Redis 16379, Server 18080)
#   make docker-e2e-down    Stop E2E Docker environment
#
# =============================================================================

# -----------------------------------------------------------------------------
# Variables
# -----------------------------------------------------------------------------

VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME  ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS     := -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)"

BIN_DIR  := ./bin
DIST_DIR := ./dist

GO      := go
GOFLAGS :=

# E2E port conventions (D-043)
E2E_REDIS_PORT  := 16379
E2E_SERVER_PORT := 18080
E2E_REDIS_DB    := 15

# -----------------------------------------------------------------------------
# Default target
# -----------------------------------------------------------------------------

.DEFAULT_GOAL := all

all: build

# -----------------------------------------------------------------------------
# Build targets
# -----------------------------------------------------------------------------

.PHONY: build build-server build-client

## build: Compile server and client binaries into ./bin/
build: build-server build-client

## build-server: Compile the xyncra-server binary
build-server:
	@mkdir -p $(BIN_DIR)
	$(GO) build $(LDFLAGS) -o $(BIN_DIR)/xyncra-server ./cmd/xyncra-server/

## build-client: Compile the xyncra-client binary
build-client:
	@mkdir -p $(BIN_DIR)
	$(GO) build $(LDFLAGS) -o $(BIN_DIR)/xyncra-client ./cmd/xyncra-client/

# -----------------------------------------------------------------------------
# Test targets
# -----------------------------------------------------------------------------

.PHONY: test test-e2e test-cli-e2e test-all

## test: Run unit tests only (skips tests requiring Redis/Docker via -short flag)
test:
	$(GO) test -short ./...

## test-e2e: Run server-side E2E tests (requires Redis on port 16379)
test-e2e:
	@if ! nc -z localhost $(E2E_REDIS_PORT) > /dev/null 2>&1; then \
		echo "ERROR: Redis is not reachable at localhost:$(E2E_REDIS_PORT) (DB $(E2E_REDIS_DB))."; \
		echo "       Start the E2E Docker environment first:"; \
		echo "         make docker-e2e-up"; \
		exit 1; \
	fi
	$(GO) test -v ./internal/e2e/

## test-cli-e2e: Run CLI E2E tests (builds client first, requires Docker E2E environment)
test-cli-e2e: build-client
	@if ! nc -z localhost $(E2E_REDIS_PORT) > /dev/null 2>&1; then \
		echo "ERROR: Redis is not reachable at localhost:$(E2E_REDIS_PORT) (DB $(E2E_REDIS_DB))."; \
		echo "       Start the E2E Docker environment first:"; \
		echo "         make docker-e2e-up"; \
		exit 1; \
	fi
	@if ! curl -sf http://localhost:$(E2E_SERVER_PORT)/health > /dev/null 2>&1; then \
		echo "ERROR: xyncra-server is not reachable at localhost:$(E2E_SERVER_PORT)."; \
		echo "       Start the E2E Docker environment first:"; \
		echo "         make docker-e2e-up"; \
		exit 1; \
	fi
	$(GO) test -v ./internal/cli/e2e/

## test-all: Run all tests (unit + server E2E + CLI E2E)
test-all: test test-e2e test-cli-e2e

# -----------------------------------------------------------------------------
# Docker targets
# -----------------------------------------------------------------------------

.PHONY: docker-build docker-up docker-down docker-e2e-up docker-e2e-down

## docker-build: Build the Docker image for xyncra-server
docker-build:
	docker build -t xyncra-server:$(VERSION) -f deploy/Dockerfile .

## docker-up: Start the production Docker environment (deploy/docker-compose.yml)
docker-up:
	docker compose -f deploy/docker-compose.yml up -d

## docker-down: Stop the production Docker environment
docker-down:
	docker compose -f deploy/docker-compose.yml down

## docker-e2e-up: Start the E2E Docker environment (Redis 16379, Server 18080, DB 15)
docker-e2e-up:
	docker compose -f deploy/docker-compose.e2e.yml up -d --wait

## docker-e2e-down: Stop the E2E Docker environment and remove volumes
docker-e2e-down:
	docker compose -f deploy/docker-compose.e2e.yml down -v

# -----------------------------------------------------------------------------
# Code quality targets
# -----------------------------------------------------------------------------

.PHONY: fmt vet tidy

## fmt: Format all Go source files
fmt:
	gofmt -w -s .

## vet: Run Go static analysis
vet:
	$(GO) vet ./...

## tidy: Tidy go.mod and go.sum
tidy:
	$(GO) mod tidy

# -----------------------------------------------------------------------------
# Release target — cross-compile for multiple platforms
# -----------------------------------------------------------------------------

.PHONY: release

RELEASE_PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64 \
	windows/arm64

## release: Cross-compile server and client for all supported platforms into ./dist/
release: clean
	@mkdir -p $(DIST_DIR)
	@for platform in $(RELEASE_PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		echo "==> Building $$os/$$arch ..."; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build $(LDFLAGS) -o $(DIST_DIR)/xyncra-server-$$os-$$arch$$ext ./cmd/xyncra-server/; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build $(LDFLAGS) -o $(DIST_DIR)/xyncra-client-$$os-$$arch$$ext ./cmd/xyncra-client/; \
	done
	@echo "Release binaries written to $(DIST_DIR)/"

# -----------------------------------------------------------------------------
# Clean target
# -----------------------------------------------------------------------------

.PHONY: clean

## clean: Remove all build artifacts (bin/, dist/, test binaries, coverage, runtime DBs, logs)
clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)
	rm -f *.test                       # Go test binaries (agent.test, e2e.test, …)
	rm -f xyncra-server xyncra-client  # Root-level compiled binaries
	rm -f xyncra-server.exe xyncra-client.exe
	rm -f xyncra.db dump.rdb           # Runtime databases
	rm -f coverage.out coverage.html   # Coverage reports
	rm -rf llm-logs-e2e/               # E2E LLM log dumps

# -----------------------------------------------------------------------------
# Help
# -----------------------------------------------------------------------------

.PHONY: help

## help: Show this help message
help:
	@echo "Xyncra Server — Available targets:"
	@echo ""
	@grep -E '^## ' Makefile | sed 's/^## /  /' | column -t -s ':'
