# agent-resume

> 恢复被 HITL（Human-In-The-Loop）中断的 Agent。

## 执行模式

IPC-only（D-036, D-114）：必须通过 Unix Socket IPC 连接运行中的 `listen` daemon。无 WebSocket fallback。

如果 daemon 未运行，直接报错：

```
错误：守护进程未运行，请先启动 xyncra-client listen
```

## Usage

```bash
xyncra-client agent-resume [flags]
```

## Flags

| Flag | Short | Type | Default | Required | Description |
|------|-------|------|---------|----------|-------------|
| `--conversation-id` | `-c` | string | `""` | Yes | 会话 ID |
| `--checkpoint-id` | | string | `""` | Yes | 来自 `[hitl]` 输出的 Checkpoint ID（通过 conversation update 获取，D-125） |
| `--interrupt-id` | | string | `""` | No | 来自 `[hitl]` 输出的 Interrupt ID（未提供时从内存查找） |
| `--answer` | | string | `""` | Yes | 对 Agent 问题的回答 |
| `--agent-id` | | string | `""` | Yes | Agent ID（如 `agent/hitl-bot`） |

## Examples

### 基本用法

在 listen 输出中收到 `[hitl]` 通知后，使用其中的 ID 恢复 Agent：

```bash
xyncra-client agent-resume \
  --conversation-id <conv-uuid> \
  --checkpoint-id cp-123 \
  --interrupt-id int-456 \
  --answer "北京" \
  --agent-id agent/hitl-bot
```

### 省略 interrupt-id

如果未提供 `--interrupt-id`，daemon 会从内存中查找最新的 interrupt：

```bash
xyncra-client agent-resume \
  --conversation-id <conv-uuid> \
  --checkpoint-id cp-123 \
  --answer "确认执行" \
  --agent-id agent/hitl-bot
```

## 完整 HITL 工作流

1. **启动守护进程监听**：

   ```bash
   xyncra-client listen --user-id alice --device-id dev1
   ```

2. **在另一个终端发送消息触发 Agent**：

   ```bash
   xyncra-client send --user-id alice --device-id dev1 \
     -c <conv-uuid> --agent-id agent/hitl-bot --content "帮我查天气"
   ```

3. **在 listen 输出中查看 [hitl] 通知**：

   ```
   [hitl] conv=<conv-uuid> agent=agent/hitl-bot checkpoint_id=cp-123
     [1] interrupt_id=int-456 question="请问您要查哪个城市？" (pending)
   ```

4. **使用 agent-resume 回复**：

   ```bash
   xyncra-client agent-resume \
     --conversation-id <conv-uuid> \
     --checkpoint-id cp-123 \
     --interrupt-id int-456 \
     --answer "北京" \
     --agent-id agent/hitl-bot
   ```

5. **Agent 继续执行**，listen 输出中会看到后续事件：

   ```
   [agent_status] agent=agent/hitl-bot conv=<conv-uuid> status=thinking
   [agent_status] agent=agent/hitl-bot conv=<conv-uuid> status=generating
   [new message] seq=43 from=agent/hitl-bot conv=<conv-uuid> "北京今天晴，气温 25°C"
   ```

## Output Format

**成功（stdout）**：

```
Agent resumed.
  Conversation: <conv-uuid>
  Checkpoint: cp-123
  Agent: agent/hitl-bot
```

**失败（stderr）**：

```
Error: agent-resume failed: checkpoint expired
Hint: Checkpoint TTL is 24h. Please resend the message to trigger a new HITL.
```

退出码：`1`。

## 错误场景

| 场景 | 错误信息 | 原因 | 解决方法 |
|------|---------|------|---------|
| daemon 未运行 | `错误：守护进程未运行，请先启动 xyncra-client listen` | daemon 未启动 | 先运行 `xyncra-client listen` |
| checkpoint 过期 | `agent-resume failed: checkpoint expired` | Checkpoint TTL（24h）已过期 | 重新发送消息触发新的 HITL |
| interrupt_id 不匹配 | `agent-resume failed: interrupt not found` | 使用了旧的 interrupt_id | 使用最新 `[hitl]` 输出中的 ID |
| 参数缺失 | `invalid params: missing required field 'checkpoint_id'` | 缺少必需参数 | 检查命令参数是否完整 |

## 相关决策

- D-036: 部分 CLI 命令为 IPC-only
- D-085: agent_resume RPC 规范
- D-114: agent-resume 为 IPC-only 命令

## 相关文档

- [listen](./listen.md) -- 启动 daemon 并接收 HITL 事件
- [send](./send.md) -- 发送消息触发 Agent 执行
- [HITL 完整流程](../scenarios/advanced.md#hitlhuman-in-the-loop完整流程) -- advanced.md 中的完整场景
- [HITL 错误处理](../scenarios/error-handling.md#hitl-错误处理) -- error-handling.md 中的错误处理
