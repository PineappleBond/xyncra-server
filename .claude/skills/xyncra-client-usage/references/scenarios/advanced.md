# Advanced Usage Scenarios

This document covers advanced configuration, custom paths, output formats, and shell scripting patterns.

---

## Environment Variables (D-034)

All global parameters support environment variables with the `XYNCRA_` prefix. Flag names use `_` in place of `-`.

### Complete Reference

| Environment Variable | Flag | Short | Default | Description |
|---------------------|------|:-----:|---------|-------------|
| `XYNCRA_USER_ID` | `--user-id` | `-u` | *(required)* | User identity |
| `XYNCRA_DEVICE_ID` | `--device-id` | | SHA256(hostname)[:8] | Device identifier (D-033) |
| `XYNCRA_SERVER` | `--server` | `-s` | `ws://localhost:8080/ws` | Server WebSocket URL |
| `XYNCRA_DB_PATH` | `--db-path` | | `~/.xyncra/{uid}/{did}/xyncra.db` | SQLite database path |
| `XYNCRA_LOG_DIR` | `--log-dir` | | `~/.xyncra/{uid}/{did}/logs/` | Log directory path |
| `XYNCRA_DEBUG` | | | *(empty)* | Set to `1` or `true` to enable debug logging |

### Priority Rules (D-034)

```
flag > environment variable > default
```

Examples:

```bash
# Uses default: ws://localhost:8080/ws
./xyncra-client listen --user-id alice

# Uses env var: wss://prod.example.com/ws
export XYNCRA_SERVER=wss://prod.example.com/ws
./xyncra-client listen --user-id alice

# Uses flag (overrides env var): ws://staging:8080/ws
export XYNCRA_SERVER=wss://prod.example.com/ws
./xyncra-client listen --user-id alice --server ws://staging:8080/ws
```

---

## Custom Paths

### Custom Database Path

Use `--db-path` or `XYNCRA_DB_PATH` to specify a non-default database location:

```bash
# Via flag
./xyncra-client listen --user-id alice --db-path /mnt/external/xyncra.db

# Via environment variable
export XYNCRA_DB_PATH=/mnt/external/xyncra.db
./xyncra-client listen --user-id alice
```

> The database file's parent directory must exist. The CLI does not create intermediate directories for custom paths.

### Custom Log Directory

Use `--log-dir` or `XYNCRA_LOG_DIR`:

```bash
# Via flag
./xyncra-client listen --user-id alice --log-dir /var/log/xyncra/

# Via environment variable
export XYNCRA_LOG_DIR=/var/log/xyncra/
./xyncra-client listen --user-id alice
```

### Combined Custom Configuration

```bash
export XYNCRA_USER_ID=alice
export XYNCRA_DEVICE_ID=server-01
export XYNCRA_SERVER=wss://xyncra.internal:8080/ws
export XYNCRA_DB_PATH=/data/xyncra/alice.db
export XYNCRA_LOG_DIR=/var/log/xyncra/alice/

# All commands now use these defaults
./xyncra-client listen
./xyncra-client send -c 550e8400 -m "Hello"
./xyncra-client list-conversations
```

---

## Debug Mode

Enable verbose debug logging by setting `XYNCRA_DEBUG`:

```bash
XYNCRA_DEBUG=1 ./xyncra-client listen --user-id alice
```

Or with `true`:

```bash
XYNCRA_DEBUG=true ./xyncra-client listen --user-id alice
```

### Debug Output

Debug mode adds detailed logging to stderr, including:

- WebSocket frame-level details (send/receive)
- IPC request/response JSON payloads
- FullSync progress (pages pulled, updates applied)
- Database query timings
- Heartbeat keepalive details

Example debug output:

```
[2026-07-09 14:30:00] [DEBUG] WS send: {"jsonrpc":"2.0","method":"heartbeat","id":42}
[2026-07-09 14:30:00] [DEBUG] WS recv: {"jsonrpc":"2.0","id":42,"result":{"code":0}}
[2026-07-09 14:30:01] [DEBUG] FullSync page 1: after_seq=100, got 100 updates, has_more=true
[2026-07-09 14:30:01] [DEBUG] FullSync page 2: after_seq=200, got 50 updates, has_more=false
[2026-07-09 14:30:01] [DEBUG] FullSync complete: 150 updates applied in 1.234s
```

---

## Output Formats

### Tabwriter Tables (D-041)

List-style commands use `text/tabwriter` for aligned column output:

**list-conversations**:

```
ID                                      Peer                  Title           Last Message
----                                    ----                  -----           ------------
550e8400-e29b-41d4-a716-446655440000   bob                   Project Alpha   2026-07-09 14:30:00
6ba7b810-9dad-11d1-80b4-00c04fd430c8   charlie                               2026-07-09 13:15:22
```

**get-messages**:

```
[#1] alice (12:34): Hello, Bob!
[#2] bob (12:35): Hi Alice!
[#3] alice (12:36): How's the project going?
```

> Time format for messages is `HH:MM` (24-hour, hours and minutes only).

### Key-Value Details

Detail commands use aligned key-value format:

**get-conversation**:

```
Conversation Details
  ID:           550e8400-e29b-41d4-a716-446655440000
  Type:         direct
  User 1:       alice
  User 2:       bob
  Peer:         bob
  Title:        Project Alpha
  Created:      2026-07-09 12:00:00
  Last Message: 2026-07-09 14:30:00
  Unread:       3
```

**send** (success):

```
Message sent.
  Message ID: 42
  Conversation: 550e8400-e29b-41d4-a716-446655440000
  Client Msg ID: f47ac10b-58cc-4372-a567-0e02b2c3d479
  Duplicate: false
```

### Log Statistics Tables

**logs stats** (without interval):

```
METHOD                  COUNT       SUCCESS     ERRORS      AVG (ms)
------                  -----       -------     ------      --------
send_message            100         95          5           1.234
create_conversation     3           3           0           2.567
mark_as_read            15          15          0           0.891
delete_message          2           2           0           1.100
```

**logs stats** (with `--interval 1h`):

```
INTERVAL                METHOD                  COUNT       SUCCESS     ERRORS      AVG (ms)
--------                ------                  -----       -------     ------      --------
2026-07-09T12:00:00Z    send_message            50          48          2           1.234
2026-07-09T13:00:00Z    send_message            50          47          3           1.350
```

Allowed interval values: `1m`, `5m`, `15m`, `1h`, `1d`.

### CSV and JSON Export (logs export)

**CSV format**:

```bash
./xyncra-client logs export --user-id alice --format csv
```

```csv
time,method,status,duration_ms,conversation_id,request_id
2026-07-09T12:34:56Z,send_message,0,1.234,550e8400,req-001
2026-07-09T12:35:00Z,mark_as_read,0,0.891,550e8400,req-002
```

**JSON format**:

```bash
./xyncra-client logs export --user-id alice --format json
```

```json
[
  {
    "time": "2026-07-09T12:34:56Z",
    "method": "send_message",
    "status": 0,
    "duration_ms": 1.234,
    "conversation_id": "550e8400",
    "request_id": "req-001"
  },
  {
    "time": "2026-07-09T12:35:00Z",
    "method": "mark_as_read",
    "status": 0,
    "duration_ms": 0.891,
    "conversation_id": "550e8400",
    "request_id": "req-002"
  }
]
```

Export limits: `--limit` range is 1-10000 (max 10000). Values outside this range are reset to 1000.

### stdout vs stderr Separation (D-041)

- **stdout**: Command output (tables, key-value pairs, exported data)
- **stderr**: Daemon logs, error messages, hints, progress information

This separation enables piping stdout to other tools while preserving error visibility:

```bash
# Export logs to a file via stdout redirect
./xyncra-client logs export --user-id alice --format csv > logs.csv

# Parse message list with grep (only stdout is piped)
./xyncra-client get-messages --user-id alice -c 550e8400 | grep "bob"
```

---

## Shell Scripting Patterns

### Pattern 1: Batch Send Messages

Send the same message to multiple conversations:

```bash
#!/bin/bash
# broadcast.sh -- Send a message to multiple conversations

USER_ID="${XYNCRA_USER_ID:?Set XYNCRA_USER_ID or pass --user-id}"
MESSAGE="${1:?Usage: broadcast.sh <message> <conv_id> [conv_id ...]}"
shift

success=0
failed=0

for conv_id in "$@"; do
  if ./xyncra-client send --user-id "$USER_ID" -c "$conv_id" -m "$MESSAGE" 2>/dev/null; then
    success=$((success + 1))
  else
    echo "Failed to send to conversation $conv_id" >&2
    failed=$((failed + 1))
  fi
done

echo "Broadcast complete: $success sent, $failed failed" >&2
```

Usage:

```bash
./broadcast.sh "Meeting at 3pm" 550e8400 6ba7b810 7cb8c920
```

### Pattern 2: Export and Analyze Logs

Export logs and perform analysis with standard Unix tools:

```bash
#!/bin/bash
# analyze-logs.sh -- Export RPC logs and find error patterns

USER_ID="${XYNCRA_USER_ID:-alice}"
OUTPUT_DIR="/tmp/xyncra-analysis"
mkdir -p "$OUTPUT_DIR"

# Export RPC logs as CSV
./xyncra-client logs export --user-id "$USER_ID" \
  --format csv --output "$OUTPUT_DIR/rpc.csv" --limit 10000

# Export notification logs
./xyncra-client logs export --user-id "$USER_ID" \
  --type notifications --format csv --output "$OUTPUT_DIR/notifications.csv"

# Count errors by method
echo "=== Error Summary ==="
tail -n +2 "$OUTPUT_DIR/rpc.csv" | \
  awk -F',' '$3 != "0" {print $2}' | \
  sort | uniq -c | sort -rn

# Find slowest requests (> 100ms)
echo ""
echo "=== Slow Requests (>100ms) ==="
tail -n +2 "$OUTPUT_DIR/rpc.csv" | \
  awk -F',' '$4 > 100 {printf "%s %s %.0fms\n", $1, $2, $4}' | \
  sort -t' ' -k3 -rn | head -20

echo ""
echo "Logs exported to $OUTPUT_DIR/"
```

### Pattern 3: Monitor New Messages

Poll for new messages using `--after-message-id`:

```bash
#!/bin/bash
# monitor.sh -- Watch for new messages in a conversation

USER_ID="${XYNCRA_USER_ID:-alice}"
CONV_ID="${1:?Usage: monitor.sh <conversation_id>}"
LAST_ID=0

echo "Monitoring conversation $CONV_ID for new messages..."
echo "Press Ctrl+C to stop."
echo ""

while true; do
  output=$(./xyncra-client get-messages --user-id "$USER_ID" \
    -c "$CONV_ID" --after-message-id "$LAST_ID" 2>/dev/null)

  if [ -n "$output" ]; then
    echo "$output"
    # Extract the highest message ID from the output
    LAST_ID=$(echo "$output" | tail -1 | grep -oP '\[#\K[0-9]+')
  fi

  sleep 5
done
```

### Pattern 4: Automated Daemon Management

Start the daemon if not running, and perform operations:

```bash
#!/bin/bash
# ensure-daemon.sh -- Start daemon if needed, then run a command

USER_ID="${XYNCRA_USER_ID:-alice}"

# Check if daemon is running by attempting a lightweight IPC call
if ! ./xyncra-client sync-updates --user-id "$USER_ID" 2>/dev/null; then
  echo "Daemon not running. Starting..." >&2
  ./xyncra-client listen --user-id "$USER_ID" &
  DAEMON_PID=$!

  # Wait for socket to appear
  DEVICE_ID=$(./xyncra-client listen --user-id "$USER_ID" --help 2>&1 | \
    grep -oP 'device.*?\(default: \K[a-f0-9]+' || echo "")

  # Alternative: just wait and check
  for i in $(seq 1 10); do
    sleep 1
    if ./xyncra-client sync-updates --user-id "$USER_ID" 2>/dev/null; then
      echo "Daemon ready (PID: $DAEMON_PID)" >&2
      break
    fi
    if [ "$i" -eq 10 ]; then
      echo "Error: daemon failed to start" >&2
      exit 1
    fi
  done
fi

# Now execute the requested command
"$@"
```

Usage:

```bash
./ensure-daemon.sh ./xyncra-client send -c 550e8400 -m "Hello"
```

### Pattern 5: Search Across All Conversations

Search for a keyword across all conversations:

```bash
#!/bin/bash
# search-all.sh -- Search for a keyword across all conversations

USER_ID="${XYNCRA_USER_ID:-alice}"
QUERY="${1:?Usage: search-all.sh <query>}"

# Get all conversation IDs
conv_ids=$(./xyncra-client list-conversations --user-id "$USER_ID" --limit 1000 2>/dev/null | \
  tail -n +3 | awk '{print $1}')

echo "Searching for: $QUERY"
echo "---"

for conv_id in $conv_ids; do
  results=$(./xyncra-client search-messages --user-id "$USER_ID" \
    -c "$conv_id" -q "$QUERY" 2>/dev/null)
  if [ -n "$results" ]; then
    echo ""
    echo "Conversation: $conv_id"
    echo "$results"
  fi
done
```

### Pattern 6: Scheduled Log Cleanup

Run periodic log cleanup (D-040, default 7-day retention):

```bash
#!/bin/bash
# cleanup-logs.sh -- Clean old logs for all users

XYNCRA_HOME="${HOME}/.xyncra"
RETAIN="7d"

for user_dir in "$XYNCRA_HOME"/*/; do
  user_id=$(basename "$user_dir")
  for device_dir in "$user_dir"*/; do
    device_id=$(basename "$device_dir")
    echo "Cleaning logs for $user_id/$device_id..."
    ./xyncra-client logs cleanup --user-id "$user_id" \
      --device-id "$device_id" --retain "$RETAIN"
  done
done
```

Add to crontab for daily execution:

```bash
0 2 * * * /path/to/cleanup-logs.sh >> /var/log/xyncra-cleanup.log 2>&1
```

---

## Time Argument Formats

Several commands accept time arguments (`--since`, `--from`, `--to`, `--retain`). Three formats are supported:

### Go Duration

```bash
# Last hour
./xyncra-client logs tail --user-id alice --since 1h

# Last 30 minutes
./xyncra-client logs tail --user-id alice --since 30m

# Last 5 seconds
./xyncra-client logs tail --user-id alice --since 5s
```

### Day Shorthand

```bash
# Last 7 days
./xyncra-client logs tail --user-id alice --since 7d

# Last 30 days
./xyncra-client logs cleanup --user-id alice --retain 30d
```

### RFC3339

```bash
# Specific start time
./xyncra-client logs search --user-id alice \
  --from 2026-07-09T12:00:00Z --to 2026-07-09T14:00:00Z
```

---

## Message ID Types (D-038)

Different commands use different types for `--message-id`. Pay attention to the type:

| Command | Flag | Type | Description |
|---------|------|------|-------------|
| `delete-message` | `--message-id` | string UUID | Message primary key (e.g., `f47ac10b-58cc-4372-a567-0e02b2c3d479`) |
| `mark-as-read` | `--message-id` | uint32 | Sequence number within conversation (e.g., `42`) |
| `get-messages` | `--after-message-id` | uint32 | Pagination cursor, sequence number (e.g., `50`) |
| `search-messages` | `--after-message-id` | uint32 | Pagination cursor for search results |

### Example: Delete vs. Mark-as-Read

```bash
# Delete a message by UUID (string)
./xyncra-client delete-message --user-id alice --message-id f47ac10b-58cc-4372-a567-0e02b2c3d479

# Mark as read by sequence number (uint32)
./xyncra-client mark-as-read --user-id alice -c 550e8400 --message-id 42
```

---

## Conversation Management Commands

### Delete and Restore (D-013, D-015)

```bash
# Soft-delete a conversation (cascade: deletes all messages too)
./xyncra-client delete-conversation --user-id alice -c 550e8400
```

```
Conversation deleted.
```

```bash
# Restore the conversation (cascade: restores all messages too)
./xyncra-client restore-conversation --user-id alice -c 550e8400
```

```
Conversation restored.
```

> Soft-delete and restore are both idempotent. Restoring an already-active conversation is a no-op. Delete is cascade (D-013): all messages in the conversation are also soft-deleted. Restore is cascade (D-015): all messages are restored.

### Delete a Single Message (D-014)

```bash
# Only the sender can delete their own message
./xyncra-client delete-message --user-id alice --message-id f47ac10b-58cc-4372-a567-0e02b2c3d479
```

```
Message deleted.
```

> Permission check (D-014): only the message sender can delete it. If alice tries to delete a message sent by bob, the server returns a permission error.

---

## Pagination

### Conversations

```bash
# First page (default --limit=20)
./xyncra-client list-conversations --user-id alice

# Next page
./xyncra-client list-conversations --user-id alice --offset 20 --limit 20
```

If more results exist, the output includes:

```
... more conversations available (use --offset to paginate)
```

### Messages

```bash
# First page (default --limit=50)
./xyncra-client get-messages --user-id alice -c 550e8400

# Next page
./xyncra-client get-messages --user-id alice -c 550e8400 --after-message-id 50 --limit 50
```

If more results exist:

```
(Use --after-message-id to see more)
```

### Search Results

Search returns results in DESC order (newest first). Use `--after-message-id` for pagination:

```bash
# First page of search (newest matches first)
./xyncra-client search-messages --user-id alice -c 550e8400 -q "Hello"

# Next page (older matches)
./xyncra-client search-messages --user-id alice -c 550e8400 -q "Hello" --after-message-id 100
```

---

## Container/Docker Usage

### Running in Docker

```bash
# Build
docker build -t xyncra-client .

# Run daemon
docker run -d --name xyncra-daemon \
  -e XYNCRA_USER_ID=alice \
  -e XYNCRA_SERVER=ws://xyncra-server:8080/ws \
  -v xyncra-data:/root/.xyncra \
  xyncra-client listen

# Send a message
docker exec xyncra-daemon xyncra-client send -c 550e8400 -m "Hello"
```

### Docker Compose with Server

```yaml
services:
  redis:
    image: redis:7
    ports:
      - "6379:6379"

  xyncra-server:
    build: .
    ports:
      - "8080:8080"
    depends_on:
      - redis

  xyncra-client:
    build: ./cmd/xyncra-client
    environment:
      - XYNCRA_USER_ID=alice
      - XYNCRA_SERVER=ws://xyncra-server:8080/ws
    volumes:
      - xyncra-data:/root/.xyncra
    command: listen
    depends_on:
      - xyncra-server
```

---

## Related Documentation

- [Basic Usage](./basic-usage.md) -- common workflows and first-time setup
- [Multi-Device](./multi-device.md) -- device ID model and data isolation
- [Offline Sync](./offline-sync.md) -- sync mechanism and recovery
- [Error Handling](./error-handling.md) -- exit codes and error patterns
