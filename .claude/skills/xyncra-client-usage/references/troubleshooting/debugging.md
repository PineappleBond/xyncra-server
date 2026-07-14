# Debugging Guide - xyncra-client CLI

This document provides step-by-step debugging techniques for diagnosing issues with the xyncra-client CLI. For common problems and quick fixes, see [common-issues.md](./common-issues.md).

---

## 1. Enable Debug Logging

The xyncra-client supports debug logging via the `XYNCRA_DEBUG` environment variable (D-034). When enabled, verbose diagnostic output is written to stderr, including:

- WebSocket connection events (connect, disconnect, reconnect)
- RPC request/response details
- Sync state transitions
- IPC server activity
- Database operations

### Usage

```bash
XYNCRA_DEBUG=1 ./xyncra-client listen --user-id alice --device-id dev1
```

Or with `true`:

```bash
XYNCRA_DEBUG=true ./xyncra-client listen --user-id alice --device-id dev1
```

### What You Will See

Normal stderr output:
```
[xyncra] Starting listener daemon...
[xyncra] Device: abc12345
[xyncra] Connecting to ws://localhost:8080/ws?user_id=alice ...
[xyncra] IPC server listening at /Users/xxx/.xyncra/alice/abc12345/xyncra.sock
[xyncra] Listening for updates... (Ctrl+C to stop)
```

With `XYNCRA_DEBUG=1`, additional lines appear:
```
[2026-07-09 12:00:00] [DEBUG] WebSocket dial: url=ws://localhost:8080/ws?user_id=alice
[2026-07-09 12:00:00] [DEBUG] WebSocket connected
[2026-07-09 12:00:01] [DEBUG] RPC request: method=sync_updates id=1
[2026-07-09 12:00:01] [DEBUG] RPC response: method=sync_updates id=1 duration=45ms
[2026-07-09 12:00:01] [DEBUG] Sync complete: applied=5 latest_seq=142
```

### Combining with Other Commands

Debug mode works with any command, but is most useful with `listen`:

```bash
XYNCRA_DEBUG=1 ./xyncra-client send --user-id alice --device-id dev1 --conversation-id <uuid> --content "test"
```

---

## 2. Database Inspection

The xyncra-client stores all data in a local SQLite database (D-035). You can inspect the database directly using the `sqlite3` command-line tool.

### Locate the Database

Default path: `~/.xyncra/{user-id}/{device-id}/xyncra.db`

```bash
ls -la ~/.xyncra/alice/abc12345/xyncra.db
```

To find your device ID, list the directories:

```bash
ls ~/.xyncra/alice/
```

### Open the Database

```bash
sqlite3 ~/.xyncra/alice/abc12345/xyncra.db
```

### Useful Queries

#### List all conversations

```sql
SELECT id, user_id1, user_id2, title, last_message_at
FROM conversations
WHERE deleted_at IS NULL
ORDER BY last_message_at DESC;
```

#### View messages in a conversation

```sql
SELECT message_id, sender_id, content, created_at
FROM messages
WHERE conversation_id = '<conversation-uuid>'
  AND deleted_at IS NULL
ORDER BY message_id ASC;
```

Replace `<conversation-uuid>` with the actual conversation ID.

#### Check sync state

```sql
SELECT * FROM sync_states;
```

This shows the `local_max_seq` (last synced sequence number) for tracking sync progress.

#### View recent RPC logs

```sql
SELECT created_at, method, status_code, duration_ms, conversation_id
FROM rpc_logs
ORDER BY created_at DESC
LIMIT 10;
```

#### View failed RPC calls

```sql
SELECT created_at, method, status_code, error_message
FROM rpc_logs
WHERE status_code < 0
ORDER BY created_at DESC;
```

Status codes follow the error code system (D-027):
- `-400`: ConnectionError
- `-401`: TimeoutError
- `-402`: SyncError
- `-100` to `-399`: Server errors

#### View notification logs

```sql
SELECT created_at, seq, type
FROM notification_logs
ORDER BY created_at DESC
LIMIT 20;
```

#### Check unread message count

```sql
SELECT c.id, c.title,
  (SELECT COUNT(*) FROM messages m
   WHERE m.conversation_id = c.id
     AND m.message_id > COALESCE(c.last_read_message_id1, 0)
     AND m.sender_id != 'alice'
     AND m.deleted_at IS NULL) AS unread_count
FROM conversations c
WHERE c.deleted_at IS NULL;
```

Replace `'alice'` with your user ID.

#### Find a message by UUID

```sql
SELECT id, message_id, sender_id, content, created_at
FROM messages
WHERE id = '<message-uuid>';
```

This returns both the string `id` (UUID) and the uint32 `message_id` (sequence number), which helps resolve the type confusion issue (D-038).

#### Check draft messages

```sql
SELECT id, conversation_id, content, created_at
FROM drafts
ORDER BY created_at DESC;
```

### Database Integrity Check

If you suspect database corruption:

```bash
sqlite3 ~/.xyncra/alice/abc12345/xyncra.db "PRAGMA integrity_check;"
```

Expected output: `ok`

---

## 3. IPC Socket Inspection

The xyncra-client uses Unix Socket + JSON-RPC 2.0 for IPC (D-030). You can inspect and test the socket directly.

### Locate the Socket

Default path: `~/.xyncra/{user-id}/{device-id}/xyncra.sock`

```bash
ls -la ~/.xyncra/alice/abc12345/xyncra.sock
```

Expected permissions: `srw-------` (0600, owner read/write only).

### Test IPC with socat

You can send raw JSON-RPC requests to the socket using `socat`:

```bash
echo '{"jsonrpc":"2.0","id":"1","method":"sync_updates"}' | \
  socat - UNIX-CONNECT:~/.xyncra/alice/abc12345/xyncra.sock
```

Expected response:
```json
{"jsonrpc":"2.0","id":"1","result":{"status":"ok"}}
```

### Test a send_message via IPC

```bash
echo '{"jsonrpc":"2.0","id":"2","method":"send_message","params":{"conversation_id":"<uuid>","content":"debug test","client_message_id":"550e8400-e29b-41d4-a716-446655440000"}}' | \
  socat - UNIX-CONNECT:~/.xyncra/alice/abc12345/xyncra.sock
```

### Check Socket is Responding

If `socat` hangs or returns "connection refused":
1. The daemon is not running -- start it with `./xyncra-client listen --user-id alice --device-id dev1`
2. The socket file is stale -- use `./xyncra-client kill --user-id alice --device-id dev1` to clean up
3. Wrong user-id or device-id -- verify the path matches your daemon's configuration

---

## 4. Lock File Inspection

The process lock mechanism (D-031) uses a lock file to prevent duplicate daemons.

### Read the Lock File

```bash
cat ~/.xyncra/alice/abc12345/xyncra.lock
```

Example output:
```json
{
  "pid": 12345,
  "started_at": "2026-07-09T12:00:00Z",
  "device_id": "abc12345"
}
```

### Check if the Process is Alive

Extract the PID and check:

```bash
PID=$(cat ~/.xyncra/alice/abc12345/xyncra.lock | grep -o '"pid":[0-9]*' | grep -o '[0-9]*')
ps -p $PID
```

If `ps` returns no output, the process is dead and the lock is stale. Use `kill` to clean up:

```bash
./xyncra-client kill --user-id alice --device-id dev1
```

### Force Remove a Stale Lock

If the `kill` command does not work, manually remove the lock file:

```bash
rm ~/.xyncra/alice/abc12345/xyncra.lock
```

Then restart the daemon.

---

## 5. Log Analysis

The xyncra-client provides built-in log management commands for inspecting RPC and notification activity.

### View Log Statistics

```bash
# Last 1 hour
./xyncra-client logs stats --user-id alice --device-id dev1 --since 1h

# Last 24 hours
./xyncra-client logs stats --user-id alice --device-id dev1 --since 24h

# Last 7 days
./xyncra-client logs stats --user-id alice --device-id dev1 --since 7d
```

Output example:
```
METHOD                  COUNT       SUCCESS     ERRORS      AVG (ms)
------                  -----       -------     ------      --------
send_message            100         95          5           1.234
sync_updates            50          48          2           12.567
create_conversation     10          10          0           2.345
```

### View Logs with Time Breakdown

```bash
./xyncra-client logs stats --user-id alice --device-id dev1 --since 24h --interval 1h
```

### Search for Errors

```bash
# All error entries
./xyncra-client logs search --user-id alice --device-id dev1 --error

# Errors for a specific method
./xyncra-client logs search --user-id alice --device-id dev1 --error --method send_message

# Errors within a time range
./xyncra-client logs search --user-id alice --device-id dev1 --error --from 2h --to 30m
```

### Search by Conversation

```bash
./xyncra-client logs search --user-id alice --device-id dev1 --conversation-id <uuid> --limit 50
```

### Search by Request ID

```bash
./xyncra-client logs search --user-id alice --device-id dev1 --request-id <request-id>
```

### View Recent Logs

```bash
# RPC logs (default)
./xyncra-client logs tail --user-id alice --device-id dev1 --limit 20

# Notification logs
./xyncra-client logs tail --user-id alice --device-id dev1 --type notifications --limit 20

# Logs since a specific time
./xyncra-client logs tail --user-id alice --device-id dev1 --since 30m
```

### Export Logs for Analysis

```bash
# Export to CSV
./xyncra-client logs export --user-id alice --device-id dev1 --format csv --output rpc_logs.csv

# Export to JSON
./xyncra-client logs export --user-id alice --device-id dev1 --format json --output rpc_logs.json

# Export notification logs
./xyncra-client logs export --user-id alice --device-id dev1 --type notifications --format csv --output notifications.csv

# Export with filters
./xyncra-client logs export --user-id alice --device-id dev1 --method send_message --from 7d --format csv --output sends.csv
```

### Clean Up Old Logs

```bash
# Preview what would be deleted (D-040, default 7-day retention)
./xyncra-client logs cleanup --user-id alice --device-id dev1 --dry-run

# Delete logs older than 1 day
./xyncra-client logs cleanup --user-id alice --device-id dev1 --retain 24h

# Clean only RPC logs
./xyncra-client logs cleanup --user-id alice --device-id dev1 --type rpc
```

---

## 6. Network Debugging

### Check WebSocket Server Reachability

```bash
# Basic connectivity check
curl -v http://localhost:8080/health
```

Expected response: `200 OK`

### Check WebSocket Upgrade

```bash
curl -v -N \
  -H "Connection: Upgrade" \
  -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" \
  -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  http://localhost:8080/ws?user_id=alice
```

Expected: HTTP 101 Switching Protocols

### Verify Server URL

The default server URL is `ws://localhost:8080/ws`. Check if you have overridden it:

```bash
echo $XYNCRA_SERVER
```

Or pass it explicitly:

```bash
./xyncra-client listen --user-id alice --device-id dev1 --server ws://myserver:8080/ws
```

### Check Port Binding

```bash
lsof -i :8080
```

### DNS Resolution (for remote servers)

```bash
nslookup myserver.example.com
```

---

## 7. Process Debugging

### Find the Daemon Process

```bash
ps aux | grep xyncra-client
```

Look for the `listen` process (not one-off commands like `send`):
```
user  12345  0.1  0.2  1234567  12345  ??  S  12:00PM  0:01.23 ./xyncra-client listen --user-id alice --device-id dev1
```

### Inspect Open Files

```bash
lsof -p <PID>
```

This shows all files opened by the daemon, including:
- `xyncra.db` -- the SQLite database
- `xyncra.sock` -- the IPC socket
- `xyncra.lock` -- the process lock file

### Inspect Network Connections

```bash
lsof -i -p <PID>
```

This shows the WebSocket connection to the server:
```
xyncra-cl 12345 user  5u  IPv4 0x1234567  0t0  TCP localhost:12345->localhost:8080 (ESTABLISHED)
```

### Send Signals

Graceful termination (SIGTERM):
```bash
kill -TERM <PID>
```

Force termination (SIGKILL):
```bash
kill -KILL <PID>
```

Or use the built-in `kill` command (D-039):
```bash
./xyncra-client kill --user-id alice --device-id dev1
./xyncra-client kill --user-id alice --device-id dev1 --force
```

### Check Process Resource Usage

```bash
# CPU and memory
top -p <PID>

# Detailed resource usage
ps -o pid,rss,vsz,%cpu,%mem,etime -p <PID>
```

---

## 8. Exit Code Reference

The CLI uses standardized exit codes (D-042):

| Code | Meaning | Common Cause |
|------|---------|--------------|
| `0` | Success | Command completed normally |
| `1` | General error | Parameter error, network error, database error |
| `2` | Precondition not met | Lock conflict (D-031), daemon not running (D-036) |
| `3` | Timeout (kill only) | SIGTERM not responded within `--timeout` (D-039) |

### Checking Exit Codes in Scripts

```bash
./xyncra-client sync-updates --user-id alice --device-id dev1
EXIT_CODE=$?

case $EXIT_CODE in
  0) echo "Sync successful" ;;
  1) echo "General error occurred" ;;
  2) echo "Daemon not running - start listen first" ;;
  *) echo "Unexpected exit code: $EXIT_CODE" ;;
esac
```

---

## 9. Client Error Code Reference

The client uses extended error codes (D-027):

| Code | Type | Description |
|------|------|-------------|
| `-400` | ConnectionError | WebSocket connection failed (server unreachable, network down) |
| `-401` | TimeoutError | RPC call timed out (request sent but no response within timeout) |
| `-402` | SyncError | Incremental sync failed (gap in sequence, sync data corrupt) |

These codes appear in RPC log entries and error messages. Use `logs search --error` to find them:

```bash
./xyncra-client logs search --user-id alice --device-id dev1 --error --method sync_updates
```

---

## Debugging Checklist

When reporting an issue, gather the following information:

1. **Daemon status:**
   ```bash
   ps aux | grep xyncra-client
   ```

2. **Lock file:**
   ```bash
   cat ~/.xyncra/{user-id}/{device-id}/xyncra.lock
   ```

3. **Socket file:**
   ```bash
   ls -la ~/.xyncra/{user-id}/{device-id}/xyncra.sock
   ```

4. **Database file:**
   ```bash
   ls -la ~/.xyncra/{user-id}/{device-id}/xyncra.db
   ```

5. **Recent error logs:**
   ```bash
   ./xyncra-client logs search --user-id {user-id} --device-id {device-id} --error --limit 20
   ```

6. **Log statistics:**
   ```bash
   ./xyncra-client logs stats --user-id {user-id} --device-id {device-id} --since 1h
   ```

7. **Server connectivity:**
   ```bash
   curl http://localhost:8080/health
   ```

8. **Debug mode output:**
   ```bash
   XYNCRA_DEBUG=1 ./xyncra-client listen --user-id {user-id} --device-id {device-id} 2>&1 | head -50
   ```

---

## Related Documentation

- [Common Issues](./common-issues.md) - FAQ-style troubleshooting
- [Command Reference](../../SKILL.md#command-table) - Full command reference
- [Architecture Overview](../architecture/overview.md) - System architecture overview
