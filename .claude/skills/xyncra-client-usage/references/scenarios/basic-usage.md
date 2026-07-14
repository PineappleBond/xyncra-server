# Basic Usage Scenarios

This document covers common day-to-day usage patterns for the `xyncra-client` CLI.

---

## Scenario 1: First-time Setup and Initial Run

### Prerequisites

- Go toolchain installed (for building from source)
- Redis running on `localhost:6379` (required by xyncra-server)

### Step 1: Build the Client

```bash
cd /path/to/xyncra-server
go build -o xyncra-client ./cmd/xyncra-client
```

### Step 2: Start the Server

```bash
./xyncra-server
```

Expected output (stderr):

```
[xyncra] Server listening on :8080
[xyncra] Redis connected at localhost:6379
```

### Step 3: Start the Listen Daemon

Open a new terminal and start the daemon. The daemon maintains a persistent WebSocket connection and receives real-time updates (D-032).

```bash
./xyncra-client listen --user-id alice --device-id dev1
```

Expected output (stderr):

```
[xyncra] Starting listener daemon...
[xyncra] Device: a1b2c3d4
[xyncra] Connecting to ws://localhost:8080/ws?user_id=alice ...
[xyncra] IPC server listening at /Users/alice/.xyncra/alice/a1b2c3d4/xyncra.sock
[xyncra] Listening for updates... (Ctrl+C to stop)
```

> The device ID `a1b2c3d4` is derived from the hostname's SHA256 hash (D-033). Your value will differ.

### Step 4: Create a Conversation

Open another terminal. Conversations use the find-or-create idempotent model (D-011): calling `create-conversation` multiple times for the same user pair returns the same conversation.

```bash
./xyncra-client create-conversation --user-id alice --device-id dev1 --peer-id bob
```

Expected output:

```
Conversation created.
  Conversation ID: 550e8400-e29b-41d4-a716-446655440000
  Peer: bob
  Title:
```

Calling it again returns the existing conversation:

```bash
./xyncra-client create-conversation --user-id alice --device-id dev1 --peer-id bob
```

```
Conversation already exists (find-or-create).
  Conversation ID: 550e8400-e29b-41d4-a716-446655440000
  Peer: bob
  Title:
```

### Step 5: Send a Message

```bash
./xyncra-client send --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000 -m "Hello, Bob!"
```

Expected output:

```
Message sent.
  Message ID: 1
  Conversation: 550e8400-e29b-41d4-a716-446655440000
  Client Msg ID: f47ac10b-58cc-4372-a567-0e02b2c3d479
  Duplicate: false
```

> The `Client Msg ID` is a UUID v4 generated automatically for idempotency (D-006). Retrying the same command produces `Duplicate: true`.

### Step 6: Query Data (Local DB, Works Offline)

All query commands read directly from the local SQLite database (D-035). They work even when the server is unreachable.

```bash
./xyncra-client list-conversations --user-id alice --device-id dev1
```

```
ID                                      Peer                  Title   Last Message
----                                    ----                  -----   ------------
550e8400-e29b-41d4-a716-446655440000   bob                           2026-07-09 12:34:56
```

```bash
./xyncra-client get-messages --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000
```

```
[#1] alice (12:34): Hello, Bob!
```

---

## Scenario 2: Daily Workflow

A typical daily session: start the daemon, check conversations, read messages, reply, and mark as read.

### Complete Command Sequence

```bash
# 1. Start the daemon
./xyncra-client listen --user-id alice --device-id dev1
# (runs in background or separate terminal)

# 2. List all conversations
./xyncra-client list-conversations --user-id alice --device-id dev1

# 3. View conversation details (includes unread count, D-012)
./xyncra-client get-conversation --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000

# 4. Read messages in a conversation
./xyncra-client get-messages --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000

# 5. Send a reply
./xyncra-client send --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000 -m "Hi Bob, how are you?"

# 6. Mark all messages as read (D-012, MAX semantics)
./xyncra-client mark-as-read --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000
```

Expected output for `get-conversation`:

```
Conversation Details
  ID:           550e8400-e29b-41d4-a716-446655440000
  Type:         direct
  User 1:       alice
  User 2:       bob
  Peer:         bob
  Title:
  Created:      2026-07-09 12:00:00
  Last Message: 2026-07-09 12:35:10
  Unread:       0
```

Expected output for `mark-as-read`:

```
Marked as read up to message #2.
```

> When `--message-id` is omitted or `0`, the daemon reads `LastProcessedMessageID` from the local database and uses it as the cursor (D-012). The MAX semantics ensure the read cursor only moves forward, never backward.

### Paginating Messages

Use `--after-message-id` to page through messages:

```bash
# First page (default --limit=50)
./xyncra-client get-messages --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000

# Next page, starting after message #50
./xyncra-client get-messages --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000 --after-message-id 50
```

---

## Scenario 3: Search and Export

### Search Messages

Search for messages containing specific text within a conversation (D-035, local DB read):

```bash
./xyncra-client search-messages --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000 -q "Hello"
```

Expected output (results in DESC order, newest first):

```
[#1] alice (12:34): Hello, Bob!
```

> Search results are returned in reverse chronological order (newest first). Use `--after-message-id` for pagination in this context.

### Export Logs to CSV

Export RPC logs for external analysis (D-040, default retention 7 days):

```bash
./xyncra-client logs export --user-id alice --device-id dev1 --format csv --output rpc-logs.csv
```

Expected output (stderr):

```
Exported to rpc-logs.csv
```

The CSV file contains columns: time, method, status, duration, conversation_id, request_id.

### Export Logs to JSON

```bash
./xyncra-client logs export --user-id alice --device-id dev1 --format json --output rpc-logs.json
```

### View Log Statistics

```bash
./xyncra-client logs stats --user-id alice --device-id dev1 --since 24h
```

Expected output:

```
METHOD                  COUNT       SUCCESS     ERRORS      AVG (ms)
------                  -----       -------     ------      --------
send_message            42          40          2           1.234
create_conversation     3           3           0           2.567
mark_as_read            15          15          0           0.891
```

With time interval grouping:

```bash
./xyncra-client logs stats --user-id alice --device-id dev1 --since 24h --interval 1h
```

```
INTERVAL                METHOD                  COUNT       SUCCESS     ERRORS      AVG (ms)
--------                ------                  -----       -------     ------      --------
2026-07-09T12:00:00Z    send_message            20          19          1           1.100
2026-07-09T13:00:00Z    send_message            22          21          1           1.368
```

### Clean Up Old Logs

Preview what would be deleted (D-040):

```bash
./xyncra-client logs cleanup --user-id alice --device-id dev1 --retain 7d --dry-run
```

```
Would delete 150 log entries older than 2026-07-02T12:00:00Z
  RPC logs: 120
  Notification logs: 30
```

Execute the cleanup:

```bash
./xyncra-client logs cleanup --user-id alice --device-id dev1 --retain 7d
```

```
Deleted 150 log entries.
  RPC logs: 120
  Notification logs: 30
```

---

## Scenario 4: Draft Management

Drafts are stored locally in SQLite and do not require network access or a running daemon (D-035).

### Save a Draft

```bash
./xyncra-client draft save --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000 -m "Hey Bob, I wanted to tell you about..."
```

```
Draft saved.
```

> Draft uses upsert semantics: saving again for the same conversation overwrites the previous draft.

### Retrieve a Draft

```bash
./xyncra-client draft get --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000
```

```
Draft for conversation 550e8400-e29b-41d4-a716-446655440000:
Hey Bob, I wanted to tell you about...
```

### Overwrite a Draft

```bash
./xyncra-client draft save --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000 -m "Revised draft content"
```

```
Draft saved.
```

### Delete a Draft

```bash
./xyncra-client draft delete --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000
```

```
Draft deleted.
```

If no draft exists:

```bash
./xyncra-client draft get --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000
```

```
No draft found for this conversation.
```

---

## Related Documentation

- [Multi-Device Scenarios](./multi-device.md) -- running multiple devices and instances
- [Offline Sync Scenarios](./offline-sync.md) -- understanding sync behavior
- [Error Handling Scenarios](./error-handling.md) -- exit codes and common errors
- [Advanced Usage](./advanced.md) -- environment variables, custom paths, scripting
