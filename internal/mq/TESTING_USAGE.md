# Message Queue Testing Guide

This document describes how to run and understand the test suite for the `internal/mq` package.

## Test Files Overview

The package contains three test files:

1. **handler_test.go** - Tests for the `TaskHandler` registry and routing logic
2. **asynq_test.go** - Tests for the Asynq broker implementation (requires Redis)
3. **options_test.go** - Tests for enqueue option configuration (already existed)

## Running Tests

### Run All Tests

```bash
# From project root
go test ./internal/mq/... -v

# Run with race detector (recommended)
go test ./internal/mq/... -race -v
```

### Run Specific Test Files

```bash
# Handler tests only
go test ./internal/mq -run TestTaskHandler -v
go test ./internal/mq -run TestProcessTask -v
go test ./internal/mq -run TestRegister -v

# Asynq broker tests only
go test ./internal/mq -run TestAsynq -v
go test ./internal/mq -run TestEnqueue -v
go test ./internal/mq -run TestStart -v

# Options tests only
go test ./internal/mq -run TestWith -v
```

### Run Specific Test Functions

```bash
# Single test function
go test ./internal/mq -run TestTaskHandler_ConcurrentRegister -v
go test ./internal/mq -run TestEnqueue_WithOptions -v
```

## Docker Redis Setup

The Asynq broker tests require a Redis instance. The test suite automatically:
1. Starts a Redis container on port 6379
2. Waits for Redis to be ready
3. Runs the tests
4. Cleans up the container

### Manual Docker Redis Management

If you need to manage the Redis container manually:

```bash
# Start Redis
docker run -d --name xyncra-test-redis -p 6379:6379 redis:7-alpine

# Check Redis status
docker ps | grep xyncra-test-redis

# Stop and remove
docker rm -f xyncra-test-redis
```

### Using Existing Redis

If you already have Redis running on port 6379, the tests will use it automatically and skip the Docker setup.

## Test Coverage

### handler_test.go

Tests the `TaskHandler` message routing system:

- **TaskHandler Creation**
  - `TestNewTaskHandler` - Verifies empty registry initialization

- **Handler Registration**
  - `TestRegister` - Tests adding handlers for task types
  - `TestUnregister` - Tests removing handlers
  - `TestHasHandler` - Tests handler existence checks

- **Task Processing**
  - `TestProcessTask_Routing` - Verifies tasks are routed to correct handlers
  - `TestProcessTask_NilTask` - Tests nil task validation
  - `TestProcessTask_UnregisteredType` - Tests error handling for unregistered types
  - `TestProcessTask_HandlerErrorPropagation` - Verifies handler errors are propagated

- **Concurrency**
  - `TestTaskHandler_ConcurrentAccess` - Tests thread-safe concurrent access
  - `TestTaskHandler_ConcurrentRegister` - Tests concurrent handler registration

### asynq_test.go

Tests the Asynq broker implementation with real Redis:

- **Broker Lifecycle**
  - `TestNewAsynqBroker` - Tests broker creation with valid/invalid configs
  - `TestAsynqBroker_Close` - Tests resource cleanup

- **Task Enqueueing**
  - `TestEnqueue_NilTask` - Tests nil task validation
  - `TestEnqueue_EmptyType` - Tests empty type validation
  - `TestEnqueue_Success` - Tests successful task enqueue
  - `TestEnqueue_WithOptions` - Tests all enqueue options (Queue, MaxRetry, Timeout, TaskID, Retention, ProcessIn, Unique)

- **Worker Processing**
  - `TestStart_ProcessesTask` - Tests end-to-end task processing with worker
  - `TestStart_NilHandler` - Tests nil handler validation
  - `TestStart_DoubleStart` - Tests that double-start fails
  - `TestStop_GracefulShutdown` - Tests graceful worker shutdown

- **Helper Functions**
  - `TestBuildAsynqOptions` - Tests conversion of enqueue options to Asynq format
  - `TestTaskIDFromContext` - Tests task ID extraction from context
  - `TestDecodeAsynqTask` - Tests task payload decoding

## Test Output

Successful test run output:

```
=== RUN   TestTaskHandler
=== RUN   TestTaskHandler/registers_handler
=== RUN   TestTaskHandler/processes_task
--- PASS: TestTaskHandler (0.00s)
    --- PASS: TestTaskHandler/registers_handler (0.00s)
    --- PASS: TestTaskHandler/processes_task (0.00s)
PASS
ok      github.com/PineappleBond/xyncra-server/internal/mq      0.123s
```

## Troubleshooting

### Redis Connection Failed

**Symptom**: Tests skip with "Redis not available"

**Solutions**:
1. Check if Docker is running: `docker ps`
2. Manually start Redis: `docker run -d --name xyncra-test-redis -p 6379:6379 redis:7-alpine`
3. Verify Redis is accessible: `docker exec xyncra-test-redis redis-cli ping` (should return PONG)

### Port 6379 Already in Use

**Symptom**: Docker container fails to start

**Solutions**:
1. Find existing Redis: `lsof -i :6379`
2. Stop it or use a different port
3. Or remove old container: `docker rm -f xyncra-test-redis`

### Race Detector Warnings

If you see race warnings:
1. Ensure all test handlers are properly synchronized
2. Check that `TaskHandler` uses mutex correctly
3. Report as a bug if it's in production code

### Test Timeout

If tests hang:
1. Check Redis container health: `docker logs xyncra-test-redis`
2. Restart Redis: `docker rm -f xyncra-test-redis && docker run -d --name xyncra-test-redis -p 6379:6379 redis:7-alpine`
3. Increase timeout: `go test ./internal/mq/... -timeout 5m`

## Test Data

Tests use the following task types:
- `test:task` - Generic test task
- `test:handler` - Handler-specific tests
- `test:concurrent` - Concurrency tests
- `test:enqueue` - Enqueue operation tests
- `test:process` - Task processing tests

All test data is ephemeral and cleaned up after each test.

## Continuous Integration

In CI environments:

```yaml
# Example GitHub Actions
- name: Start Redis
  run: docker run -d --name redis -p 6379:6379 redis:7-alpine

- name: Run MQ tests
  run: go test ./internal/mq/... -race -v

- name: Cleanup
  if: always()
  run: docker rm -f redis
```

## Code Coverage

View test coverage:

```bash
# Generate coverage report
go test ./internal/mq/... -coverprofile=coverage.out

# View in browser
go tool cover -html=coverage.out

# View in terminal
go tool cover -func=coverage.out
```

Target coverage: >80% for critical paths (handler routing, enqueue validation).
