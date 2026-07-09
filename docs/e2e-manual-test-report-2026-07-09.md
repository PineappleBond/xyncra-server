# Xyncra Client E2E Manual Test Report

**Date:** 2026-07-09
**Tester:** Claude (Manual CLI Binary Testing)
**Branch:** main

---

## Summary

| Group | Name | Scenarios | Pass | Fail | Pass Rate |
|-------|------|-----------|------|------|-----------|
| A | Smoke Test | 2 | 2 | 0 | 100% |
| B | Daemon Lifecycle | 19 | 19 | 0 | 100% |
| D | Message Operations | 23 | 22 | 1 | 96% |
| E | Conversation Ops | 2 | 2 | 0 | 100% |
| F | Query Commands | 18 | 18 | 0 | 100% |
| G | Draft Management | 8 | 8 | 0 | 100% |
| H | Logs Management | 25 | 25 | 0 | 100% |
| I | Sync Operations | 6 | 6 | 0 | 100% |
| J | Multi-Instance + IPC | 15 | 15 | 0 | 100% |
| K | Error Handling | 10 | 10 | 0 | 100% |
| L | Resilience | 1 | 1 | 0 | 100% |
| M | Advanced | 1 | 1 | 0 | 100% |
| **Total** | | **130** | **129** | **1** | **99.2%** |

**Note:** D-09 initially failed because `delete-message --message-id` requires the Message UUID primary key, but the `send` command did not display it. **Fixed** during this test run by adding UUID output to `send`. After fix, D-09 passes. The table above reflects post-fix results.

---

## Bugs Found and Fixed

### Bug 1: sync-updates returns exit code 1 instead of 2 (D-036/D-042)

- **Severity:** Medium
- **Scenario:** I-02, I-06
- **Description:** When `sync-updates` is called without a running daemon, it returned exit code 1 (generic error) instead of exit code 2 (precondition not met) as specified by D-036 and D-042.
- **Root Cause:** `internal/cli/sync.go` used `return fmt.Errorf(...)` which propagates through Cobra as exit code 1, instead of `os.Exit(2)`.
- **Fix:** Changed `return fmt.Errorf(...)` to `os.Exit(2)` in `runSyncUpdates` when IPC connection fails.
- **Files Changed:**
  - `internal/cli/sync.go` - Use `os.Exit(2)` for daemon-not-running
  - `internal/cli/sync_test.go` - Updated test that called through `cobra.Execute()` (incompatible with `os.Exit`)
  - `internal/cli/e2e/cli_e2e_test.go` - Updated expected exit code from 1 to 2

### Bug 2: delete-message unusable from CLI (D-09)

- **Severity:** High
- **Scenario:** D-09
- **Description:** `delete-message --message-id` expects the Message primary key UUID, but no CLI command displayed this UUID. The `send` output showed `Message ID` (uint32 sequence), `Client Msg ID` (idempotency UUID), and `Conversation` ID, but NOT the Message.ID UUID needed for deletion.
- **Root Cause:** `printSendResult()` in `internal/cli/send.go` did not print `result.Message.ID`.
- **Fix:** Added `UUID: <id>` line to `printSendResult` output, between `Message ID` and `Conversation`.
- **Files Changed:**
  - `internal/cli/send.go` - Added UUID output line
  - `internal/cli/send_test.go` - Updated test to verify UUID in output

---

## Product Decision Coverage

| Decision | Description | Covered | Result |
|----------|-------------|---------|--------|
| D-006 | client_message_id idempotency (Duplicate: true) | A-02 | PASS (server-side; client auto-generates UUID) |
| D-008 | MessageID monotonically increasing | A-02, D-20 | PASS |
| D-011 | create-conversation find-or-create idempotent | A-02, E-02 | PASS |
| D-012 | mark-as-read MAX semantics | D-15 | PASS |
| D-013 | delete-conversation cascade soft-delete | (via existing E2E) | PASS |
| D-014 | delete-message sender-only permission | D-11 | PASS |
| D-015 | restore-conversation cascade restore | (via existing E2E) | PASS |
| D-030 | IPC socket path = ~/.xyncra/{uid}/{did}/xyncra.sock | B-17 | PASS |
| D-031 | fcntl process lock, stale lock detection | B-02, B-11 | PASS |
| D-032 | IPC priority, WS fallback | J-12 to J-15 | PASS |
| D-033 | device-id = hostname SHA256[:8] | B-14 | PASS |
| D-034 | XYNCRA_ env prefix, flag > env > default | B-08, B-09, B-10 | PASS |
| D-035 | Query commands read local SQLite | F-18 | PASS |
| D-036 | sync-updates IPC-only, exit 2 when daemon not running | I-02, I-06 | PASS (after fix) |
| D-037 | create-conversation uses --peer-id | A-02 | PASS |
| D-038 | delete-message --message-id (UUID), mark-as-read --message-id (uint32) | D-09, D-13 | PASS |
| D-039 | kill default SIGTERM, --force SIGKILL | B-03, B-04 | PASS |
| D-040 | logs retain 7d | H-21 to H-25 | PASS |
| D-041 | tabwriter aligned output, stdout/stderr separation | F-02, K-09 | PASS |
| D-042 | Exit codes: 0=success, 1=error, 2=precondition, 3=timeout | K-10 | PASS |
| D-044 | WS unreachable: daemon doesn't exit, IPC always available | B-07 | PASS |
| D-045 | create-conversation pushes UserUpdate + MQ to both users | E-01 | PASS (after Docker rebuild) |

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
| CLI-E2E-B12 | Kill with no daemon running | PASS |
| CLI-E2E-B13 | Kill cleans stale lock (D-039) | PASS |
| CLI-E2E-B14 | Custom --device-id | PASS |
| CLI-E2E-B15 | Different device_id isolation | PASS |
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
| CLI-E2E-D09 | Delete own message (D-014) | PASS (after fix) |
| CLI-E2E-D10 | Delete non-existent message | PASS |
| CLI-E2E-D11 | Delete permission denied (D-014) | PASS |
| CLI-E2E-D12 | Delete without --message-id | PASS |
| CLI-E2E-D13 | Mark-as-read specific message (D-012) | PASS |
| CLI-E2E-D14 | Mark-as-read all (message-id=0) | PASS |
| CLI-E2E-D15 | Mark-as-read MAX semantics (D-012) | PASS |
| CLI-E2E-D16 | Mark-as-read without --conversation-id | PASS |
| CLI-E2E-D17 | Send long message (256 chars) | PASS |
| CLI-E2E-D18 | Send special chars | PASS |
| CLI-E2E-D19 | Send unicode message | PASS |
| CLI-E2E-D20 | get-messages monotonic MessageID (D-008) | PASS |
| CLI-E2E-D21 | get-messages --after-message-id pagination | PASS |
| CLI-E2E-D22 | search-messages basic | PASS |
| CLI-E2E-D23 | search-messages no results | PASS |

### Group E: Conversation Ops

| Scenario | Description | Result |
|----------|-------------|--------|
| CLI-E2E-E01 | create-conversation notifies peer (D-045) | PASS |
| CLI-E2E-E02 | Duplicate create no notification (D-045) | PASS |

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
| CLI-E2E-H21 | logs cleanup --dry-run | PASS |
| CLI-E2E-H22 | logs cleanup basic | PASS |
| CLI-E2E-H23 | logs cleanup --retain | PASS |
| CLI-E2E-H24 | logs cleanup --type rpc | PASS |
| CLI-E2E-H25 | logs cleanup --type notifications | PASS |

### Group I: Sync Operations

| Scenario | Description | Result |
|----------|-------------|--------|
| CLI-E2E-I01 | sync-updates with daemon | PASS |
| CLI-E2E-I02 | sync-updates no daemon (exit 2, D-036) | PASS (after fix) |
| CLI-E2E-I03 | sync-updates double sync | PASS |
| CLI-E2E-I04 | sync after new data | PASS |
| CLI-E2E-I05 | Daemon initial FullSync | PASS |
| CLI-E2E-I06 | sync-updates IPC-only (D-036) | PASS (after fix) |

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
| CLI-E2E-M01 | Custom --db-path and --log-dir | PASS |

---

## Existing Automated E2E Test Suite

After the bug fixes, the existing automated CLI E2E test suite was run:

```
go test -count=1 ./internal/cli/e2e/
Result: 136 passed, 0 failed
```

---

## Environment

| Component | Details |
|-----------|---------|
| OS | macOS (Darwin 25.2.0) |
| Go | 1.24+ |
| Redis | 7-alpine (Docker, port 16379, DB 15) |
| Server | Docker container (port 18080) |
| Client | Compiled binary (./xyncra-client) |
| E2E HOME | /tmp/xe2e-* (isolated temp directory) |
| Test Users | alice-e2e, bob-e2e, charlie-e2e |
| Device ID | dev1 (default) |

---

## Code Quality

- `gofmt -l .` — No issues
- `go vet ./...` — No issues
- `go test -short ./internal/cli/` — 193 passed, 0 failed
- `go test -count=1 ./internal/cli/e2e/` — 136 passed, 0 failed

---

## Files Changed

| File | Change |
|------|--------|
| `internal/cli/sync.go` | Use `os.Exit(2)` for daemon-not-running (D-036/D-042) |
| `internal/cli/sync_test.go` | Updated test incompatible with `os.Exit(2)` |
| `internal/cli/send.go` | Added UUID output to `printSendResult` |
| `internal/cli/send_test.go` | Updated test to verify UUID in output |
| `internal/cli/e2e/cli_e2e_test.go` | Updated expected exit code from 1 to 2 |
| `docs/e2e-manual-test-report-2026-07-09.md` | This report |
