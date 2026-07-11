# Agent E2E 测试文档

## 概述

Phase 6 客户端 Agent 集成与端到端验证的测试文档。覆盖以下测试层次：

| 层次 | 文件 | 描述 |
|------|------|------|
| 服务端单元测试 | `internal/agent/broadcast_test.go` | BroadcastHelper payload 格式验证 |
| 客户端单元测试 | `pkg/client/agent_test.go` | IsAgentUser 辅助函数验证 |
| CLI 单元测试 | `internal/cli/listen_test.go` | CLI 显示逻辑（thinking vs typing）|
| E2E 集成测试 | `internal/cli/e2e/agent_e2e_test.go` | 端到端 Agent 交互流程 |

## 环境要求

### 基础依赖

| 服务 | 端口 | 用途 |
|------|------|------|
| Redis | 16379 | E2E 测试专用（DB 15） |
| Xyncra Server | 18080 | WebSocket + HTTP |

### Agent 功能依赖

| 环境变量 | 用途 | 必需? |
|----------|------|-------|
| `DASHSCOPE_API_KEY` | DashScope/Qwen LLM API 访问 | Agent E2E 测试需要 |

如果 `DASHSCOPE_API_KEY` 未设置，Agent E2E 测试会自动 skip。

## 运行方式

### 全部单元测试

```bash
go test ./internal/agent/ ./pkg/client/ ./internal/cli/ -v
```

### 仅 Agent 相关测试

```bash
# BroadcastHelper payload 测试
go test ./internal/agent/ -run "TestSendStreamUpdate|TestSendTyping" -v

# IsAgentUser 测试
go test ./pkg/client/ -run "TestIsAgentUser" -v

# CLI 显示逻辑测试
go test ./internal/cli/ -run "TestCLIUpdateHandler_OnTyping|TestCLIUpdateHandler_OnStreaming" -v
```

### E2E 测试

```bash
# 运行所有 Agent E2E 测试（无 API key 时 skip）
go test -v -run TestAgentE2E ./internal/cli/e2e/ -timeout 120s

# 仅运行不需要 API key 的测试
go test -v -run TestAgentE2E_IsAgentUser ./internal/cli/e2e/ -timeout 30s
```

### 带完整 Agent E2E

```bash
# 确保 Redis 和 Server 已启动
redis-server --port 16379 &
go run ./cmd/xyncra-server/ --port 18080 --redis-addr localhost:16379 &

# 设置 API key
export DASHSCOPE_API_KEY=your-api-key

# 运行 E2E 测试
go test -v -run TestAgentE2E ./internal/cli/e2e/ -timeout 120s
```

## 测试用例说明

### 单元测试

| ID | 文件 | 测试名 | 描述 | 预期结果 |
|----|------|--------|------|---------|
| UT-001 | broadcast_test.go | TestSendStreamUpdate_PayloadIncludesUserIDAndTimestamp | 验证 streaming payload 包含 user_id 和 timestamp | payload.UserID = agent, payload.Timestamp > 0 |
| UT-002 | broadcast_test.go | TestSendTyping_PayloadIncludesUserIDAndTimestamp | 验证 typing payload 包含 user_id 和 timestamp | payload.UserID = agent, payload.Timestamp > 0 |
| UT-003 | broadcast_test.go | TestSendStreamUpdate_JSONFieldNames | 验证 JSON 字段名与客户端期望一致 | 包含 text（非 content）|
| UT-004 | broadcast_test.go | TestSendTyping_JSONFieldNames | 验证 JSON 字段名与客户端期望一致 | 包含 user_id, timestamp |
| UT-005 | agent_test.go | TestIsAgentUser | 验证 IsAgentUser 判断逻辑 | 8 个子用例覆盖各种边界情况 |
| UT-006 | listen_test.go | TestCLIUpdateHandler_OnTyping_AgentUser | 验证 agent typing 显示为 [thinking] | 输出包含 [thinking] |
| UT-007 | listen_test.go | TestCLIUpdateHandler_OnTyping_HumanUser | 验证 human typing 显示为 [typing] | 输出包含 [typing]，不含 [thinking] |
| UT-008 | listen_test.go | TestCLIUpdateHandler_OnStreaming_AgentUser | 验证 agent streaming 显示为 [agent] | 输出包含 [agent] |

### E2E 测试

| ID | 测试名 | 描述 | 需要 Server | 需要 API Key |
|----|--------|------|:-----------:|:------------:|
| AE-001 | TestAgentE2E_FullFlow | 完整 Agent 交互流程：human 发消息 → agent 回复 → 本地 DB 同步 | ✓ | ✓ |
| AE-002 | TestAgentE2E_IsAgentUser | IsAgentUser 辅助函数验证 | ✗ | ✗ |
| AE-003 | TestAgentE2E_ConversationWithAgentSynced | 与 agent 的会话创建和同步 | ✓ | ✗ |
| AE-004 | TestAgentE2E_AgentPrefixInConversation | agent 前缀在会话中正确保留 | ✓ | ✗ |
| — | TestAgentE2E_NonAgentUnaffected | 回归：human-to-human 消息不受影响 | ✓ | ✗ |
| — | TestAgentE2E_AgentDBPath | daemon DB 路径构建正确性 | ✓ | ✗ |

## 相关产品决策

| 决策 | 描述 | 测试覆盖 |
|------|------|---------|
| D-050 | Ephemeral Push (Seq=0) | UT-001, UT-002（Seq=0 验证）|
| D-051 | 累积文本流式模式 | UT-003（text 字段名）|
| D-054 | Agent UserID 命名约定 | UT-005, AE-002 |
| D-065 | Agent 思考状态展示 | UT-006, UT-007 |
| D-067 | Agent 错误消息策略 | AE-001（完整流程含错误路径）|

## 已知限制

1. **E2E 测试需要外部服务**：Redis 和 Server 必须预先启动，测试不会自动拉起
2. **Agent 完整流程测试需要 API key**：`TestAgentE2E_FullFlow` 需要 `DASHSCOPE_API_KEY` 环境变量
3. **Server 需要启用 Agent 支持**：Server 启动时必须注入 `AgentRegistry`（D-063 nil-safe）
4. **弱网 Agent 测试未覆盖**：Agent 交互过程中的网络断连恢复场景暂未测试

## 后续改进方向

1. 添加 mock LLM 支持，使 E2E 测试无需外部 API key
2. 添加 Agent 错误消息策略（D-067）的专项 E2E 测试
3. 添加 Agent 幂等性（D-071）的 E2E 测试
4. 添加多轮对话上下文保持的 E2E 测试
5. 添加 Agent 弱网韧性测试（与 weaknet_test.go 模式结合）
