# conversations

> Manage conversations: create, delete, restore, list, and inspect.

This document covers 5 commands: `create-conversation`, `delete-conversation`, `restore-conversation`, `list-conversations`, and `get-conversation`.

---

## create-conversation

> Create a 1-on-1 conversation with another user.

### Execution Mode

IPC+WS fallback (D-032).

### Usage

```bash
xyncra-client create-conversation [flags]
```

### Flags

| Flag | Type | Default | Required | Description |
|------|------|---------|----------|-------------|
| `--peer-id` | string | `""` | Yes | Peer user ID (required) |
| `--title` | string | `""` | No | Conversation title |

### Examples

Create a conversation with user `bob`:

```bash
xyncra-client create-conversation --user-id alice --peer-id bob
```

Create with a title:

```bash
xyncra-client create-conversation --user-id alice --peer-id bob --title "Project Chat"
```

### Output Format

**New conversation created (stdout):**

```
Conversation created.
  Conversation ID: <conv-uuid>
  Peer: bob
  Title: Project Chat
```

**Already exists (find-or-create, D-011):**

```
Conversation already exists (find-or-create).
  Conversation ID: <conv-uuid>
  Peer: bob
  Title: Project Chat
```

**Both modes failed (stderr):**

```
Error: Cannot create conversation.
  Cause 1: <ipc_error>
  Cause 2: <ws_error>
Hint: Start the daemon first: xyncra-client listen --user-id alice
```

### Notes

- **--peer-id, not --user-id** (D-037): This flag is named `--peer-id` to avoid shadowing the global `--user-id` flag. Writing `--user-id bob` here would be ambiguous.
- **Find-or-create idempotency** (D-011): Repeated calls with the same (user_id, peer_id) pair return the existing conversation. The server checks `GetByUsers` and returns `duplicate=true` if the conversation already exists.

---

## delete-conversation

> Soft-delete a conversation and all its messages.

### Execution Mode

IPC+WS fallback (D-032).

### Usage

```bash
xyncra-client delete-conversation [flags]
```

### Flags

| Flag | Short | Type | Default | Required | Description |
|------|-------|------|---------|----------|-------------|
| `--conversation-id` | `-c` | string | `""` | Yes | Conversation ID (required) |

### Examples

Delete a conversation:

```bash
xyncra-client delete-conversation --user-id alice -c <conv-uuid>
```

### Output Format

**Success (stdout):**

```
Conversation deleted.
```

**Both modes failed (stderr):**

```
Error: Cannot delete conversation.
  Cause 1: <ipc_error>
  Cause 2: <ws_error>
Hint: Start the daemon first: xyncra-client listen --user-id alice
```

### Notes

- **Cascade soft-delete** (D-013): Deleting a conversation also soft-deletes all messages under it. Both operations execute in a single database transaction.
- **Current simplification**: Conversation is a shared record between both users. Deleting it affects both sides (GORM soft-delete applies to both).

---

## restore-conversation

> Restore a previously soft-deleted conversation.

### Execution Mode

IPC+WS fallback (D-032).

### Usage

```bash
xyncra-client restore-conversation [flags]
```

### Flags

| Flag | Short | Type | Default | Required | Description |
|------|-------|------|---------|----------|-------------|
| `--conversation-id` | `-c` | string | `""` | Yes | Conversation ID (required) |

### Examples

Restore a deleted conversation:

```bash
xyncra-client restore-conversation --user-id alice -c <conv-uuid>
```

### Output Format

**Success (stdout):**

```
Conversation restored.
```

**Both modes failed (stderr):**

```
Error: Cannot restore conversation.
  Cause 1: <ipc_error>
  Cause 2: <ws_error>
Hint: Start the daemon first: xyncra-client listen --user-id alice
```

### Notes

- **Cascade restore** (D-015): Restoring a conversation also restores all its soft-deleted messages. Both operations execute in a single transaction.
- **Idempotent on non-deleted conversations**: Calling `restore-conversation` on a conversation that is not deleted is a no-op -- it returns the current conversation without error.

---

## list-conversations

> List conversations from local database.

### Execution Mode

Local DB read (D-035): Reads directly from local SQLite. **Works offline** -- no network or daemon required.

### Usage

```bash
xyncra-client list-conversations [flags]
```

### Flags

| Flag | Type | Default | Required | Description |
|------|------|---------|----------|-------------|
| `--offset` | int | `0` | No | Pagination offset |
| `--limit` | int | `20` | No | Maximum number of conversations to show |

### Examples

List first page of conversations:

```bash
xyncra-client list-conversations --user-id alice
```

Paginate to the next page:

```bash
xyncra-client list-conversations --user-id alice --offset 20 --limit 10
```

### Output Format (stdout)

```
ID                                    Peer                  Title                         Last Message
-----------------------------------------------------------------------------------------------------
550e8400-e29b-41d4-a716-446655440000  bob                   Project Chat                  2026-07-09 12:34:56
660e8400-e29b-41d4-a716-446655440001  charlie               Lunch plans                   2026-07-09 11:20:00
... more conversations available (use --offset to paginate)
```

The `... more conversations available` hint appears when there are more results beyond the current page.

### Notes

- **Pagination**: Fetches `limit+1` records internally to detect `hasMore`. Only `limit` records are displayed.
- **Time format**: Last Message time is formatted as `2006-01-02 15:04:05`.
- **Offline available** (D-035): Data reflects the state at last sync, not real-time.

---

## get-conversation

> Show conversation details from local database.

### Execution Mode

Local DB read (D-035): Reads directly from local SQLite. **Works offline** -- no network or daemon required.

### Usage

```bash
xyncra-client get-conversation [flags]
```

### Flags

| Flag | Short | Type | Default | Required | Description |
|------|-------|------|---------|----------|-------------|
| `--conversation-id` | `-c` | string | `""` | Yes | Conversation ID (required) |

### Examples

Show conversation details:

```bash
xyncra-client get-conversation --user-id alice -c <conv-uuid>
```

### Output Format (stdout)

```
Conversation Details
  ID:           550e8400-e29b-41d4-a716-446655440000
  Type:         direct
  User 1:       alice
  User 2:       bob
  Peer:         bob
  Title:        Project Chat
  Created:      2026-07-09 10:00:00
  Last Message: 2026-07-09 12:34:56
  Unread:       3
```

### Error Messages

```
get-conversation: conversation <uuid> not found
```

Exit code: `1`.

### Notes

- **Unread count** (D-012): Calculated based on the current user's read cursor (`LastReadMessageID1` or `LastReadMessageID2`) vs `LastProcessedMessageID`.
- **Peer identification**: Automatically identifies the peer by selecting the user ID that is NOT the current user (`UserID2` if `UserID1 == current_user`, otherwise `UserID1`).
- **Offline available** (D-035): Data reflects the state at last sync.
