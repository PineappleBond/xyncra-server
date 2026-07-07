# Testing Guide for `internal/server`

## Prerequisites

### Docker Redis

All Redis-backed tests require a running Redis instance on port **16379**.

**Start the test Redis container:**

```bash
docker run -d --name xyncra-test-redis -p 16379:6379 redis:7-alpine
```

**Verify it is running:**

```bash
docker exec xyncra-test-redis redis-cli ping
# Expected output: PONG
```

**If the container already exists but is stopped:**

```bash
docker start xyncra-test-redis
```

**If you need to recreate it:**

```bash
docker rm -f xyncra-test-redis
docker run -d --name xyncra-test-redis -p 16379:6379 redis:7-alpine
```

## Running Tests

### Run all tests in the server package

```bash
go test -v ./internal/server/...
```

### Run with race detector

```bash
go test -v -race ./internal/server/...
```

### Run a specific test

```bash
# Only BaseServer tests
go test -v -run TestBaseServer ./internal/server/...

# Only RedisConnectionStore tests
go test -v -run TestRedisConnectionStore ./internal/server/...

# Only ServerConfig tests
go test -v -run TestServerConfig ./internal/server/...

# Only ConnectionInfo tests
go test -v -run TestConnectionInfo ./internal/server/...
```

### Run with coverage

```bash
go test -v -coverprofile=coverage.out ./internal/server/...
go tool cover -html=coverage.out -o coverage.html
```

## Test Scenarios Covered

### ServerConfig (5 sub-tests)

| Test | Description |
|------|-------------|
| `TestServerConfig_Validate/valid_config` | All required fields present |
| `TestServerConfig_Validate/missing_store` | Store is nil |
| `TestServerConfig_Validate/missing_broker` | Broker is nil |
| `TestServerConfig_Validate/missing_connection_store` | ConnectionStore is nil |
| `TestServerConfig_Validate/all_fields_nil` | Entirely zero-value config |

### NewBaseServer (2 tests)

| Test | Description |
|------|-------------|
| `TestNewBaseServer` | Successful creation with valid config |
| `TestNewBaseServer_InvalidConfig` | Rejection of configs missing Store, Broker, or ConnectionStore |

### NewBaseServerFromOptions (3 tests)

| Test | Description |
|------|-------------|
| `TestNewBaseServerFromOptions` | Successful creation via functional options |
| `TestNewBaseServerFromOptions_WithAddrIgnoresEmpty` | Empty address string is ignored |
| `TestNewBaseServerFromOptions_MissingRequired` | Missing required dependencies returns error |

### BaseServer Lifecycle (7 tests)

| Test | Description |
|------|-------------|
| `TestBaseServer_StartStop` | Normal Start -> context cancel -> Stop flow |
| `TestBaseServer_Stop` | Direct Stop() call terminates Start() |
| `TestBaseServer_GracefulStop` | GracefulStop waits for clean shutdown |
| `TestBaseServer_GracefulStop_Timeout` | GracefulStop with expired context returns timeout error |
| `TestBaseServer_StartTwice` | Second Start returns ErrServerAlreadyRunning |
| `TestBaseServer_StartWithCancelledContext` | Start with already-cancelled context returns error |
| `TestBaseServer_StopBeforeStart` | Stop before Start is a safe no-op |
| `TestBaseServer_ConcurrentStartStop` | Concurrent Stop calls do not panic |

### ConnectionInfo (1 test, 6 sub-tests)

| Test | Description |
|------|-------------|
| `TestConnectionInfo_IsExpired/not_expired_with_zero_TTL` | Zero TTL = never expires |
| `TestConnectionInfo_IsExpired/not_expired_with_negative_TTL` | Negative TTL = never expires |
| `TestConnectionInfo_IsExpired/not_expired_within_TTL` | Within TTL window |
| `TestConnectionInfo_IsExpired/expired_past_TTL` | Past TTL window |
| `TestConnectionInfo_IsExpired/exactly_at_TTL_boundary` | Boundary condition |
| `TestConnectionInfo_IsExpired/just_past_TTL_boundary` | One second past TTL |

### RedisConnectionStore - Constructors (6 tests)

| Test | Description |
|------|-------------|
| `TestNewRedisConnectionStore` | Successful creation with default TTL |
| `TestNewRedisConnectionStore_CustomTTL` | Custom default TTL |
| `TestNewRedisConnectionStore_EmptyAddr` | Empty address rejected |
| `TestNewRedisConnectionStore_BadAddr` | Unreachable address rejected |
| `TestNewRedisConnectionStoreFromClient` | Creation from external redis.Client |
| `TestNewRedisConnectionStoreFromClient_ZeroTTL` | Zero TTL falls back to default |

### RedisConnectionStore - CRUD Operations (21 tests)

| Test | Description |
|------|-------------|
| `TestRedisConnectionStore_Add` | Add new connection |
| `TestRedisConnectionStore_Add_OverwritesExisting` | Re-Add overwrites metadata |
| `TestRedisConnectionStore_Add_NilInfo` | Nil info rejected |
| `TestRedisConnectionStore_Add_EmptyID` | Empty connection ID rejected |
| `TestRedisConnectionStore_Add_EmptyUserID` | Empty user ID rejected |
| `TestRedisConnectionStore_Get` | Successful retrieval with all fields |
| `TestRedisConnectionStore_Get_NotFound` | Returns ErrConnectionNotFound |
| `TestRedisConnectionStore_Get_EmptyID` | Empty ID rejected |
| `TestRedisConnectionStore_Remove` | Successful removal |
| `TestRedisConnectionStore_Remove_Nonexistent` | No-op for nonexistent |
| `TestRedisConnectionStore_Remove_EmptyID` | Empty ID rejected |
| `TestRedisConnectionStore_Remove_CleansUpUserSet` | User set cleaned after remove |
| `TestRedisConnectionStore_Exists` | True for existing, false for missing |
| `TestRedisConnectionStore_Exists_EmptyID` | Empty ID rejected |
| `TestRedisConnectionStore_Update` | Metadata updated successfully |
| `TestRedisConnectionStore_Update_NotFound` | Returns ErrConnectionNotFound |
| `TestRedisConnectionStore_Update_EmptyID` | Empty ID rejected |
| `TestRedisConnectionStore_Refresh` | TTL refreshed, UpdatedAt advances |
| `TestRedisConnectionStore_Refresh_NotFound` | Returns ErrConnectionNotFound |
| `TestRedisConnectionStore_Refresh_EmptyID` | Empty ID rejected |

### RedisConnectionStore - User-Level Operations (9 tests)

| Test | Description |
|------|-------------|
| `TestRedisConnectionStore_ListByUser` | Lists all connections for a user |
| `TestRedisConnectionStore_ListByUser_NoConnections` | Returns empty slice |
| `TestRedisConnectionStore_ListByUser_EmptyUserID` | Empty user ID rejected |
| `TestRedisConnectionStore_ListByUser_CleansStaleEntries` | Expired entries cleaned from user set |
| `TestRedisConnectionStore_CountByUser` | Correct count |
| `TestRedisConnectionStore_CountByUser_NoConnections` | Returns 0 |
| `TestRedisConnectionStore_CountByUser_EmptyUserID` | Empty user ID rejected |
| `TestRedisConnectionStore_RemoveByUser` | Removes all user connections and info keys |
| `TestRedisConnectionStore_RemoveByUser_NoConnections` | Returns 0 removed |
| `TestRedisConnectionStore_RemoveByUser_EmptyUserID` | Empty user ID rejected |

### RedisConnectionStore - Health, Close, TTL (6 tests)

| Test | Description |
|------|-------------|
| `TestRedisConnectionStore_Ping` | Ping succeeds |
| `TestRedisConnectionStore_Close` | Close on owned client |
| `TestRedisConnectionStore_Close_DoesNotCloseExternalClient` | External client unaffected |
| `TestRedisConnectionStore_TTL_Expiration` | Connection evicted after default TTL |
| `TestRedisConnectionStore_CustomPerConnectionTTL` | Per-connection TTL honored |
| `TestRedisConnectionStore_KeyPrefix` | Multi-tenant isolation via key prefix |

### RedisConnectionStore - Concurrency (1 test)

| Test | Description |
|------|-------------|
| `TestRedisConnectionStore_ConcurrentAddGet` | 20 concurrent goroutines adding/getting |

### RedisConnectionStoreConfig (1 test, 3 sub-tests)

| Test | Description |
|------|-------------|
| `TestRedisConnectionStoreConfig_resolveDefaultTTL/zero_uses_package_default` | Zero -> 30m |
| `TestRedisConnectionStoreConfig_resolveDefaultTTL/negative_uses_package_default` | Negative -> 30m |
| `TestRedisConnectionStoreConfig_resolveDefaultTTL/custom_value` | Custom value preserved |

### WebSocketServer - Construction (3 tests)

| Test | Description |
|------|-------------|
| `TestNewWebSocketServer_AllOptions` | Server created with all options (path, buffers, compression, timeouts) |
| `TestNewWebSocketServer_MissingConnectionStore` | Missing ConnectionStore returns error |
| `TestNewWebSocketServer_Defaults` | Default path "/ws" and handler applied |

### WebSocketServer - Lifecycle (2 tests)

| Test | Description |
|------|-------------|
| `TestWebSocketServer_StartStop` | Server starts and stops cleanly via context cancel |
| `TestWebSocketServer_GracefulStop` | GracefulStop disconnects all active clients |

### WebSocketServer - Paths (3 tests)

| Test | Description |
|------|-------------|
| `TestWebSocketServer_DefaultPath` | Default "/ws" path accepts connections |
| `TestWebSocketServer_CustomPath` | Custom path via WSWithPath works |
| `TestWebSocketServer_WrongPath` | Non-existent path returns error |

### WebSocketServer - Custom Auth & Utilities (4 tests)

| Test | Description |
|------|-------------|
| `TestWebSocketServer_CustomAuthenticate` | Custom authenticate function used during upgrade |
| `TestWebSocketServer_ClientCountAndClientsByUser` | Accurate client counting across users |
| `TestWebSocketServer_MessageHandlerInstance` | Returns the configured MessageHandler |
| `TestWebSocketMsg_BroadcastUpdates_NoClients` | Broadcasting to absent user is a no-op |

### WebSocket Connection Tests (7 tests)

| Test | Description |
|------|-------------|
| `TestWebSocketConn_MissingUserID` | Connection without user_id rejected (401) |
| `TestWebSocketConn_EmptyUserID` | Connection with empty user_id rejected (401) |
| `TestWebSocketConn_ClientCountIncrease` | ClientCount increases after connect |
| `TestWebSocketConn_ClientCountDecrease` | ClientCount decreases after disconnect |
| `TestWebSocketConn_MultipleConnectionsSameUser` | Same user can have multiple connections |
| `TestWebSocketConn_RegisteredInConnectionStore` | Connection registered in Redis ConnectionStore |
| `TestWebSocketConn_ConnectionRemovedFromStoreOnDisconnect` | Connection removed from store on disconnect |

### Message Communication Tests (9 tests)

| Test | Description |
|------|-------------|
| `TestWebSocketMsg_RequestResponse` | Request triggers response (unknown method error) |
| `TestWebSocketMsg_MethodHandler` | Registered MethodHandler processes requests |
| `TestWebSocketMsg_UnknownMethodReturnsError` | Unknown method returns error response |
| `TestWebSocketMsg_BroadcastUpdates` | BroadcastUpdates reaches target user only |
| `TestWebSocketMsg_BroadcastUpdates_NilUpdates` | Nil updates returns error |
| `TestWebSocketMsg_MalformedMessageDoesNotDisconnect` | Malformed JSON does not disconnect client |
| `TestWebSocketMsg_HandlerErrorReturnsErrorResponse` | Handler error propagated to client |
| `TestWebSocketMsg_ConcurrentRequestResponse` | Multiple concurrent request-response flows |
| `TestDefaultMessageHandler_NonRequestTypes` | Non-Request packages are ignored |

### DefaultMessageHandler Tests (5 tests)

| Test | Description |
|------|-------------|
| `TestDefaultMessageHandler_RegisterMethod` | Method registered and invoked via HandleMessage |
| `TestDefaultMessageHandler_RegisterMethodFunc` | Convenience wrapper RegisterMethodFunc works |
| `TestDefaultMessageHandler_SetFallback` | Fallback handler invoked for unknown methods |
| `TestDefaultMessageHandler_ConcurrentRegisterAndCall` | Concurrent register and invoke are safe |
| `TestDefaultMessageHandler_MethodOverwrite` | Re-registering overwrites previous handler |

### Client Tests (6 tests)

| Test | Description |
|------|-------------|
| `TestClient_SendAndReceive` | Messages sent via SendPackage delivered to peer |
| `TestClient_SendBufferFullDropsMessage` | Full send buffer drops messages without blocking |
| `TestClient_CloseIdempotent` | Multiple Close calls do not panic |
| `TestClient_SendAfterClose` | Send after Close is a no-op |
| `TestClient_Accessors` | UserID and ConnID return correct values |
| `TestClient_PingPong` | Server sends periodic pings, connection stays alive |

## Running Only WebSocket Tests

```bash
# All WebSocket-related tests
go test -v -run "TestNewWebSocketServer|TestWebSocketServer_|TestWebSocketConn_|TestWebSocketMsg_|TestDefaultMessageHandler_|TestClient_" ./internal/server/...

# Only connection tests
go test -v -run "TestWebSocketConn" ./internal/server/...

# Only message handler tests
go test -v -run "TestDefaultMessageHandler" ./internal/server/...
```

## Cleanup

### Stop the test Redis container

```bash
docker stop xyncra-test-redis
```

### Remove the test Redis container entirely

```bash
docker rm -f xyncra-test-redis
```

The tests use Redis database 15 and flush the database before and after each
test function, so they should not leave any residual data. If you need to
manually flush the test database:

```bash
docker exec xyncra-test-redis redis-cli -n 15 FLUSHDB
```
