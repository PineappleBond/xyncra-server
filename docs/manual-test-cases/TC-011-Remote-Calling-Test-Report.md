# TC-011 Remote Calling 测试报告

> **日期**: 2026-07-22
> **Git Commit**: 602b9a9 (dirty)
> **测试者**: Claude Code (自动化)
> **环境**: Docker E2E + 真实 LLM (mimo-v2.5-pro)
> **E2E_HOME**: /tmp/xe2e-YxxBMf

---

## 测试执行记录

### 阶段 1: 函数注册验证 (D-115)

| 步骤 | 结果 | 备注 |
|------|------|------|
| 步骤 1.1: 启动 daemon | PASS | daemon 启动成功，进程存活 |
| 步骤 1.2: 内置函数自动注册 | PASS | 3 个函数注册成功 (ping, get_device_info, get_time) |

**验证数据**:
- Daemon 日志: `[INFO] functions registered count=3`
- Server 日志: `system.register_functions: registered 3 functions for userID=alice deviceID=test-device-alice`

**重要发现**: 函数注册在 daemon 的 `--user-id` 下。如果使用 `--user-id alice`，函数注册在 alice 下；如果使用 `--user-id agent/weather-bot`，函数注册在 agent/weather-bot 下。

---

### 阶段 2: 端到端调用链路

| 步骤 | 结果 | 备注 |
|------|------|------|
| 步骤 2.1: 创建 Agent 会话 | PASS | CONV_ID=d0c62b4a-42b6-430b-96bc-215f9eee663c |
| 步骤 2.2: 发送消息触发函数调用 | PASS | 消息发送成功 |
| 步骤 2.3: 等待 Agent 处理 | PASS | Agent 处理完成 |
| 步骤 2.4: RemoteCalling 创建到 DB | PASS | RC 记录创建，method=ping, device_id=agent-device-1 |
| 步骤 2.5: Conversation agent_status 更新 | PASS | agent_status=tool_calling |
| 步骤 2.6: Redis Checkpoint | PASS | checkpoint key 存在 |
| 步骤 2.7: 客户端自动拉取 RemoteCallings | **FAIL** | daemon 不会自动拉取和处理 RemoteCallings |
| 步骤 2.8: agent_resume 上报结果 | PASS (手动) | 手动调用 agent-resume 成功 |
| 步骤 2.9: Agent 恢复执行 | **FAIL** | RemoteCalling 过期后 cleanup 任务重新触发 agent，形成无限循环 |

**关键问题**:

1. **BUG-001: daemon 不会自动处理 RemoteCallings**
   - daemon 运行在 `agent/weather-bot` 下，注册了函数
   - 当 agent 调用客户端函数时，创建 RemoteCalling (status=pending)
   - 但 daemon 没有轮询或推送机制来发现和处理 RemoteCallings
   - `SendConversationUpdate` 只广播给 humanUserID (alice)，不广播给 agent
   - daemon 的 `OnConversation` 只在 `asking_user` 状态下显示 RemoteCallings，不处理 `tool_calling` 状态

2. **BUG-002: RemoteCalling 过期后无限循环**
   - RemoteCalling 30 秒超时
   - 超时后 cleanup 任务标记为 expired
   - cleanup 任务发现 "all resolved/expired" 后重新触发 agent 执行
   - Agent 再次调用客户端函数，创建新的 RemoteCalling
   - 新的 RemoteCalling 又超时，形成无限循环
   - 观察到 5 分钟内创建了 3 个 RemoteCalling (全部过期)

3. **BUG-003: agent_resume 使用过期的 checkpoint_id**
   - 第一次 RemoteCalling 的 checkpoint_id 为 `13ef848f`
   - 第二次 RemoteCalling 的 checkpoint_id 为 `c56580a0`
   - cleanup 任务使用旧的 checkpoint_id 触发 resume，找不到 resolved 的 RemoteCalling
   - 日志: `agent resume: no resolved remote callings found for checkpoint`

---

### 阶段 3: DeviceID 路由

| 步骤 | 结果 | 备注 |
|------|------|------|
| 步骤 3.1-3.6 | **SKIP** | 由于阶段 2 的 BUG-001，无法进行自动测试 |

---

### 阶段 4: 状态流转

| 步骤 | 结果 | 备注 |
|------|------|------|
| 步骤 4.1.2: pending -> resolved | PASS (手动) | 手动 agent-resume 成功，status=resolved |
| 步骤 4.2.4: pending -> cancelled | **SKIP** | 需要 cancel_remote_calls RPC，未测试 |
| 步骤 4.3.4: pending -> expired | PASS | RemoteCalling 30 秒超时后自动标记为 expired |
| 步骤 4.4.3: 幂等性 | **SKIP** | 由于 BUG-001，无法进行自动测试 |

---

### 阶段 5: 边缘场景

| 步骤 | 结果 | 备注 |
|------|------|------|
| 步骤 5.1.2: 并行调用 | **SKIP** | 由于 BUG-001，无法进行自动测试 |
| 步骤 5.2.4: 断线重连 | **SKIP** | 由于 BUG-001，无法进行自动测试 |
| 步骤 5.3.4: 服务器重启 | **SKIP** | 由于 BUG-001，无法进行自动测试 |
| 步骤 5.3.5: 重启后恢复 | **SKIP** | 由于 BUG-001，无法进行自动测试 |
| 步骤 5.4.2: 上报重试 | **SKIP** | 由于 BUG-001，无法进行自动测试 |

---

### 阶段 6: HITL 统一

| 步骤 | 结果 | 备注 |
|------|------|------|
| 步骤 6.1.3: ask_user 创建 RC | **SKIP** | 需要 LLM 触发 ask_user，未测试 |
| 步骤 6.1.7: ask_user 恢复 | **SKIP** | 依赖 6.1.3 |
| 步骤 6.2.3: ask_user_choice 创建 RC | **SKIP** | 需要 LLM 触发 ask_user_choice，未测试 |
| 步骤 6.2.5: ask_user_choice 恢复 | **SKIP** | 依赖 6.2.3 |

---

## 测试用例文档问题修复

### 问题 1: 函数注册用户 ID 不匹配

**原文档假设**: Alice daemon 注册的函数对 agent/weather-bot 可见
**实际情况**: commit 602b9a9 (`refactor(agent): use agent userID for dynamic tool lookup instead of caller device`) 改变了行为，DynamicToolProvider 使用 agent 自己的 userID 查找函数，而不是调用者的 userID

**修复方案**: 需要在 agent 自己的 userID 下注册函数（运行 daemon 时使用 `--user-id agent/weather-bot`）

### 问题 2: 自动处理 RemoteCallings 的假设不成立

**原文档假设**: daemon 会自动拉取并处理 RemoteCallings
**实际情况**: daemon 没有自动轮询或推送机制来处理 `tool_calling` 状态的 RemoteCallings

**修复方案**: 需要实现以下机制之一：
- daemon 轮询 `get_remote_callings` RPC
- 服务器向 agent 的设备广播 RemoteCalling 通知
- daemon 在收到 `tool_calling` 状态的 conversation update 时自动拉取 RemoteCallings

### 问题 3: 远程函数调用超时时间过短

**原文档假设**: 30 秒超时足够
**实际情况**: 对于手动测试，30 秒超时太短；对于自动测试，如果 daemon 不自动处理，超时会导致无限循环

**修复方案**: 增加超时时间或实现自动处理机制

---

## 发现的 Bug

### BUG-001: daemon 不会自动处理 RemoteCallings (严重)

**描述**: 当 agent 调用客户端函数时，创建 RemoteCalling (status=pending)，但注册函数的 daemon 不会自动发现和处理这些 RemoteCallings。

**根因**:
1. `SendConversationUpdate` 只广播给 `humanUserID`，不广播给 agent 的设备
2. daemon 的 `OnConversation` 只在 `asking_user` 状态下显示 RemoteCallings
3. daemon 没有轮询 `get_remote_callings` 的机制
4. client 库没有 `get_remote_callings` 的调用

**影响**: 客户端函数调用功能完全不可用，除非手动调用 `agent-resume`

**建议修复**:
1. 在 daemon 中添加轮询机制，定期调用 `get_remote_callings` RPC
2. 或者在服务器端向 agent 的设备广播 RemoteCalling 通知
3. 或者在 `OnConversation` 中处理 `tool_calling` 状态，自动拉取并执行 RemoteCallings

### BUG-002: RemoteCalling 过期后无限循环 (严重)

**描述**: 当 RemoteCalling 过期后，cleanup 任务重新触发 agent 执行，agent 再次调用客户端函数，创建新的 RemoteCalling，新的 RemoteCalling 又过期，形成无限循环。

**根因**:
1. cleanup 任务在 RemoteCalling 过期后重新触发 agent 执行
2. agent 执行时再次调用客户端函数
3. 新的 RemoteCalling 又过期

**影响**: 服务器资源浪费，数据库中积累大量过期的 RemoteCalling 记录

**观察数据**: 5 分钟内创建了 3 个 RemoteCalling (checkpoint c56580a0):
- 98985cda: resolved (手动)
- 16a9f22e: expired
- 5ad763f9: pending (将要过期)

**建议修复**:
1. cleanup 任务不应该重新触发 agent 执行，而是标记为失败并通知用户
2. 或者限制 RemoteCalling 的重试次数
3. 或者在 agent 执行时检查是否有过期的 RemoteCalling，如果有则停止重试

### BUG-003: agent_resume 使用过期的 checkpoint_id (中等)

**描述**: cleanup 任务使用旧的 checkpoint_id 触发 resume，但此时 agent 已经创建了新的 checkpoint。

**根因**:
1. 第一次 RemoteCalling 的 checkpoint_id 为 `13ef848f`
2. 第二次 RemoteCalling 的 checkpoint_id 为 `c56580a0`
3. cleanup 任务使用旧的 checkpoint_id 触发 resume

**影响**: resume 失败，日志显示 `agent resume: no resolved remote callings found for checkpoint`

**建议修复**:
1. cleanup 任务应该使用最新的 checkpoint_id
2. 或者在 resume 时检查 checkpoint 是否仍然有效

### BUG-004: 测试用例文档过时 (低)

**描述**: 测试用例文档假设函数注册在调用者 (alice) 下，对 agent 可见。但 commit 602b9a9 改变了行为。

**影响**: 按照文档执行测试会导致 agent 看不到注册的函数

**建议修复**: 更新测试用例文档，说明需要在 agent 自己的 userID 下注册函数

---

## 服务端问题分析

### 问题 1: 缺少客户端函数调用的自动执行机制

**分析**: 当前架构中，客户端函数调用依赖以下流程：
1. Agent 调用客户端函数 → 创建 RemoteCalling (status=pending)
2. 注册函数的 daemon 发现 RemoteCalling
3. daemon 执行函数并调用 `agent-resume` 上报结果
4. Agent 恢复执行

但步骤 2 没有实现，导致流程中断。

**建议**: 实现以下机制之一：
- daemon 轮询 `get_remote_callings` RPC
- 服务器向 agent 的设备广播 RemoteCalling 通知
- daemon 在收到 `tool_calling` 状态的 conversation update 时自动拉取 RemoteCallings

### 问题 2: cleanup 任务逻辑错误

**分析**: cleanup 任务在 RemoteCalling 过期后重新触发 agent 执行，但没有检查是否有其他 pending 的 RemoteCalling。这导致无限循环。

**建议**: 修改 cleanup 任务逻辑：
1. 不要重新触发 agent 执行
2. 标记 RemoteCalling 为失败
3. 通知用户 RemoteCalling 超时

### 问题 3: checkpoint_id 管理混乱

**分析**: 当前 checkpoint_id 在每次 agent 执行时生成新的，但 cleanup 任务使用旧的 checkpoint_id 触发 resume。这导致 resume 失败。

**建议**: 在 resume 时使用最新的 checkpoint_id，或者在 cleanup 时检查 checkpoint 是否仍然有效。

---

## 测试结论

**总体结果**: **FAIL** (3/20 步骤通过)

**通过的步骤**:
- 阶段 1: 函数注册验证 (2/2)
- 阶段 2: 部分步骤 (4/9)
- 阶段 4: 部分步骤 (1/4)

**失败的步骤**:
- 阶段 2: 客户端自动拉取 RemoteCallings (BUG-001)
- 阶段 2: Agent 恢复执行 (BUG-002, BUG-003)

**跳过的步骤**:
- 阶段 3-6: 由于 BUG-001，无法进行自动测试

**关键发现**:
1. 客户端函数调用功能完全不可用（daemon 不会自动处理 RemoteCallings）
2. RemoteCalling 过期后会形成无限循环
3. 测试用例文档过时，需要更新

**建议**:
1. 优先修复 BUG-001（daemon 自动处理 RemoteCallings）
2. 修复 BUG-002（cleanup 任务逻辑）
3. 更新测试用例文档
4. 重新执行完整测试
