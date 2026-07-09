# Common Issues - xyncra-client CLI Troubleshooting

This document provides FAQ-style solutions for common issues encountered when using the xyncra-client CLI. Each issue follows the pattern: **Symptoms -> Cause -> Solution**.

---

## 1. "listen already running" but process is not visible

**Symptoms:**

```
Error: listen: lock is held by another process (PID: 12345)
```

But `ps aux | grep xyncra-client` shows no running process.

**Cause:**

Stale lock file (D-031). The listen daemon crashed or was killed (e.g., SIGKILL) without cleaning up its lock file. The flock mechanism releases automatically on process death, but the lock file itself (`xyncra.lock`) remains on disk with the old PID recorded inside.

**Solution:**

1. Verify the PID is not running:
   ```bash
   ps -p 12345
   ```
   If no output, the process is gone.

2. Manually remove the stale lock file:
   ```bash
   rm ~/.xyncra/{user-id}/{device-id}/xyncra.lock
   ```
   Replace `{user-id}` and `{device-id}` with your actual values. The default device ID can be found by running:
   ```bash
   ./xyncra-client listen --user-id alice 2>&1 | grep "Device:"
   ```

3. Alternatively, use the `kill` command which handles stale locks automatically:
   ```bash
   ./xyncra-client kill --user-id alice
   ```
   Output: `Daemon process (PID: 12345) is not running. Cleaning up stale files.`

4. Then restart the daemon:
   ```bash
   ./xyncra-client listen --user-id alice
   ```

---

## 2. Messages do not appear for the recipient

**Symptoms:**

You send a message successfully (`Message sent.` output), but the recipient does not see it.

**Cause:**

One of the following:
- The listen daemon is not running on the recipient's side (no WebSocket connection to receive push notifications).
- The xyncra-server is unreachable (network issue or server down).
- The WebSocket connection has been disconnected (network interruption, server restart).

**Solution:**

1. Verify the recipient's daemon is running:
   ```bash
   ps aux | grep xyncra-client
   ```

2. If the daemon is not running, start it:
   ```bash
   ./xyncra-client listen --user-id <recipient-user-id>
   ```

3. Trigger a manual sync to pull any missed updates:
   ```bash
   ./xyncra-client sync-updates --user-id <recipient-user-id>
   ```
   Note: `sync-updates` requires the daemon to be running (D-036). If the daemon is not running, start it first.

4. Check the server is reachable:
   ```bash
   curl http://localhost:8080/health
   ```

5. Check server logs for delivery errors.

---

## 3. "conversation not found in local database"

**Symptoms:**

```
Error: get-conversation: conversation <uuid> not found
```

Or:

```
Error: get-messages: conversation <uuid> not found
```

**Cause:**

The local SQLite database has not been synced yet (D-035). The `list-conversations`, `get-conversation`, `get-messages`, and `search-messages` commands read directly from the local database, which is populated by the listen daemon's sync process. If the daemon has never run, or the conversation was created on another device and not yet synced, the data will be missing.

**Solution:**

1. Start the listen daemon to trigger synchronization:
   ```bash
   ./xyncra-client listen --user-id alice
   ```

2. Wait for the initial sync to complete (watch for `[new message]` or `[conversation]` output).

3. In a separate terminal, run the query again:
   ```bash
   ./xyncra-client get-conversation --user-id alice --conversation-id <uuid>
   ```

4. If the conversation was just created on this device, verify it exists by listing all conversations:
   ```bash
   ./xyncra-client list-conversations --user-id alice
   ```

---

## 4. Socket connection refused

**Symptoms:**

```
Error: Cannot send message.
  Cause 1: dial unix /Users/xxx/.xyncra/alice/abc12345/xyncra.sock: connect: connection refused
  Cause 2: <websocket_error>
Hint: Start the daemon first: xyncra-client listen --user-id alice
```

**Cause:**

- The listen daemon is not running.
- The `--user-id` or `--device-id` is incorrect, pointing to a socket path that does not exist.
- The socket file was deleted or the daemon crashed without cleanup.

**Solution:**

1. Check if the socket file exists:
   ```bash
   ls -la ~/.xyncra/{user-id}/{device-id}/xyncra.sock
   ```

2. Verify the `--user-id` and `--device-id` values. The default device ID is derived from the hostname (D-033). Confirm by checking the directory:
   ```bash
   ls ~/.xyncra/{user-id}/
   ```

3. Start the daemon:
   ```bash
   ./xyncra-client listen --user-id alice
   ```

4. If the daemon is running but the socket is missing, kill and restart:
   ```bash
   ./xyncra-client kill --user-id alice
   ./xyncra-client listen --user-id alice
   ```

5. For IPC+WS fallback commands (D-032), the WebSocket fallback should still work even without the daemon. If both fail, check the server URL:
   ```bash
   echo $XYNCRA_SERVER
   # Default: ws://localhost:8080/ws
   ```

---

## 5. "database is locked"

**Symptoms:**

```
Error: database is locked
```

This typically occurs when running local database commands (e.g., `list-conversations`, `get-messages`) while the daemon is performing a large sync operation.

**Cause:**

Multiple processes are attempting to write to the SQLite database simultaneously. The local database uses WAL mode with `busy_timeout(5000)` (D-035), which allows concurrent reads during writes and tolerates short write conflicts. However, prolonged write operations can still cause lock contention.

**Solution:**

1. Wait a few seconds and retry. The 5-second busy timeout should resolve transient conflicts.

2. Ensure only one daemon instance is running per (user_id, device_id) pair:
   ```bash
   ps aux | grep xyncra-client
   ```

3. The CLI query commands (`list-conversations`, `get-conversation`, `get-messages`, `search-messages`) are read-only (D-035). They should not cause write locks. If you see this error from a query command, another process may be holding a write lock.

4. Check for stale processes and clean up:
   ```bash
   ./xyncra-client kill --user-id alice
   ./xyncra-client listen --user-id alice
   ```

5. If the problem persists, check the database file integrity:
   ```bash
   sqlite3 ~/.xyncra/{user-id}/{device-id}/xyncra.db "PRAGMA integrity_check;"
   ```

---

## 6. Wrong message ID type in delete-message or mark-as-read

**Symptoms:**

```
Error: Cannot delete message.
  Cause 1: invalid params: message_id must be a string
```

Or:

```
Error: Cannot mark as read.
  Cause 1: invalid params: message_id must be a number
```

**Cause:**

Two different `--message-id` flags exist with different types (D-038):

| Command | Flag | Type | Description |
|---------|------|------|-------------|
| `delete-message` | `--message-id` | `string` (UUID) | Message primary key ID |
| `mark-as-read` | `--message-id` | `uint32` | Message sequence number (MessageID) |
| `get-messages` | `--after-message-id` | `uint32` | Pagination cursor (MessageID) |

Using the wrong type will cause a parameter error.

**Solution:**

For `delete-message`, use the string UUID:
```bash
./xyncra-client delete-message --user-id alice --message-id "550e8400-e29b-41d4-a716-446655440000"
```

For `mark-as-read`, use the uint32 sequence number:
```bash
./xyncra-client mark-as-read --user-id alice --conversation-id <uuid> --message-id 42
```

To find the correct values, query the local database:
```bash
./xyncra-client get-messages --user-id alice --conversation-id <uuid>
```

Output shows `[<message_id>]` (uint32 sequence number) for each message. The string UUID is not displayed in `get-messages` output; use `sqlite3` to query it directly (see [debugging.md](./debugging.md)).

---

## 7. "logs cleanup" does not delete any entries

**Symptoms:**

```
Would delete 0 log entries older than 2026-07-02T12:00:00Z
  RPC logs: 0
  Notification logs: 0
```

Or after removing `--dry-run`, still `Deleted 0 log entries.`

**Cause:**

- The `--dry-run` flag is set, which only previews what would be deleted without actually deleting (D-040).
- The `--retain` duration is too short, meaning the cutoff timestamp is too recent and no logs are older than it.
- The default retention is 7 days (168h) (D-040). If your logs are all within the last 7 days, nothing will be deleted.

**Solution:**

1. Remove `--dry-run` to actually perform deletion:
   ```bash
   ./xyncra-client logs cleanup --user-id alice
   ```

2. To delete more aggressively, reduce the retention period:
   ```bash
   # Delete logs older than 1 day
   ./xyncra-client logs cleanup --user-id alice --retain 24h

   # Delete logs older than 1 hour
   ./xyncra-client logs cleanup --user-id alice --retain 1h
   ```

3. Preview before deleting:
   ```bash
   ./xyncra-client logs cleanup --user-id alice --retain 24h --dry-run
   ```

4. To clean only a specific log type:
   ```bash
   ./xyncra-client logs cleanup --user-id alice --type rpc
   ./xyncra-client logs cleanup --user-id alice --type notifications
   ```

---

## 8. "sync-updates" fails with "daemon not running"

**Symptoms:**

```
Error: daemon not running.
Hint: Start with 'xyncra-client listen --user-id <user>'
```

Exit code: 2 (D-042).

**Cause:**

`sync-updates` is an IPC-only command (D-036). It triggers the daemon's `FullSync` flow and has no WebSocket fallback (unlike other commands that use IPC+WS fallback per D-032). If the daemon is not running, the command cannot proceed.

**Solution:**

1. Start the daemon first:
   ```bash
   ./xyncra-client listen --user-id alice
   ```

2. In a separate terminal, run the sync:
   ```bash
   ./xyncra-client sync-updates --user-id alice
   ```

3. If the daemon fails to start (e.g., lock conflict), check for stale locks (see [Issue #1](#1-listen-already-running-but-process-is-not-visible)).

---

## 9. "create-conversation" fails with "peer-id" confusion

**Symptoms:**

```
Error: invalid params: peer_id is required
```

Or accidentally using `--user-id` instead of `--peer-id`:

```bash
# Wrong - this sets the current user, not the peer
./xyncra-client create-conversation --user-id alice --user-id bob
```

**Cause:**

`create-conversation` uses `--peer-id` (not `--user-id`) to specify the other user in the conversation (D-037). This avoids shadowing the global `--user-id` flag.

**Solution:**

Use the correct flag:
```bash
./xyncra-client create-conversation --user-id alice --peer-id bob --title "Chat with Bob"
```

Remember:
- `--user-id` = who you are (current user, global flag)
- `--peer-id` = who you are creating a conversation with (local flag)

---

## 10. Environment variables are not taking effect

**Symptoms:**

You set `XYNCRA_USER_ID=alice` but the CLI still prompts for `--user-id`.

**Cause:**

Flag resolution priority is: flag > environment variable > default (D-034). If a flag is explicitly provided (even with an empty value), it may override the environment variable depending on how the CLI parses it. Also, the environment variable name must use the `XYNCRA_` prefix with underscores (not hyphens).

**Solution:**

1. Verify environment variable names:
   | Flag | Environment Variable |
   |------|---------------------|
   | `--user-id` | `XYNCRA_USER_ID` |
   | `--device-id` | `XYNCRA_DEVICE_ID` |
   | `--server` | `XYNCRA_SERVER` |
   | `--db-path` | `XYNCRA_DB_PATH` |
   | `--log-dir` | `XYNCRA_LOG_DIR` |
   | (debug mode) | `XYNCRA_DEBUG` |

2. Export and verify:
   ```bash
   export XYNCRA_USER_ID=alice
   echo $XYNCRA_USER_ID
   ./xyncra-client list-conversations
   ```

3. Do not mix flag and environment variable for the same parameter. The flag always wins (D-034).

---

## Quick Reference

| Symptom | Likely Cause | First Action |
|---------|-------------|--------------|
| "lock is held" + no process | Stale lock (D-031) | `./xyncra-client kill --user-id alice` |
| Messages not received | Daemon not running | Start `listen` on recipient side |
| "conversation not found" | Local DB not synced (D-035) | Start `listen` to sync data |
| "connection refused" on socket | Daemon not running / wrong path | Check `ls ~/.xyncra/{uid}/{did}/xyncra.sock` |
| "database is locked" | Concurrent writes | Wait and retry; check for stale processes |
| Wrong message-id type | Type confusion (D-038) | Use string UUID for delete, uint32 for mark-as-read |
| "logs cleanup" no-op | `--dry-run` or retention too short | Remove `--dry-run`, adjust `--retain` |
| "daemon not running" on sync | IPC-only command (D-036) | Start `listen` first |
| "peer-id" error | Flag naming (D-037) | Use `--peer-id`, not `--user-id` |
| Env vars ignored | Wrong names or flag override | Use `XYNCRA_` prefix, check priority (D-034) |

---

## Related Documentation

- [Debugging Guide](./debugging.md) - Advanced debugging techniques
- [Command Reference](../../SKILL.md#command-table) - Full command reference
- [Architecture Overview](../architecture/overview.md) - System architecture overview
