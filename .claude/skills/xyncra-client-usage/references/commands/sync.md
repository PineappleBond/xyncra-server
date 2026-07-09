# sync-updates

> Trigger a full sync of updates from the server via the daemon.

## WARNING: Requires Running Daemon (D-036)

**This command REQUIRES the `listen` daemon to be running.** It communicates exclusively via IPC (Unix Socket) -- there is **no WebSocket fallback**.

This is an intentional exception to D-032 (IPC+WS fallback). The daemon manages sync state (`localMaxSeq`, WebSocket connection, deduplication via `NotificationLog`). A standalone WebSocket connection would race with the daemon's sync state and cause data corruption.

## Execution Mode

IPC-only (D-036): Connects to the running `listen` daemon via Unix Socket IPC only. No standalone fallback.

Timeout: 30 seconds.

## Usage

```bash
xyncra-client sync-updates [flags]
```

## Flags

No command-specific flags. Uses global persistent flags only.

## Examples

Trigger a sync while the daemon is running:

```bash
xyncra-client sync-updates --user-id alice
```

### Error: Daemon Not Running

If the daemon is not running:

```
Error: daemon not running.
Hint: Start with 'xyncra-client listen --user-id alice'
```

Exit code: `2` (D-042 -- precondition not met).

### Error: IPC Timeout

If the daemon is running but the sync takes longer than 30 seconds:

```
Error: <timeout_error>
```

Exit code: `1`.

## Output Format

**Success (stdout):**

```
Sync complete.
```

The daemon performs a FullSync cycle: paginated pull of all updates from the server (starting from `after_seq=0` or the current `localMaxSeq`), followed by the ApplyUpdate chain (save messages, update conversations, update read cursors, handle deletions).

## Notes

- **Why no fallback?** (D-036): The daemon holds `localMaxSeq` and a deduplication state (NotificationLog). An independent WebSocket calling `sync_updates` RPC directly would conflict with the daemon's SQLite writes and potentially cause duplicate update processing.
- **Typical use case**: Run this when you suspect the daemon missed some updates (e.g., after a network interruption that the daemon's automatic reconnect did not recover from).
- **Daemon auto-syncs**: The daemon automatically syncs on startup and receives real-time updates via WebSocket. Manual `sync-updates` is only needed for recovery scenarios.
- **See also**: `listen` command for starting the daemon.
