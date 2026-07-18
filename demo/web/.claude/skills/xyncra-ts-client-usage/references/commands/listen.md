# listen / kill

> Start and manage the message update listener daemon.

`listen` starts a long-running daemon that connects to the server via WebSocket, receives real-time updates, and exposes an IPC (Unix Socket) server for other CLI commands. `kill` terminates the running daemon.

## Execution Mode

- **listen**: Daemon process (acquires process lock, opens IndexedDB, starts IPC server + WebSocket client, auto-registers built-in functions)
- **kill**: OS-level process management (reads lock file, sends signals)

---

## listen

### Usage

```bash
xyncra-client listen [flags]
```

### Flags

No command-specific flags. Uses global persistent flags only (`--user-id`, `--device-id`, `--server`, `--db-path`, `--log-dir`).

### Examples

Start the daemon for user `alice`:

```bash
xyncra-client listen --user-id alice --device-id dev1
```

Start with custom server and device:

```bash
xyncra-client listen --user-id alice --device-id dev1 --server ws://10.0.0.1:8080/ws --device-id mydevice
```

Start with custom device metadata (D-115):

```bash
xyncra-client listen --user-id alice --device-id dev1 --device-info '{"name":"MacBook","os":"darwin","app_version":"1.2.3"}'
```

### Output Format

**stderr** (daemon status):

```
[xyncra] Starting listener daemon...
[xyncra] Device: abc12345
[xyncra] Connecting to ws://localhost:8080/ws?user_id=alice ...
[xyncra] IPC server listening at /Users/alice/.xyncra/alice/abc12345/xyncra.sock
[xyncra] Registered built-in functions: ping, get_device_info, get_time
[xyncra] Listening for updates... (Ctrl+C to stop)
```

**stdout** (update events):

```
[new message] seq=42 from=bob conv=<conv-uuid> "Hello!"
[delete message] conv=<conv-uuid> msg=<msg-uuid>
[mark read] conv=<conv-uuid> msg_id=10
[conversation] id=<conv-uuid> title="Chat with Bob"
[gap] seq=99
```

### HITL Event Output (D-085, D-125)

When an Agent triggers the Human-in-the-Loop flow, the CLI daemon's `OnConversation` handler detects `agent_status == "asking_user"` and displays checkpoint_id, interrupt_id, and question_text in `[hitl]` format:

```
[hitl] conv=<conv-uuid> agent=agent/hitl-bot checkpoint_id=<uuid>
  [1] interrupt_id=<uuid> question="Execute this operation?" (pending)
[agent_status] agent=agent/hitl-bot conv=<conv-uuid> status=thinking
[agent_timeout] agent=agent/hitl-bot conv=<conv-uuid> reason="agent execution timed out"
```

| Event | Description | Key Fields |
|-------|-------------|------------|
| `[hitl]` | HITL notification (via `conversation update`, D-118/D-124/D-125) | `checkpoint_id`, `interrupt_id`, `question` |
| `agent_status` | Agent status change | `status` (thinking/tool_calling/generating/idle/asking_user) |
| `agent_timeout` | Agent execution timeout | `reason` |

> After receiving `[hitl]` output, use `xyncra-client agent-resume` to reply with user input. See [agent-resume](./agent-resume.md).

**How it works** (D-125):

- HITL notifications are carried entirely by `conversation update` (Pull-on-Notification model, D-118/D-124)
- When a conversation update arrives and `agent_status == "asking_user"`, the daemon calls `get_conversation` RPC to fetch HITL questions
- Questions are displayed in `[hitl]` format on stdout, no longer relying on redundant `agent_question` / `agent_checkpoint_created` ephemeral events

### Built-in Functions Auto-Registration (D-115)

On startup, the daemon automatically registers the following built-in functions via `system.register_functions` RPC. No separate registration command is needed.

| Function | Description | Parameters |
|----------|-------------|------------|
| `ping` | Echo test for ReverseRPC channel | `message` (string, optional) |
| `get_device_info` | Device info (hostname, OS, arch, pid) | none |
| `get_time` | Current device time (UTC, unix, timezone) | none |

These functions can be invoked by the server/agent through ReverseRPC.

### Daemon Lifecycle

1. **Lock acquisition** (D-031): Acquires `fs-ext` lock on `~/.xyncra/{user_id}/{device_id}/xyncra.lock`. Detects stale locks by checking if the PID in the lock file is alive; auto-cleans if the process is dead.
2. **Database open**: Opens IndexedDB (via `fake-indexeddb`) with database name from `--db-path` (TS-D-012). The `--db-path` value is used as the Dexie database name, not a file path.
3. **IPC server start**: Creates Unix Socket at `~/.xyncra/{user_id}/{device_id}/xyncra.sock` with permissions `0600` (D-030).
4. **WebSocket connect**: Connects to server with `?user_id=<user_id>`, starts heartbeat (30s interval) and reconnection polling (1s interval).
5. **Built-in function registration** (D-115): Automatically registers `ping`, `get_device_info`, `get_time` via `system.register_functions` RPC. Attaches `--device-info` metadata if provided.
6. **Initial sync**: Automatically pulls offline Updates (FullSync) on startup.
7. **Signal handling**: Blocks on SIGINT/SIGTERM. On signal, gracefully shuts down (close WebSocket, close DB, release lock).
8. **Log auto-cleanup** (D-040): Periodic cleanup of old RPC/notification logs at 1-hour intervals.

### Lock Conflict Error

If another `listen` process is already running for the same (user_id, device_id):

```
listen: listen already running (PID: 12345)
```

Exit code: `2` (D-042).

### Error Messages (stderr)

| Error | Cause | Exit Code |
|-------|-------|-----------|
| `listen: <error>` | Context creation failed | 1 |
| `listen: listen already running (PID: <n>)` | Process already running (D-031) | 2 |
| `listen: open db: <error>` | IndexedDB open failed | 1 |
| `listen: create client: <error>` | Client creation failed | 1 |
| `listen: start ipc server: <error>` | IPC server start failed | 1 |

### Exit Codes (D-042)

| Code | Meaning |
|------|---------|
| `0` | Normal exit (signal received, graceful shutdown) |
| `2` | Lock conflict (daemon already running) |

---

## kill

### Usage

```bash
xyncra-client kill [flags]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | `false` | Force kill with SIGKILL instead of SIGTERM |
| `--timeout` | duration | `5s` | Timeout to wait for process to exit |

### Examples

Gracefully stop the daemon:

```bash
xyncra-client kill
```

Force kill if daemon does not respond:

```bash
xyncra-client kill --force
```

Wait 10 seconds before giving up:

```bash
xyncra-client kill --timeout 10s
```

### Output Format (stderr)

**No daemon running:**

```
No running daemon found.
```

**Stale lock (process dead, lock file remains):**

```
Daemon process (PID: 12345) is not running. Cleaning up stale files.
```

**Successful termination:**

```
Daemon terminated (PID: 12345). Files cleaned up.
```

**Timeout (SIGTERM not responded):**

```
Error: process did not exit within 5s. Use --force to force kill
```

### Kill Behavior (D-039)

1. Read PID from lock file (`~/.xyncra/{user_id}/{device_id}/xyncra.lock`)
2. Check if process is alive (`process.kill(pid, 0)`)
3. If dead: clean up stale lock file + socket file, report
4. If alive: send SIGTERM, poll for exit (200ms interval)
5. If `--timeout` expires and process still running: exit code 3 (unless `--force`, then send SIGKILL)
6. After process exits: remove lock file and socket file

### Exit Codes (D-039, D-042)

| Code | Meaning |
|------|---------|
| `0` | Successfully terminated (or process already gone) |
| `1` | General error |
| `3` | Timeout: SIGTERM not responded, `--force` not used |

---

## Notes

- **Lock scope** (D-031): Lock is per (user_id, device_id). Different user or device combinations can run `listen` simultaneously without conflict.
- **Lock mechanism**: Uses `fs-ext` (Node.js native addon) instead of Go's `syscall.Flock`. Same semantics: exclusive file lock, per-directory scope.
- **stderr vs stdout** (D-041): Daemon status messages go to stderr; update events go to stdout. This allows `xyncra-client listen 2>/dev/null` to see only events.
- **Device ID** (D-033): Default device ID is derived from hostname SHA256 hash (first 8 hex chars), ensuring deterministic but anonymous identification.
- **Environment variables** (D-034): `XYNCRA_USER_ID`, `XYNCRA_DEVICE_ID`, `XYNCRA_SERVER`, `XYNCRA_DB_PATH`, `XYNCRA_LOG_DIR` can substitute for flags.
- **Debug mode**: Set `XYNCRA_DEBUG=1` or `XYNCRA_DEBUG=true` for verbose logging.
- **IndexedDB persistence** (TS-D-012): In Node.js, `fake-indexeddb` is used as an in-memory IndexedDB polyfill. Data is NOT persisted to disk across daemon restarts. On each startup, a FullSync is performed to rebuild local state from the server.
- **Built-in functions** (D-115): The daemon auto-registers `ping`, `get_device_info`, and `get_time` on startup. The old `register-functions` subcommand is no longer needed.
