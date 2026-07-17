# GitHub CI/CD Design

**Date**: 2026-07-18
**Status**: Draft
**Scope**: GitHub Actions CI workflow + Docker image publishing to GHCR

---

## 1. Overview

Single-file GitHub Actions workflow (`.github/workflows/ci.yml`) that:

- Runs lint, unit tests, and Docker build verification on every PR and push to main
- Runs full E2E test suite only on push to main
- Publishes Docker image to GitHub Container Registry (GHCR) only after all checks pass on main

## 2. Triggers

```yaml
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
```

- **Push to main**: final validation + image publish
- **PR targeting main**: fast feedback gate before merge

## 3. Concurrency Control

```yaml
concurrency:
  group: ci-${{ github.ref }}
  cancel-in-progress: true
```

- Same ref (PR branch or main) only keeps the latest run; older runs auto-cancel
- Different PRs do not interfere with each other
- Saves GitHub Actions minutes on rapid successive pushes

## 4. Permissions

```yaml
permissions:
  contents: read
  packages: write   # required for GHCR push
```

## 5. Environment

```yaml
env:
  GO_VERSION: '1.26'
  GOLANGCI_LINT_VERSION: 'v2.1'
```

- **Go**: single version (matches `go.mod` and `deploy/Dockerfile`). No version matrix — xyncra-server is an application, not a library.
- **golangci-lint**: pinned to `v2.1`. Pre-push hook checks and reports the installed version vs required version.

## 6. Job Topology

```text
┌─────────────┐  ┌──────────────┐  ┌───────────────┐
│   lint      │  │  unit-test   │  │  docker-build │
│  (PR+main)  │  │  (PR+main)   │  │  (PR+main)    │
└─────────────┘  └──────────────┘  └───────────────┘
        │                │                  │
        └────────────────┴──────────────────┘
                         │
            ┌────────────┴────────────┐
            │  condition: push to main │
            │  (needs: all 3 above)   │
            ├─────────────────────────┤
            │                         │
     ┌──────▼──────┐          ┌───────▼───────┐
     │  server-e2e │          │   cli-e2e     │
     │ (main only) │          │  (main only)  │
     └──────┬──────┘          └───────┬───────┘
            │                         │
            └────────────┬────────────┘
                         │
                ┌────────▼────────┐
                │  push-to-ghcr   │
                │  (main only)    │
                │  tag: <sha7>    │
                └─────────────────┘
```

## 7. Job Details

### 7.1 `lint` (PR + main)

**Purpose**: Static analysis, fast feedback.

```yaml
runs-on: ubuntu-latest
steps:
  - uses: actions/checkout@v4
  - uses: actions/setup-go@v5
    with:
      go-version: ${{ env.GO_VERSION }}
  - uses: golangci/golangci-lint-action@v6
  - run: go vet ./...
```

### 7.2 `unit-test` (PR + main)

**Purpose**: Run unit tests with database dependencies.

```yaml
runs-on: ubuntu-latest
services:
  postgres:
    image: postgres:16
    env:
      POSTGRES_USER: sequify
      POSTGRES_PASSWORD: sequify
      POSTGRES_DB: sequify
    ports:
      - 5432:5432
  mysql:
    image: mysql:8
    env:
      MYSQL_ROOT_PASSWORD: sequify
      MYSQL_USER: sequify
      MYSQL_PASSWORD: sequify
      MYSQL_DATABASE: sequify
    ports:
      - 3306:3306
steps:
  - uses: actions/checkout@v4
  - uses: actions/setup-go@v5
    with:
      go-version: ${{ env.GO_VERSION }}
      cache: true   # auto-cache Go modules + build cache
  - run: make test
```

`make test` invokes `go test -short ./...`. Store tests run against SQLite (in-memory), PostgreSQL, and MySQL. The PostgreSQL and MySQL service containers provide the required database backends.

### 7.3 `docker-build` (PR + main)

**Purpose**: Verify deploy/Dockerfile builds successfully. Catches deploy/Dockerfile drift before merge.

```yaml
runs-on: ubuntu-latest
steps:
  - uses: actions/checkout@v4
  - run: docker build -t xyncra-server:${{ github.sha }} .
```

Does not push. Validation only.

### 7.4 `server-e2e` (main only)

**Purpose**: Server-side E2E tests with Redis.

```yaml
needs: [lint, unit-test, docker-build]
if: github.event_name == 'push' && github.ref == 'refs/heads/main'
runs-on: ubuntu-latest
steps:
  - uses: actions/checkout@v4
  - uses: actions/setup-go@v5
    with:
      go-version: ${{ env.GO_VERSION }}
  - run: docker compose -f deploy/docker-compose.e2e.yml up -d --wait
  - run: make test-e2e
```

Starts Redis on port 16379 (DB 15), runs `./internal/e2e/` tests.

### 7.5 `cli-e2e` (main only)

**Purpose**: CLI E2E tests with full Docker stack.

```yaml
needs: [lint, unit-test, docker-build]
if: github.event_name == 'push' && github.ref == 'refs/heads/main'
runs-on: ubuntu-latest
steps:
  - uses: actions/checkout@v4
  - uses: actions/setup-go@v5
    with:
      go-version: ${{ env.GO_VERSION }}
  - run: make build-client
  - run: docker compose -f deploy/docker-compose.e2e.yml up -d --wait
  - run: make test-cli-e2e
```

Starts full stack (Redis + xyncra-server), runs `./internal/cli/e2e/` tests.

### 7.6 `push-to-ghcr` (main only)

**Purpose**: Build and push Docker image to GitHub Container Registry.

```yaml
needs: [server-e2e, cli-e2e]
if: github.event_name == 'push' && github.ref == 'refs/heads/main'
runs-on: ubuntu-latest
steps:
  - uses: actions/checkout@v4
  - uses: docker/login-action@v3
    with:
      registry: ghcr.io
      username: ${{ github.actor }}
      password: ${{ secrets.GITHUB_TOKEN }}
  - run: |
      SHA_SHORT=$(echo ${{ github.sha }} | cut -c1-7)
      docker build -t ghcr.io/${{ github.repository }}:${SHA_SHORT} .
      docker push ghcr.io/${{ github.repository }}:${SHA_SHORT}
```

**Image tag**: First 7 characters of commit SHA (e.g., `ghcr.io/pineapplebond/xyncra-server:25933c8`).

**Authentication**: `GITHUB_TOKEN` is auto-injected by GitHub Actions. No manual secret configuration required.

## 8. Workflow File Structure

Single file: `.github/workflows/ci.yml`

Contains all 6 jobs in one workflow. Jobs 1-3 run in parallel on every trigger. Jobs 4-6 run only on push to main, with dependency chain enforcing "all checks pass → push image".

## 9. What This Design Does NOT Include

- **Branch protection rules**: GitHub repo settings, not workflow code. Recommended: require PR status checks (`lint`, `unit-test`, `docker-build`) to pass before merge.
- **Coverage upload**: Project has `coverage.out` locally. CI does not upload coverage artifacts. Can be added later if needed.
- **Notifications**: No Slack/email on failure. GitHub Actions UI shows run status.
- **Image tags beyond commit SHA**: No `latest` tag, no branch tags. Clean, simple tagging.
- **Multi-arch builds**: deploy/Dockerfile builds for linux/amd64 only (matches deployment target). ARM builds can be added via `docker/build-push-action` with `platforms: linux/amd64,linux/arm64` if needed.
- **Docker layer caching in CI**: `docker-build` and `push-to-ghcr` both run `docker build` from scratch. Acceptable for now; can optimize with `docker/build-push-action` + BuildKit cache later.

## 10. Success Criteria

- PR targeting main triggers `lint`, `unit-test`, `docker-build` within ~1 minute
- Push to main triggers full pipeline (all 6 jobs)
- Image is published to GHCR only when all checks pass
- Image tag is short commit SHA (7 chars)
- Concurrency control cancels outdated runs on same ref
- No manual secret configuration required (uses `GITHUB_TOKEN`)

## 11. Out of Scope

- CD deployment to production environments (k8s, ECS, etc.)
- Staging environment promotion
- Release notes / changelog generation
- Semantic versioning tags
- Multi-repo workflows
