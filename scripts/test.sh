#!/bin/bash
# Test script for Xyncra Server.
#
# Starts a disposable Redis container on port 16379 (if not already running)
# and runs the Go test suite with race detection.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

TEST_REDIS_NAME="xyncra-test-redis"
TEST_REDIS_PORT="16379"

cd "${PROJECT_ROOT}"

# ---------------------------------------------------------------------------
# Start test Redis instance if not already running.
# ---------------------------------------------------------------------------
if ! docker ps --format '{{.Names}}' | grep -q "^${TEST_REDIS_NAME}$"; then
    echo "==> Starting test Redis container (${TEST_REDIS_NAME}) on port ${TEST_REDIS_PORT}..."
    docker rm -f "${TEST_REDIS_NAME}" 2>/dev/null || true
    docker run -d \
        --name "${TEST_REDIS_NAME}" \
        -p "${TEST_REDIS_PORT}:6379" \
        redis:7-alpine
    echo "==> Waiting for Redis to be ready..."
    for i in $(seq 1 10); do
        if docker exec "${TEST_REDIS_NAME}" redis-cli ping 2>/dev/null | grep -q PONG; then
            break
        fi
        sleep 1
    done
else
    echo "==> Test Redis container (${TEST_REDIS_NAME}) is already running."
fi

# ---------------------------------------------------------------------------
# Run tests.
# ---------------------------------------------------------------------------
echo "==> Running tests..."
go test ./... -count=1 -race -v
