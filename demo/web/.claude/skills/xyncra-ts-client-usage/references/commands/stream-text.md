# stream-text

> Send streaming text to a conversation.

## Execution Mode

IPC-only (D-036, D-051): Connects to the running `listen` daemon via Unix Socket IPC. No WebSocket fallback.

Streaming text is fire-and-forget (D-051): streaming chunks are not persisted (seq=0), not retried, and delivered to conversation peers in real time via the daemon's WebSocket. If the daemon is not running there is nothing to broadcast, so standalone fallback is pointless.

## Usage

```bash
xyncra-client stream-text [flags]
```

## Flags

| Flag | Short | Type | Default | Required | Description |
|------|-------|------|---------|----------|-------------|
| `--conversation-id` | `-c` | string | `""` | Yes | Conversation ID (required) |
| `--stream-id` | | string | `""` | Yes | Stream ID (client-generated UUID, must be consistent across chunks of the same stream) |
| `--text` | | string | `""` | Yes | Cumulative text content (each chunk contains the full text so far, not just the delta) |
| `--done` | | bool | `false` | No | Mark stream as done (`is_done=true`) |

## Examples

Send the first chunk of a stream:

```bash
xyncra-client stream-text --user-id alice --device-id dev1 \
  -c <conv-uuid> --stream-id 550e8400-e29b-41d4-a716-446655440000 \
  --text "Hello"
```

Output:

```
Streaming text sent to conversation <conv-uuid>
```

Send subsequent chunks (cumulative text):

```bash
xyncra-client stream-text --user-id alice --device-id dev1 \
  -c <conv-uuid> --stream-id 550e8400-e29b-41d4-a716-446655440000 \
  --text "Hello, world!"
```

Mark the stream as done:

```bash
xyncra-client stream-text --user-id alice --device-id dev1 \
  -c <conv-uuid> --stream-id 550e8400-e29b-41d4-a716-446655440000 \
  --text "Hello, world! This is the final text." --done
```

Output:

```
Streaming done sent to conversation <conv-uuid>
```

### Error: Daemon Not Running

If the daemon is not running:

```
Error: daemon not running.
Hint: Start with 'xyncra-client listen --user-id <user>'
```

Exit code: `2` (D-042 -- precondition not met).

## Output Format

**Success (stdout):**

Sending a chunk:

```
Streaming text sent to conversation <conv-uuid>
```

Marking as done:

```
Streaming done sent to conversation <conv-uuid>
```

**Failure (stderr):**

```
Error: stream-text: <error message>
```

Exit code: `1`.

## Notes

- **Fire-and-forget** (D-051): Streaming text chunks are ephemeral with `seq=0`. They are broadcast to conversation peers via the daemon's WebSocket but are NOT persisted to any database. If delivery fails, no retry is attempted. The final message should be sent via `xyncra-client send` for persistence.
- **Cumulative text**: The `--text` flag contains the full text accumulated so far (not a delta). Each chunk replaces the previous one on the receiver side. This allows receivers to render the latest state without tracking individual deltas.
- **Stream ID**: The `--stream-id` is a client-generated UUID that groups chunks belonging to the same streaming session. All chunks of a single stream must use the same `--stream-id`. The final `--done` chunk must also use the same `--stream-id`.
- **IPC-only** (D-036): The daemon is required because streaming chunks must be sent over the persistent WebSocket connection with low latency.
- **Typical flow**:
  1. Generate a UUID for `--stream-id`
  2. Send chunks as text accumulates (e.g., from an LLM token stream)
  3. Send the final chunk with `--done` to signal completion
  4. Optionally follow up with `xyncra-client send` to persist the final message
- **Installation**: The CLI is installed via npm (`@xyncra/client-cli`). Requires Node.js 20+.
