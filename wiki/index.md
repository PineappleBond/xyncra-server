---
last_updated: 2026-07-17
---

# Xyncra Server Wiki

Xyncra 项目的多视角知识库。每个视角独立成库，包含索引和详细话题文档。

## 视角列表

| 视角 | 关注点 | 目标读者 |
|------|--------|----------|
| [架构](architecture/index.md) | 系统架构决策、组件关系、数据流、协议设计 | 架构师、全体开发者 |
| [开发](development/index.md) | 编码规范、目录结构、开发环境、可替代性 | 开发者 |
| [测试](testing/index.md) | 测试策略、E2E 测试、手动测试、CLI 测试工具 | 测试人员、开发者 |
| [接入者](onboarding/index.md) | 快速开始、配置说明、API 参考、Client 使用 | 外部接入者、新用户 |
| [文档管理](docs-management/index.md) | Wiki 规范、文档编写指南、README 维护 | 文档维护者 |
| [Skill](skills/index.md) | Vibe Coding 指南、Claude Code Command、Agent 开发 | AI 辅助开发者 |
| [运维/DevOps](devops/index.md) | 部署拓扑、环境配置、CI/CD、备份恢复 | 运维人员、开发者 |
| [可观测性](observability/index.md) | 日志规范、Metrics、链路追踪、健康检查 | 开发者、SRE |
| [依赖/生态](dependencies/index.md) | 第三方库选型、Licensing、升级策略 | 架构师、开发者 |

## 文档库规范

- **索引 + 详细说明书**：每个视角包含 `index.md`（索引）和若干主题文档
- **单主题拆分**：一个文档只负责一个主题，过大的文档应拆分
- **文档语言**：中文（项目主要语言）
- **文件路径**：`wiki/<视角>/<topic>.md`
