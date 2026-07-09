# Offline Sync Scenarios

This document explains how `xyncra-client` synchronizes data between the server and local SQLite database, and how it handles various connectivity conditions.

---

## Sync Mechanism Overview

The sync system is built on three components:

1. **Server Push (UserUpdate)**: The server pushes `UserUpdate` events to connected daemons via WebSocket (D-028). Each update has a monotonically increasing sequence number (`seq`) within the user's scope.

2. **Local Tracking (`localMaxSeq`)**: The daemon maintains a `localMaxSeq` value in the local SQLite database, recording the highest `seq` it has processed. This is the synchronization cursor.

3. **FullSync for Gap Filling**: When the daemon detects a gap between its `localMaxSeq` and the server's latest seq (e.g., after reconnection), it triggers a `FullSync` to pull all missed updates via the `sync_updates` RPC (D-009).

### Update Types (D-028)

All operations share the same seq space. The `Type` field determines how the client processes each update:

| Type | Description | Client Action |
|------|-------------|---------------|
| `message` | New message | Persist to `messages` table |
| `delete_message` | Message deleted | Soft-delete local message |
| `mark_read` | Read cursor updated | Update local read position |
| `conversation` | Conversation state change | Update local conversation record |
| `gap` | Gap placeholder (runtime only, D-029) | Skip (no persist) |

---

## Scenario 1: Normal Online Operation

### Setup

The daemon is running and connected to the server.

```bash
./xyncra-client listen --user-id alice
# [xyncra] Starting listener daemon...
# [xyncra] Device: a1b2c3d4
# [xyncra] Connecting to ws://localhost:8080/ws?user_id=alice ...
# [xyncra] Listening for updates... (Ctrl+C to stop)
```

### Behavior

1. The daemon maintains a persistent WebSocket connection to the server
2. When another user sends a message to alice, the server pushes a `UserUpdate` with `type: "message"`
3. The daemon receives the update, persists the message to `xyncra.db`, and advances `localMaxSeq`

### Real-time Output

When Bob sends a message to alice, the daemon prints to stdout:

```
[new message] seq=42 from=bob conv=550e8400 "Hey Alice, are you there?"
```

When alice marks a message as read on another device:

```
[mark read] conv=550e8400 msg_id=41
```

### Query After Sync

Query commands read from the local database (D-035) and immediately reflect the latest synced state:

```bash
./xyncra-client get-messages --user-id alice -c 550e8400
```

```
[#41] bob (14:22): Hey Alice, are you there?
[#42] bob (14:23): Let me know when you're free
```

---

## Scenario 2: Brief Network Disruption (< 30 minutes)

### What Happens

1. The WebSocket connection drops (network blip, WiFi reconnection, etc.)
2. The daemon detects the disconnection
3. The daemon automatically attempts to reconnect with backoff
4. Upon reconnection, the daemon triggers a `FullSync` to fill any gaps

### Timeline

```
t=0:00   WebSocket disconnected
t=0:01   Daemon detects disconnection
t=0:02   Reconnection attempt #1 (fails)
t=0:05   Reconnection attempt #2 (fails)
t=0:10   Reconnection attempt #3 (succeeds)
t=0:10   FullSync triggered: after_seq=<localMaxSeq>
t=0:11   Missing updates received and applied
t=0:11   Normal operation resumed
```

### Daemon Output During Reconnection

```
[2026-07-09 14:30:00] [WARN] WebSocket disconnected, reconnecting...
[2026-07-09 14:30:10] [INFO] WebSocket reconnected
[2026-07-09 14:30:10] [INFO] FullSync started (after_seq=40)
[2026-07-09 14:30:11] [INFO] FullSync complete (processed 5 updates)
```

### Verification

After reconnection, verify data is up to date:

```bash
./xyncra-client sync-updates --user-id alice
```

```
Sync complete.
```

> `sync-updates` triggers an additional FullSync through the daemon. This is IPC-only (D-036) -- it requires the daemon to be running.

---

## Scenario 3: Extended Offline Period (> 30 days)

### The 30-Day Boundary

The server retains `UserUpdate` records for 30 days (D-016). Updates older than 30 days are permanently deleted by the background cleanup goroutine.

### What Happens After 30+ Days Offline

1. The daemon cannot reconnect (or reconnects but finds the server has cleaned old updates)
2. A normal `FullSync` may not recover all data because the server's `sync_updates` RPC only has updates from the last 30 days
3. The local database may have stale data that cannot be incrementally repaired

### Recovery Strategy

For extended offline recovery:

1. Start the daemon and let it connect:
   ```bash
   ./xyncra-client listen --user-id alice
   ```

2. Trigger a FullSync:
   ```bash
   ./xyncra-client sync-updates --user-id alice
   ```
   ```
   Sync complete.
   ```

3. If data still appears incomplete, the gap is likely due to server-side cleanup (D-016). The local database reflects whatever the server could provide.

> **Note**: The current architecture does not support full database reconstruction from the server. For complete data recovery after extended offline periods, the server would need to provide a full data snapshot endpoint (not yet implemented).

### Preventing Data Loss

To avoid extended offline gaps:
- Run the daemon regularly, even briefly, to advance `localMaxSeq`
- Monitor daemon health with `kill` to detect stale processes (D-039)

---

## Scenario 4: Server Restart

### What Happens

1. The server process restarts (deployment, crash, maintenance)
2. All WebSocket connections drop
3. Daemons detect disconnection and begin reconnecting
4. The server comes back online
5. Daemons reconnect one by one
6. Each daemon triggers a `FullSync` upon reconnection

### Timeline

```
t=0:00   Server stops
t=0:00   All client connections drop
t=0:01   Daemons detect disconnection
t=0:02   Daemons begin reconnection attempts
t=0:30   Server restarts
t=0:31   Daemons reconnect successfully
t=0:31   Each daemon triggers FullSync
t=0:32   All daemons resynchronized
```

### Daemon Output

```
[2026-07-09 15:00:00] [WARN] WebSocket disconnected, reconnecting...
[2026-07-09 15:00:30] [INFO] WebSocket reconnected
[2026-07-09 15:00:30] [INFO] FullSync started (after_seq=100)
[2026-07-09 15:00:31] [INFO] FullSync complete (processed 0 updates)
```

If no updates occurred during the restart, FullSync processes 0 updates and the daemon resumes normal operation.

### Manual Sync After Server Restart

If you want to explicitly force a resync:

```bash
./xyncra-client sync-updates --user-id alice
```

```
Sync complete.
```

---

## Manual Sync (D-036)

The `sync-updates` command triggers the daemon's `FullSync` flow via IPC.

### Requirements

- The daemon **must** be running (IPC-only, no WebSocket fallback)
- The daemon must have a connection to the server

### Successful Sync

```bash
./xyncra-client sync-updates --user-id alice
```

```
Sync complete.
```

### Daemon Not Running

```bash
./xyncra-client sync-updates --user-id alice
```

```
Error: daemon not running.
Hint: Start with 'xyncra-client listen --user-id alice'
```

Exit code: `2` (D-042, prerequisite not met)

### Why IPC-Only?

`sync-updates` is an exception to the normal IPC+WS fallback strategy (D-032). The reasons (D-036):

1. **State consistency**: The daemon holds `localMaxSeq` and the WebSocket connection. An independent WebSocket connection calling `sync_updates` directly would compete with the daemon for SQLite writes.
2. **Deduplication safety**: The daemon's `syncManager` uses the `NotificationLog` table to deduplicate updates. An independent connection could cause duplicate processing.
3. **FullSync is the daemon's responsibility**: It manages pagination, debounced pulling, and the `ApplyUpdate` chain.

---

## Understanding localMaxSeq

### What It Tracks

`localMaxSeq` is a single integer per `(user_id, device_id)` that records the highest `seq` value the daemon has successfully processed and persisted.

### How It Advances

```
Server seq space:  1  2  3  4  5  6  7  8  9  10
                                              ^
                                          localMaxSeq = 10
```

When new updates arrive:

```
Server seq space:  1  2  3  4  5  6  7  8  9  10  11  12  13
                                                      ^
                                                  localMaxSeq = 13
```

### Gap Detection

If the daemon's `localMaxSeq` is `10` but it receives an update with `seq=13`, it detects a gap (seqs 11-12 are missing) and triggers FullSync to fill them.

### All Types Share the Same Seq Space (D-028)

```
seq=100  type=message       → new message persisted
seq=101  type=mark_read     → read cursor advanced
seq=102  type=conversation  → conversation updated
seq=103  type=delete_message → message soft-deleted
```

After processing seq=103, `localMaxSeq` = 103.

---

## Sync State Summary

| State | localMaxSeq | Connection | Data Freshness |
|-------|:-----------:|:----------:|:--------------:|
| Online, active | Current | Connected | Real-time |
| Brief disconnect | Stale | Reconnecting | Seconds/minutes behind |
| Reconnected | Catching up | Connected | FullSync in progress |
| Extended offline | Very stale | May fail | Up to 30 days recoverable |
| Daemon stopped | Frozen | None | Last sync point |

---

## Related Documentation

- [Basic Usage](./basic-usage.md) -- daily workflow with sync
- [Multi-Device](./multi-device.md) -- per-device sync isolation
- [Error Handling](./error-handling.md) -- daemon-not-running errors
- [Advanced Usage](./advanced.md) -- environment variables for server URL
