# 依赖/生态视角

关注第三方依赖的管理策略，防止依赖腐化。

## 主题文档

| 文档 | 说明 |
|------|------|
| [依赖选型理由](dependency-rationale.md) | 为什么选这个库不选那个 |
| [Licensing](licensing.md) | 第三方库 License 合规性 |
| [升级策略](upgrade-strategy.md) | 依赖版本管理、升级流程 |
| [Vendor 管理](vendor-management.md) | Go Vendor 管理策略 |

## 核心原则

1. **最少依赖**：能不引入就不引入
2. **选型有依据**：每个依赖必须有书面选型理由
3. **License 合规**：所有依赖必须与项目 License 兼容
4. **主动升级**：定期审查并升级依赖版本
