# Skill 清单

> last_updated: 2026-07-17

## 项目已有的 Claude Code Skills

所有 Skill 文件位于 `.claude/skills/` 目录下。每个 Skill 是一个独立的 Markdown 文件，大多包含 `name` 和 `description` 元数据用于自动触发（`xyncra-client-usage` 除外，其未使用 YAML frontmatter）。

### Eino 框架 Skills（Go AI Agent 框架）

| Skill | 描述 | 触发场景 |
|-------|------|----------|
| [eino-guide](../../.claude/skills/eino-guide/SKILL.md) | Eino 框架概述、概念导航、架构说明 | 询问 Eino 是什么、如何开始、架构相关问题时 |
| [eino-component](../../.claude/skills/eino-component/SKILL.md) | 组件选型、配置和使用：ChatModel、Embedding、Retriever、Indexer、Tool、Document、Prompt | 需要配置或选择 Eino 组件时 |
| [eino-compose](../../.claude/skills/eino-compose/SKILL.md) | Graph、Chain、Workflow 编排：DAG、流式处理、分支并行、检查点 | 构建多步骤推理管道时 |
| [eino-agent](../../.claude/skills/eino-agent/SKILL.md) | ADK Agent 构建：ChatModelAgent、DeepAgent、TurnLoop、Middleware、Runner | 创建或修改 AI Agent 时 |

### Xyncra 项目特定 Skills

| Skill | 描述 | 触发场景 |
|-------|------|----------|
| [xyncra-task-planner](../../.claude/skills/xyncra-task-planner/SKILL.md) | 任务规划器：将粗略需求转化为详细执行提示词 | 需要规划复杂功能实现时 |
| [xyncra-manual-test](../../.claude/skills/xyncra-manual-test/SKILL.md) | 手动 E2E 测试文档生成：包含 Mermaid 流程图和 Shell 命令 | 需要编写端到端测试场景时 |
| [xyncra-client-usage](../../.claude/skills/xyncra-client-usage/SKILL.md) | 描述 Xyncra Client 的对外 SDK/API 使用方式 | 开发客户端集成或 SDK 使用时 |

## Skill 使用方式

### 自动触发

当你的描述匹配 Skill 文件中的 `description` 字段时，AI 会自动加载对应的 Skill 指令。例如：

- 说"创建一个新的 AI Agent" → 自动加载 `eino-agent`
- 说"写一个 E2E 测试" → 自动加载 `xyncra-manual-test`

### 手动加载

你也可以在对话中显式引用：

```
请在执行之前先加载 xyncra-task-planner Skill。
```

### 加载效果

Skill 加载后，AI 会获得：
1. 该领域的完整上下文和最佳实践
2. 代码示例和配置模板
3. 常见陷阱和注意事项
4. 相关文件路径参考

## Skill 的元数据格式

每个 Skill 以 YAML frontmatter 开头：

```yaml
---
name: my-skill
description: 简短描述，用于自动触发匹配
---
```

- `name`：全局唯一，推荐用 `project-` 前缀区分
- `description`：用于自动触发匹配，描述该 Skill 适用的场景

## 自定义 Command 体系

除了 Skill 文件外，项目还定义了一系列快捷 Command（当前通过 AGENTS.md 或工具上下文实现）。这些 Command 是更轻量的操作入口。

参见 [Claude Code Commands](claude-commands.md) 了解完整 Command 列表。

## 创建新 Skill

### 流程

1. **识别模式**：确认该操作在项目中重复出现
2. **参考模板**：查看现有 Skill 的格式（如 `eino-agent/SKILL.md`）
3. **编写指令**：
   - YAML frontmatter（name + description）
   - 背景说明（该领域的概念和术语）
   - 操作步骤（清晰的步骤指南）
   - 代码示例（可复用的模板）
   - 注意事项（已知陷阱）
4. **保存文件**：放入 `.claude/skills/<name>/SKILL.md`
5. **测试触发**：用相关描述测试是否能正确加载

### 最佳实践

- **单一职责**：一个 Skill 只做一件事
- **最小上下文**：提供刚好足够的信息，不要冗余
- **可测试**：Skill 给出的示例应可实际运行
- **保持更新**：代码库变更后同步更新 Skill

### 示例结构

```markdown
---
name: my-new-skill
description: 这个 Skill 做什么用，触发词包括 A、B、C
---

# 标题

## 背景
为什么需要这个 Skill

## 步骤
1. 第一步
2. 第二步

## 代码示例
```go
// 示例代码
```

## 注意事项
- 已知问题
- 常见错误
```

## Skill 维护原则

1. **定期审查**：每季度检查 Skill 是否仍然适用
2. **废弃标记**：不再适用的 Skill 在文件名加 `.deprecated` 后缀
3. **版本关联**：注明适用的框架或工具版本
4. **删除谨慎**：不确定是否还有用时，保留但标记为"可能过时"

## Skill 文件约定

### 命名规范

- Skill 目录名：`<skill-name>`（小写字母，连字符分隔）
- Skill 文件名：`SKILL.md`
- 目录放在 `.claude/skills/` 下

### 目录结构

```
.claude/skills/
├── eino-agent/
│   └── SKILL.md
├── eino-component/
│   └── SKILL.md
├── xyncra-task-planner/
│   └── SKILL.md
└── my-new-skill/
    └── SKILL.md
```

### 元数据规范

```yaml
---
name: xyncra-task-planner
description: |
  Xyncra 任务规划器 — 将粗略的开发任务转化为可直接执行的详细提示词。
  分析代码上下文、调度子代理从多角度审查方案、识别设计决策、记录产品决策，
  最终输出一份自包含的执行提示词，保存到文件供用户在新窗口运行。
---
```

`description` 的编写规范：
- 以动词或动作描述开头
- 2-4 句话说明功能和触发场景
- 包含关键触发词，便于 AI 自动匹配
- 避免过于宽泛的描述导致误触发

## Skill 生命周期

### 创建阶段

1. **需求识别**：发现重复性工作模式
2. **初步编写**：生成核心指令
3. **试用验证**：在实际任务中使用
4. **优化调整**：根据使用反馈完善

### 使用阶段

- 持续使用，AI 自动或手动触发
- 根据框架/工具版本更新内容
- 记录常见问题和解决方案

### 废弃阶段

- 如果 Skill 不再适用（如工具版本出现破坏性变更）
- 标记为废弃，但保留文件供参考（加 `.deprecated` 后缀）
- 创建新的 Skill 替代旧功能

## Skill 质量评估

### 评估维度

| 维度 | 说明 | 评估方式 |
|------|------|----------|
| 触发准确率 | 描述是否能准确触发 Skill | 观察自动触发频率 |
| 内容完整性 | 是否覆盖了该领域的核心场景 | 用户反馈 |
| 指令清晰度 | 操作步骤是否明确可执行 | 测试执行成功率 |
| 代码质量 | 示例代码是否可运行 | 执行验证 |
| 更新时效性 | 是否与最新代码同步 | 定期检查 |

### 反馈指标

- Skill 在对话中被手动请求加载的次数（说明自动触发不够准确）
- 用户在使用过程中的纠正次数（说明指令不够清晰）
- Skill 产生的结果需要大幅修改的次数（说明内容质量不够高）

## Project 级别的 Skill 与通用 Skill

### 通用 Skill（GStack Skills）

由 GStack 框架管理，存放在 `~/.agents/skills/`，覆盖通用开发场景：
- `docker-expert`：Docker 容器化
- `golang-testing`：Go 测试模式
- `frontend-design`：前端设计
- 更多可在 `~/.agents/skills/` 中查看

### 项目 Skill

存放在项目 `.claude/skills/` 目录下：
- Xyncra 项目的特定知识、配置和流程
- 跟随项目版本控制
- 新开发者 clone 项目后即可使用

### 如何选择

```
问题是通用的（Docker、Go 测试）？
├── 是 → 使用通用 Skill（~/.agents/skills/）
└── 否（项目特定） → 使用项目 Skill（.claude/skills/）
```

## Skill 依赖管理

Skill 可以引用其他 Skill 或项目文件：

```markdown
<!-- 引用项目文档 -->
请参考 [Agent 开发指南](../../wiki/skills/agent-development.md)

<!-- 引用其他 Skill -->
在编写 Agent 时，请先加载 eino-agent Skill。

<!-- 引用代码 -->
参考 `internal/agent/executor.go` 中的 `NewAgentExecutor` 实现。
```

Skill 内部不应该包含大量重复内容，而是通过引用将上下文链接到其他文档。
