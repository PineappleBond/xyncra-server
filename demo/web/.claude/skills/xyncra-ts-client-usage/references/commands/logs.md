# logs

> View and manage RPC and notification logs.

All log commands are forwarded to the running `listen` daemon via IPC. The daemon owns the IndexedDB database where logs are stored. **The daemon must be running.**

---

## logs tail

> Show recent log entries.

### Execution Mode

IPC-only (D-036): Connects to the running `listen` daemon via Unix Socket IPC.

### Usage

```bash
xyncra-client logs tail [flags]
```

### Flags

| Flag | Type | Default | Required | Description |
|------|------|---------|----------|-------------|
| `--type` | string | `"rpc"` | No | Log type: `rpc` or `notifications` |
| `--limit` | int | `50` | No | Maximum number of entries to show |
| `--since` | string | `"1h"` | No | Show entries since (e.g. `1h`, `30m`, `7d`) |

### Examples

Show recent RPC logs:

```bash
xyncra-client logs tail --user-id alice --device-id dev1
```

Show notification logs from the last 30 minutes:

```bash
xyncra-client logs tail --user-id alice --device-id dev1 --type notifications --since 30m --limit 20
```

### Error: Daemon Not Running

If the daemon is not running:

```
Error: daemon not running.
Hint: Start with 'xyncra-client listen --user-id alice --device-id dev1'
```

Exit code: `2` (D-042 -- precondition not met).

### Output Format (stdout)

**RPC logs:**

```
TIME                    METHOD                  STATUS      DURATION        CONVERSATION
----                    ------                  ------      --------        ------------
2026-07-09T12:34:56Z    send_message            0           1.234ms         550e8400-e29b-41d4-...
2026-07-09T12:34:55Z    heartbeat               0           0.123ms
2026-07-09T12:34:50Z    mark_as_read            0           2.345ms         550e8400-e29b-41d4-...
```

**Notification logs:**

```
TIME                    SEQ     TYPE
----                    ---     ----
2026-07-09T12:34:56Z    123     message
2026-07-09T12:34:55Z    122     mark_read
2026-07-09T12:34:50Z    121     conversation
```

---

## logs search

> Search log entries with filters.

### Execution Mode

IPC-only (D-036): Connects to the running `listen` daemon via Unix Socket IPC.

### Usage

```bash
xyncra-client logs search [flags]
```

### Flags

| Flag | Type | Default | Required | Description |
|------|------|---------|----------|-------------|
| `--type` | string | `"rpc"` | No | Log type: `rpc` or `notifications` |
| `--method` | string | `""` | No | Filter by RPC method |
| `--error` | bool | `false` | No | Show only error entries (status = -1) |
| `--from` | string | `""` | No | Start time (duration or RFC3339) |
| `--to` | string | `""` | No | End time (duration or RFC3339) |
| `--conversation-id` | string | `""` | No | Filter by conversation ID (RPC only) |
| `--request-id` | string | `""` | No | Get specific entry by request ID (RPC only) |
| `--limit` | int | `100` | No | Maximum number of entries to return |

### Examples

Search for failed RPC calls:

```bash
xyncra-client logs search --user-id alice --device-id dev1 --error
```

Search for a specific method in a time range:

```bash
xyncra-client logs search --user-id alice --device-id dev1 --method send_message --from 2h --to 30m
```

Look up a specific request by ID:

```bash
xyncra-client logs search --user-id alice --device-id dev1 --request-id 550e8400-e29b-41d4-a716-446655440000
```

### Output Format

Same as `logs tail` (column-aligned tables).

### Notes

- **--error**: Filters entries where status code is `-1` (error).
- **--request-id**: Returns a single RPC log entry matching the given request ID.
- **--conversation-id**: Only applicable to RPC logs. Filters by the conversation ID associated with the RPC call.

---

## logs stats

> Show RPC log statistics.

### Execution Mode

IPC-only (D-036): Connects to the running `listen` daemon via Unix Socket IPC.

### Usage

```bash
xyncra-client logs stats [flags]
```

### Flags

| Flag | Type | Default | Required | Description |
|------|------|---------|----------|-------------|
| `--since` | string | `"24h"` | No | Statistics time window (e.g. `1h`, `24h`, `7d`) |
| `--interval` | string | `""` | No | Group by interval: `1m`, `5m`, `15m`, `1h`, `1d` |

### Examples

Show stats for the last 24 hours:

```bash
xyncra-client logs stats --user-id alice --device-id dev1
```

Show stats grouped by 1-hour intervals:

```bash
xyncra-client logs stats --user-id alice --device-id dev1 --since 7d --interval 1h
```

### Output Format (stdout)

**Without --interval:**

```
METHOD                  COUNT       SUCCESS     ERRORS      AVG (ms)
------                  -----       -------     ------      --------
send_message            100         95          5           1.234
heartbeat               500         500         0           0.123
mark_as_read            50          48          2           2.345
```

**With --interval:**

```
INTERVAL                METHOD                  COUNT       SUCCESS     ERRORS      AVG (ms)
--------                ------                  -----       -------     ------      --------
2026-07-09T12:00:00Z    send_message            50          48          2           1.234
2026-07-09T12:00:00Z    heartbeat               250         250         0           0.123
2026-07-09T11:00:00Z    send_message            50          47          3           1.567
```

### Stats Interval

The `--interval` flag accepts **exactly 5 values** only:

| Value | Description |
|-------|-------------|
| `1m` | Group by 1 minute |
| `5m` | Group by 5 minutes |
| `15m` | Group by 15 minutes |
| `1h` | Group by 1 hour |
| `1d` | Group by 1 day |

Other values are rejected.

---

## logs export

> Export logs to CSV or JSON.

### Execution Mode

IPC-only (D-036): Connects to the running `listen` daemon via Unix Socket IPC.

### Usage

```bash
xyncra-client logs export [flags]
```

### Flags

| Flag | Short | Type | Default | Required | Description |
|------|-------|------|---------|----------|-------------|
| `--type` | | string | `"rpc"` | No | Log type: `rpc` or `notifications` |
| `--format` | | string | `"csv"` | No | Export format: `csv` or `json` |
| `--output` | `-o` | string | `""` | No | Output file path (default: stdout) |
| `--method` | | string | `""` | No | Filter by RPC method (RPC only) |
| `--from` | | string | `""` | No | Start time (duration or RFC3339) |
| `--to` | | string | `""` | No | End time (duration or RFC3339) |
| `--limit` | | int | `1000` | No | Maximum number of entries to export (max 10000) |

### Examples

Export RPC logs to CSV on stdout:

```bash
xyncra-client logs export --user-id alice --device-id dev1 > logs.csv
```

Export notification logs to JSON file:

```bash
xyncra-client logs export --user-id alice --device-id dev1 --type notifications --format json -o notifications.json
```

Export a specific method's logs for a time range:

```bash
xyncra-client logs export --user-id alice --device-id dev1 --method send_message --from 7d --format csv -o sends.csv
```

### Output

- **stdout** (default): Data written to standard output.
- **File** (`-o <path>`): Data written to file. When writing to a file, stderr shows `Exported to <path>`.

### Notes

- **Limit range**: `--limit` must be between 1 and 10000. Values outside this range are reset to 1000.
- **CSV and JSON formats** are implemented by the daemon's store layer.

---

## logs cleanup

> Delete old log entries.

### Execution Mode

IPC-only (D-036): The daemon performs the cleanup on its IndexedDB database.

### Usage

```bash
xyncra-client logs cleanup [flags]
```

### Flags

| Flag | Type | Default | Required | Description |
|------|------|---------|----------|-------------|
| `--retain` | string | `"168h"` | No | Retention duration (e.g. `168h`, `7d`) |
| `--dry-run` | bool | `false` | No | Show what would be deleted without deleting |
| `--type` | string | `"all"` | No | Log type to clean: `rpc`, `notifications`, or `all` |

### Examples

Preview what would be cleaned up (default 7-day retention):

```bash
xyncra-client logs cleanup --user-id alice --device-id dev1 --dry-run
```

Clean up logs older than 3 days:

```bash
xyncra-client logs cleanup --user-id alice --device-id dev1 --retain 72h
```

Clean up only RPC logs:

```bash
xyncra-client logs cleanup --user-id alice --device-id dev1 --type rpc --retain 168h
```

### Output Format (stdout)

**Dry run:**

```
Would delete 1234 log entries older than 2026-07-02 12:34:56
  RPC logs: 1000
  Notification logs: 234
```

**Actual deletion:**

```
Deleted 1234 log entries.
  RPC logs: 1000
  Notification logs: 234
```

### Notes

- **Default retention** (D-040): 7 days (168h). Both `rpcLogs` and `notificationLogs` IndexedDB tables are cleaned simultaneously.
- **--type options**: `rpc` (only RPC logs), `notifications` (only notification logs), `all` (both, default).
- **Note**: `cleanup` does NOT support RFC3339 timestamps for `--retain` -- only duration format (`168h`, `72h`) and days (`7d`, `3d`).

---

## Time Format

Multiple commands accept time arguments (`--since`, `--from`, `--to`, `--retain`). The parser supports three formats:

| Format | Examples | Supported By |
|--------|----------|--------------|
| Duration | `1h`, `30m`, `5s`, `168h` | tail, search, export, stats, cleanup |
| Days shorthand | `7d`, `30d`, `3d` | tail, search, export, stats, cleanup |
| RFC3339 | `2026-07-09T12:34:56Z` | tail, search, export, stats |

**Exception**: `logs cleanup --retain` does NOT support RFC3339. Only duration and days shorthand.

The time is resolved relative to `Date.now()`:
- `1h` means "1 hour ago from now"
- `7d` means "7 days ago from now" (equivalent to `168h`)
- `2026-07-09T12:34:56Z` means that exact timestamp

---

## Notes

- **IPC-only** (D-036): Unlike the Go client which reads SQLite directly, the TS client forwards all log operations to the daemon via IPC. The daemon owns the IndexedDB (Dexie.js) database.
- **Storage** (TS-D-012): Log data is stored in the IndexedDB `rpcLogs` and `notificationLogs` tables. The `--db-path` flag is redefined as an IndexedDB database name, not a file path.
- **Two log types**: `rpcLogs` (outgoing RPC calls with method, status, duration) and `notificationLogs` (incoming notifications with seq and type).
- **Auto-cleanup** (D-040): The `listen` daemon automatically runs log cleanup every 1 hour with the default 7-day retention. Manual `logs cleanup` is for ad-hoc maintenance.
- **Output format**: All tabular output uses auto-sized column alignment (equivalent to Go's `text/tabwriter`).
- **Installation**: The CLI is installed via npm (`@xyncra/client-cli`). Requires Node.js 20+.
