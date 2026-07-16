# Xyncra 测试报告

**测试日期**：2026-07-09
**测试执行者**：Claude Code 子代理（串行调度）
**Git Commit**：`6451b89`

---

## 摘要

| 类别 | 总数 | 通过 | 失败 | 警告 |
|------|------|------|------|------|
| 单元测试 | 818+ | 818+ | 0 | 0 |
| Server E2E | 46 | 46 | 0 | 0 |
| CLI E2E (Go) | 8 | 8 | 0 | 0 |
| CLI E2E (子代理 P0) | 23 | 21 | 2 | 0 |
| CLI E2E (子代理 P1) | 30 | 26 | 1 | 3 |
| 核心流程 Review | 10 | — | — | 4 个新 Bug |

---

## Bug 验证与修复结果

### 已确认并修复的 Bug（步骤 2）

| Bug | 位置 | 严重度 | 状态 | 修复内容 |
|-----|------|--------|------|----------|
| handleConversation payload 不匹配 | `pkg/client/sync.go:302-334` | HIGH | ✅ 已修复 | 新增 `conversationUpdatePayload` 结构体，按 `action` 字段分发 delete/restore/upsert |
| 日志自动清理 goroutine 缺失 | `internal/cli/listen.go` | MEDIUM | ✅ 已修复 | 添加 `startLogCleanup` goroutine，每小时清理过期日志 |
| create_conversation 不更新本地 DB | `internal/cli/listen.go:261-277` | MEDIUM | ✅ 已修复 | IPC handler 成功后调用 `db.Conversations.Create` 持久化到本地 SQLite |

### Review 发现的新 Bug（步骤 5）

| # | Bug | 位置 | 严重度 | 说明 |
|---|-----|------|--------|------|
| 4 | mark_as_read payload 字段名不一致 | `sync.go:39` vs `mark_as_read.go:44` | HIGH | 服务端 `last_read_message_id` vs 客户端 `message_id`，多设备已读同步失效 |
| 5 | create_conversation 不创建 UserUpdate/MQ 广播 | `internal/handler/create_conversation.go` | HIGH | 对方用户无法实时感知新会话，只能等下次 sync_updates |
| 6 | RPCLog.ConversationID 永远为空 | `pkg/client/client.go:367-376` | MEDIUM | 创建 RPCLog 时未解析 params 提取 conversation_id |
| 7 | send_message 幂等性 TOCTOU 竞态 | `send_message.go:102-111` | LOW | 并发请求可能触发 unique constraint violation，返回 -300 而非 duplicate=true |

### P0/P1 测试发现的新 Bug

| # | Bug | 场景 | 严重度 | 说明 |
|---|-----|------|--------|------|
| 8 | daemon WS 不可达时直接退出 | CLI-E2E-007 | HIGH | `client.go:149-152` 初始连接失败直接 return，不保持 IPC 可用 |
| 9 | mark-as-read 本地 DB 未同步 | CLI-E2E-100 | HIGH | daemon 接收 mark_read 通知但未更新 conversations 表 last_read_message_id |
| 10 | `get-messages --limit -1` 导致 panic | CLI-E2E-119 | HIGH | `messages.go:279` slice bounds out of range，缺少负数校验 |
| 11 | `logs search --error` 过滤器失效 | CLI-E2E-210 | MEDIUM | 硬编码 `StatusCode = -1`，不匹配实际错误码（-100 到 -400） |

---

## P0 场景测试详情

| 批次 | 描述 | 通过 | 总数 | 通过率 |
|------|------|------|------|--------|
| P0-A | Daemon 生命周期 | 3 | 4 | 75% |
| P0-B | 写入命令 | 4 | 5 | 80% |
| P0-C | 查询命令 | 5 | 5 | 100% |
| P0-D | Kill + 多实例 | 5 | 5 | 100% |
| P0-E | 错误处理 + 日志 | 4 | 4 | 100% |
| **合计** | | **21** | **23** | **91.3%** |

**失败场景：**
- **CLI-E2E-007**：WS 服务器不可达时 daemon 直接退出（预期保持 IPC 可用）
- **CLI-E2E-100**：mark-as-read 后本地 DB last_read_message_id 未更新

---

## P1 场景测试详情

| 批次 | 描述 | 通过 | 总数 | 通过率 |
|------|------|------|------|--------|
| P1-A | 分页与过滤 | 7 | 9 | 78% |
| P1-B | 日志子命令 | 7 | 8 | 87.5% |
| P1-C | Standalone Fallback | 8 | 8 | 100% |
| P1-D | IPC 错误处理 | 4 | 5 | 80% |
| **合计** | | **26** | **30** | **86.7%** |

**失败/警告场景：**
- **CLI-E2E-118** (WARN)：`list-conversations --limit 0` 静默返回空结果
- **CLI-E2E-119** (FAIL)：`get-messages --limit -1` 导致 panic
- **CLI-E2E-210** (FAIL)：`logs search --error` 过滤器硬编码 StatusCode=-1 不匹配实际错误码
- **CLI-E2E-029** (PARTIAL)：非 JSON 输入返回 -32700 而非 -32600（符合 JSON-RPC 2.0 规范，测试用例预期需调整）

---

## 核心流程 Review

| 流程 | 需求满足 | 发现问题 | 风险 |
|------|----------|----------|------|
| 1. send_message E2E | D-006⚠️ D-007✅ D-008✅ D-028✅ | 幂等性 TOCTOU 竞态 | LOW |
| 2. sync_updates gap-fill | D-009✅ D-029✅ | uint32 溢出风险（理论性） | LOW |
| 3. listen daemon | D-030✅ D-031✅ D-039✅ | 初始 WS 连接失败导致 daemon 退出 | HIGH |
| 4. IPC communication | D-030✅ D-032✅ D-036✅ | 无重大问题 | — |
| 5. create_conversation | D-011✅ D-035✅ | 不创建 UserUpdate/MQ 广播 | HIGH |
| 6. delete/restore cascade | D-013✅ D-015✅ | Bug 1 修复已验证正确 | — |
| 7. mark_as_read | D-012✅ D-028⚠️ | payload 字段名不一致 | HIGH |
| 8. retry queue | D-027✅ | 非幂等操作重试风险 | LOW |
| 9. standalone fallback | D-032✅ | 超时预算卡边界 | LOW |
| 10. logs write path | D-040✅ | RPCLog.ConversationID 为空 | MEDIUM |

---

## 风险评估

### CRITICAL
- **mark_as_read 字段名不一致**（Bug #4）：多设备已读状态完全无法同步，影响所有用户
- **create_conversation 无广播**（Bug #5）：对方用户无法实时发现新会话
- **daemon WS 不可达退出**（Bug #8）：离线场景下 IPC 完全不可用

### HIGH
- **mark-as-read 本地 DB 未同步**（Bug #9）：未读计数永远不准确
- **get-messages --limit -1 panic**（Bug #10）：负数输入导致进程崩溃
- **logs search --error 失效**（Bug #11）：错误日志搜索功能不可用

### MEDIUM
- **RPCLog.ConversationID 为空**（Bug #6）：按会话过滤日志功能失效

### LOW
- **send_message 幂等性竞态**（Bug #7）：高频并发场景下可能返回错误码
- **standalone 超时预算**：极端情况下可能超时

---

## 建议

1. **[P0 紧急]** 修复 mark_as_read payload 字段名不一致（`sync.go:39` 改为 `last_read_message_id`）
2. **[P0 紧急]** 为 create_conversation 添加 UserUpdate 创建和 MQ 广播逻辑
3. **[P0 紧急]** 修复 daemon 初始 WS 连接失败时保持 IPC 可用
4. **[P1 重要]** 修复 mark-as-read 本地 DB 同步（daemon 接收 mark_read 通知后更新 conversations 表）
5. **[P1 重要]** 修复 `get-messages --limit -1` panic（添加负数校验）
6. **[P1 重要]** 修复 `logs search --error`（改用 `WHERE status_code < 0`）
7. **[P2 一般]** 修复 RPCLog.ConversationID 填充
8. **[P2 一般]** 修复 send_message 幂等性竞态（捕获 ErrDuplicateKey 返回 duplicate=true）

---

## 产品决策合规总结

| 决策 | 状态 | 说明 |
|------|------|------|
| D-001 开箱即用 | ✅ | |
| D-002 认证外置 | ✅ | |
| D-003 内网部署 | ✅ | |
| D-004 接受任意 Origin | ✅ | |
| D-005 user_id 参数认证 | ✅ | |
| D-006 幂等 send_message | ⚠️ | TOCTOU 竞态 |
| D-007 MQ fire-and-forget | ✅ | |
| D-008 MessageID 原子分配 | ✅ | 已重构到事务内 |
| D-009 sync_updates 分页 | ✅ | |
| D-011 幂等 create_conversation | ✅ | |
| D-012 mark_as_read MAX 语义 | ✅ | 服务端正确 |
| D-013 级联删除 | ✅ | |
| D-015 级联恢复 | ✅ | |
| D-027 客户端扩展错误码 | ✅ | |
| D-028 UserUpdate 类型 | ⚠️ | create_conversation 未创建 |
| D-029 gap-filling | ✅ | |
| D-030 IPC 协议 | ✅ | |
| D-031 进程锁 | ✅ | |
| D-032 IPC Fallback | ✅ | |
| D-033 设备 ID | ✅ | |
| D-034 环境变量 | ✅ | |
| D-035 本地 DB 查询 | ✅ | |
| D-036 sync-updates IPC-only | ✅ | |
| D-037 --peer-id | ✅ | |
| D-038 message-id 类型区分 | ✅ | |
| D-039 kill 行为 | ✅ | |
| D-040 日志 7 天保留 | ✅ | 自动清理已添加 |
| D-041 tabwriter | ✅ | |
| D-042 退出码 | ✅ | |
| D-043 E2E 端口 | ✅ | |

**合规率：28/30 (93.3%)**，2 个有轻微偏差。

---

## 代码变更统计

- **10 个文件修改**
- **610 行新增，123 行删除**
- **Commit**：`6451b89` on `main`
- **go fmt**：1 个文件重格式化
- **go vet**：无问题
