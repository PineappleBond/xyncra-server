# draft

> Manage message drafts (local only).

All draft operations are forwarded to the running `listen` daemon via IPC. The daemon owns the IndexedDB database and performs the actual read/write. **The daemon must be running.**

---

## draft save

> Save or update a draft for a conversation.

### Execution Mode

IPC-only (D-036): Connects to the running `listen` daemon via Unix Socket IPC. No WebSocket fallback.

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

### Error: Daemon Not Running

If the daemon is not running:

```
Error: daemon not running.
Hint: Start with 'xyncra-client listen --user-id alice --device-id dev1'
```

Exit code: `2` (D-042 -- precondition not met).

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

IPC-only (D-036): Reads from the daemon's IndexedDB via Unix Socket IPC.

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

### Error: Daemon Not Running

If the daemon is not running:

```
Error: daemon not running.
Hint: Start with 'xyncra-client listen --user-id alice --device-id dev1'
```

Exit code: `2`.

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

IPC-only (D-036): Deletes from the daemon's IndexedDB via Unix Socket IPC.

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

### Error: Daemon Not Running

If the daemon is not running:

```
Error: daemon not running.
Hint: Start with 'xyncra-client listen --user-id alice --device-id dev1'
```

Exit code: `2`.

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

- **IPC-only** (D-036): Unlike the Go client which reads/writes SQLite directly, the TS client forwards all draft operations to the daemon via IPC. The daemon owns the IndexedDB (Dexie.js) database.
- **Storage**: Drafts are stored in the `drafts` table of the IndexedDB database (TS-D-012). The `--db-path` flag is redefined as an IndexedDB database name, not a file path.
- **Local only**: All draft data stays in the local IndexedDB database. Drafts are NOT synced to the server or other devices.
- **One draft per conversation**: The data model enforces a single draft per conversation via UPSERT.
- **Offline available**: Drafts can be saved, retrieved, and deleted as long as the daemon is running, even without a network connection (the daemon maintains the local DB).
- **Installation**: The CLI is installed via npm (`@xyncra/client-cli`). Requires Node.js 20+.
