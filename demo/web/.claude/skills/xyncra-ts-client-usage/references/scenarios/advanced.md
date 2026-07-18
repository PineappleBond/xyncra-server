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
| `XYNCRA_DB_PATH` | `--db-path` | | `~/.xyncra/{uid}/{did}/xyncra.db` | IndexedDB database name (TS-D-012) |
| `XYNCRA_LOG_DIR` | `--log-dir` | | `~/.xyncra/{uid}/{did}/logs/` | Log directory path |
| `XYNCRA_DEBUG` | | | *(empty)* | Set to `1` or `true` to enable debug logging |

### `--db-path` Semantic Change (TS-D-012)

In the Go client, `--db-path` specifies a SQLite file path on disk. In the TS client, it is redefined as an **IndexedDB database name**. Although the default value looks like a file path (e.g., `~/.xyncra/user123/abc12345/xyncra.db`), it is actually used as the Dexie.js IndexedDB database name. In Node.js, `fake-indexeddb` provides the IndexedDB implementation, and data lives in process memory.

This means:
- No database file is created on disk
- Data is lost when the daemon process exits
- The `--db-path` value is just a namespace identifier for the IndexedDB instance

### Priority Rules (D-034)

```
flag > environment variable > default
```

Examples:

```bash
# Uses default: ws://localhost:8080/ws
xyncra-client listen --user-id alice --device-id dev1

# Uses env var: wss://prod.example.com/ws
export XYNCRA_SERVER=wss://prod.example.com/ws
xyncra-client listen --user-id alice --device-id dev1

# Uses flag (overrides env var): ws://staging:8080/ws
export XYNCRA_SERVER=wss://prod.example.com/ws
xyncra-client listen --user-id alice --device-id dev1 --server ws://staging:8080/ws
```

---

## Custom Paths

### Custom Database Name (IndexedDB)

Use `--db-path` or `XYNCRA_DB_PATH` to specify a non-default IndexedDB database name:

```bash
# Via flag
xyncra-client listen --user-id alice --device-id dev1 --db-path custom-db-name

# Via environment variable
export XYNCRA_DB_PATH=custom-db-name
xyncra-client listen --user-id alice --device-id dev1
```

> **TS-D-012**: Unlike the Go client, this is NOT a file path. It is the Dexie.js IndexedDB database name. The daemon uses this name to namespace its in-memory data store.

### Custom Log Directory

Use `--log-dir` or `XYNCRA_LOG_DIR`:

```bash
# Via flag
xyncra-client listen --user-id alice --device-id dev1 --log-dir /var/log/xyncra/

# Via environment variable
export XYNCRA_LOG_DIR=/var/log/xyncra/
xyncra-client listen --user-id alice --device-id dev1
```

### Combined Custom Configuration

```bash
export XYNCRA_USER_ID=alice
export XYNCRA_DEVICE_ID=server-01
export XYNCRA_SERVER=wss://xyncra.internal:8080/ws
export XYNCRA_DB_PATH=alice-server01
export XYNCRA_LOG_DIR=/var/log/xyncra/alice/

# All commands now use these defaults
xyncra-client listen
xyncra-client send -c 550e8400 -m "Hello"
xyncra-client list-conversations
```

---

## Debug Mode

Enable verbose debug logging by setting `XYNCRA_DEBUG`:

```bash
XYNCRA_DEBUG=1 xyncra-client listen --user-id alice --device-id dev1
```

Or with `true`:

```bash
XYNCRA_DEBUG=true xyncra-client listen --user-id alice --device-id dev1
```

### Debug Output

Debug mode adds detailed logging to stderr, including:

- WebSocket frame-level details (send/receive)
- IPC request/response JSON payloads
- FullSync progress (pages pulled, updates applied)
- IndexedDB (Dexie.js) operation timings
- Heartbeat keepalive details

Example debug output:

```
[2026-07-09 14:30:00] [DEBUG] WS send: {"jsonrpc":"2.0","method":"heartbeat","id":42}
[2026-07-09 14:30:00] [DEBUG] WS recv: {"jsonrpc":"2.0","id":42,"result":{"code":0}}
[2026-07-09 14:30:01] [DEBUG] FullSync page 1: after_seq=100, got 100 updates, has_more=true
[2026-07-09 14:30:01] [DEBUG] FullSync page 2: after_seq=200, got 50 updates, has_more=false
[2026-07-09 14:30:01] [DEBUG] FullSync complete: 150 updates applied in 1.234s
[2026-07-09 14:30:01] [DEBUG] Dexie: transactions=42, puts=150, gets=10
```

---

## Output Formats

### Tabwriter Tables (D-041)

List-style commands use aligned column output:

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
  UUID: f47ac10b-58cc-4372-a567-0e02b2c3d479
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
xyncra-client logs export --user-id alice --device-id dev1 --format csv
```

```csv
time,method,status,duration_ms,conversation_id,request_id
2026-07-09T12:34:56Z,send_message,0,1.234,550e8400,req-001
2026-07-09T12:35:00Z,mark_as_read,0,0.891,550e8400,req-002
```

**JSON format**:

```bash
xyncra-client logs export --user-id alice --device-id dev1 --format json
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
xyncra-client logs export --user-id alice --device-id dev1 --format csv > logs.csv

# Parse message list with grep (only stdout is piped)
xyncra-client get-messages --user-id alice --device-id dev1 -c 550e8400 | grep "bob"
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
  if xyncra-client send --user-id "$USER_ID" --device-id "${DEVICE_ID:-dev1}" -c "$conv_id" -m "$MESSAGE" 2>/dev/null; then
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
xyncra-client logs export --user-id "$USER_ID" --device-id "${DEVICE_ID:-dev1}" \
  --format csv --output "$OUTPUT_DIR/rpc.csv" --limit 10000

# Export notification logs
xyncra-client logs export --user-id "$USER_ID" --device-id "${DEVICE_ID:-dev1}" \
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

Poll for new messages using `--after-message-id` (requires daemon):

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
  output=$(xyncra-client get-messages --user-id "$USER_ID" --device-id "${DEVICE_ID:-dev1}" \
    -c "$CONV_ID" --after-message-id "$LAST_ID" 2>/dev/null)

  if [ -n "$output" ]; then
    echo "$output"
    # Extract the highest message ID from the output
    LAST_ID=$(echo "$output" | tail -1 | grep -oP '\[#\K[0-9]+')
  fi

  sleep 5
done
```

> Note: This script requires the daemon to be running since `get-messages` is an IPC-only command in the TS client.

### Pattern 4: Automated Daemon Management

Start the daemon if not running, and perform operations:

```bash
#!/bin/bash
# ensure-daemon.sh -- Start daemon if needed, then run a command

USER_ID="${XYNCRA_USER_ID:-alice}"

# Check if daemon is running by attempting a lightweight IPC call
if ! xyncra-client sync-updates --user-id "$USER_ID" --device-id "${DEVICE_ID:-dev1}" 2>/dev/null; then
  echo "Daemon not running. Starting..." >&2
  xyncra-client listen --user-id "$USER_ID" --device-id "${DEVICE_ID:-dev1}" &
  DAEMON_PID=$!

  for i in $(seq 1 10); do
    sleep 1
    if xyncra-client sync-updates --user-id "$USER_ID" --device-id "${DEVICE_ID:-dev1}" 2>/dev/null; then
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
./ensure-daemon.sh xyncra-client send -c 550e8400 -m "Hello"
```

### Pattern 5: Search Across All Conversations

Search for a keyword across all conversations (requires daemon):

```bash
#!/bin/bash
# search-all.sh -- Search for a keyword across all conversations

USER_ID="${XYNCRA_USER_ID:-alice}"
QUERY="${1:?Usage: search-all.sh <query>}"

# Get all conversation IDs (requires daemon)
conv_ids=$(xyncra-client list-conversations --user-id "$USER_ID" --device-id "${DEVICE_ID:-dev1}" --limit 1000 2>/dev/null | \
  tail -n +3 | awk '{print $1}')

echo "Searching for: $QUERY"
echo "---"

for conv_id in $conv_ids; do
  results=$(xyncra-client search-messages --user-id "$USER_ID" --device-id "${DEVICE_ID:-dev1}" \
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
    xyncra-client logs cleanup --user-id "$user_id" --device-id "${device_id:-dev1}" \
      --device-id "$device_id" --retain "$RETAIN"
  done
done
```

Add to crontab for daily execution:

```bash
0 2 * * * /path/to/cleanup-logs.sh >> /var/log/xyncra-cleanup.log 2>&1
```

### Pattern 7: Typing Indicator with Auto-clear

Send a typing indicator and automatically clear it after a delay:

```bash
#!/bin/bash
# typing-indicator.sh -- Send typing indicator with auto-clear

USER_ID="${XYNCRA_USER_ID:-alice}"
CONV_ID="${1:?Usage: typing-indicator.sh <conversation_id>}"

# Start typing
xyncra-client set-typing --user-id "$USER_ID" --device-id "${DEVICE_ID:-dev1}" \
  -c "$CONV_ID" 2>/dev/null

# Wait for the user to finish (or timeout after 30s)
sleep 30

# Stop typing
xyncra-client set-typing --user-id "$USER_ID" --device-id "${DEVICE_ID:-dev1}" \
  -c "$CONV_ID" --stop 2>/dev/null
```

---

## Time Argument Formats

Several commands accept time arguments (`--since`, `--from`, `--to`, `--retain`). Three formats are supported:

### Go Duration

```bash
# Last hour
xyncra-client logs tail --user-id alice --device-id dev1 --since 1h

# Last 30 minutes
xyncra-client logs tail --user-id alice --device-id dev1 --since 30m

# Last 5 seconds
xyncra-client logs tail --user-id alice --device-id dev1 --since 5s
```

### Day Shorthand

```bash
# Last 7 days
xyncra-client logs tail --user-id alice --device-id dev1 --since 7d

# Last 30 days
xyncra-client logs cleanup --user-id alice --device-id dev1 --retain 30d
```

### RFC3339

```bash
# Specific start time
xyncra-client logs search --user-id alice --device-id dev1 \
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
xyncra-client delete-message --user-id alice --device-id dev1 --message-id f47ac10b-58cc-4372-a567-0e02b2c3d479

# Mark as read by sequence number (uint32)
xyncra-client mark-as-read --user-id alice --device-id dev1 -c 550e8400 --message-id 42
```

---

## Conversation Management Commands

### Delete and Restore (D-013, D-015)

```bash
# Soft-delete a conversation (cascade: deletes all messages too)
xyncra-client delete-conversation --user-id alice --device-id dev1 -c 550e8400
```

```
Conversation deleted. 5 message(s) removed.
```

```bash
# Restore the conversation (cascade: restores all messages too)
xyncra-client restore-conversation --user-id alice --device-id dev1 -c 550e8400
```

```
Conversation restored. 5 message(s) recovered.
```

> Soft-delete and restore are both idempotent. Restoring an already-active conversation is a no-op. Delete is cascade (D-013): all messages in the conversation are also soft-deleted. Restore is cascade (D-015): all messages are restored.

### Delete a Single Message (D-014)

```bash
# Only the sender can delete their own message
xyncra-client delete-message --user-id alice --device-id dev1 --message-id f47ac10b-58cc-4372-a567-0e02b2c3d479
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
xyncra-client list-conversations --user-id alice --device-id dev1

# Next page
xyncra-client list-conversations --user-id alice --device-id dev1 --offset 20 --limit 20
```

If more results exist, the output includes:

```
... more conversations available (use --offset to paginate)
```

### Messages

```bash
# First page (default --limit=50)
xyncra-client get-messages --user-id alice --device-id dev1 -c 550e8400

# Next page
xyncra-client get-messages --user-id alice --device-id dev1 -c 550e8400 --after-message-id 50 --limit 50
```

If more results exist:

```
(Use --after-message-id to see more)
```

### Search Results

Search returns results in DESC order (newest first). Use `--after-message-id` for pagination:

```bash
# First page of search (newest matches first)
xyncra-client search-messages --user-id alice --device-id dev1 -c 550e8400 -q "Hello"

# Next page (older matches)
xyncra-client search-messages --user-id alice --device-id dev1 -c 550e8400 -q "Hello" --after-message-id 100
```

---

## Container/Docker Usage

### Running in Docker

```bash
# Build
cd /path/to/xyncra-server/packages/xyncra-client-cli
docker build -t xyncra-client .

# Run daemon
docker run -d --name xyncra-daemon \
  -e XYNCRA_USER_ID=alice \
  -e XYNCRA_SERVER=ws://xyncra-server:8080/ws \
  xyncra-client listen

# Send a message
docker exec xyncra-daemon xyncra-client send -c 550e8400 -m "Hello"
```

> **Note**: In Docker, the TS client uses `fake-indexeddb` for IndexedDB. Data lives in container memory and is lost on container restart unless you implement a persistence layer.

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
    build: ./packages/xyncra-client-cli
    environment:
      - XYNCRA_USER_ID=alice
      - XYNCRA_SERVER=ws://xyncra-server:8080/ws
    command: listen
    depends_on:
      - xyncra-server
```

---

## HITL (Human-In-The-Loop) Full Flow

When an Agent is configured with the `ask_user` tool, the Agent can request user input during execution, forming a HITL interaction loop. The following demonstrates the complete HITL flow.

### Prerequisites

- Agent registered and configured with `ask_user` tool (e.g., `agent/hitl-bot`)
- Daemon is running (`xyncra-client listen`)

### Step 1: Start Daemon Listen

```bash
xyncra-client listen --user-id alice --device-id dev1
```

The daemon outputs HITL-related events to stdout (D-085):

```
[xyncra] IPC server listening at /Users/alice/.xyncra/alice/dev1/xyncra.sock
[xyncra] Listening for updates... (Ctrl+C to stop)
```

### Step 2: Send Message to Trigger Agent

In another terminal, send a message to trigger Agent execution:

```bash
xyncra-client send --user-id alice --device-id dev1 \
  -c <conv-uuid> --content "Help me with a task"
```

### Step 3: Observe [hitl] Notification

In the listen terminal, the Agent requests user input via the `ask_user` tool. The daemon detects `agent_status == "asking_user"` and displays HITL information in `[hitl]` format (D-125):

```
[agent_status] agent=agent/hitl-bot conv=<conv-uuid> status=thinking
[agent_status] agent=agent/hitl-bot conv=<conv-uuid> status=asking_user
[hitl] conv=<conv-uuid> agent=agent/hitl-bot checkpoint_id=cp-abc123
  [1] interrupt_id=int-def456 question="What do you need help with?" (pending)
```

Key information:
- `checkpoint_id`: Agent execution checkpoint, required for resume
- `interrupt_id`: Unique identifier for this interruption, optionally provided to resume
- `question`: The question the Agent asks the user

### Step 4: Resume with agent-resume

In a third terminal (or script), use `agent-resume` to reply to the Agent's question:

```bash
xyncra-client agent-resume \
  --conversation-id <conv-uuid> \
  --checkpoint-id cp-abc123 \
  --interrupt-id int-def456 \
  --answer "I need help with a task" \
  --agent-id agent/hitl-bot
```

Output:

```
Agent resumed.
  Conversation: <conv-uuid>
  Checkpoint: cp-abc123
  Agent: agent/hitl-bot
```

### Step 5: Agent Continues Execution

After receiving the answer, the Agent continues execution. Observe subsequent events in the listen terminal:

```
[agent_status] agent=agent/hitl-bot conv=<conv-uuid> status=thinking
[agent_status] agent=agent/hitl-bot conv=<conv-uuid> status=generating
[new message] id=<msg-uuid> seq=43 from=agent/hitl-bot conv=<conv-uuid> "Here's what I found..."
[agent_status] agent=agent/hitl-bot conv=<conv-uuid> status=idle
```

### Shell Script Automation for HITL

In automation scenarios, you can parse listen output and auto-reply:

```bash
#!/bin/bash
# auto-hitl.sh -- Automatically respond to Agent HITL requests

CONV_ID="${1:?Usage: auto-hitl.sh <conversation_id>}"
AGENT_ID="${2:?Usage: auto-hitl.sh <conv_id> <agent_id>}"

# Start listen and capture [hitl] output in background
xyncra-client listen --user-id alice --device-id dev1 2>/dev/null | \
  grep --line-buffered '\[hitl\]' | \
  while read -r line; do
    # Parse checkpoint_id
    checkpoint_id=$(echo "$line" | grep -oP 'checkpoint_id=\K[^ ]+')
    # Parse subsequent line for interrupt_id and question
    read -r next_line
    interrupt_id=$(echo "$next_line" | grep -oP 'interrupt_id=\K[^ ]+')
    question=$(echo "$next_line" | grep -oP 'question="\K[^"]+')

    echo "Agent asks: $question"

    # Auto-reply (replace with LLM API call or other logic)
    xyncra-client agent-resume \
      --conversation-id "$CONV_ID" \
      --checkpoint-id "$checkpoint_id" \
      --interrupt-id "$interrupt_id" \
      --answer "Auto-confirmed" \
      --agent-id "$AGENT_ID"
  done
```

---

## Related Documentation

- [Basic Usage](./basic-usage.md) -- common workflows and first-time setup
- [Multi-Device](./multi-device.md) -- device ID model and data isolation
- [Offline Sync](./offline-sync.md) -- sync mechanism and recovery
- [Error Handling](./error-handling.md) -- exit codes and error patterns
