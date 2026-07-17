---
name: xyncra-tracing
description: Add manual OpenTelemetry tracing spans to store/server methods
triggers:
  - when adding tracing to a new method
  - when creating a new store or server class
  - when asked about tracing conventions
---

# Xyncra Tracing Guide

## Quick Start (5 minutes)

### DB Layer Method

1. Add span constant to `internal/tracing/attributes.go`:
```go
SpanDBMyEntityMyOp = "db.my_entity.my_operation"
```

2. Add span to method:
```go
func (s *MyStore) MyOperation(ctx context.Context, id string) (result *Model, err error) {
    ctx, finish := startSpan(ctx, tracing.SpanDBMyEntityMyOp,
        attribute.String(tracing.AttrConversationID, id))
    defer func() { finish(err) }()
    // ... existing logic
}
```

### Redis Layer Method

1. Add span constant to `internal/tracing/attributes.go`:
```go
SpanRedisMyStoreMyOp = "redis.my_store.my_operation"
```

2. Add span to method:
```go
func (s *RedisMyStore) MyOperation(ctx context.Context, id string) (result *Info, err error) {
    ctx, finish := startRedisSpan(ctx, tracing.SpanRedisMyStoreMyOp,
        attribute.String(tracing.AttrConnID, id))
    defer func() { finish(err) }()
    // ... existing logic
}
```

## Naming Convention

| Pattern | Example | Use For |
|---------|---------|---------|
| `db.<entity>.<op>` | `db.conversation.get` | Store methods |
| `redis.<store>.<op>` | `redis.connection.add` | Redis methods |

Constant names: `SpanDB<Entity><Op>` / `SpanRedis<Store><Op>` (PascalCase, no underscores)

## Available Attribute Keys

All in `internal/tracing/attributes.go` with `xyncra.` prefix:
- `AttrUserID` — user identifier
- `AttrDeviceID` — device identifier
- `AttrConnID` — connection identifier
- `AttrConversationID` — conversation identifier
- `AttrAgentID` — agent identifier
- `AttrMethod` — RPC method name

## Rules

1. **Always use named returns** — `defer func() { finish(err) }()` needs the `err` variable
2. **Pass span-wrapped ctx** to inner calls — creates correct parent-child hierarchy
3. **No auto-instrumentation** — D-127 forbids otelgorm/redisotel/otelhttp
4. **Test your span** — add a spot-check test using tracetest.SpanRecorder

## Verification

```bash
# Verify constant is unique
grep -rn "db.my_entity.my_operation" internal/tracing/attributes.go

# Build check
go build ./...

# Test
go test ./internal/store/... -run "TestMy.*Span" -v
```
