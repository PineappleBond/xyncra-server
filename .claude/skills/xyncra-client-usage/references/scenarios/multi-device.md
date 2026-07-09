# Multi-Device Scenarios

This document covers how `xyncra-client` handles multiple devices, instances, and data isolation.

---

## Device ID Model (D-033)

Each `xyncra-client` instance is identified by a `(user_id, device_id)` pair. The device ID determines data isolation boundaries.

### Default Device ID Generation

When `--device-id` is not specified, the client computes a device ID from the machine's hostname:

```
device_id = SHA256(hostname)[:8]   (first 8 hex characters)
```

This provides:
- **Anonymity**: The real hostname is never exposed (D-033)
- **Determinism**: The same machine always produces the same device ID
- **Sufficient uniqueness**: 32-bit hex space (4 billion values) is more than enough for device identification

### Example

```bash
# On a machine with hostname "macbook-pro.local"
./xyncra-client listen --user-id alice
# Device ID: sha256("macbook-pro.local")[:8] = "a1b2c3d4"

# Verify the device ID in the daemon output
# [xyncra] Device: a1b2c3d4
```

### Manual Override

You can explicitly set the device ID with `--device-id` or the `XYNCRA_DEVICE_ID` environment variable (D-034):

```bash
./xyncra-client listen --user-id alice --device-id my-laptop
```

---

## Scenario 1: Same User, Two Different Machines

Alice uses her MacBook and Desktop PC with the same user ID. Each machine automatically gets a different device ID because their hostnames differ.

### Machine A: MacBook

```bash
# hostname: macbook-pro.local
./xyncra-client listen --user-id alice
# [xyncra] Device: a1b2c3d4
# [xyncra] IPC server listening at /Users/alice/.xyncra/alice/a1b2c3d4/xyncra.sock
```

Data directory: `~/.xyncra/alice/a1b2c3d4/`

### Machine B: Desktop

```bash
# hostname: desktop-pc.local
./xyncra-client listen --user-id alice
# [xyncra] Device: e5f6a7b8
# [xyncra] IPC server listening at /Users/alice/.xyncra/alice/e5f6a7b8/xyncra.sock
```

Data directory: `~/.xyncra/alice/e5f6a7b8/`

### Result

Both daemons connect to the server as `user_id=alice`. The server treats them as two separate connections for the same user. Each machine maintains:
- Its own WebSocket connection
- Its own SQLite database (`xyncra.db`)
- Its own IPC socket (`xyncra.sock`)
- Its own lock file (`xyncra.lock`)
- Its own log directory (`logs/`)

Updates from the server (via `UserUpdate`, D-028) are delivered to both devices. Each device independently tracks its own `localMaxSeq` and sync state.

---

## Scenario 2: Same Machine, Same User, Different Device IDs

You can run multiple instances on the same machine by specifying different device IDs manually.

### Instance 1

```bash
./xyncra-client listen --user-id alice --device-id device1
# [xyncra] Device: device1
# [xyncra] IPC server listening at /Users/alice/.xyncra/alice/device1/xyncra.sock
```

Data directory: `~/.xyncra/alice/device1/`

### Instance 2

```bash
./xyncra-client listen --user-id alice --device-id device2
# [xyncra] Device: device2
# [xyncra] IPC server listening at /Users/alice/.xyncra/alice/device2/xyncra.sock
```

Data directory: `~/.xyncra/alice/device2/`

### Result

Each instance has completely isolated data:

| Resource | Instance 1 | Instance 2 |
|----------|-----------|-----------|
| Directory | `~/.xyncra/alice/device1/` | `~/.xyncra/alice/device2/` |
| Database | `device1/xyncra.db` | `device2/xyncra.db` |
| Socket | `device1/xyncra.sock` | `device2/xyncra.sock` |
| Lock | `device1/xyncra.lock` | `device2/xyncra.lock` |
| Logs | `device1/logs/` | `device2/logs/` |

Querying from a specific instance:

```bash
# Read data from device1's local database (D-035)
./xyncra-client list-conversations --user-id alice --device-id device1

# Read data from device2's local database
./xyncra-client list-conversations --user-id alice --device-id device2
```

---

## Scenario 3: Parallel Multi-Instance Operation

Two daemons running simultaneously with different device IDs maintain independent WebSocket connections to the server.

### Setup

```bash
# Terminal 1
./xyncra-client listen --user-id alice --device-id work
# [xyncra] Device: work
# [xyncra] Connecting to ws://localhost:8080/ws?user_id=alice ...
# [xyncra] Listening for updates... (Ctrl+C to stop)

# Terminal 2
./xyncra-client listen --user-id alice --device-id personal
# [xyncra] Device: personal
# [xyncra] Connecting to ws://localhost:8080/ws?user_id=alice ...
# [xyncra] Listening for updates... (Ctrl+C to stop)
```

### Server-Side Behavior

The server sees two WebSocket connections from `user_id=alice`:
- Connection 1: `device=work`
- Connection 2: `device=personal`

Both receive `UserUpdate` pushes (D-028). Each daemon independently processes updates and persists to its own SQLite database.

### Sending from Different Instances

```bash
# Send via the "work" daemon (IPC, D-030)
./xyncra-client send --user-id alice --device-id work -c 550e8400 -m "Work message"

# Send via the "personal" daemon (IPC, D-030)
./xyncra-client send --user-id alice --device-id personal -c 550e8400 -m "Personal message"
```

Both messages are sent as `alice` to the same conversation. The server assigns different `client_message_id` UUIDs (D-006) for each, ensuring idempotency.

### Lock Isolation (D-031)

The process lock is per `(user_id, device_id)`. Attempting to start a second daemon with the same device ID fails:

```bash
# Terminal 1
./xyncra-client listen --user-id alice --device-id work
# Running normally

# Terminal 3 (same device ID)
./xyncra-client listen --user-id alice --device-id work
# Error: listen already running (PID: 12345)
# Exit code: 2
```

But different device IDs succeed in parallel:

```bash
# Terminal 1
./xyncra-client listen --user-id alice --device-id work
# Running normally

# Terminal 2 (different device ID)
./xyncra-client listen --user-id alice --device-id personal
# Also running normally -- no lock conflict
```

---

## Data Isolation Details

### Directory Structure

```
~/.xyncra/
  alice/
    a1b2c3d4/              # Device 1 (auto-generated from hostname)
      xyncra.db            # SQLite database (WAL mode)
      xyncra.sock          # Unix domain socket (IPC, D-030)
      xyncra.lock          # Process lock file (D-031)
      logs/                # Log directory
    e5f6a7b8/              # Device 2 (different machine)
      xyncra.db
      xyncra.sock
      xyncra.lock
      logs/
    work/                  # Device 3 (manual ID)
      xyncra.db
      xyncra.sock
      xyncra.lock
      logs/
    personal/              # Device 4 (manual ID)
      xyncra.db
      xyncra.sock
      xyncra.lock
      logs/
  bob/
    c3d4e5f6/              # Bob's device on his machine
      xyncra.db
      xyncra.sock
      xyncra.lock
      logs/
```

### File Permissions

- Directories: `0700` (owner-only access)
- Socket files: `0600` (owner-only read/write)
- Lock files: managed by `flock` (kernel-level, D-031)

### Database Isolation

Each instance's `xyncra.db` is a separate SQLite database with WAL mode enabled. This allows:
- Concurrent reads from query commands (D-035) while the daemon writes
- `busy_timeout(5000)` to handle brief write locks

### What Is Shared vs. Isolated

| Data | Shared Across Devices | Isolated Per Device |
|------|:---:|:---:|
| Messages (on server) | Yes | |
| Conversations (on server) | Yes | |
| UserUpdates (on server) | Yes | |
| Local SQLite database | | Yes |
| IPC socket | | Yes |
| Process lock | | Yes |
| Log files | | Yes |
| Drafts (local) | | Yes |
| Read cursor (`localMaxSeq`) | | Yes |

---

## Managing Multiple Daemons

### List Running Daemons

Each daemon writes its PID to the lock file. Check which daemons are running:

```bash
# Check specific device
ls -la ~/.xyncra/alice/*/xyncra.lock

# View PID from lock file
cat ~/.xyncra/alice/work/xyncra.lock
```

### Stop Specific Daemons

```bash
# Stop the "work" daemon
./xyncra-client kill --user-id alice --device-id work

# Stop the "personal" daemon
./xyncra-client kill --user-id alice --device-id personal
```

### Stop All Daemons for a User

```bash
for device_dir in ~/.xyncra/alice/*/; do
  device_id=$(basename "$device_dir")
  ./xyncra-client kill --user-id alice --device-id "$device_id" 2>/dev/null
done
```

---

## Related Documentation

- [Basic Usage](./basic-usage.md) -- first-time setup and daily workflows
- [Offline Sync](./offline-sync.md) -- how sync works across devices
- [Error Handling](./error-handling.md) -- lock conflicts and exit codes
- [Advanced Usage](./advanced.md) -- environment variables and custom paths
