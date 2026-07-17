---
last_updated: 2026-07-17
---

# 架构视角

关注 Xyncra 系统的整体架构设计、组件关系、数据流和关键架构决策。

## 主题文档

| 文档 | 说明 |
|------|------|
| [系统架构概览](system-architecture.md) | 整体架构、分层设计、核心组件 |
| [协议设计](protocol-design.md) | WebSocket 协议、3 层信封、RPC 方法 |
| [数据流](data-flow.md) | 消息收发流程、状态同步、补发机制 |
| [组件关系](component-relationships.md) | Server / Store / MQ / Agent 依赖与交互 |
| [架构决策记录](design-decisions.md) | 关键架构决策（ADR） |

## 核心原则

1. **松散耦合**：各层通过接口依赖，实现可替换
2. **先落库后处理**：消息先持久化再异步处理
3. **离线优先**：Client 本地 DB 优先，按需拉取
4. **可观测性驱动**：每个关键路径留有日志和 Metrics
