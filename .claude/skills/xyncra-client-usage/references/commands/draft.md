# draft

> Manage message drafts (local only).

All draft operations read and write directly to the local SQLite database. **No network or daemon required.**

---

## draft save

> Save or update a draft for a conversation.

### Execution Mode

Local DB read/write. No network required.

### Usage

```bash
xyncra-client draft save [flags]
```

### Flags

| Flag | Short | Type | Default | Required | Description |
|------|-------|------|---------|----------|-------------|
| `--conversation-id` | `-c` | string | `""` | Yes | Conversation ID (required) |
| `--content` | `-m` | string | `""` | Yes | Draft content (required) |

### Examples

Save a draft:

```bash
xyncra-client draft save --user-id alice --device-id dev1 -c <conv-uuid> -m "Hey, I was thinking about..."
```

Update an existing draft (overwrites):

```bash
xyncra-client draft save --user-id alice --device-id dev1 -c <conv-uuid> -m "Updated draft content"
```

### Output Format

**Success (stdout):**

```
Draft saved.
```

### Notes

- **UPSERT semantics**: Each conversation can have at most one draft. If a draft already exists for the conversation, it is overwritten with the new content.
- **Draft ID**: Automatically generated as UUID v4 on first save. Subsequent saves update the same record.

---

## draft get

> Retrieve the draft for a conversation.

### Execution Mode

Local DB read. No network required.

### Usage

```bash
xyncra-client draft get [flags]
```

### Flags

| Flag | Short | Type | Default | Required | Description |
|------|-------|------|---------|----------|-------------|
| `--conversation-id` | `-c` | string | `""` | Yes | Conversation ID (required) |

### Examples

Get the draft for a conversation:

```bash
xyncra-client draft get --user-id alice --device-id dev1 -c <conv-uuid>
```

### Output Format

**Draft found (stdout):**

```
Draft for conversation <conv-uuid>:
Hey, I was thinking about...
```

**No draft found (stdout):**

```
No draft found for this conversation.
```

---

## draft delete

> Delete the draft for a conversation.

### Execution Mode

Local DB write. No network required.

### Usage

```bash
xyncra-client draft delete [flags]
```

### Flags

| Flag | Short | Type | Default | Required | Description |
|------|-------|------|---------|----------|-------------|
| `--conversation-id` | `-c` | string | `""` | Yes | Conversation ID (required) |

### Examples

Delete a draft:

```bash
xyncra-client draft delete --user-id alice --device-id dev1 -c <conv-uuid>
```

### Output Format

**Draft deleted (stdout):**

```
Draft deleted.
```

**No draft found (stdout):**

```
No draft found for this conversation.
```

### Notes

- **Idempotent**: Deleting a draft that does not exist is a no-op -- no error is returned.

---

## Notes

- **Local only**: All draft data stays in the local SQLite database. Drafts are NOT synced to the server or other devices.
- **One draft per conversation**: The data model enforces a single draft per conversation via UPSERT.
- **Offline available**: Drafts can be saved, retrieved, and deleted without any network connection.
- **Database location**: Drafts are stored in `~/.xyncra/{user_id}/{device_id}/xyncra.db` alongside other local data.
