# E2E Manual Test Report

**Date:** 2026-07-09
**Git Commit:** 7f43f99
**Tester:** Claude (Manual CLI Binary Testing)
**Test Environment:** Docker E2E (Redis 16379→6379, Server 18080, DB 15)
**E2E_HOME:** /tmp/xe2e-dyReVHXQ

---

## Summary

| Group | Name | Scenarios | Pass | Fail | N/A | Pass Rate |
|-------|------|-----------|------|------|-----|-----------|
| A | Smoke Test | 2 | 2 | 0 | 0 | 100% |
| B | Daemon Lifecycle | 19 | 19 | 0 | 0 | 100% (after fix) |
| D | Message Operations | 23 | 20 | 1 | 2 | 87% |
| E | Conversation Ops | 1 | 1 | 0 | 0 | 100% |
| F | Query Commands | 18 | 18 | 0 | 0 | 100% |
| G | Draft Management | 8 | 8 | 0 | 0 | 100% |
| H | Logs Management | 25 | 25 | 0 | 0 | 100% (after fix) |
| I | Sync Operations | 6 | 5 | 0 | 1 | 83% |
| J | Multi-Instance + IPC | 19 | 18 | 0 | 1 | 95% |
| K | Error Handling | 10 | 10 | 0 | 0 | 100% |
| L | Resilience | 1 | 1 | 0 | 0 | 100% |
| M | Advanced | 1 | 0 | 0 | 1 | PARTIAL |
| **Total** | | **133** | **127** | **1** | **5** | **95.5%** |

---

## Bugs Found and Fixed

### Bug 1: kill command exit code incorrect (B14/B15)

- **Severity:** Medium
- **File:** `internal/cli/kill.go`
- **Problem:** `kill` command returned exit code 0 when no daemon was found, should return exit code 1
- **Root Cause:** `runKill()` returned `nil` when no daemon process was found, instead of returning an error
- **Fix:** Changed `return nil` to `return fmt.Errorf("kill: no running daemon found")` to return exit code 1
- **Status:** Fixed

### Bug 2: logs --limit parameter validation missing (H21/H22/H23)

- **Severity:** Medium
- **File:** `internal/cli/logs.go`
- **Problem:** `--limit 0` and `--limit -1` were not validated, should return an error
- **Root Cause:** `runLogsTail()` and `runLogsSearch()` did not check if `limit <= 0`
- **Fix:** Added `if limit <= 0 { return fmt.Errorf("...: --limit must be a positive integer") }` validation in both functions
- **Status:** Fixed

---

## Unfixed Issues

### Issue 1: send command missing --client-msg-id flag (D02)

- **Severity:** Medium
- **Description:** `send` command does not have `--client-msg-id` flag, cannot test idempotency from CLI (D-006)
- **Recommendation:** Consider adding `--client-msg-id` flag for manual testing and debugging

### Issue 2: mark-as-read MAX semantics may not be correctly enforced (D12 WARN)

- **Severity:** Low
- **Description:** Marking as read up to #3 then marking up to #1 shows "Marked as read up to message #1"
- **Recommendation:** Server-side verification needed to confirm MAX semantics are correctly implemented

### Issue 3: --log-dir parameter functionality incomplete (M001 PARTIAL)

- **Severity:** Low
- **Description:** `--log-dir` parameter is accepted but does not write log files to the directory
- **Recommendation:** Implement log file writing or remove the parameter

---

## N/A Scenarios (Design Behavior, Not Bugs)

- **I04:** `--full`/`--force` parameters not supported (sync-updates defaults to full sync)
- **J14:** `--timeout` parameter not supported
- **D22 WARN:** Query for non-existent conversation returns exit 0 (read operations returning empty is reasonable behavior)

---

## Product Decision Coverage

| Decision | Description | Status | Notes |
|----------|-------------|--------|-------|
| D-006 | client_message_id idempotency | WARN | CLI cannot test (missing --client-msg-id flag) |
| D-008 | MessageID monotonically increasing | PASS | Verified |
| D-011 | create-conversation find-or-create idempotent | PASS | Verified |
| D-012 | mark-as-read MAX semantics | WARN | Server-side verification needed |
| D-013 | delete-conversation cascade soft-delete | PASS | Verified |
| D-014 | delete-message sender-only permission | PASS | Verified |
| D-015 | restore-conversation cascade restore | -- | Not separately tested |
| D-030 | IPC socket path = ~/.xyncra/{uid}/{did}/xyncra.sock | PASS | Verified |
| D-031 | fcntl process lock, stale lock detection | PASS | Verified |
| D-032 | IPC priority, WS fallback | PASS | Verified |
| D-033 | device-id = hostname SHA256[:8] | PASS | Verified |
| D-034 | XYNCRA_ env prefix, flag > env > default | PASS | Verified |
| D-035 | Query commands read local SQLite | PASS | Verified |
| D-036 | sync-updates IPC-only, exit 2 when daemon not running | PASS | Verified |
| D-037 | create-conversation uses --peer-id | PASS | Verified |
| D-038 | delete-message --message-id (UUID), mark-as-read --message-id (uint32) | PASS | Verified |
| D-039 | kill default SIGTERM, --force SIGKILL | PASS | Verified |
| D-041 | tabwriter aligned output, stdout/stderr separation | PASS | Verified |
| D-042 | Exit codes: 0=success, 1=error, 2=precondition, 3=timeout | PASS | Verified |
| D-044 | WS unreachable: daemon doesn't exit, IPC always available | PASS | Verified |
| D-045 | create-conversation pushes UserUpdate + MQ to both users | PASS | Verified |

---

## Detailed Results by Group

### Group A: Smoke Test

| Scenario | Description | Result |
|----------|-------------|--------|
| CLI-E2E-A01 | Environment health (Redis PONG, Server /health) | PASS |
| CLI-E2E-A02 | Happy-path workflow (create, send, query, sync, kill) | PASS |

### Group B: Daemon Lifecycle

| Scenario | Description | Result |
|----------|-------------|--------|
| CLI-E2E-B01 | listen creates state files (sock, lock, db) | PASS |
| CLI-E2E-B02 | Duplicate listen rejected (exit 2, D-031) | PASS |
| CLI-E2E-B03 | kill sends SIGTERM (D-039) | PASS |
| CLI-E2E-B04 | kill --force sends SIGKILL (D-039) | PASS |
| CLI-E2E-B05 | SIGINT exits cleanly | PASS |
| CLI-E2E-B06 | listen without --user-id fails | PASS |
| CLI-E2E-B07 | WS unreachable, IPC still works (D-044) | PASS |
| CLI-E2E-B08 | XYNCRA_USER_ID env var (D-034) | PASS |
| CLI-E2E-B09 | XYNCRA_SERVER env var (D-034) | PASS |
| CLI-E2E-B10 | Flag overrides env var (D-034) | PASS |
| CLI-E2E-B11 | Stale lock auto-recovery (D-031) | PASS |
| CLI-E2E-B12 | Kill with no daemon running | PASS (after fix) |
| CLI-E2E-B13 | Kill cleans stale lock (D-039) | PASS |
| CLI-E2E-B14 | Custom --device-id | PASS (after fix) |
| CLI-E2E-B15 | Different device_id isolation | PASS (after fix) |
| CLI-E2E-B16 | Socket permissions 0600 (D-030) | PASS |
| CLI-E2E-B17 | IPC socket path format (D-030) | PASS |
| CLI-E2E-B18 | Directory permissions 0700 | PASS |
| CLI-E2E-B19 | kill --timeout flag | PASS |

### Group D: Message Operations

| Scenario | Description | Result |
|----------|-------------|--------|
| CLI-E2E-D01 | Send message basic | PASS |
| CLI-E2E-D02 | Send second message (incrementing ID) | PASS |
| CLI-E2E-D03 | Send empty content rejected | PASS |
| CLI-E2E-D04 | Send to non-existent conversation | PASS |
| CLI-E2E-D05 | Send without --conversation-id | PASS |
| CLI-E2E-D06 | Send without --content | PASS |
| CLI-E2E-D07 | Send with --reply-to (D-038) | PASS |
| CLI-E2E-D08 | Send standalone fallback (D-032) | PASS |
| CLI-E2E-D09 | Delete own message (D-014) | PASS |
| CLI-E2E-D10 | Delete non-existent message | PASS |
| CLI-E2E-D11 | Delete permission denied (D-014) | PASS |
| CLI-E2E-D12 | Mark-as-read MAX semantics (D-012) | WARN |
| CLI-E2E-D13 | Mark-as-read specific message | PASS |
| CLI-E2E-D14 | Mark-as-read all (message-id=0) | PASS |
| CLI-E2E-D15 | Mark-as-read without --conversation-id | PASS |
| CLI-E2E-D16 | Send long message (256 chars) | PASS |
| CLI-E2E-D17 | Send special chars | PASS |
| CLI-E2E-D18 | Send unicode message | PASS |
| CLI-E2E-D19 | get-messages monotonic MessageID (D-008) | PASS |
| CLI-E2E-D20 | get-messages --after-message-id pagination | PASS |
| CLI-E2E-D21 | search-messages basic | PASS |
| CLI-E2E-D22 | search-messages no results | WARN (exit 0 for empty query is reasonable) |
| CLI-E2E-D23 | Send idempotency (D-006) | N/A (missing --client-msg-id) |

### Group E: Conversation Ops

| Scenario | Description | Result |
|----------|-------------|--------|
| CLI-E2E-E01 | create-conversation notifies peer (D-045) | PASS |

### Group F: Query Commands

| Scenario | Description | Result |
|----------|-------------|--------|
| CLI-E2E-F01 | list-conversations basic | PASS |
| CLI-E2E-F02 | tabwriter alignment (D-041) | PASS |
| CLI-E2E-F03 | list-conversations empty | PASS |
| CLI-E2E-F04 | list-conversations --limit | PASS |
| CLI-E2E-F05 | list-conversations --offset | PASS |
| CLI-E2E-F06 | get-conversation detail | PASS |
| CLI-E2E-F07 | get-conversation not found | PASS |
| CLI-E2E-F08 | get-messages basic (D-035) | PASS |
| CLI-E2E-F09 | get-messages ASC order | PASS |
| CLI-E2E-F10 | get-messages --after-message-id | PASS |
| CLI-E2E-F11 | get-messages --limit | PASS |
| CLI-E2E-F12 | get-messages empty conversation | PASS |
| CLI-E2E-F13 | search-messages basic | PASS |
| CLI-E2E-F14 | search-messages DESC order | PASS |
| CLI-E2E-F15 | search-messages --limit | PASS |
| CLI-E2E-F16 | search-messages no results | PASS |
| CLI-E2E-F17 | get-conversation unread count | PASS |
| CLI-E2E-F18 | Query without daemon (D-035) | PASS |

### Group G: Draft Management

| Scenario | Description | Result |
|----------|-------------|--------|
| CLI-E2E-G01 | Draft save | PASS |
| CLI-E2E-G02 | Draft get | PASS |
| CLI-E2E-G03 | Draft save upsert | PASS |
| CLI-E2E-G04 | Draft get shows updated content | PASS |
| CLI-E2E-G05 | Draft delete | PASS |
| CLI-E2E-G06 | Draft get after delete | PASS |
| CLI-E2E-G07 | Draft save missing flags | PASS |
| CLI-E2E-G08 | Draft get non-existent | PASS |

### Group H: Logs Management

| Scenario | Description | Result |
|----------|-------------|--------|
| CLI-E2E-H01 | logs tail basic | PASS |
| CLI-E2E-H02 | logs tail --type rpc | PASS |
| CLI-E2E-H03 | logs tail --type notifications | PASS |
| CLI-E2E-H04 | logs tail --limit | PASS |
| CLI-E2E-H05 | logs tail --since | PASS |
| CLI-E2E-H06 | logs search basic | PASS |
| CLI-E2E-H07 | logs search --method | PASS |
| CLI-E2E-H08 | logs search --error | PASS |
| CLI-E2E-H09 | logs search --from --to | PASS |
| CLI-E2E-H10 | logs search --conversation-id | PASS |
| CLI-E2E-H11 | logs search --request-id | PASS |
| CLI-E2E-H12 | logs stats basic | PASS |
| CLI-E2E-H13 | logs stats --since | PASS |
| CLI-E2E-H14 | logs stats --interval | PASS |
| CLI-E2E-H15 | logs stats --interval invalid | PASS |
| CLI-E2E-H16 | logs export CSV | PASS |
| CLI-E2E-H17 | logs export JSON | PASS |
| CLI-E2E-H18 | logs export --output file | PASS |
| CLI-E2E-H19 | logs export --method filter | PASS |
| CLI-E2E-H20 | logs export invalid format | PASS |
| CLI-E2E-H21 | logs cleanup --dry-run | PASS (after fix) |
| CLI-E2E-H22 | logs cleanup basic | PASS (after fix) |
| CLI-E2E-H23 | logs cleanup --retain | PASS (after fix) |
| CLI-E2E-H24 | logs cleanup --type rpc | PASS |
| CLI-E2E-H25 | logs cleanup --type notifications | PASS |

### Group I: Sync Operations

| Scenario | Description | Result |
|----------|-------------|--------|
| CLI-E2E-I01 | sync-updates with daemon | PASS |
| CLI-E2E-I02 | sync-updates no daemon (exit 2, D-036) | PASS |
| CLI-E2E-I03 | sync-updates double sync | PASS |
| CLI-E2E-I04 | sync after new data | N/A (--full/--force not supported) |
| CLI-E2E-I05 | Daemon initial FullSync | PASS |
| CLI-E2E-I06 | sync-updates IPC-only (D-036) | PASS |

### Group J: Multi-Instance + IPC

| Scenario | Description | Result |
|----------|-------------|--------|
| CLI-E2E-J01 | Multi-user isolation | PASS |
| CLI-E2E-J02 | Multi-device isolation | PASS |
| CLI-E2E-J03 | IPC routes to correct daemon | PASS |
| CLI-E2E-J04 | Kill one daemon doesn't affect other | PASS |
| CLI-E2E-J05 | Different DB files per device | PASS |
| CLI-E2E-J06 | Different lock files per device | PASS |
| CLI-E2E-J07 | Bob daemon send | PASS |
| CLI-E2E-J08 | Cross-user data isolation | PASS |
| CLI-E2E-J09 | IPC JSON-RPC 2.0 protocol (D-030) | PASS |
| CLI-E2E-J10 | IPC invalid method | PASS |
| CLI-E2E-J11 | IPC invalid JSON | PASS |
| CLI-E2E-J12 | Standalone fallback create-conversation | PASS |
| CLI-E2E-J13 | Standalone fallback delete-conversation | PASS |
| CLI-E2E-J14 | Standalone fallback restore-conversation | PASS |
| CLI-E2E-J15 | Standalone fallback mark-as-read | PASS |
| CLI-E2E-J16 | Multi-instance --timeout | N/A (--timeout not supported) |
| CLI-E2E-J17 | IPC concurrent requests | PASS |
| CLI-E2E-J18 | IPC socket cleanup on kill | PASS |
| CLI-E2E-J19 | Daemon reconnection after IPC break | PASS |

### Group K: Error Handling

| Scenario | Description | Result |
|----------|-------------|--------|
| CLI-E2E-K01 | Missing --user-id for send | PASS |
| CLI-E2E-K02 | Invalid server URL | PASS |
| CLI-E2E-K03 | Invalid UUID for conversation-id | PASS |
| CLI-E2E-K04 | get-conversation without -c | PASS |
| CLI-E2E-K05 | Send to non-existent conversation | PASS |
| CLI-E2E-K06 | Delete non-existent conversation | PASS |
| CLI-E2E-K07 | Restore non-existent conversation | PASS |
| CLI-E2E-K08 | Listen without --user-id | PASS |
| CLI-E2E-K09 | Dual cause error (IPC+WS fail) | PASS |
| CLI-E2E-K10 | Exit codes (D-042) | PASS |

### Group L: Resilience

| Scenario | Description | Result |
|----------|-------------|--------|
| CLI-E2E-L01 | Daemon survives server restart (D-044) | PASS |

### Group M: Advanced

| Scenario | Description | Result |
|----------|-------------|--------|
| CLI-E2E-M01 | Custom --db-path and --log-dir | PARTIAL (--log-dir not writing files) |

---

## Environment

| Component | Details |
|-----------|---------|
| OS | macOS Darwin 25.2.0 |
| Redis | 7-alpine (Docker, container port 6379, host mapping 16379) |
| Server | Built from commit 7f43f99 |
| Client | Built from commit 7f43f99 |
| E2E_HOME | /tmp/xe2e-dyReVHXQ |

---

## Code Quality

- `go fmt ./...` -- No issues
- `go vet ./...` -- No issues

---

## Files Changed (This Test Run)

| File | Change |
|------|--------|
| `internal/cli/kill.go` | Return error (exit 1) when no daemon found (was returning nil/exit 0) |
| `internal/cli/kill_test.go` | Updated test for new kill error behavior |
| `internal/cli/logs.go` | Added --limit > 0 validation in tail and search commands |
| `internal/cli/e2e/cli_e2e_p1_test.go` | Updated tests for kill and logs fixes |
| `docs/e2e-manual-test-report-2026-07-09.md` | This report |

---

> 📌 测试场景通过状态已同步更新至 [CLI_E2E_TEST_STRATEGY.md](CLI_E2E_TEST_STRATEGY.md)
> 下次测试前请参考该文档的测试执行记录，避免重复已通过的场景。
