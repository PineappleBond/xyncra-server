# Common Issues - xyncra-client TypeScript CLI Troubleshooting

This document provides FAQ-style solutions for common issues encountered when using the xyncra-client TypeScript CLI. Each issue follows the pattern: **Symptoms -> Cause -> Solution**.

---

## 1. "listen already running" but process is not visible

**Symptoms:**

```
Error: listen already running (PID: 12345)
```

But `ps aux | grep xyncra-client` shows no running process.

**Cause:**

Stale lock file (D-031). The listen daemon crashed or was killed (e.g., SIGKILL) without cleaning up its lock file. The `fs-ext` lock mechanism releases automatically on process death, but the lock file itself (`xyncra.lock`) remains on disk with the old PID recorded inside.

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
   Replace `{user-id}` and `{device-id}` with your actual values.

3. Alternatively, use the `kill` command which handles stale locks automatically:
   ```bash
   xyncra-client kill --user-id alice --device-id dev1
   ```
   Output: `Daemon process (PID: 12345) is not running. Cleaning up stale files.`

4. Then restart the daemon:
   ```bash
   xyncra-client listen --user-id alice --device-id dev1
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
   xyncra-client listen --user-id <recipient-user-id> --device-id <device>
   ```

3. Trigger a manual sync to pull any missed updates:
   ```bash
   xyncra-client sync-updates --user-id <recipient-user-id> --device-id <device>
   ```
   Note: `sync-updates` requires the daemon to be running (D-036). If the daemon is not running, start it first.

4. Check the server is reachable:
   ```bash
   curl http://localhost:8080/health
   ```

5. Check server logs for delivery errors.

---

## 3. "conversation not found in IndexedDB"

**Symptoms:**

```
Error: get-conversation: conversation <uuid> not found
```

Or:

```
Error: get-messages: conversation <uuid> not found
```

**Cause:**

The daemon's IndexedDB has not been synced yet (TS-D-012). The `list-conversations`, `get-conversation`, `get-messages`, and `search-messages` commands read from the daemon's IndexedDB via IPC. If the daemon has never run, or the conversation was created on another device and not yet synced, the data will be missing.

**Solution:**

1. Start the listen daemon to trigger synchronization:
   ```bash
   xyncra-client listen --user-id alice --device-id dev1
   ```

2. Wait for the initial sync to complete (watch for `[new message]` or `[conversation]` output).

3. In a separate terminal, run the query again:
   ```bash
   xyncra-client get-conversation --user-id alice --device-id dev1 --conversation-id <uuid>
   ```

4. If the conversation was just created on this device, verify it exists by listing all conversations:
   ```bash
   xyncra-client list-conversations --user-id alice --device-id dev1
   ```

---

## 4. Socket connection refused

**Symptoms:**

```
Error: Cannot send message.
  Cause 1: connect ECONNREFUSED /Users/xxx/.xyncra/alice/abc12345/xyncra.sock
  Cause 2: <websocket_error>
Hint: Start the daemon first: xyncra-client listen --user-id alice --device-id dev1
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
   xyncra-client listen --user-id alice --device-id dev1
   ```

4. If the daemon is running but the socket is missing, kill and restart:
   ```bash
   xyncra-client kill --user-id alice --device-id dev1
   xyncra-client listen --user-id alice --device-id dev1
   ```

5. For IPC+WS fallback commands (D-032), the WebSocket fallback should still work even without the daemon. If both fail, check the server URL:
   ```bash
   echo $XYNCRA_SERVER
   # Default: ws://localhost:8080/ws
   ```

---

## 5. EADDRINUSE: address already in use

**Symptoms:**

```
Error: listen EADDRINUSE: address already in use /Users/xxx/.xyncra/alice/abc12345/xyncra.sock
```

**Cause:**

This is a Node.js-specific error. Another process has already bound the Unix socket file, or a stale socket file remains from a crashed daemon. Unlike the Go client which may report this differently, the TS client surfaces the standard Node.js `EADDRINUSE` error.

**Solution:**

1. Check if a daemon is already running:
   ```bash
   ps aux | grep xyncra-client
   ```

2. If a daemon is running, use it directly instead of starting a new one:
   ```bash
   xyncra-client send --user-id alice --device-id dev1 -c 550e8400 -m "Hello"
   ```

3. If no daemon is running, remove the stale socket file:
   ```bash
   rm ~/.xyncra/{user-id}/{device-id}/xyncra.sock
   xyncra-client listen --user-id alice --device-id dev1
   ```

4. To find what is using the socket:
   ```bash
   lsof ~/.xyncra/{user-id}/{device-id}/xyncra.sock
   ```

---

## 6. fake-indexeddb memory limit exceeded

**Symptoms:**

```
Error: JavaScript heap out of memory
```

Or the daemon process is killed by the OS (OOM killer).

**Cause:**

This is a TS-specific issue. The `fake-indexeddb` library stores IndexedDB data in Node.js process memory. Unlike the Go client's SQLite file which is backed by disk, the TS client's IndexedDB has no file persistence and is limited by available Node.js heap memory.

When the daemon syncs a large amount of data (many messages, large conversations), it may exhaust the default V8 heap size (~1.5 GB for Node.js 20+).

**Solution:**

1. Increase Node.js heap size:
   ```bash
   NODE_OPTIONS="--max-old-space-size=4096" xyncra-client listen --user-id alice --device-id dev1
   ```
   This increases the heap to 4 GB.

2. If the problem persists, consider reducing the amount of data stored locally:
   ```bash
   # Clean up old logs to reduce memory usage
   xyncra-client logs cleanup --user-id alice --device-id dev1 --retain 24h
   ```

3. Monitor memory usage:
   ```bash
   # Check daemon memory usage
   ps -o pid,rss,%mem,command -p $(pgrep -f "xyncra-client listen")
   ```

---

## 7. Node.js version incompatibility

**Symptoms:**

```
Error: The Node.js version is too old. xyncra-client requires Node.js >= 20.
```

Or various syntax/feature errors during build or runtime:

```
SyntaxError: Unexpected token '??='
TypeError: structuredClone is not a function
```

**Cause:**

The TS client requires Node.js >= 20. Older versions lack required features like `structuredClone`, `??=` operator, and modern `fs` APIs.

**Solution:**

1. Check your Node.js version:
   ```bash
   node --version
   ```

2. If below v20, upgrade Node.js:
   ```bash
   # Using nvm
   nvm install 20
   nvm use 20

   # Or using Homebrew
   brew install node@20
   ```

3. Verify the version:
   ```bash
   node --version
   # Should show v20.x.x or higher
   ```

4. Rebuild the client:
   ```bash
   cd /path/to/xyncra-server/packages/xyncra-client-cli
   npm run build
   ```

---

## 8. npm workspace build issues

**Symptoms:**

```
Error: Cannot find module '@xyncra/client-core'
```

Or during build:

```
npm ERR! Could not resolve dependency:
npm ERR! peer @xyncra/protocol@"^1.0.0" from @xyncra/client-cli@1.0.0
```

**Cause:**

The TS client is part of an npm workspace. The `@xyncra/client-cli` depends on `@xyncra/client-core` and `@xyncra/protocol` packages. If the workspace is not properly set up or dependencies are not installed, the build will fail.

**Solution:**

1. Install all workspace dependencies from the repo root:
   ```bash
   cd /path/to/xyncra-server
   npm install
   ```

2. Build all packages in dependency order:
   ```bash
   # Build protocol first (no dependencies)
   cd packages/xyncra-protocol && npm run build

   # Build client-core (depends on protocol)
   cd ../xyncra-client-core && npm run build

   # Build client-cli (depends on client-core and protocol)
   cd ../xyncra-client-cli && npm run build
   ```

3. Or use the workspace build from root:
   ```bash
   cd /path/to/xyncra-server
   npm run build --workspaces
   ```

4. Link the CLI globally:
   ```bash
   cd packages/xyncra-client-cli
   npm link
   ```

5. Verify the binary is available:
   ```bash
   xyncra-client --help
   ```

---

## 9. Wrong message ID type in delete-message or mark-as-read

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
xyncra-client delete-message --user-id alice --device-id dev1 --message-id "550e8400-e29b-41d4-a716-446655440000"
```

For `mark-as-read`, use the uint32 sequence number:
```bash
xyncra-client mark-as-read --user-id alice --device-id dev1 --conversation-id <uuid> --message-id 42
```

To find the correct values, query via the daemon:
```bash
xyncra-client get-messages --user-id alice --device-id dev1 --conversation-id <uuid>
```

Output shows `[<message_id>]` (uint32 sequence number) for each message.

---

## 10. "logs cleanup" does not delete any entries

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
   xyncra-client logs cleanup --user-id alice --device-id dev1
   ```

2. To delete more aggressively, reduce the retention period:
   ```bash
   # Delete logs older than 1 day
   xyncra-client logs cleanup --user-id alice --device-id dev1 --retain 24h

   # Delete logs older than 1 hour
   xyncra-client logs cleanup --user-id alice --device-id dev1 --retain 1h
   ```

3. Preview before deleting:
   ```bash
   xyncra-client logs cleanup --user-id alice --device-id dev1 --retain 24h --dry-run
   ```

4. To clean only a specific log type:
   ```bash
   xyncra-client logs cleanup --user-id alice --device-id dev1 --type rpc
   xyncra-client logs cleanup --user-id alice --device-id dev1 --type notifications
   ```

---

## 11. "sync-updates" fails with "daemon not running"

**Symptoms:**

```
Error: daemon not running.
Hint: Start with 'xyncra-client listen --user-id <user> --device-id <device>'
```

Exit code: 2 (D-042).

**Cause:**

`sync-updates` is an IPC-only command (D-036). It triggers the daemon's `FullSync` flow and has no WebSocket fallback (unlike other commands that use IPC+WS fallback per D-032). If the daemon is not running, the command cannot proceed.

**Solution:**

1. Start the daemon first:
   ```bash
   xyncra-client listen --user-id alice --device-id dev1
   ```

2. In a separate terminal, run the sync:
   ```bash
   xyncra-client sync-updates --user-id alice --device-id dev1
   ```

3. If the daemon fails to start (e.g., lock conflict), check for stale locks (see [Issue #1](#1-listen-already-running-but-process-is-not-visible)).

---

## 12. "create-conversation" fails with "peer-id" confusion

**Symptoms:**

```
Error: invalid params: peer_id is required
```

Or accidentally using `--user-id` instead of `--peer-id`:

```bash
# Wrong - this sets the current user, not the peer
xyncra-client create-conversation --user-id alice --device-id dev1 --user-id bob
```

**Cause:**

`create-conversation` uses `--peer-id` (not `--user-id`) to specify the other user in the conversation (D-037). This avoids shadowing the global `--user-id` flag.

**Solution:**

Use the correct flag:
```bash
xyncra-client create-conversation --user-id alice --device-id dev1 --peer-id bob --title "Chat with Bob"
```

Remember:
- `--user-id` = who you are (current user, global flag)
- `--peer-id` = who you are creating a conversation with (local flag)

---

## 13. Environment variables are not taking effect

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
   xyncra-client list-conversations
   ```

3. Do not mix flag and environment variable for the same parameter. The flag always wins (D-034).

---

## 14. Data lost after daemon restart

**Symptoms:**

After restarting the daemon, all previously synced data (conversations, messages) appears to be gone.

**Cause:**

This is a TS-specific behavior (TS-D-012). The TS client uses `fake-indexeddb` in Node.js, which stores data in process memory. When the daemon process exits, all in-memory IndexedDB data is lost. This differs from the Go client where SQLite persists data to a file on disk.

On restart, the daemon performs a FullSync to rebuild its local state from the server.

**Solution:**

1. After restarting, trigger a sync:
   ```bash
   xyncra-client sync-updates --user-id alice --device-id dev1
   ```

2. Wait for the sync to complete before querying:
   ```bash
   # Wait for sync, then query
   xyncra-client list-conversations --user-id alice --device-id dev1
   ```

3. If data is still missing, the server may have cleaned old updates (> 30 days, D-016). See [Offline Sync - Extended Offline Period](../scenarios/offline-sync.md#scenario-3-extended-offline-period--30-days).

---

## 15. WebSocket connection refused

**Symptoms:**

```
Error: connect ECONNREFUSED 127.0.0.1:8080
```

**Cause:**

Server is not running or port is misconfigured.

**Solution:**

1. Check server process:
   ```bash
   ps aux | grep xyncra-server
   ```

2. Check server port:
   ```bash
   curl http://localhost:8080/health
   ```

3. Start the server:
   ```bash
   ./xyncra-server
   ```

---

## Quick Reference

| Symptom | Likely Cause | First Action |
|---------|-------------|--------------|
| "lock is held" + no process | Stale lock (D-031) | `xyncra-client kill --user-id alice --device-id dev1` |
| Messages not received | Daemon not running | Start `listen` on recipient side |
| "conversation not found" | IndexedDB not synced (TS-D-012) | Start `listen` to sync data |
| "ECONNREFUSED" on socket | Daemon not running / wrong path | Check `ls ~/.xyncra/{uid}/{did}/xyncra.sock` |
| "EADDRINUSE" | Stale socket or duplicate daemon | `rm ~/.xyncra/{uid}/{did}/xyncra.sock` |
| "JavaScript heap out of memory" | fake-indexeddb memory limit | Set `NODE_OPTIONS="--max-old-space-size=4096"` |
| "Node.js version too old" | Node.js < 20 | Upgrade to Node.js >= 20 |
| "Cannot find module" | npm workspace not set up | `npm install` from repo root |
| Wrong message-id type | Type confusion (D-038) | Use string UUID for delete, uint32 for mark-as-read |
| "logs cleanup" no-op | `--dry-run` or retention too short | Remove `--dry-run`, adjust `--retain` |
| "daemon not running" on sync | IPC-only command (D-036) | Start `listen` first |
| "peer-id" error | Flag naming (D-037) | Use `--peer-id`, not `--user-id` |
| Env vars ignored | Wrong names or flag override | Use `XYNCRA_` prefix, check priority (D-034) |
| Data lost after restart | fake-indexeddb in-memory (TS-D-012) | Run `sync-updates` after restart |
| WS "ECONNREFUSED" | Server not running | Start `./xyncra-server`, check `curl :8080/health` |

---

## Related Documentation

- [Debugging Guide](./debugging.md) - Advanced debugging techniques
- [Command Reference](../../SKILL.md#command-table) - Full command reference
- [Architecture Overview](../architecture/overview.md) - System architecture overview
