# messages

> Manage messages: delete, mark as read, list, and search.

This document covers 4 commands: `delete-message`, `mark-as-read`, `get-messages`, and `search-messages`.

---

## delete-message

> Soft-delete a message (sender only).

### Execution Mode

IPC+WS fallback (D-032).

### Usage

```bash
xyncra-client delete-message [flags]
```

### Flags

| Flag | Type | Default | Required | Description |
|------|------|---------|----------|-------------|
| `--message-id` | string | `""` | Yes | Message UUID to delete (required) |

### Examples

Delete a message by its UUID:

```bash
xyncra-client delete-message --user-id alice --device-id dev1 --message-id 550e8400-e29b-41d4-a716-446655440000
```

### Output Format

**Success (stdout):**

```
Message deleted.
```

**Both modes failed (stderr):**

```
Error: Cannot delete message.
  Cause 1: <ipc_error>
  Cause 2: <ws_error>
Hint: Start the daemon first: xyncra-client listen --user-id alice --device-id dev1
```

### Notes

- **--message-id is a string UUID** (D-038): This is the `Message.ID` primary key, NOT the uint32 sequence number. Use the value from `get-messages` output or IPC results.
- **Sender-only permission** (D-014): Only the message sender (`SenderID == current user_id`) can delete the message. Non-senders receive a permission error.
- **Soft-delete**: The message record is marked as deleted but remains in the database.

---

## mark-as-read

> Mark messages as read in a conversation.

### Execution Mode

IPC+WS fallback (D-032).

### Usage

```bash
xyncra-client mark-as-read [flags]
```

### Flags

| Flag | Short | Type | Default | Required | Description |
|------|-------|------|---------|----------|-------------|
| `--conversation-id` | `-c` | string | `""` | Yes | Conversation ID (required) |
| `--message-id` | | uint32 | `0` | No | Message sequence number to mark as read (0 = mark all as read) |

### Examples

Mark all messages as read in a conversation:

```bash
xyncra-client mark-as-read --user-id alice --device-id dev1 -c <conv-uuid>
```

Mark up to a specific message sequence number:

```bash
xyncra-client mark-as-read --user-id alice --device-id dev1 -c <conv-uuid> --message-id 42
```

### Output Format

**Success (stdout):**

```
Marked as read up to message #42.
```

When using `--message-id 0` (mark all), the output shows the actual latest sequence number:

```
Marked as read up to message #100.
```

**Both modes failed (stderr):**

```
Error: Cannot mark as read.
  Cause 1: <ipc_error>
  Cause 2: <ws_error>
Hint: Start the daemon first: xyncra-client listen --user-id alice --device-id dev1
```

### Notes

- **--message-id is uint32** (D-038): This is the `Message.MessageID` sequence number within the conversation, NOT the string UUID. Do NOT confuse with `delete-message --message-id` which takes a string UUID.
- **0 = mark all** (D-012): When `--message-id` is `0`, the client reads `LastProcessedMessageID` from the local database and uses that value. This effectively marks all messages as read.
- **MAX semantics** (D-012): The read cursor only moves forward. If the current read position is already beyond the provided `message_id`, the server silently ignores the update (no error). This prevents multi-device race conditions from moving the cursor backward.

---

## get-messages

> List messages from the daemon's local database.

### Execution Mode

IPC-only: Connects to the running `listen` daemon via Unix Socket IPC. The daemon reads from its local IndexedDB (Dexie.js) and returns results. **Requires daemon to be running.**

> **Note**: Unlike the Go client (which reads SQLite directly), the TS client uses IndexedDB via `fake-indexeddb` in Node.js. Since IndexedDB is an in-process store, query commands must go through the daemon via IPC.

### Usage

```bash
xyncra-client get-messages [flags]
```

### Flags

| Flag | Short | Type | Default | Required | Description |
|------|-------|------|---------|----------|-------------|
| `--conversation-id` | `-c` | string | `""` | Yes | Conversation ID (required) |
| `--after-message-id` | | uint32 | `0` | No | Show messages after this sequence number |
| `--limit` | | int | `50` | No | Maximum number of messages to show |

### Examples

Get recent messages in a conversation:

```bash
xyncra-client get-messages --user-id alice --device-id dev1 -c <conv-uuid>
```

Get messages after a specific sequence number:

```bash
xyncra-client get-messages --user-id alice --device-id dev1 -c <conv-uuid> --after-message-id 20 --limit 10
```

### Output Format (stdout)

Messages are displayed in **ascending chronological order** (oldest first):

```
[#1] alice (10:00): Hello!
[#2] bob (10:01): Hi there!
[#3] alice (10:05): How are you?
...
(Use --after-message-id to see more)
```

The `(Use --after-message-id to see more)` hint appears when there are more results beyond the current page.

### Notes

- **ASC order**: Messages are returned in chronological order (oldest first, ascending by `MessageID`).
- **Pagination**: Uses `--after-message-id` as an exclusive cursor. Fetches `limit+1` records internally to detect `hasMore`.
- **Time format**: Message timestamps displayed as `HH:MM` (24-hour format, e.g., `15:04`).
- **--after-message-id is uint32** (D-038): Sequence number, not UUID.
- **Requires daemon**: Data reflects the state at last sync. The daemon must be running.

---

## search-messages

> Search messages in the daemon's local database.

### Execution Mode

IPC-only: Connects to the running `listen` daemon via Unix Socket IPC. The daemon reads from its local IndexedDB (Dexie.js) and returns results. **Requires daemon to be running.**

### Usage

```bash
xyncra-client search-messages [flags]
```

### Flags

| Flag | Short | Type | Default | Required | Description |
|------|-------|------|---------|----------|-------------|
| `--conversation-id` | `-c` | string | `""` | Yes | Conversation ID (required) |
| `--query` | `-q` | string | `""` | Yes | Search query (required) |
| `--after-message-id` | | uint32 | `0` | No | Pagination cursor: show messages with sequence number lower than this value (search returns DESC order) |
| `--limit` | | int | `50` | No | Maximum number of messages to show |

### Examples

Search for messages containing a keyword:

```bash
xyncra-client search-messages --user-id alice --device-id dev1 -c <conv-uuid> -q "meeting"
```

Paginate search results:

```bash
xyncra-client search-messages --user-id alice --device-id dev1 -c <conv-uuid> -q "meeting" --after-message-id 50 --limit 20
```

### Output Format (stdout)

Messages are displayed in **descending order** (newest first):

```
[#50] bob (14:30): Let's schedule the meeting
[#42] alice (12:15): Meeting at 3pm?
[#30] bob (10:00): Any meeting notes?
...
(Use --after-message-id to see more)
```

### Notes

- **DESC order**: Unlike `get-messages`, search results are returned in reverse chronological order (newest first). This matches typical search UX expectations.
- **Pagination with DESC** (D-038): `--after-message-id` filters to messages with sequence numbers **lower** than the cursor value. Pass the lowest sequence number from the current result set to see older results.
- **Requires daemon**: Data reflects the state at last sync. The daemon must be running.

---

## WARNING: --message-id Type Distinction (D-038)

Different commands use different types for message identification. Mixing them up will cause errors:

| Command | Flag | Type | Value Example | Identifies |
|---------|------|------|---------------|------------|
| `delete-message` | `--message-id` | **string UUID** | `550e8400-e29b-41d4-...` | `Message.ID` (primary key) |
| `mark-as-read` | `--message-id` | **uint32** | `42` | `Message.MessageID` (sequence number) |
| `get-messages` | `--after-message-id` | **uint32** | `42` | `Message.MessageID` (sequence cursor) |
| `search-messages` | `--after-message-id` | **uint32** | `42` | `Message.MessageID` (sequence cursor) |

Always check the flag help text (`--help`) to confirm the expected type.
