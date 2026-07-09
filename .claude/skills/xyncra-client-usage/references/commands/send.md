# send

> Send a message to a conversation.

## Execution Mode

IPC+WS fallback (D-032):
1. **Primary**: Connect to the running `listen` daemon via Unix Socket IPC and forward the send request.
2. **Fallback**: If IPC fails (daemon not running), establish a standalone WebSocket short connection directly to the server.

Timeout: 15 seconds for both modes.

## Usage

```bash
xyncra-client send [flags]
```

## Flags

| Flag | Short | Type | Default | Required | Description |
|------|-------|------|---------|----------|-------------|
| `--conversation-id` | `-c` | string | `""` | Yes | Conversation ID (required) |
| `--content` | `-m` | string | `""` | Yes | Message content (required) |
| `--reply-to` | | uint32 | `0` | No | Message sequence number to reply to |

## Examples

Send a text message:

```bash
xyncra-client send --user-id alice -c <conv-uuid> -m "Hello, world!"
```

Send a reply to a specific message:

```bash
xyncra-client send --user-id alice -c <conv-uuid> -m "I agree" --reply-to 42
```

Send via environment variables (no flags):

```bash
export XYNCRA_USER_ID=alice
xyncra-client send -c <conv-uuid> -m "Hello!"
```

### Error / Both Modes Fail

```
Error: Cannot send message.
  Cause 1: dial unix /Users/alice/.xyncra/alice/abc12345/xyncra.sock: connect: connection refused
  Cause 2: dial tcp 127.0.0.1:8080: connect: connection refused
Hint: Start the daemon first: xyncra-client listen --user-id alice
```

## Output Format

**Success (stdout):**

```
Message sent.
  Message ID: 42
  Conversation: <conv-uuid>
  Client Msg ID: 550e8400-e29b-41d4-a716-446655440000
  Duplicate: false
```

`Duplicate: true` indicates the message was already sent (idempotency hit via `client_message_id`, D-006).

**Failure (stderr):** See error format above. Exit code: `1`.

## Notes

- **Idempotency** (D-006): A `client_message_id` (UUID v4) is automatically generated for each send. If the same `client_message_id` is detected by the server (e.g., due to retry), it returns the previously persisted message with `duplicate=true` instead of creating a new record.
- **IPC+WS fallback** (D-032): The daemon path is preferred because it reuses the persistent WebSocket connection. The fallback path creates a temporary connection, which is slower and does not receive real-time updates.
- **`--reply-to`** is a uint32 message sequence number (D-038), matching the `MessageID` field (not the string UUID `ID` field).
