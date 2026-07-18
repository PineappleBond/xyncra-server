# set-typing

> Send a typing indicator to a conversation.

## Execution Mode

IPC-only (D-036, D-050): Connects to the running `listen` daemon via Unix Socket IPC. No WebSocket fallback.

Typing indicators are fire-and-forget ephemeral messages (D-050): they are not persisted (seq=0), not retried, and not acknowledged by the server. If the daemon is not running there is nothing to broadcast, so standalone fallback is pointless.

## Usage

```bash
xyncra-client set-typing [flags]
```

## Flags

| Flag | Short | Type | Default | Required | Description |
|------|-------|------|---------|----------|-------------|
| `--conversation-id` | `-c` | string | `""` | Yes | Conversation ID (required) |
| `--stop` | | bool | `false` | No | Stop typing (default: start typing) |

## Examples

Start typing in a conversation:

```bash
xyncra-client set-typing --user-id alice --device-id dev1 -c <conv-uuid>
```

Output:

```
Typing indicator sent to conversation <conv-uuid>
```

Stop typing (e.g., when the user clears the input field):

```bash
xyncra-client set-typing --user-id alice --device-id dev1 -c <conv-uuid> --stop
```

Output:

```
Typing indicator cleared for conversation <conv-uuid>
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

Start typing:

```
Typing indicator sent to conversation <conv-uuid>
```

Stop typing:

```
Typing indicator cleared for conversation <conv-uuid>
```

**Failure (stderr):**

```
Error: set-typing: <error message>
```

Exit code: `1`.

## Notes

- **Fire-and-forget** (D-050): Typing indicators are ephemeral messages with `seq=0`. They are broadcast to the conversation peers via the daemon's WebSocket but are NOT persisted to any database (neither server nor client). If delivery fails, no retry is attempted.
- **IPC-only** (D-036): The daemon is required because typing indicators must be sent over the persistent WebSocket connection. A standalone WebSocket fallback would add latency for a UI-presence signal that must be fast.
- **Typical usage**: Call `set-typing` when the user starts typing in the input box; call `set-typing --stop` when the user clears the input, sends the message, or navigates away.
- **Installation**: The CLI is installed via npm (`@xyncra/client-cli`). Requires Node.js 20+.
