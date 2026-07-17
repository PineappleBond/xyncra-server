---
last_updated: 2026-07-17
---

# 运维/DevOps 视角

关注生产环境的部署、运维、监控和持续交付。

## 主题文档

| 文档 | 说明 |
|------|------|
| [部署拓扑](deployment-topology.md) | 部署架构、网络拓扑、组件分布 |
| [环境配置](environment-config.md) | 环境变量、配置文件、密钥管理 |
| [监控告警](monitoring-alerting.md) | 监控指标、告警规则、Dashboard |
| [日志采集](logging.md) | 结构化日志、日志采集、聚合、查询 |
| [性能分析](profiling.md) | pprof 和 Pyroscope 持续性能分析 |
| [告警规则](alerting.md) | Prometheus 告警规则和 AlertManager 配置 |
| [备份恢复](backup-recovery.md) | 数据备份策略、灾难恢复 |
| [CI/CD](ci-cd.md) | 持续集成和持续部署流水线 |

## 核心原则

1. **不可变基础设施**：环境配置全部代码化
2. **可重现部署**：任何环境应可一键重建
3. **防御性运维**：变更必须有回滚方案
4. **安全默认值**：默认配置遵循安全最佳实践
