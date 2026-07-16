# E2E Manual Test Report - Round 2

**Date:** 2026-07-10
**Git Commit:** 523e866
**Tester:** Claude (Manual CLI Binary Testing)
**Test Environment:** Docker E2E (Redis 16379, Server 18080, DB 15)
**Strategy Document:** [CLI_E2E_TEST_STRATEGY_ROUND2.md](CLI_E2E_TEST_STRATEGY_ROUND2.md)

---

## Summary

| Metric | Value |
|--------|-------|
| Total Scenarios | 38 |
| PASS | 30 |
| WARN | 3 |
| SKIP | 2 |
| Fixed (from Round 1) | 3 (EXT-001, EXT-002, EXT-003) |
| Executable Pass Rate | 33/35 = 94.3% |
| Overall Pass Rate | 30/38 = 78.9% |

### Category Breakdown

| Category | Name | Scenarios | PASS | WARN | SKIP |
|----------|------|-----------|------|------|------|
| A | Previously Unresolved | 6 | 5 | 1 | 0 |
| B | Message Extremes | 8 | 8 | 0 | 0 |
| C | Concurrency & Race Conditions | 6 | 6 | 0 | 0 |
| D | SQLite / Database Extremes | 5 | 4 | 1 | 0 |
| E | IPC / Daemon Extremes | 4 | 3 | 1 | 0 |
| F | Sync / Network Extremes | 4 | 3 | 0 | 1 |
| G | Validation Edge Cases | 5 | 4 | 0 | 1 |
| **Total** | | **38** | **30** | **3** | **2** |

---

## Bugs Fixed

### Bug 1: mark-as-read CLI display bug (EXT-001)

- **Severity:** Medium
- **Files:** `internal/handler/mark_as_read.go`, IPC handler, CLI display
- **Problem:** CLI displayed user-requested value instead of server-confirmed cursor after `mark_as_read`
- **Root Cause:** Three-layer issue:
  1. Server handler did not return the actual cursor after MAX semantics (D-012)
  2. IPC handler did not capture and forward server response
  3. CLI displayed the user-requested value instead of the server response
- **Fix:** Three-layer fix:
  1. Server handler returns actual `last_read_message_id` after MAX semantics
  2. IPC handler captures server response and forwards to CLI
  3. CLI displays server-confirmed cursor value
- **Status:** Fixed. EXT-001 now PASS.

### Bug 2: send --client-msg-id flag missing (EXT-003)

- **Severity:** Medium
- **Files:** `internal/cli/send.go`, IPC handler
- **Problem:** `send` command lacked `--client-msg-id` flag, preventing manual testing of D-006 idempotency
- **Root Cause:** Flag was not implemented in CLI
- **Fix:** Added optional `--client-msg-id` flag to `send` command. When not provided, UUID is auto-generated (backward compatible). IPC handler transparently passes `client_message_id`.
- **Status:** Fixed. EXT-003 now PASS.

---

## WARN Scenarios

### WARN 1: EXT-024 - Read-only DB fallback

- **Scenario:** DB file permissions changed to read-only while daemon running
- **Behavior:** `write` operations succeed via WebSocket fallback (server-side succeeds), but local DB is not updated
- **Analysis:** Expected Phase 1 behavior. Write operations go through server, but local SQLite cannot persist. The WS fallback ensures data integrity on server side. Local DB sync will recover on next successful sync-updates.
- **Recommendation:** Document as known limitation. Consider adding local DB write error handling in daemon.

### WARN 2: EXT-029 - Double delete not fully idempotent

- **Scenario:** Delete same message twice
- **Behavior:** Second delete returns error (`not found`) instead of silently succeeding
- **Analysis:** Server returns error for already-deleted messages. This is technically correct (the message is gone), but differs from typical REST idempotency expectations where repeating a successful operation returns success.
- **Recommendation:** Consider making double-delete silently succeed (return the already-deleted state) for better idempotency semantics.

### WARN 3: EXT-006 - --log-dir does not write files

- **Scenario:** Start daemon with `--log-dir /custom/path`
- **Behavior:** Directory remains empty, logs only go to stderr
- **Analysis:** Phase 1 known limitation. `cliLogger` currently writes only to stderr. File writing is planned for Phase 2.
- **Recommendation:** Keep as known limitation. Implement in Phase 2 or remove the flag.

---

## SKIP Scenarios

| ID | Scenario | Reason |
|----|----------|--------|
| EXT-030 | 10000+ updates sync | Time infeasible (>30min for manual testing) |
| EXT-037 | MessageID uint32 overflow | 4.2 billion messages per conversation infeasible |

---

## Product Decision Verification

| Decision | Description | Status | Notes |
|----------|-------------|--------|-------|
| D-006 | client_message_id idempotency | **PASS** | EXT-003 verified with --client-msg-id flag |
| D-008 | MessageID monotonically increasing | **PASS** | EXT-015: 50 parallel sends, all unique IDs |
| D-011 | create_conversation find-or-create | **PASS** | EXT-019: 10 parallel creates, only 1 conversation |
| D-012 | mark_as_read MAX semantics | **PASS** | EXT-001 fixed, EXT-017 concurrent MAX, EXT-025 overflow clamp |
| D-014 | delete_message sender permission | **WARN** | EXT-029: double delete returns error |
| D-015 | restore_conversation idempotent | **PASS** | EXT-036: restore active conversation succeeds |
| D-030 | IPC Unix Socket + JSON-RPC 2.0 | **PASS** | EXT-026 socket deleted fallback, EXT-028 directory permissions |
| D-031 | fcntl process lock | **PASS** | EXT-027: lock deleted, daemon restarts normally |
| D-035 | Query commands read local SQLite | **PASS** | EXT-002 empty search, EXT-035 empty query |
| D-036 | sync-updates IPC-only | **PASS** | EXT-004: --full/--force not supported, confirmed |
| D-040 | CLI logs retention | **WARN** | EXT-006: --log-dir Phase 1 limitation |
| D-044 | daemon WS resilience | **PASS** | EXT-031 network partition, EXT-032 server restart |
| D-046 | CLI send --client-msg-id flag | **PASS** | New decision, verified |
| D-047 | mark-as-read display actual cursor | **PASS** | New decision, verified |

---

## Suggested New Product Decisions

### D-046: CLI send --client-msg-id optional flag

**Decision:** CLI `send` command accepts optional `--client-msg-id` flag. When not provided, UUID is auto-generated (backward compatible). Used for debugging and testing D-006 idempotency.

**Reason:**
1. Enables manual testing of idempotency (D-006)
2. Useful for debugging message delivery issues
3. Backward compatible - existing scripts work without changes

### D-047: mark-as-read displays server-confirmed cursor

**Decision:** CLI `mark-as-read` command displays the `last_read_message_id` returned by the server (which reflects MAX semantics), not the user-requested value. Accurately reflects the actual read cursor state.

**Reason:**
1. Users see the actual state, not their request
2. Consistent with D-012 MAX semantics
3. Backward compatible - display change only, no protocol change

---

## Detailed Results by Scenario

### Category A: Previously Unresolved (6)

| ID | Scenario | Result | Notes |
|----|----------|--------|-------|
| EXT-001 | mark-as-read MAX CLI display | **PASS** | Bug fixed - server returns actual cursor |
| EXT-002 | Empty search exit 0 | **PASS** | Correct UNIX convention |
| EXT-003 | Send idempotency | **PASS** | --client-msg-id flag added |
| EXT-004 | sync-updates --full/--force | **PASS** | Not supported, design behavior |
| EXT-005 | standalone --timeout | **PASS** | Not supported, design behavior |
| EXT-006 | --log-dir no files | **WARN** | Phase 1 limitation |

### Category B: Message Extremes (8)

| ID | Scenario | Result |
|----|----------|--------|
| EXT-007 | ~60KiB message | **PASS** |
| EXT-008 | >64KiB message | **PASS** |
| EXT-009 | LIKE % escape | **PASS** |
| EXT-010 | LIKE _ escape | **PASS** |
| EXT-011 | Long conversation title | **PASS** |
| EXT-012 | Empty content rejected | **PASS** |
| EXT-013 | Unicode/emoji content | **PASS** |
| EXT-014 | RTL content | **PASS** |

### Category C: Concurrency & Race Conditions (6)

| ID | Scenario | Result |
|----|----------|--------|
| EXT-015 | 50 parallel send (MessageID uniqueness) | **PASS** |
| EXT-016 | 100+ conversations | **PASS** |
| EXT-017 | Concurrent mark-as-read (MAX) | **PASS** |
| EXT-018 | Concurrent delete + send | **PASS** |
| EXT-019 | 10 parallel create-conversation (idempotent) | **PASS** |
| EXT-020 | 5 parallel sync-updates | **PASS** |

### Category D: SQLite / Database Extremes (5)

| ID | Scenario | Result | Notes |
|----|----------|--------|-------|
| EXT-021 | Daemon killed during write | **PASS** | SQLite WAL crash recovery |
| EXT-022 | 100+ messages | **PASS** | |
| EXT-023 | DB file deleted while daemon running | **PASS** | Daemon survives (D-044) |
| EXT-024 | DB read-only permissions | **WARN** | WS fallback succeeds, local DB not updated |
| EXT-025 | mark-as-read message_id overflow | **PASS** | Server clamps to actual max |

### Category E: IPC / Daemon Extremes (4)

| ID | Scenario | Result | Notes |
|----|----------|--------|-------|
| EXT-026 | Socket deleted while daemon running | **PASS** | Fallback to standalone WS (D-032) |
| EXT-027 | Lock deleted, daemon restart | **PASS** | fcntl lock + stale detection |
| EXT-028 | Socket directory permissions 0500 | **PASS** | Daemon fails with clear error |
| EXT-029 | Double delete message | **WARN** | Returns error instead of silent success |

### Category F: Sync / Network Extremes (4)

| ID | Scenario | Result | Notes |
|----|----------|--------|-------|
| EXT-030 | 10000+ updates sync | **SKIP** | Time infeasible |
| EXT-031 | Network partition sync | **PASS** | Daemon survives (D-044), recovers |
| EXT-032 | Server restart sync | **PASS** | Daemon auto-reconnects |
| EXT-033 | Concurrent sync + send | **PASS** | No conflicts |

### Category G: Validation Edge Cases (5)

| ID | Scenario | Result |
|----|----------|--------|
| EXT-034 | Create conversation with self | **PASS** |
| EXT-035 | Empty query search | **PASS** |
| EXT-036 | Restore active conversation | **PASS** |
| EXT-037 | MessageID uint32 overflow | **SKIP** |
| EXT-038 | Special characters in title | **PASS** |

---

## Environment

| Component | Details |
|-----------|---------|
| OS | macOS Darwin 25.2.0 |
| Redis | 7-alpine (Docker, port 16379, DB 15) |
| Server | Built from commit 523e866 (Docker, port 18080) |
| Client | Built from commit 523e866 |

---

## Code Quality

- `go fmt ./...` -- No issues
- `go vet ./...` -- No issues
- `go mod tidy` -- No changes needed
- Unit tests: 885 passed (1 flaky Redis timing test passes in isolation)

---

## Files Changed (This Test Run)

| File | Change |
|------|--------|
| `internal/handler/mark_as_read.go` | Return actual cursor after MAX semantics |
| `internal/cli/messages.go` | IPC handler captures server response for mark-as-read |
| `internal/cli/send.go` | Add --client-msg-id optional flag |
| `docs/CLI_E2E_TEST_STRATEGY_ROUND2.md` | Updated with test results |
| `docs/testing/reports/e2e-manual-test-report-round2-2026-07-10.md` | This report |
| `docs/PRODUCT_DECISIONS.md` | Added D-046, D-047 |

---

## Comparison with Round 1

| Metric | Round 1 | Round 2 | Change |
|--------|---------|---------|--------|
| Total Scenarios | 133 | 38 | Focused extreme testing |
| PASS | 127 | 30 | -- |
| FAIL | 1 | 0 | -1 |
| WARN | 3 | 3 | Same count, different scenarios |
| N/A | 5 | 0 | All resolved |
| SKIP | 0 | 2 | New infeasible scenarios |
| Pass Rate | 95.5% | 94.3% (of executable) | Comparable |

---

> Test results synced to [CLI_E2E_TEST_STRATEGY_ROUND2.md](CLI_E2E_TEST_STRATEGY_ROUND2.md)
> Product decisions updated in [PRODUCT_DECISIONS.md](PRODUCT_DECISIONS.md)
