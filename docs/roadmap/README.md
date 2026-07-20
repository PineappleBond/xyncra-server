# Xyncra Server 产品路线图

**日期**: 2026-07-20
**状态**: Draft
**作者**: Xyncra Team

---

## 愿景

**让任何前端应用都能被 AI Agent 操作。** 开发者只需要声明式描述自己的 UI，SDK 自动生成 Agent 可调用的工具，Agent 能"看见"页面状态并可靠地执行多步骤操作。

## 当前状态

| 模块 | 状态 | 说明 |
|------|------|------|
| 服务端（Go） | ✅ Phase 1-8 完成 | 分布式消息后端、Agent 运行时、HITL、子 Agent、MCP |
| 客户端 SDK | ✅ 基础完成 | 4 个包：protocol / client-core / client-web / client-cli |
| 函数注册 | ✅ 链路打通 | `system.register_functions` + `DynamicToolProvider` + ReverseRPC |
| Demo | ✅ 最小可用 | Ant Design Pro + 悬浮 AI 助手 + 4 个手动注册函数 |
| UI Schema | ❌ 不存在 | 无声明式 UI 描述，函数全部手写 |
| Agent UI 感知 | ❌ 不存在 | Agent 不知道当前页面、组件状态 |
| 多页面流程 | ❌ 不存在 | 无跨页面操作编排 |

## 核心矛盾

当前模式：开发者手动为每个 UI 操作编写一个函数 → **50 个页面 × 10 个操作 = 500 个函数，不可行。**

目标模式：开发者声明式描述 UI 结构 → SDK 自动生成工具 → Agent 能理解并操作 → **50 个 schema = 500+ 个工具，一劳永逸。**

---

## 里程碑总览

```
M1: UI Schema 引擎          ← 核心突破，解决函数爆炸问题
  ↓
M2: Agent UI Context 注入   ← 让 Agent 能"看见" UI
  ↓
M3: 多页面流程引擎          ← 跨页面操作编排
  ↓
M4: 开发者工具链            ← 降低接入成本
  ↓
M5: 生产级增强              ← 可靠性、安全性、性能
```

---

## M1: UI Schema 引擎

**理想状态**：开发者用声明式 schema 描述页面结构，SDK 自动生成 Agent 可调用的工具。一个 CRUD 页面的开发者只需写一个 schema 声明，Agent 自动获得 10+ 个可调用工具。

| Phase | 名称 | 目标 |
|-------|------|------|
| M1-P1 | Schema 类型系统 | 定义 `PageSchema`、`ComponentSchema`、`ActionSchema` 核心类型 |
| M1-P2 | Schema → Tool 转换器 | 将 schema 自动转换为 `FunctionInfo[]` 并注册到服务端 |
| M1-P3 | React SchemaProvider | 开发者传入 schema，组件自动处理注册/反注册/路由感知 |
| M1-P4 | 组件状态采集器 | 自动采集组件当前状态（表格数据、表单值、筛选条件） |
| M1-P5 | 内置组件库 | 预置 Table / Form / List / Modal 等常见组件的 schema 模板 |

**详情文档**: [M1-ui-schema-engine.md](./M1-ui-schema-engine.md)

---

## M2: Agent UI Context 注入

**理想状态**：Agent 能实时感知客户端 UI 状态——用户在哪个页面、表格里有什么数据、表单填了什么。Agent 基于 UI 上下文做决策，而不是盲目调用工具。

| Phase | 名称 | 目标 |
|-------|------|------|
| M2-P1 | UI State 协议扩展 | 新增 `system.report_ui_state` RPC，客户端上报结构化 UI 状态 |
| M2-P2 | Agent System Prompt 注入 | 服务端将 UI context 注入 Agent 的 system prompt |
| M2-P3 | 状态变化驱动更新 | 页面切换、数据变化时自动重新上报，Agent 始终看到最新状态 |
| M2-P4 | Agent 决策链路优化 | Agent 先读 context → 选 tool → 执行 → 确认结果 → 循环 |

**详情文档**: [M2-agent-ui-context.md](./M2-agent-ui-context.md)

---

## M3: 多页面流程引擎

**理想状态**：Agent 能执行跨页面的多步骤操作。用户说「帮我下单 iPhone」，Agent 自动完成：搜索商品 → 加购物车 → 确认订单 → 提交，任何步骤失败都能处理。

| Phase | 名称 | 目标 |
|-------|------|------|
| M3-P1 | Flow Schema 定义 | 定义多步骤流程的声明式描述格式 |
| M3-P2 | 页面就绪检测 | 导航后等待目标页面 schema 注册完成，超时重试 |
| M3-P3 | 流程执行器 | Agent 有 `start_flow` 工具，自动编排导航→执行→检查→下一步 |
| M3-P4 | 错误恢复策略 | 步骤失败时重试 / 回退 / 报告用户，支持手动干预 |

**详情文档**: [M3-multi-page-flow.md](./M3-multi-page-flow.md)

---

## M4: 开发者工具链

**理想状态**：新开发者 30 分钟内完成一个页面的 Agent 接入。有 CLI 工具自动生成初始 schema，有可视化编辑器调试，有调试面板实时查看 Agent 行为。

| Phase | 名称 | 目标 |
|-------|------|------|
| M4-P1 | Schema 生成器 CLI | 扫描现有代码自动生成初始 schema |
| M4-P2 | Schema 调试面板 | 浏览器内浮层，实时查看注册的 functions、Agent 调用日志 |
| M4-P3 | 模板和脚手架 | 常见页面类型（CRUD、详情、仪表盘）的 schema 模板 |
| M4-P4 | 文档站 | 接入指南、API 参考、最佳实践 |

**详情文档**: [M4-developer-toolchain.md](./M4-developer-toolchain.md)

---

## M5: 生产级增强

**理想状态**：系统在生产环境稳定运行。Agent 操作有权限控制和审计日志，函数分层注册避免 tool 爆炸，多框架支持覆盖主流生态。

| Phase | 名称 | 目标 |
|-------|------|------|
| M5-P1 | 操作权限系统 | readonly / write / dangerous 三级权限，Agent 自动遵守 |
| M5-P2 | 操作审计日志 | 每次操作记录 who/when/what/result，可回溯 |
| M5-P3 | 函数分层注册 | 按页面路由 + 场景过滤 tools，避免 Agent tool 爆炸 |
| M5-P4 | 性能优化 | 批量注册、增量更新、UI state 节流、Schema 缓存 |
| M5-P5 | 多框架支持 | Vue 3 adapter / Svelte adapter / 纯 JS adapter |

**详情文档**: [M5-production-hardening.md](./M5-production-hardening.md)

---

## 关键风险

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| Schema 表达力不足 | 开发者无法描述复杂 UI | M1-P5 内置组件库 + 扩展点 |
| Agent Tool 爆炸（>30 个） | LLM 选 tool 准确率下降 | M5-P3 分层注册 + 运行时过滤 |
| UI 状态与 Agent 认知不一致 | Agent 基于过期信息操作 | M2-P3 状态变化驱动更新 |
| 多步骤操作中途失败 | 用户体验断裂 | M3-P4 错误恢复 + HITL 介入 |
| 安全性（Agent 操作危险动作） | 数据损失 | M5-P1 权限系统 + M1 已有 confirm 机制 |

---

## 文档索引

| 文档 | 内容 |
|------|------|
| [README.md](./README.md)（本文件） | 里程碑索引和总览 |
| [M1-ui-schema-engine.md](./M1-ui-schema-engine.md) | M1 UI Schema 引擎详细设计 |
| [M2-agent-ui-context.md](./M2-agent-ui-context.md) | M2 Agent UI Context 注入详细设计 |
| [M3-multi-page-flow.md](./M3-multi-page-flow.md) | M3 多页面流程引擎详细设计 |
| [M4-developer-toolchain.md](./M4-developer-toolchain.md) | M4 开发者工具链详细设计 |
| [M5-production-hardening.md](./M5-production-hardening.md) | M5 生产级增强详细设计 |
