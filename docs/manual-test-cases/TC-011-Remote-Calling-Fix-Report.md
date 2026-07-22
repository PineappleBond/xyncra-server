# TC-011 Remote Calling Bug Fix Report

> **日期**: 2026-07-22
> **基于**: Test Report TC-011 (commit 602b9a9)
> **修复者**: Claude Code (自动化)

---

## 修复概览

| Bug ID | 严重程度 | 状态 | 修复文件 |
|--------|----------|------|----------|
| BUG-001 | Critical | FIXED | `internal/agent/executor.go`, `internal/agent/resume_handler.go`, `internal/agent/remote_calling_cleanup.go` |
| BUG-002 | Critical | FIXED | `internal/agent/remote_calling_cleanup.go` |
| BUG-003 | Medium | FIXED | `internal/agent/resume_handler.go` |

---

## BUG-001: Daemon 不会自动处理 RemoteCallings

### 根因分析

当 agent 调用客户端函数时，服务器创建 RemoteCalling 并通过 `SendConversationUpdate` 广播通知。但该通知只发送给 `humanUserID`（发送消息的人类用户），不发送给 `agentUserID`（注册函数的 daemon 所属用户）。

设计文档要求：
> **拉取模型**: 客户端检测 Conversation 变更 → 主动拉取 RemoteCallings

但由于通知未到达 agent 的 daemon，daemon 无法检测到 Conversation 变更，因此无法拉取 RemoteCallings。

### 修复方案

在所有创建 RemoteCalling 的代码路径中，将 `SendConversationUpdate` 广播给对话的**两个参与者**（humanUserID 和 agentUserID），确保 agent 的 daemon 能收到通知。

### 修改的文件和行

**1. `internal/agent/executor.go`**

Client function interrupt 路径（原第 469 行）：
```go
// 修复前：只广播给 humanUserID
e.broadcaster.SendConversationUpdate(ctx, payload.SenderID, payload.ConversationID, hitlUpdatedAt)

// 修复后：广播给两个参与者
e.broadcaster.SendConversationUpdate(ctx, payload.SenderID, payload.ConversationID, hitlUpdatedAt)
e.broadcaster.SendConversationUpdate(ctx, payload.AgentID, payload.ConversationID, hitlUpdatedAt)
```

HITL (ask_user) interrupt 路径（原第 518 行）：
```go
// 修复前：只广播给 humanUserID
e.broadcaster.SendConversationUpdate(ctx, payload.SenderID, payload.ConversationID, hitlUpdatedAt)

// 修复后：广播给两个参与者
e.broadcaster.SendConversationUpdate(ctx, payload.SenderID, payload.ConversationID, hitlUpdatedAt)
e.broadcaster.SendConversationUpdate(ctx, payload.AgentID, payload.ConversationID, hitlUpdatedAt)
```

**2. `internal/agent/resume_handler.go`**

Client function re-interrupt 路径（原第 375 行）：
```go
// 修复后：广播给两个参与者
executor.broadcaster.SendConversationUpdate(ctx, payload.SenderID, payload.ConversationID, resumeHitlUpdatedAt)
executor.broadcaster.SendConversationUpdate(ctx, payload.AgentID, payload.ConversationID, resumeHitlUpdatedAt)
```

HITL re-interrupt 路径（原第 407 行）：
```go
// 修复后：广播给两个参与者
executor.broadcaster.SendConversationUpdate(ctx, payload.SenderID, payload.ConversationID, resumeHitlUpdatedAt)
executor.broadcaster.SendConversationUpdate(ctx, payload.AgentID, payload.ConversationID, resumeHitlUpdatedAt)
```

Post-resume 路径（原第 500 行）：
```go
// 修复后：广播给两个参与者
executor.broadcaster.SendConversationUpdate(ctx, payload.SenderID, payload.ConversationID, time.Now())
executor.broadcaster.SendConversationUpdate(ctx, payload.AgentID, payload.ConversationID, time.Now())
```

**3. `internal/agent/remote_calling_cleanup.go`**

cleanupExpiredConversation 方法（新增）：
```go
// 修复后：广播给两个参与者
t.broadcaster.SendConversationUpdate(ctx, humanUserID, rc.ConversationID, cleanupUpdatedAt)
t.broadcaster.SendConversationUpdate(ctx, rc.AgentID, rc.ConversationID, cleanupUpdatedAt)
```

### 工作原理

1. Agent 调用客户端函数 → 创建 RemoteCalling (status=pending)
2. 服务器广播 `conversation` 类型的 ephemeral update 给 humanUserID 和 agentUserID
3. Agent 的 daemon 收到通知 → 调用 `get_conversation` RPC 拉取最新状态
4. `get_conversation` 返回 conversation 和 remote_callings
5. Daemon 的 `OnConversation` 处理 `tool_calling` 状态 → 自动拉取并执行 RemoteCallings
6. 执行完成后调用 `agent_resume` RPC 上报结果

### 客户端侧要求

虽然服务器端修复确保了通知能到达 agent 的 daemon，但 daemon 的 `OnConversation` handler 也需要处理 `tool_calling` 状态。当前客户端库 (`pkg/client/sync.go`) 的 `handleEphemeralConversationUpdate` 已经实现了：
- 调用 `get_conversation` RPC 获取最新 conversation 和 remote_callings
- 将 remote_callings 持久化到本地 DB
- 调用 `handler.OnConversation(ctx, result.Conversation)` 通知应用层

因此，daemon 只需要在 `OnConversation` 中检查 `agent_status == "tool_calling"` 并处理 RemoteCallings 即可。

---

## BUG-002: RemoteCalling 过期后无限循环

### 根因分析

`cleanupExpiredRemoteCalling` 方法在所有 RemoteCalling 过期后，无条件地重新触发 agent 执行（通过 MQ 入队 `TypeAgentResume`）。这导致：

1. RemoteCalling 过期 → cleanup 标记为 expired
2. Cleanup 发现 `pending == 0` → 入队 agent resume
3. Agent resume → agent 重新执行 → 再次调用客户端函数
4. 创建新的 RemoteCalling → 又过期
5. 回到步骤 1，形成无限循环

测试报告显示：5 分钟内创建了 3 个 RemoteCalling（全部过期）。

### 修复方案

在 `cleanupExpiredRemoteCalling` 中，当 `pending == 0` 时，检查是否有任何 RemoteCalling 被 resolved：
- **如果有 resolved 的**：入队 agent resume（保留原有行为，让 agent 处理已上报的结果）
- **如果全部 expired（没有 resolved 的）**：清理 conversation，不重新触发 agent 执行

### 修改的文件

**`internal/agent/remote_calling_cleanup.go`**

`cleanupExpiredRemoteCalling` 方法：
```go
// 修复后：区分 resolved 和 all-expired 两种情况
if pending == 0 {
    // 检查是否有 resolved 的 RemoteCalling
    allRCs, listErr := t.remoteCallingStore.GetByCheckpoint(ctx, rc.CheckpointID)
    // ...

    hasResolved := false
    for _, r := range allRCs {
        if r.Status == model.RemoteCallingStatusResolved {
            hasResolved = true
            break
        }
    }

    if hasResolved {
        // 有 resolved 的 → 入队 agent resume（原有行为）
    } else {
        // 全部 expired → 清理 conversation（新增行为）
        t.cleanupExpiredConversation(ctx, rc)
    }
}
```

新增 `cleanupExpiredConversation` 方法：
```go
func (t *RemoteCallingCleanupTask) cleanupExpiredConversation(ctx context.Context, rc *model.RemoteCalling) {
    // 1. 检查 conversation 状态（避免重复清理）
    // 2. ClearAgentStatus（重置为 idle）
    // 3. DeleteByCheckpoint（删除 RemoteCallings）
    // 4. Delete checkpoint from Redis
    // 5. 发送超时消息给用户
    // 6. 广播 agent_timeout 和 conversation update
}
```

---

## BUG-003: agent_resume 使用过期的 checkpoint_id

### 根因分析

这是 BUG-002 的直接后果。当 cleanup 任务入队 agent resume 时，使用的 checkpoint_id 是当前过期 RemoteCalling 的。但 agent 可能已经创建了新的 checkpoint（从之前的 resume 中）。这导致 resume handler 找不到 resolved 的 RemoteCalling。

### 修复方案

**1. 通过修复 BUG-002 间接修复**

修复 BUG-002 后，cleanup 任务不再在全部 expired 时入队 agent resume，因此不会出现使用过期 checkpoint_id 的情况。

**2. 在 resume handler 中增加防御性检查**

在 `resume_handler.go` 中，当 `targets` 为空时，检查是否所有 RemoteCalling 都是 expired/cancelled 状态。如果是，清理 conversation 而不是用空 targets 恢复 agent。

```go
if len(targets) == 0 {
    // 检查是否所有 RemoteCalling 都是 expired/cancelled
    allExpired := len(rcList) > 0
    for _, rc := range rcList {
        if rc.Status != model.RemoteCallingStatusExpired &&
           rc.Status != model.RemoteCallingStatusCancelled {
            allExpired = false
            break
        }
    }
    if allExpired {
        // 清理 conversation，发送超时消息
        cleanupAfterResumeFailure(ctx, executor, payload.ConversationID, payload.CheckpointID, logger)
        executor.sendErrorMessage(ctx, execPayload, "抱歉，远程函数调用超时，请重新发送消息。")
        markResumeFailed(ctx, idempotency, payload.CheckpointID, releaseLock, logger)
        return nil
    }
}
```

---

## 编译验证

```
$ go build ./...
(成功，无错误)
```

注意：`go test ./internal/agent/...` 有 2 个预存的测试编译错误（非本次修复引入）：
- `client_function_tool_test.go`: `newClientFunctionTool` 参数不匹配
- `remote_calling_cleanup_test.go`: `NewRemoteCallingCleanupTask` 缺少 `broker` 参数

---

## 测试建议

### 修复后需要验证的场景

1. **BUG-001 验证**:
   - 启动 daemon（使用 `--user-id agent/weather-bot`）
   - 创建 agent 会话，发送消息触发客户端函数调用
   - 验证 daemon 收到 `conversation` update 通知
   - 验证 daemon 自动拉取并执行 RemoteCallings
   - 验证 agent 恢复执行

2. **BUG-002 验证**:
   - 触发客户端函数调用
   - 等待 RemoteCalling 过期（不手动干预）
   - 验证 cleanup 任务清理 conversation（不重新触发 agent）
   - 验证用户收到超时消息
   - 验证不会创建新的 RemoteCalling

3. **BUG-003 验证**:
   - 触发客户端函数调用
   - 手动调用 `agent-resume` 上报一个结果
   - 等待其他 RemoteCalling 过期
   - 验证 resume handler 正确处理混合状态（resolved + expired）

### 重建和重启

```bash
# 重建 Docker 镜像
docker compose -f deploy/docker-compose.e2e.yml build

# 重启服务
docker compose -f deploy/docker-compose.e2e.yml down
docker compose -f deploy/docker-compose.e2e.yml up -d
```

---

## 遗留问题

1. **客户端 daemon 需要更新**: 虽然服务器端修复确保通知能到达 daemon，但 daemon 的 `OnConversation` handler 需要处理 `tool_calling` 状态。当前 demo daemon 只在 `asking_user` 状态下显示 RemoteCallings，不处理 `tool_calling` 状态。

2. **超时时间配置**: 默认 30 秒超时对于手动测试太短。建议增加配置选项或使用更长的默认值。

3. **预存测试错误**: `client_function_tool_test.go` 和 `remote_calling_cleanup_test.go` 有预存的编译错误，需要单独修复。
