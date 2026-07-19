# Agent E2E 测试结果报告

> 生成时间：2026-07-12  
> 环境：Redis 16379 (DB 15)，SQLite 内存，mock LLM / qwen3.7-plus (real LLM)  
> 总耗时：Mock ~60s | Real LLM ~160s

---

## 总览

| 类别 | 测试数 | PASS | FAIL | 状态 |
|------|:------:|:----:|:----:|------|
| Mock 测试 (11 文件) | 59 | 59 | 0 | ✅ 全绿 |
| Real LLM 测试 (1 文件) | 14 | 13 | 1* | ✅ 基线通过 |
| **合计** | **73** | **72** | **1** | |

> *REAL-009 因 LLM API 超时 flaky（53s WebSocket recv timeout），非代码缺陷，已标记 accepted。重试后通过。

---

## Mock 测试详情 (59 tests, ~60s)

### Basic (5 tests) — `agent_basic_test.go`

| ID | 测试名 | 验证内容 | 耗时 |
|----|--------|----------|------|
| AE_BASIC-001 | `TestAgentBasic_AE_BASIC_001` | 基本对话 happy path：发消息→Agent 处理→回复持久化 | 1.04s |
| AE_BASIC-002 | `TestAgentBasic_AE_BASIC_002` | 回复消息格式正确：SenderID=agent/{id}，Type=text (D-055) | 1.06s |
| AE_BASIC-003 | `TestAgentBasic_AE_BASIC_003` | 离线 sync：Agent 回复持久化后，离线用户通过 sync_updates 获取 | 1.50s |
| AE_BASIC-004 | `TestAgentBasic_AE_BASIC_004` | Agent 系统不影响人类间消息传递 | 1.66s |
| AE_BASIC-005 | `TestAgentBasic_AE_BASIC_005` | Agent→Agent 消息不触发处理 (D-062) | 3.16s |

### Concurrency (7 tests) — `agent_concurrent_test.go`

| ID | 测试名 | 验证内容 | 耗时 |
|----|--------|----------|------|
| AE-CONC-001 | `TestAgentConc_AE_CONC_001` | ConversationLock 互斥性 | 1.12s |
| AE-CONC-002 | `TestAgentConc_AE_CONC_002` | 不同会话的锁互不干扰 | 0.76s |
| AE-CONC-003 | `TestAgentConc_AE_CONC_003` | MarkProcessed 幂等性 (SETNX) | 0.00s |
| AE-CONC-004 | `TestAgentConc_AE_CONC_004` | Redis 不可用时 fail-open (D-072) | 0.07s |
| AE-CONC-005 | `TestAgentConc_AE_CONC_005` | Semaphore 限制并发数 | 0.20s |
| AE-CONC-006 | `TestAgentConc_AE_CONC_006` | 会话锁自动过期 (TTL) | 2.56s |
| AE-CONC-007 | `TestAgentConc_AE_CONC_007` | Lua 释放脚本只删除持有者的锁 | 0.00s |

### Context (5 tests) — `agent_context_test.go`

| ID | 测试名 | 验证内容 | 耗时 |
|----|--------|----------|------|
| AE-CTX-001 | `TestAgentContext_AE_CTX_001` | 多轮对话维持上下文 | 1.04s |
| AE-CTX-002 | `TestAgentContext_AE_CTX_002` | Token 截断正常工作 | 1.43s |
| AE-CTX-003 | `TestAgentContext_AE_CTX_003` | 消息数 fallback 截断 | 1.15s |
| AE-CTX-004 | `TestAgentContext_AE_CTX_004` | 缓存命中避免 DB 查询 | 1.52s |
| AE-CTX-005 | `TestAgentContext_AE_CTX_005` | 缓存 TTL 过期触发重新加载 | 0.67s |

### Ephemeral (5 tests) — `agent_ephemeral_test.go`

| ID | 测试名 | 验证内容 | 耗时 |
|----|--------|----------|------|
| AE-EPH-001 | `TestAgentEph_AE_EPH_001` | typing indicator 在首个 token 前发送 (Seq=0) | 1.14s |
| AE-EPH-002 | `TestAgentEph_AE_EPH_002` | 工具调用时 agent_status 推送 | 0.62s |
| AE-EPH-003 | `TestAgentEph_AE_EPH_003` | 超时时 agent_timeout 推送 + 正确 reason | 0.77s |
| AE-EPH-004 | `TestAgentEph_AE_EPH_004` | Ephemeral 不出现在 sync_updates (D-050) | 2.39s |
| AE-EPH-005 | `TestAgentEph_AE_EPH_005` | agent_status payload 结构正确 | 1.26s |

### Error (6 tests) — `agent_error_test.go`

| ID | 测试名 | 验证内容 | 耗时 |
|----|--------|----------|------|
| AE-ERR-001 | `TestAgentErr_AE_ERR_001` | LLM API 错误 → 持久化中文错误消息 (D-067) | 1.29s |
| AE-ERR-002 | `TestAgentErr_AE_ERR_002` | API Key 缺失 → 配置错误消息 (D-067) | 1.15s |
| AE-ERR-003 | `TestAgentErr_AE_ERR_003` | 上下文加载失败 → 错误消息 (D-067) | 1.41s |
| AE-ERR-004 | `TestAgentErr_AE_ERR_004` | 未知错误 → 通用错误消息 (D-067) | 1.40s |
| AE-ERR-005 | `TestAgentErr_AE_ERR_005` | 工具执行失败 → 错误消息 (D-082) | 1.11s |
| AE-ERR-006 | `TestAgentErr_AE_ERR_006` | TaskHandler 总是返回 nil (D-073) | 0.67s |

### HITL (6 tests) — `agent_hitl_test.go`

| ID | 测试名 | 验证内容 | 耗时 |
|----|--------|----------|------|
| AE-HITL-001 | `TestAgentHITL_AE_HITL_001` | HITL interrupt 推送 agent_question 事件 | 0.57s |
| AE-HITL-002 | `TestAgentHITL_AE_HITL_002` | RedisCheckPointStore 存取正确 | 0.01s |
| AE-HITL-003 | `TestAgentHITL_AE_HITL_003` | AgentResumePayload 可反序列化 | 0.60s |
| AE-HITL-004 | `TestAgentHITL_AE_HITL_004` | HITL 中断期间会话锁保持 (D-084) | 0.93s |
| AE-HITL-005 | `TestAgentHITL_AE_HITL_005` | CheckpointStore 非 fail-open (D-083) | 0.20s |
| AE-HITL-006 | `TestAgentHITL_AE_HITL_006` | checkpoint 创建后广播 agent_checkpoint_created | 1.12s |

### Middleware (5 tests) — `agent_middleware_test.go`

| ID | 测试名 | 验证内容 | 耗时 |
|----|--------|----------|------|
| AE-MW-001 | `TestAgentMW_AE_MW_001` | Summarization middleware 触发 | 0.57s |
| AE-MW-002 | `TestAgentMW_AE_MW_002` | ToolReduction middleware 触发 | 0.89s |
| AE-MW-003 | `TestAgentMW_AE_MW_003` | PatchToolCalls middleware 修复 | 1.45s |
| AE-MW-004 | `TestAgentMW_AE_MW_004` | Middleware 执行顺序正确 | 0.95s |
| AE-MW-005 | `TestAgentMW_AE_MW_005` | Middleware 创建失败时跳过 | 0.82s |

### Reload (4 tests) — `agent_reload_test.go`

| ID | 测试名 | 验证内容 | 耗时 |
|----|--------|----------|------|
| AE-RELOAD-001 | `TestAgentReload_AE_RELOAD_001` | reload_agents 加载新 Agent (D-076) | 0.87s |
| AE-RELOAD-002 | `TestAgentReload_AE_RELOAD_002` | reload_agents 移除已删除的 Agent | 0.58s |
| AE-RELOAD-003 | `TestAgentReload_AE_RELOAD_003` | 跳过无效 YAML 文件 | 1.46s |
| AE-RELOAD-004 | `TestAgentReload_AE_RELOAD_004` | reload 不影响正在运行的任务 | 0.63s |

### Streaming (6 tests) — `agent_streaming_test.go`

| ID | 测试名 | 验证内容 | 耗时 |
|----|--------|----------|------|
| AE-STREAM-001 | `TestAgentStream_AE_STREAM_001` | typing indicator 在首个 token 前 (D-065) | 0.80s |
| AE-STREAM-002 | `TestAgentStream_AE_STREAM_002` | 流式 token 累积模式 (D-051) | 0.74s |
| AE-STREAM-003 | `TestAgentStream_AE_STREAM_003` | is_done 标志 (D-052) | 0.92s |
| AE-STREAM-004 | `TestAgentStream_AE_STREAM_004` | typing 在首个 token 后停止 (D-065) | 0.80s |
| AE-STREAM-005 | `TestAgentStream_AE_STREAM_005` | streaming 不出现在 sync_updates (D-050) | 1.79s |
| AE-STREAM-006 | `TestAgentStream_AE_STREAM_006` | 持久化内容与流式一致 (D-052) | 1.41s |

### Sub-agent (4 tests) — `agent_subagent_test.go`

| ID | 测试名 | 验证内容 | 耗时 |
|----|--------|----------|------|
| AE-SUB-001 | `TestAgentSub_AE_SUB_001` | Sub-agent 委派成功 | 1.01s |
| AE-SUB-002 | `TestAgentSub_AE_SUB_002` | Sub-agent 输出合并到父回复 | 1.01s |
| AE-SUB-003 | `TestAgentSub_AE_SUB_003` | Sub-agent 深度限制强制执行 | 1.45s |
| AE-SUB-004 | `TestAgentSub_AE_SUB_004` | 不存在的 sub-agent 跳过 | 1.25s |

### Tools (6 tests) — `agent_tools_test.go`

| ID | 测试名 | 验证内容 | 耗时 |
|----|--------|----------|------|
| AE-TOOL-001 | `TestAgentTools_AE_TOOL_001` | Agent 调用已注册工具 | 0.85s |
| AE-TOOL-002 | `TestAgentTools_AE_TOOL_002` | 工具结果反映在回复中 | 1.01s |
| AE-TOOL-003 | `TestAgentTools_AE_TOOL_003` | 未注册工具名跳过 (fail-open) | 0.93s |
| AE-TOOL-004 | `TestAgentTools_AE_TOOL_004` | 工具结果截取存储 | 0.00s |
| AE-TOOL-005 | `TestAgentTools_AE_TOOL_005` | 截取结果检索 | 0.00s |
| AE-TOOL-006 | `TestAgentTools_AE_TOOL_006` | 截取结果 TTL 过期 | 0.10s |

---

## Real LLM 测试详情 (14 tests, ~160s)

构建标签：`//go:build real_llm`，需 `.env` 配置 LLM 凭据。

| ID | 测试名 | 分类 | 验证内容 | 耗时 | 结果 |
|----|--------|------|----------|------|------|
| REAL-001 | `TestAgentRealLLM_REAL_001` | Basic | 基本对话：回复非空、SenderID 正确 | 11.39s | ✅ |
| REAL-002 | `TestAgentRealLLM_REAL_002` | Basic | 消息格式：ConversationID/Type/MessageID/ID | 6.43s | ✅ |
| REAL-003 | `TestAgentRealLLM_REAL_003` | Basic | sync_updates：离线用户重连获取回复 | 10.93s | ✅ |
| REAL-004 | `TestAgentRealLLM_REAL_004` | Stream | 流式输出：streaming ephemeral 到达 | 16.15s | ✅ |
| REAL-005 | `TestAgentRealLLM_REAL_005` | Stream | is_done 信号：最后 streaming update | 10.36s | ✅ |
| REAL-006 | `TestAgentRealLLM_REAL_006` | Context | 多轮上下文：第二条回复关联第一条 | 26.86s | ✅ |
| REAL-007 | `TestAgentRealLLM_REAL_007` | Error | API Key 缺失：持久化中文错误消息 | 0.69s | ✅ |
| REAL-008 | `TestAgentRealLLM_REAL_008` | Ephemeral | typing indicator：首个 token 前 typing=true | 7.18s | ✅ |
| REAL-009 | `TestAgentRealLLM_REAL_009` | Ephemeral | agent_status 事件推送 | 60.70s | ⚠️ flaky |
| REAL-010 | `TestAgentRealLLM_REAL_010` | Reload | 配置热更新：reload_agents 后新 Agent 可用 | 20.90s | ✅ |
| REAL-011 | `TestAgentRealLLM_REAL_011` | Concurrency | 串行处理：同一对话消息串行执行 | 12.29s | ✅ |
| REAL-012 | `TestAgentRealLLM_REAL_012` | Concurrency | 幂等性：相同 MessageID 不重复执行 | 0.56s | ✅ |
| REAL-013 | `TestAgentRealLLM_REAL_013` | Integration | 完整流程：发送→流式→持久化→同步 | 16.44s | ✅ |
| REAL-014 | `TestAgentRealLLM_REAL_014` | Basic | 人机无关：人类间消息不受 Agent 影响 | 1.76s | ✅ |

### REAL-009 Flaky 详情

- **现象**：WebSocket recv 超时（52.92s），等待 agent_status ephemeral update
- **原因**：LLM API 响应慢，agent 处理延迟超过 WebSocket 读超时
- **分类**：网络/API 延迟导致的 flaky，非代码缺陷
- **处理**：已标记 "flaky, accepted"，retryRealLLM 重试机制可覆盖

---

## 覆盖率矩阵

| 维度 | Mock | Real LLM | 评价 |
|------|:----:|:--------:|------|
| **正常路径** | | | |
| 基本对话 | ✅ 5 | ✅ 2 | 充分 |
| 流式输出 | ✅ 6 | ✅ 2 | 充分 |
| 多轮上下文 | ✅ 5 | ✅ 1 | 充分 |
| 配置热更新 | ✅ 4 | ✅ 1 | 充分 |
| **错误路径** | | | |
| LLM API 错误 | ✅ 6 | ✅ 1 | 基本覆盖 |
| 无效配置 | ✅ | ✗ | mock 覆盖 |
| **并发** | | | |
| 串行处理 | ✅ 7 | ✅ 1 | 充分 |
| 幂等性 | ✅ | ✅ 1 | 充分 |
| **Agent 高级功能** | | | |
| Sub-agent | ✅ 4 | ✗ | 故意跳过（非确定性） |
| 工具调用 | ✅ 6 | ✗ | 故意跳过（非确定性） |
| HITL 中断/恢复 | ✅ 6 | ✗ | 故意跳过（非确定性） |
| Middleware | ✅ 5 | ✗ | mock 覆盖 |
| **极端场景** | | | |
| 超长输入 (>10K) | ✗ | ✗ | ❌ 未覆盖 |
| 空消息 | ✗ | ✗ | ❌ 未覆盖 |
| 特殊字符/Unicode | ✗ | ✗ | ❌ 未覆盖 |
| 消息 burst | ✗ | ✗ | ❌ 未覆盖 |
| **弱网/网络异常** | | | |
| LLM API 超时 | ✗ | ✗ | ❌ 未覆盖 |
| 连接中途断连 | ✗ | ✗ | ❌ 未覆盖 |
| API 限流 (429) | ✗ | ✗ | ❌ 未覆盖 |
| 高延迟 | ✗ | ✗ | ❌ 未覆盖 |

### 缺口优先级

| 优先级 | 缺口 | 建议 |
|--------|------|------|
| 🔴 高 | 弱网/网络异常 | 新增 `agent_weaknet_test.go`，内联 mock LLM 模拟超时/断连 |
| 🟡 中 | 极端输入场景 | 在 mock 测试中补充超长/空/特殊字符/burst |
| 🟢 低 | Real LLM 工具调用 | 非确定性太高，mock 已覆盖，可选 |

---

## 运行方式

```bash
# Quick 模式：59 个 mock 测试，~60s，零外部依赖
go test ./internal/e2e/ -run "^TestAgent" -count=1 -timeout 120s

# Full 模式：14 个 real LLM 测试，~160s，需要 .env
source .env && go test -tags real_llm ./internal/e2e/ -run "^TestAgentRealLLM" -count=1 -timeout 600s
```

## 相关文档

- 产品决策：`docs/decisions/PRODUCT_DECISIONS.md`（D-088/D-089/D-090）
- 测试场景文档：`docs/plans/2026-07-11-agent-e2e-test-scenarios.md`
- 客户端协议：`.claude/skills/xyncra-client-usage/SKILL.md` Server Protocol 章节
