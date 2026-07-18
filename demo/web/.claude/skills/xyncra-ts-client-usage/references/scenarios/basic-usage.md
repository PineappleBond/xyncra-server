# Basic Usage Scenarios

This document covers common day-to-day usage patterns for the `xyncra-client` TypeScript CLI.

---

## Scenario 1: First-time Setup and Initial Run

### Prerequisites

- Node.js >= 20 installed
- Redis running on `localhost:6379` (required by xyncra-server)

### Step 1: Build and Install the Client

```bash
cd /path/to/xyncra-server/packages/xyncra-client-cli
npm run build
npm link
```

> `npm link` makes the `xyncra-client` binary available globally.

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
xyncra-client listen --user-id alice --device-id dev1
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
xyncra-client create-conversation --user-id alice --device-id dev1 --peer-id bob
```

Expected output:

```
Conversation created.
  Conversation ID: 550e8400-e29b-41d4-a716-446655440000
  Peer: bob
```

Calling it again returns the existing conversation:

```bash
xyncra-client create-conversation --user-id alice --device-id dev1 --peer-id bob
```

```
Conversation already exists (find-or-create).
  Conversation ID: 550e8400-e29b-41d4-a716-446655440000
  Peer: bob
```

### Step 5: Send a Message

```bash
xyncra-client send --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000 -m "Hello, Bob!"
```

Expected output:

```
Message sent.
  Message ID: 1
  UUID: <message-uuid>
  Conversation: 550e8400-e29b-41d4-a716-446655440000
  Client Msg ID: f47ac10b-58cc-4372-a567-0e02b2c3d479
  Duplicate: false
```

> The `Client Msg ID` is a UUID v4 generated automatically for idempotency (D-006). Retrying the same command produces `Duplicate: true`.

### Step 6: Query Data (Requires Running Daemon)

All query commands read from the daemon's IndexedDB via IPC. The daemon **must** be running for query commands to work (TS-D-012). Unlike the Go client which reads a local SQLite file directly, the TS client stores data in IndexedDB (Dexie.js) which lives in the daemon process memory.

```bash
xyncra-client list-conversations --user-id alice --device-id dev1
```

```
ID                                      Peer                  Title   Last Message
----                                    ----                  -----   ------------
550e8400-e29b-41d4-a716-446655440000   bob                           2026-07-09 12:34:56
```

```bash
xyncra-client get-messages --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000
```

```
[#1] alice (12:34): Hello, Bob!
```

> If the daemon is not running, query commands will fail with an IPC connection error. See [Error Handling](./error-handling.md) for details.

---

## Scenario 2: Daily Workflow

A typical daily session: start the daemon, check conversations, read messages, reply, and mark as read.

### Complete Command Sequence

```bash
# 1. Start the daemon
xyncra-client listen --user-id alice --device-id dev1
# (runs in background or separate terminal)

# 2. List all conversations (requires daemon)
xyncra-client list-conversations --user-id alice --device-id dev1

# 3. View conversation details (includes unread count, D-012) (requires daemon)
xyncra-client get-conversation --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000

# 4. Read messages in a conversation (requires daemon)
xyncra-client get-messages --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000

# 5. Send a reply
xyncra-client send --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000 -m "Hi Bob, how are you?"

# 6. Mark all messages as read (D-012, MAX semantics)
xyncra-client mark-as-read --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000
```

Expected output for `get-conversation`:

```
Conversation Details
  ID:           550e8400-e29b-41d4-a716-446655440000
  Type:         direct
  User 1:       alice
  User 2:       bob
  Peer:         bob
  Created:      2026-07-09 12:00:00
  Last Message: 2026-07-09 12:35:10
  Unread:       0
```

Expected output for `mark-as-read`:

```
Marked as read up to message #2.
```

> When `--message-id` is omitted or `0`, the daemon reads `LastProcessedMessageID` from IndexedDB and uses it as the cursor (D-012). The MAX semantics ensure the read cursor only moves forward, never backward.

### Paginating Messages

Use `--after-message-id` to page through messages:

```bash
# First page (default --limit=50)
xyncra-client get-messages --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000

# Next page, starting after message #50
xyncra-client get-messages --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000 --after-message-id 50
```

---

## Scenario 3: Search, Export, and Typing Indicators

### Search Messages

Search for messages containing specific text within a conversation (requires daemon, IPC-only):

```bash
xyncra-client search-messages --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000 -q "Hello"
```

Expected output (results in DESC order, newest first):

```
[#1] alice (12:34): Hello, Bob!
```

> Search results are returned in reverse chronological order (newest first). Use `--after-message-id` for pagination in this context.

### Set Typing Indicator

Notify the peer that you are typing (D-050, fire-and-forget, IPC-only):

```bash
xyncra-client set-typing --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000
```

> This is a fire-and-forget command. It sends a typing event through the daemon's WebSocket connection. No response is expected.

### Stream Text

Send streaming text output to a conversation (D-051, fire-and-forget, IPC-only):

```bash
echo "Generating response..." | xyncra-client stream-text --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000
```

### Export Logs to CSV

Export RPC logs for external analysis (D-040, default retention 7 days):

```bash
xyncra-client logs export --user-id alice --device-id dev1 --format csv --output rpc-logs.csv
```

Expected output (stderr):

```
Exported to rpc-logs.csv
```

The CSV file contains columns: time, method, status, duration, conversation_id, request_id.

### Export Logs to JSON

```bash
xyncra-client logs export --user-id alice --device-id dev1 --format json --output rpc-logs.json
```

### View Log Statistics

```bash
xyncra-client logs stats --user-id alice --device-id dev1 --since 24h
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
xyncra-client logs stats --user-id alice --device-id dev1 --since 24h --interval 1h
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
xyncra-client logs cleanup --user-id alice --device-id dev1 --retain 7d --dry-run
```

```
Would delete 150 log entries older than 2026-07-02T12:00:00Z
  RPC logs: 120
  Notification logs: 30
```

Execute the cleanup:

```bash
xyncra-client logs cleanup --user-id alice --device-id dev1 --retain 7d
```

```
Deleted 150 log entries.
  RPC logs: 120
  Notification logs: 30
```

---

## Scenario 4: Draft Management

Drafts are stored in the daemon's IndexedDB and require a running daemon (TS-D-012).

### Save a Draft

```bash
xyncra-client draft save --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000 -m "Hey Bob, I wanted to tell you about..."
```

```
Draft saved.
```

> Draft uses upsert semantics: saving again for the same conversation overwrites the previous draft.

### Retrieve a Draft

```bash
xyncra-client draft get --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000
```

```
Draft for conversation 550e8400-e29b-41d4-a716-446655440000:
Hey Bob, I wanted to tell you about...
```

### Overwrite a Draft

```bash
xyncra-client draft save --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000 -m "Revised draft content"
```

```
Draft saved.
```

### Delete a Draft

```bash
xyncra-client draft delete --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000
```

```
Draft deleted.
```

If no draft exists:

```bash
xyncra-client draft get --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000
```

```
No draft found for this conversation.
```

---

## Scenario 5: Delete and Restore

### Delete a Conversation

Soft-delete a conversation (cascade: deletes all messages too, D-013):

```bash
xyncra-client delete-conversation --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000
```

```
Conversation deleted. 5 message(s) removed.
```

### Restore a Conversation

Restore the conversation (cascade: restores all messages too, D-015):

```bash
xyncra-client restore-conversation --user-id alice --device-id dev1 -c 550e8400-e29b-41d4-a716-446655440000
```

```
Conversation restored. 5 message(s) recovered.
```

### Delete a Single Message

Only the sender can delete their own message (D-014):

```bash
xyncra-client delete-message --user-id alice --device-id dev1 --message-id f47ac10b-58cc-4372-a567-0e02b2c3d479
```

```
Message deleted.
```

> The `--message-id` for `delete-message` is a string UUID (the message primary key). Do not confuse it with the uint32 sequence number used by `mark-as-read` (D-038).

---

## Scenario 6: Agent HITL (Human-In-The-Loop) Resume

### Prerequisites

- Agent registered with `ask_user` tool configured
- Daemon is running

### Step 1: Send Message to Trigger Agent

```bash
xyncra-client send --user-id alice --device-id dev1 \
  -c <conv-uuid> --content "Help me with a task"
```

### Step 2: Observe [hitl] Notification in Listen Output

```
[agent_status] agent=agent/hitl-bot conv=<conv-uuid> status=asking_user
[hitl] conv=<conv-uuid> agent=agent/hitl-bot checkpoint_id=cp-abc123
  [1] interrupt_id=int-def456 question="What do you need help with?" (pending)
```

### Step 3: Resume the Agent

```bash
xyncra-client agent-resume \
  --conversation-id <conv-uuid> \
  --checkpoint-id cp-abc123 \
  --interrupt-id int-def456 \
  --answer "I need help with a task" \
  --agent-id agent/hitl-bot
```

```
Agent resumed.
  Conversation: <conv-uuid>
  Checkpoint: cp-abc123
  Agent: agent/hitl-bot
```

---

## Related Documentation

- [Multi-Device Scenarios](./multi-device.md) -- running multiple devices and instances
- [Offline Sync Scenarios](./offline-sync.md) -- understanding sync behavior
- [Error Handling Scenarios](./error-handling.md) -- exit codes and common errors
- [Advanced Usage](./advanced.md) -- environment variables, custom paths, scripting
