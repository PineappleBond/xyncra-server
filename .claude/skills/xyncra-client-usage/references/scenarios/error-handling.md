# Error Handling Scenarios

This document covers exit codes, common error patterns, and how to handle errors in scripts.

---

## Exit Code Reference (D-042)

| Exit Code | Meaning | Example Scenarios |
|:---------:|---------|-------------------|
| `0` | Success | Command executed successfully |
| `1` | General error | RPC failure, invalid parameters, database error, network error |
| `2` | Prerequisite not met | Lock conflict (D-031), daemon not running (D-036) |
| `3` | Timeout (kill only) | SIGTERM not responded within `--timeout` (D-039) |

---

## Common Error Patterns

### Error 1: All Delivery Methods Failed

**Cause**: Both IPC (Unix Socket) and WebSocket connections failed (D-032).

**Error Message**:

```
Error: Cannot send message.
  Cause 1: dial unix /Users/alice/.xyncra/alice/a1b2c3d4/xyncra.sock: connect: connection refused
  Cause 2: dial tcp 127.0.0.1:8080: connect: connection refused
Hint: Start the daemon first: xyncra-client listen --user-id alice
```

**Exit Code**: `1`

**Diagnosis**:
- `Cause 1` (IPC) failed: The daemon is not running, or the socket file does not exist
- `Cause 2` (WebSocket) failed: The server is not running or not reachable at `127.0.0.1:8080`

**Resolution**:

```bash
# Step 1: Start the daemon (fixes Cause 1)
./xyncra-client listen --user-id alice &

# Step 2: If you also need server connectivity, start the server (fixes Cause 2)
./xyncra-server &

# Step 3: Retry the command
./xyncra-client send --user-id alice -c 550e8400 -m "Hello"
```

> The dual-mode failure report always shows both causes and a hint. The format is (D-032):
> ```
> IPC failed: <ipc_error>
> WebSocket failed: <ws_error>
> Hint: <suggestion>
> ```

---

### Error 2: Listen Already Running (Lock Conflict)

**Cause**: Another daemon with the same `(user_id, device_id)` is already running. The process lock (D-031) prevents duplicate daemons.

**Error Message**:

```
Error: listen already running (PID: 12345)
```

**Exit Code**: `2`

**Diagnosis**:
- The lock file `~/.xyncra/{user_id}/{device_id}/xyncra.lock` is held by PID 12345
- The daemon is actively running, or the lock is stale (process died without cleanup)

**Resolution**:

Option A -- Use the existing daemon:

```bash
# The daemon is already running, just send commands through it
./xyncra-client send --user-id alice -c 550e8400 -m "Hello"
```

Option B -- Stop the existing daemon and restart:

```bash
# Graceful stop
./xyncra-client kill --user-id alice

# Restart
./xyncra-client listen --user-id alice
```

Option C -- Force kill (if daemon is unresponsive):

```bash
./xyncra-client kill --user-id alice --force
./xyncra-client listen --user-id alice
```

### Stale Lock Detection

If the process from the lock file is no longer alive, the daemon detects the stale lock and automatically cleans up:

```bash
# Process 12345 has already exited, but lock file remains
./xyncra-client listen --user-id alice
# [xyncra] Stale lock detected (PID: 12345, process not running). Cleaning up.
# [xyncra] Starting listener daemon...
```

---

### Error 3: Daemon Not Running (sync-updates)

**Cause**: The `sync-updates` command requires a running daemon. It is IPC-only with no WebSocket fallback (D-036).

**Error Message**:

```
Error: daemon not running.
Hint: Start with 'xyncra-client listen --user-id alice'
```

**Exit Code**: `2`

**Diagnosis**:
- The daemon is not running for this `(user_id, device_id)` combination
- The IPC socket file does not exist

**Resolution**:

```bash
# Start the daemon first
./xyncra-client listen --user-id alice &

# Wait for it to connect, then sync
sleep 2
./xyncra-client sync-updates --user-id alice
```

> Unlike other commands (send, create-conversation, etc.), `sync-updates` does NOT fall back to a standalone WebSocket connection. This is intentional (D-036) to avoid state conflicts with the daemon's sync manager.

---

### Error 4: Missing User ID

**Cause**: Neither `--user-id` flag nor `XYNCRA_USER_ID` environment variable is set.

**Error Message**:

```
Error: user-id is required
```

**Exit Code**: `1`

**Resolution**:

Option A -- Provide the flag:

```bash
./xyncra-client send --user-id alice -c 550e8400 -m "Hello"
```

Option B -- Set the environment variable (D-034):

```bash
export XYNCRA_USER_ID=alice
./xyncra-client send -c 550e8400 -m "Hello"
```

---

### Error 5: Conversation Not Found (Local DB)

**Cause**: The query command reads from the local SQLite database (D-035), but the conversation has not been synced yet.

**Error Message**:

```
Error: get-conversation: conversation 550e8400 not found
```

**Exit Code**: `1`

**Diagnosis**:
- The conversation exists on the server but has not been synced to the local database
- The conversation ID is incorrect

**Resolution**:

```bash
# Ensure the daemon is running and has synced
./xyncra-client sync-updates --user-id alice

# Then retry
./xyncra-client get-conversation --user-id alice -c 550e8400
```

> Query commands (list-conversations, get-conversation, get-messages, search-messages) always read from the local database. If data is missing, it means the daemon has not yet synced it.

---

### Error 6: Kill Command Timeout

**Cause**: The daemon did not respond to SIGTERM within the specified timeout (D-039).

**Error Message**:

```
Error: process did not exit within 5s. Use --force to force kill
```

**Exit Code**: `3`

**Resolution**:

```bash
# Use --force to send SIGKILL
./xyncra-client kill --user-id alice --force

# Or increase the timeout
./xyncra-client kill --user-id alice --timeout 10s
```

---

## Dual-Mode Failure Report Format

When a command that uses IPC+WS fallback (D-032) fails, the error always follows this format:

```
Error: Cannot <action>.
  Cause 1: <ipc_error>
  Cause 2: <ws_error>
Hint: Start the daemon first: xyncra-client listen --user-id <user>
```

This applies to:
- `send`
- `create-conversation`
- `delete-conversation`
- `restore-conversation`
- `delete-message`
- `mark-as-read`

The two causes help you diagnose which subsystem failed:
- **Cause 1 (IPC)**: Usually "connection refused" (daemon not running) or a socket permission error
- **Cause 2 (WebSocket)**: Usually "connection refused" (server not running) or a network error

---

## Client Error Codes (D-027)

The client defines three additional error codes in the `-400` range:

| Code | Type | Description |
|:----:|------|-------------|
| `-400` | `ConnectionError` | WebSocket connection failed (network unreachable, server not started) |
| `-401` | `TimeoutError` | RPC call timed out (request sent but no response within timeout) |
| `-402` | `SyncError` | Incremental sync failed (gap in seq, sync_updates error) |

These errors appear in the daemon's log output and in RPC error responses. They follow the same `HandlerError` pattern as server errors (D-017).

---

## Handling Exit Codes in Scripts

### Basic Pattern

```bash
./xyncra-client send --user-id alice -c 550e8400 -m "Test message"
exit_code=$?

if [ $exit_code -eq 0 ]; then
  echo "Message sent successfully"
elif [ $exit_code -eq 2 ]; then
  echo "Prerequisite not met -- is the daemon running?"
  echo "Try: xyncra-client listen --user-id alice"
else
  echo "Command failed (exit code: $exit_code)"
fi
```

### Retry with Backoff

```bash
max_retries=3
retry_delay=2

for i in $(seq 1 $max_retries); do
  ./xyncra-client send --user-id alice -c 550e8400 -m "Test message"
  exit_code=$?

  if [ $exit_code -eq 0 ]; then
    echo "Success"
    break
  elif [ $exit_code -eq 2 ]; then
    echo "Daemon not ready, retrying in ${retry_delay}s..."
    sleep $retry_delay
    retry_delay=$((retry_delay * 2))
  else
    echo "Failed with exit code $exit_code"
    break
  fi
done
```

### Conditional Sync

```bash
# Check if daemon is running before attempting sync
./xyncra-client sync-updates --user-id alice 2>/dev/null
if [ $? -ne 0 ]; then
  echo "Daemon not running. Starting..."
  ./xyncra-client listen --user-id alice &
  sleep 3
  ./xyncra-client sync-updates --user-id alice
fi
```

### Batch Send with Error Tracking

```bash
success=0
failed=0
skipped=0

for conv_id in "$@"; do
  ./xyncra-client send --user-id alice -c "$conv_id" -m "Broadcast" 2>/dev/null
  exit_code=$?

  case $exit_code in
    0) success=$((success + 1)) ;;
    2) skipped=$((skipped + 1)) ;;
    *) failed=$((failed + 1)) ;;
  esac
done

echo "Results: $success sent, $skipped skipped, $failed failed"
```

### Safe Kill and Restart

```bash
# Kill daemon (ignore "not running" errors)
./xyncra-client kill --user-id alice 2>/dev/null

# Clean start
./xyncra-client listen --user-id alice &
daemon_pid=$!

# Wait for daemon to be ready
for i in $(seq 1 10); do
  if [ -S "$HOME/.xyncra/alice/$(hostname | shasum -a 256 | cut -c1-8)/xyncra.sock" ]; then
    echo "Daemon ready (PID: $daemon_pid)"
    break
  fi
  sleep 1
done
```

---

## JSON-RPC Error Codes

For debugging daemon IPC communication, these are the JSON-RPC error codes used internally:

| Code | Meaning |
|:----:|---------|
| `-32700` | Parse error (malformed JSON) |
| `-32600` | Invalid Request |
| `-32601` | Method not found |
| `-32602` | Invalid params (missing or wrong-type parameters) |
| `-32000` | Server error |
| `-300` | Generic server error |

These errors are typically seen in daemon logs when debug mode is enabled (`XYNCRA_DEBUG=1`).

---

## Related Documentation

- [Basic Usage](./basic-usage.md) -- common workflows
- [Offline Sync](./offline-sync.md) -- sync errors and recovery
- [Advanced Usage](./advanced.md) -- debug mode and environment variables
