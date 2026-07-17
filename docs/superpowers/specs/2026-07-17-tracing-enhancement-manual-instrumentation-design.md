# Tracing Enhancement: Remove Auto-Instrumentation, Implement Manual Business-Level Tracing

## 1. Problem Statement

The current implementation uses automatic instrumentation libraries (otelgorm, redisotel, otelhttp) which pollute the Jaeger Operation list with low-level infrastructure operations:
- `GORM query`, `get`, `ping` (database operations)
- Redis commands
- HTTP client requests

These operations lack business context and make it difficult to identify root causes of issues.

## 2. Goals

1. **Clean Operation List**: Jaeger Operation list should only show trigger-layer operations (WebSocket handlers, MQ processors, system loops)
2. **Business-Level Tracing**: Database and Redis operations should be traced at the business operation level, not the infrastructure level
3. **Sustainable Methodology**: Provide tools and guidelines for adding tracing to new code in the future
4. **Documentation**: Comprehensive documentation explaining the rationale and methodology

## 3. Design Decisions

### 3.1 Remove Automatic Instrumentation

**Libraries to Remove:**
- `github.com/uptrace/opentelemetry-go-extra/otelgorm` - GORM auto-instrumentation
- `github.com/redis/go-redis/extra/redisotel/v9` - Redis auto-instrumentation
- `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` - HTTP client auto-instrumentation

**Rationale:**
Automatic instrumentation creates spans for every low-level operation (individual SQL queries, Redis commands, HTTP requests), which:
- Pollutes the Operation list with non-business-meaningful names
- Makes it hard to identify actual business operations
- Creates excessive span overhead
- Doesn't align with our tracing strategy of focusing on trigger-layer operations

### 3.2 Manual Instrumentation Strategy

**Span Naming Convention:**

```
Trigger Layer (appears in Operation list):
- ws.handler.<method>           # WebSocket message handlers
- mq.process.<task>             # Message queue task processors
- system.loop.<name>            # Background loop iterations

Business Layer (child spans, not in Operation list):
- db.<entity>.<operation>       # Database operations (repository level)
- redis.<store>.<operation>     # Redis operations (store level)
```

**Why This Structure:**
- Trigger-layer spans represent the entry points that initiate all downstream work
- Business-layer spans provide visibility into DB/Redis performance without cluttering the Operation list
- Clear separation between "what triggered this" and "what resources were used"

### 3.3 Implementation Approach

**Code Changes:**

1. **Remove auto-instrumentation code:**
   - `cmd/xyncra-server/main.go`: Remove `otelhttp.NewTransport`, `redisotel.InstrumentTracing`
   - `internal/store/db.go`: Remove `otelgorm.NewPlugin()`
   - `internal/server/redis_connection_store.go`: Remove `redisotel.InstrumentTracing`

2. **Add tracing helpers:**
   - `internal/store/tracing.go`: DB tracing helpers
   - `internal/server/tracing.go`: Redis tracing helpers (extend existing file)

3. **Implement manual tracing:**
   - Add tracing to all repository methods (DB operations)
   - Add tracing to all store methods (Redis operations)

### 3.4 Tracing Helper Design

**DB Tracing Helper:**
```go
// internal/store/tracing.go
func startDBSpan(ctx context.Context, operation string, attrs ...attribute.KeyValue) (context.Context, func(error)) {
    ctx, span := serverTracer.Start(ctx, operation, trace.WithAttributes(attrs...))
    return ctx, func(err error) {
        if err != nil {
            span.SetStatus(codes.Error, err.Error())
            span.RecordError(err)
        }
        span.End()
    }
}
```

**Redis Tracing Helper:**
```go
// internal/server/tracing.go (extend existing)
func startRedisSpan(ctx context.Context, operation string, attrs ...attribute.KeyValue) (context.Context, func(error)) {
    ctx, span := serverTracer.Start(ctx, operation, trace.WithAttributes(attrs...))
    return ctx, func(err error) {
        if err != nil {
            span.SetStatus(codes.Error, err.Error())
            span.RecordError(err)
        }
        span.End()
    }
}
```

**Usage Example:**
```go
func (r *UserRepository) GetByID(ctx context.Context, id string) (*User, error) {
    ctx, finish := startDBSpan(ctx, "db.user.get",
        attribute.String("xyncra.user.id", id),
    )
    defer finish(nil)
    
    // ... DB operation
    if err != nil {
        finish(err)
        return nil, err
    }
    return user, nil
}
```

## 4. Deliverables

### 4.1 Code Changes

1. Remove automatic instrumentation dependencies from `go.mod`
2. Remove auto-instrumentation code from:
   - `cmd/xyncra-server/main.go`
   - `internal/store/db.go`
   - `internal/server/redis_connection_store.go`
3. Add tracing helpers:
   - `internal/store/tracing.go`
   - Extend `internal/server/tracing.go`
4. Implement manual tracing for:
   - All repository methods in `internal/store/`
   - All store methods in `internal/server/`

### 4.2 Skill Creation

**Location:** `.claude/skills/xyncra-tracing/SKILL.md`

**Purpose:** Reusable guide for adding tracing to new code

**Contents:**
1. When to use this skill (triggers)
2. Scanning methods to find code needing tracing
3. Step-by-step guide for adding tracing
4. Code templates for DB/Redis/Service layers
5. Naming conventions reference
6. Verification methods

### 4.3 Documentation Updates

**Dependencies:**
- `wiki/dependencies/dependency-rationale.md` - Add section: Why we removed automatic instrumentation libraries
  - Problems with automatic instrumentation
  - Operation list pollution
  - Manual vs automatic trade-offs
  - Decision criteria for future instrumentation libraries

- `wiki/dependencies/vendor-management.md` - Update: Evaluation criteria for third-party instrumentation libraries

**Development:**
- `wiki/development/coding-standards.md` - Add: Tracing coding standards
- `wiki/development/developer-guide.md` - Add: Tracing practices section

**Architecture:**
- `wiki/architecture/system-architecture.md` - Update: Tracing architecture diagram
- `wiki/architecture/design-decisions.md` - Add: Manual vs automatic instrumentation decision

**New Documentation:**
- `wiki/development/tracing-guide.md` - Comprehensive tracing guide:
  - Naming conventions
  - How to add tracing (step-by-step)
  - Code examples
  - Best practices
  - Common issues and solutions

## 5. Methodology for Future Tracing Coverage

### 5.1 Scanning Methods

**Find all repository methods needing tracing:**
```bash
# List all public repository methods
grep -r "func.*Repository.*" internal/store/ | grep -v "test"

# Find methods without tracing
grep -L "startDBSpan" internal/store/*_repository.go
```

**Find all store methods needing tracing:**
```bash
# List all public store methods
grep -r "func.*Store.*" internal/server/ | grep -v "test"

# Find methods without tracing
grep -L "startRedisSpan" internal/server/*_store.go
```

### 5.2 Adding Tracing Workflow

1. Identify the method to trace
2. Determine span name following naming convention
3. Add span creation at method entry
4. Add business-relevant attributes
5. Use defer to ensure span completion
6. Record errors appropriately
7. Verify in Jaeger

### 5.3 Code Review Checklist

- [ ] All public repository methods have tracing
- [ ] All public store methods have tracing
- [ ] Span names follow `<layer>.<component>.<operation>` convention
- [ ] Business-relevant attributes are included
- [ ] Errors are properly recorded
- [ ] Tracing guide was followed (see `.claude/skills/xyncra-tracing/SKILL.md`)

## 6. Success Criteria

1. **Jaeger Operation List**: Only shows trigger-layer operations (ws.handler.*, mq.process.*, system.loop.*)
2. **Trace Visibility**: DB and Redis operations are visible as child spans with meaningful names
3. **Performance**: No performance degradation from removing auto-instrumentation
4. **Completeness**: All critical repository and store methods have tracing
5. **Maintainability**: Team can easily add tracing to new code using the skill and guide

## 7. Risks and Mitigations

**Risk 1: Incomplete tracing coverage after removing auto-instrumentation**
- **Mitigation**: Use scanning methods to systematically identify all methods needing tracing
- **Mitigation**: Create skill and guide to ensure consistency

**Risk 2: Performance overhead from manual instrumentation**
- **Mitigation**: Manual instrumentation is lighter than auto-instrumentation (fewer spans)
- **Mitigation**: Use sampling for high-frequency operations

**Risk 3: Inconsistent tracing implementation across team**
- **Mitigation**: Comprehensive skill and guide
- **Mitigation**: Code review checklist
- **Mitigation**: Examples for common patterns

## 8. Implementation Order

1. Create tracing skill and guide (enable team to work efficiently)
2. Remove automatic instrumentation dependencies
3. Add tracing helpers (DB and Redis)
4. Implement manual tracing for repository methods
5. Implement manual tracing for store methods
6. Update documentation
7. Verify in Jaeger

## 9. Open Questions

None - all questions resolved during brainstorming.
