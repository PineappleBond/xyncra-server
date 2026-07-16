# Error Handling Scenarios

This document covers exit codes, common error patterns, and how to handle errors in scripts.

---

## Exit Code Reference (D-042)

| Exit Code | Meaning | Example Scenarios |
|:---------:|---------|-------------------|
| `0` | Success | Command executed successfully |
| `1` | General error | RPC failure, invalid parameters, database error, network error |
| `2` | Prerequisite not met | Lock conflict (D-031), daemon not running (D-036) |
| `3` | Timeout (kill only) | SIGTERM not responded within `--timeout` (D-039) |

---

## Common Error Patterns

### Error 1: All Delivery Methods Failed

**Cause**: Both IPC (Unix Socket) and WebSocket connections failed (D-032).

**Error Message**:

```
Error: Cannot send message.
  Cause 1: dial unix /Users/alice/.xyncra/alice/a1b2c3d4/xyncra.sock: connect: connection refused
  Cause 2: dial tcp 127.0.0.1:8080: connect: connection refused
Hint: Start the daemon first: xyncra-client listen --user-id alice --device-id dev1
```

**Exit Code**: `1`

**Diagnosis**:
- `Cause 1` (IPC) failed: The daemon is not running, or the socket file does not exist
- `Cause 2` (WebSocket) failed: The server is not running or not reachable at `127.0.0.1:8080`

**Resolution**:

```bash
# Step 1: Start the daemon (fixes Cause 1)
./xyncra-client listen --user-id alice --device-id dev1 &

# Step 2: If you also need server connectivity, start the server (fixes Cause 2)
./xyncra-server &

# Step 3: Retry the command
./xyncra-client send --user-id alice --device-id dev1 -c 550e8400 -m "Hello"
```

> The dual-mode failure report always shows both causes and a hint. The format is (D-032):
> ```
> IPC failed: <ipc_error>
> WebSocket failed: <ws_error>
> Hint: <suggestion>
> ```

---

### Error 2: Listen Already Running (Lock Conflict)

**Cause**: Another daemon with the same `(user_id, device_id)` is already running. The process lock (D-031) prevents duplicate daemons.

**Error Message**:

```
Error: listen already running (PID: 12345)
```

**Exit Code**: `2`

**Diagnosis**:
- The lock file `~/.xyncra/{user_id}/{device_id}/xyncra.lock` is held by PID 12345
- The daemon is actively running, or the lock is stale (process died without cleanup)

**Resolution**:

Option A -- Use the existing daemon:

```bash
# The daemon is already running, just send commands through it
./xyncra-client send --user-id alice --device-id dev1 -c 550e8400 -m "Hello"
```

Option B -- Stop the existing daemon and restart:

```bash
# Graceful stop
./xyncra-client kill --user-id alice --device-id dev1

# Restart
./xyncra-client listen --user-id alice --device-id dev1
```

Option C -- Force kill (if daemon is unresponsive):

```bash
./xyncra-client kill --user-id alice --device-id dev1 --force
./xyncra-client listen --user-id alice --device-id dev1
```

### Stale Lock Detection

If the process from the lock file is no longer alive, the daemon detects the stale lock and automatically cleans up:

```bash
# Process 12345 has already exited, but lock file remains
./xyncra-client listen --user-id alice --device-id dev1
# [xyncra] Stale lock detected (PID: 12345, process not running). Cleaning up.
# [xyncra] Starting listener daemon...
```

---

### Error 3: Daemon Not Running (sync-updates)

**Cause**: The `sync-updates` command requires a running daemon. It is IPC-only with no WebSocket fallback (D-036).

**Error Message**:

```
Error: daemon not running.
Hint: Start with 'xyncra-client listen --user-id alice --device-id dev1'
```

**Exit Code**: `2`

**Diagnosis**:
- The daemon is not running for this `(user_id, device_id)` combination
- The IPC socket file does not exist

**Resolution**:

```bash
# Start the daemon first
./xyncra-client listen --user-id alice --device-id dev1 &

# Wait for it to connect, then sync
sleep 2
./xyncra-client sync-updates --user-id alice --device-id dev1
```

> Unlike other commands (send, create-conversation, etc.), `sync-updates` does NOT fall back to a standalone WebSocket connection. This is intentional (D-036) to avoid state conflicts with the daemon's sync manager.

---

### Error 4: Missing User ID

**Cause**: Neither `--user-id` flag nor `XYNCRA_USER_ID` environment variable is set.

**Error Message**:

```
Error: user-id is required
```

**Exit Code**: `1`

**Resolution**:

Option A -- Provide the flag:

```bash
./xyncra-client send --user-id alice --device-id dev1 -c 550e8400 -m "Hello"
```

Option B -- Set the environment variable (D-034):

```bash
export XYNCRA_USER_ID=alice
./xyncra-client send -c 550e8400 -m "Hello"
```

---

### Error 5: Conversation Not Found (Local DB)

**Cause**: The query command reads from the local SQLite database (D-035), but the conversation has not been synced yet.

**Error Message**:

```
Error: get-conversation: conversation 550e8400 not found
```

**Exit Code**: `1`

**Diagnosis**:
- The conversation exists on the server but has not been synced to the local database
- The conversation ID is incorrect

**Resolution**:

```bash
# Ensure the daemon is running and has synced
./xyncra-client sync-updates --user-id alice --device-id dev1

# Then retry
./xyncra-client get-conversation --user-id alice --device-id dev1 -c 550e8400
```

> Query commands (list-conversations, get-conversation, get-messages, search-messages) always read from the local database. If data is missing, it means the daemon has not yet synced it.

---

### Error 6: Kill Command Timeout

**Cause**: The daemon did not respond to SIGTERM within the specified timeout (D-039).

**Error Message**:

```
Error: process did not exit within 5s. Use --force to force kill
```

**Exit Code**: `3`

**Resolution**:

```bash
# Use --force to send SIGKILL
./xyncra-client kill --user-id alice --device-id dev1 --force

# Or increase the timeout
./xyncra-client kill --user-id alice --device-id dev1 --timeout 10s
```

---

### Error 7: agent-resume — Daemon Not Running

**Cause**: `agent-resume` 是 IPC-only 命令（D-114），必须连接运行中的 daemon。无 WebSocket fallback。

**Error Message**:

```
错误：守护进程未运行，请先启动 xyncra-client listen
```

**Exit Code**: `2`

**Diagnosis**:
- daemon 未启动，IPC socket 文件不存在
- `agent-resume` 不像 `send` 那样有 WebSocket fallback（D-036, D-114）

**Resolution**:

```bash
# 先启动 daemon
./xyncra-client listen --user-id alice --device-id dev1 &

# 等待 daemon 就绪
sleep 2

# 再执行 agent-resume
./xyncra-client agent-resume \
  --conversation-id <conv-uuid> \
  --checkpoint-id cp-123 \
  --answer "确认" \
  --agent-id agent/hitl-bot
```

> 注意：如果 daemon 未运行时 Agent 触发了 HITL，checkpoint 可能已经过期（24h TTL）。需要先重新发送消息触发新的 Agent 执行。

---

### Error 8: agent-resume — Checkpoint Expired

**Cause**: Agent 的 checkpoint 有 24 小时 TTL，超过后 checkpoint 失效，无法恢复。

**Error Message**:

```
Error: agent-resume failed: checkpoint expired
Hint: Checkpoint TTL is 24h. Please resend the message to trigger a new HITL.
```

**Exit Code**: `1`

**Diagnosis**:
- 从收到 `[hitl]` 通知到执行 `agent-resume` 之间超过了 24 小时
- daemon 在此期间重启过，内存中的 checkpoint 数据丢失

**Resolution**:

```bash
# 重新发送消息触发新的 Agent 执行
./xyncra-client send --user-id alice --device-id dev1 \
  -c <conv-uuid> --agent-id agent/hitl-bot --content "帮我查天气"

# 等待新的 [hitl] 通知
# 然后使用新的 checkpoint_id 和 interrupt_id 进行 resume
./xyncra-client agent-resume \
  --conversation-id <conv-uuid> \
  --checkpoint-id <new-checkpoint-id> \
  --interrupt-id <new-interrupt-id> \
  --answer "北京" \
  --agent-id agent/hitl-bot
```

---

### Error 9: agent-resume — Interrupt ID 不匹配

**Cause**: 使用了过时的 `interrupt_id`。多轮 HITL 中，每次 `[hitl]` 通知都会生成新的 `interrupt_id`。

**Error Message**:

```
Error: agent-resume failed: interrupt not found
Hint: Use the interrupt_id from the latest [hitl] output.
```

**Exit Code**: `1`

**Diagnosis**:
- 在多轮 HITL 中，使用了上一轮的 `interrupt_id`
- daemon 重启后，旧的 interrupt 记录已丢失

**Resolution**:

```bash
# 查看 listen 输出，找到最新的 [hitl] 通知
# [hitl] conv=<conv-uuid> agent=agent/hitl-bot checkpoint_id=cp-new
#   [1] interrupt_id=int-new question="..." (pending)

# 使用最新的 ID
./xyncra-client agent-resume \
  --conversation-id <conv-uuid> \
  --checkpoint-id cp-new \
  --interrupt-id int-new \
  --answer "确认" \
  --agent-id agent/hitl-bot
```

> 如果省略 `--interrupt-id`，daemon 会从内存中自动查找最新的 interrupt（适用于单轮 HITL 场景）。

---

### Error 10: 多轮 HITL — Resume 后又触发新的 HITL

**Cause**: Agent resume 后继续执行，可能需要更多用户输入，再次触发 HITL 中断。

**现象**：

```bash
# 第一轮 resume 成功
Agent resumed.

# listen 输出中立即出现新的 [hitl] 通知
[agent_status] agent=agent/hitl-bot conv=<conv-uuid> status=thinking
[agent_status] agent=agent/hitl-bot conv=<conv-uuid> status=asking_user
[hitl] conv=<conv-uuid> agent=agent/hitl-bot checkpoint_id=cp-002
  [1] interrupt_id=int-002 question="需要包含空气质量信息吗？" (pending)
```

**这不是错误**。这是 Agent 的正常行为——在一次执行中多次请求用户输入。

**Resolution**:

继续使用 `agent-resume` 回复，使用新的 `checkpoint_id` 和 `interrupt_id`：

```bash
./xyncra-client agent-resume \
  --conversation-id <conv-uuid> \
  --checkpoint-id cp-002 \
  --interrupt-id int-002 \
  --answer "是的，请包含" \
  --agent-id agent/hitl-bot
```

循环此过程直到 Agent 输出 `[agent_status] status=idle`，表示执行完成。

> 提示：在脚本中，可以用循环监听 `[hitl]` 输出并自动回复，实现完全自动化的 HITL 流程。参见 [HITL Shell 脚本自动化](../scenarios/advanced.md#hitlhuman-in-the-loop完整流程)。

---

## Dual-Mode Failure Report Format

When a command that uses IPC+WS fallback (D-032) fails, the error always follows this format:

```
Error: Cannot <action>.
  Cause 1: <ipc_error>
  Cause 2: <ws_error>
Hint: Start the daemon first: xyncra-client listen --user-id <user> --device-id <device>
```

This applies to:
- `send`
- `create-conversation`
- `delete-conversation`
- `restore-conversation`
- `delete-message`
- `mark-as-read`

The two causes help you diagnose which subsystem failed:
- **Cause 1 (IPC)**: Usually "connection refused" (daemon not running) or a socket permission error
- **Cause 2 (WebSocket)**: Usually "connection refused" (server not running) or a network error

---

## Client Error Codes (D-027)

The client defines three additional error codes in the `-400` range:

| Code | Type | Description |
|:----:|------|-------------|
| `-400` | `ConnectionError` | WebSocket connection failed (network unreachable, server not started) |
| `-401` | `TimeoutError` | RPC call timed out (request sent but no response within timeout) |
| `-402` | `SyncError` | Incremental sync failed (gap in seq, sync_updates error) |

These errors appear in the daemon's log output and in RPC error responses. They follow the same `HandlerError` pattern as server errors (D-017).

---

## Handling Exit Codes in Scripts

### Basic Pattern

```bash
./xyncra-client send --user-id alice --device-id dev1 -c 550e8400 -m "Test message"
exit_code=$?

if [ $exit_code -eq 0 ]; then
  echo "Message sent successfully"
elif [ $exit_code -eq 2 ]; then
  echo "Prerequisite not met -- is the daemon running?"
  echo "Try: xyncra-client listen --user-id alice --device-id dev1"
else
  echo "Command failed (exit code: $exit_code)"
fi
```

### Retry with Backoff

```bash
max_retries=3
retry_delay=2

for i in $(seq 1 $max_retries); do
  ./xyncra-client send --user-id alice --device-id dev1 -c 550e8400 -m "Test message"
  exit_code=$?

  if [ $exit_code -eq 0 ]; then
    echo "Success"
    break
  elif [ $exit_code -eq 2 ]; then
    echo "Daemon not ready, retrying in ${retry_delay}s..."
    sleep $retry_delay
    retry_delay=$((retry_delay * 2))
  else
    echo "Failed with exit code $exit_code"
    break
  fi
done
```

### Conditional Sync

```bash
# Check if daemon is running before attempting sync
./xyncra-client sync-updates --user-id alice --device-id dev1 2>/dev/null
if [ $? -ne 0 ]; then
  echo "Daemon not running. Starting..."
  ./xyncra-client listen --user-id alice --device-id dev1 &
  sleep 3
  ./xyncra-client sync-updates --user-id alice --device-id dev1
fi
```

### Batch Send with Error Tracking

```bash
success=0
failed=0
skipped=0

for conv_id in "$@"; do
  ./xyncra-client send --user-id alice --device-id dev1 -c "$conv_id" -m "Broadcast" 2>/dev/null
  exit_code=$?

  case $exit_code in
    0) success=$((success + 1)) ;;
    2) skipped=$((skipped + 1)) ;;
    *) failed=$((failed + 1)) ;;
  esac
done

echo "Results: $success sent, $skipped skipped, $failed failed"
```

### Safe Kill and Restart

```bash
# Kill daemon (ignore "not running" errors)
./xyncra-client kill --user-id alice --device-id dev1 2>/dev/null

# Clean start
./xyncra-client listen --user-id alice --device-id dev1 &
daemon_pid=$!

# Wait for daemon to be ready
for i in $(seq 1 10); do
  if [ -S "$HOME/.xyncra/alice/$(hostname | shasum -a 256 | cut -c1-8)/xyncra.sock" ]; then
    echo "Daemon ready (PID: $daemon_pid)"
    break
  fi
  sleep 1
done
```

---

## JSON-RPC Error Codes

For debugging daemon IPC communication, these are the JSON-RPC error codes used internally:

| Code | Meaning |
|:----:|---------|
| `-32700` | Parse error (malformed JSON) |
| `-32600` | Invalid Request |
| `-32601` | Method not found |
| `-32602` | Invalid params (missing or wrong-type parameters) |
| `-32000` | Server error |
| `-300` | Generic server error |

These errors are typically seen in daemon logs when debug mode is enabled (`XYNCRA_DEBUG=1`).

---

## Related Documentation

- [Basic Usage](./basic-usage.md) -- common workflows
- [Offline Sync](./offline-sync.md) -- sync errors and recovery
- [Advanced Usage](./advanced.md) -- debug mode and environment variables
