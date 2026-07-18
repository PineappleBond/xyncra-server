# reload-agents

> Hot-reload Agent configuration in the running daemon.

## Execution Mode

IPC-only (D-076, D-036): Connects to the running `listen` daemon via Unix Socket IPC. No WebSocket fallback. The daemon reloads Agent configuration from the server.

## Usage

```bash
xyncra-client reload-agents [flags]
```

## Flags

This command has no command-level flags. Global flags (`--user-id`, `--device-id`, etc.) apply.

## Examples

Reload Agent configuration:

```bash
xyncra-client reload-agents --user-id alice --device-id dev1
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

```
Agents reloaded. N agent(s) loaded.
```

**Failure (stderr):**

```
Error: reload-agents: <error message>
```

Exit code: `1`.

## Notes

- **IPC-only** (D-076, D-036): The daemon must be running. No WebSocket fallback.
- **Hot-reload**: Reloads Agent configuration without restarting the daemon. Useful after adding or modifying Agent definitions on the server.
- **Installation**: The CLI is installed via npm (`@xyncra/client-cli`). Requires Node.js 20+.
