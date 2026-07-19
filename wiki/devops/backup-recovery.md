---
last_updated: 2026-07-17
---

# 备份恢复

## 概述

本文档描述 Xyncra Server 的数据备份策略和灾难恢复方案。确保在数据丢失或系统故障时能快速恢复服务。

## 需要备份的数据

| 数据类型 | 存储位置 | 重要程度 | 说明 |
|----------|----------|----------|------|
| 数据库 | SQLite 文件 / PostgreSQL / MySQL | 关键 | 消息、会话、用户数据 |
| Redis 数据 | Redis RDB/AOF | 重要 | 连接状态、Agent Checkpoint |
| Agent 配置 | `agents/*.md` | 次要 | 可通过 Git 恢复 |
| 环境配置 | `.env.*` 文件 | 重要 | API Key 等敏感配置 |

### 数据库内容

数据库包含：

| 表 | 说明 | 数据量预估 |
|----|------|------------|
| `messages` | 消息记录 | 增长较快 |
| `conversations` | 会话元数据 | 中等 |
| `user_updates` | 用户更新记录 | 增长较快 |
| `questions` | HITL 问题记录 | 较少 |

### Redis 数据内容

Redis 包含：

| Key 类型 | 说明 | 持久化要求 |
|----------|------|------------|
| `asynq:*` | 消息队列任务 | 可重建 |
| `connection:*` | WebSocket 连接状态 | 中等（重启可重建）|
| `agent:checkpoint:*` | Agent 检查点 | 重要（HITL 恢复） |
| `agent:idempotency:*` | 幂等性 Key | 中等 |
| `agent:lock:*` | 会话锁 | 可重建 |
| `pending:*` | 待处理请求 | 中等 |

## 备份策略

### 数据库备份

#### SQLite 备份

```bash
#!/bin/bash
# 每日 SQLite 备份脚本

BACKUP_DIR="/backup/sqlite"
DB_PATH="/data/xyncra.db"
DATE=$(date +%Y%m%d_%H%M%S)
RETENTION_DAYS=30

mkdir -p $BACKUP_DIR

# 方式一：文件拷贝（需服务停止或使用 WAL 模式）
sqlite3 $DB_PATH ".backup $BACKUP_DIR/xyncra_$DATE.db"

# 方式二：VACUUM INTO（SQLite 3.27+）
sqlite3 $DB_PATH "VACUUM INTO '$BACKUP_DIR/xyncra_$DATE.db'"

# 压缩
gzip $BACKUP_DIR/xyncra_$DATE.db

# 清理过期备份
find $BACKUP_DIR -name "xyncra_*.db.gz" -mtime +$RETENTION_DAYS -delete
```

#### PostgreSQL 备份

```bash
# 逻辑备份
pg_dump -h localhost -U xyncra -d xyncra -F c -f /backup/pg/xyncra_$(date +%Y%m%d).dump

# 物理备份（归档模式）
pg_basebackup -h localhost -U xyncra -D /backup/pg/base_$(date +%Y%m%d) -X stream
```

#### MySQL 备份

```bash
# 逻辑备份
mysqldump -h localhost -u xyncra -p xyncra > /backup/mysql/xyncra_$(date +%Y%m%d).sql

# 物理备份（XtraBackup）
xtrabackup --backup --target-dir=/backup/mysql/$(date +%Y%m%d)
```

### Redis 数据备份

```bash
#!/bin/bash
# Redis 备份脚本

BACKUP_DIR="/backup/redis"
DATE=$(date +%Y%m%d)
RETENTION_DAYS=7

mkdir -p $BACKUP_DIR

# 触发持久化
redis-cli SAVE

# 复制 RDB 文件
cp /data/dump.rdb $BACKUP_DIR/dump_$DATE.rdb

# 压缩
gzip $BACKUP_DIR/dump_$DATE.rdb

# 清理过期备份
find $BACKUP_DIR -name "dump_*.rdb.gz" -mtime +$RETENTION_DAYS -delete
```

### 配置文件备份

```bash
# Agent 配置和 .env 模板通过版本控制管理
# ⚠️ 不要提交真实 .env 文件（可能包含 API Key），仅提交 .env.*.example 模板
git add agents/*.md .env.*.example
git commit -m "backup: agent config and env templates"
```

## 备份频率与保留

| 数据类型 | 备份频率 | 保留时间 | 备份类型 |
|----------|----------|----------|----------|
| 数据库 | 每日 | 30 天 | 全量备份 |
| Redis | 每 6 小时 | 7 天 | RDB 快照 |
| Agent 配置 | 每次变更 | 永久（Git） | 增量 |
| 环境配置 | 每次变更 | 永久（Git + 加密） | 全量 |

## 恢复流程

### 场景一：数据库损坏

```bash
# 1. 停止服务
make docker-down

# 2. 恢复数据库
# SQLite:
gunzip -c /backup/sqlite/xyncra_20240101.db.gz > /data/xyncra.db

# PostgreSQL:
pg_restore -h localhost -U xyncra -d xyncra -c /backup/pg/xyncra_20240101.dump

# 3. 启动服务
make docker-up

# 4. 验证数据
curl http://localhost:8080/health
# 确认消息和会话数据完整
```

### 场景二：Redis 数据丢失

```bash
# 1. 恢复 RDB 文件
gunzip -c /backup/redis/dump_20240101.rdb.gz > /data/dump.rdb

# 2. 重启 Redis（会自动加载 RDB）
make docker-down
make docker-up

# 3. 验证
redis-cli ping
# 检查 key 是否恢复
redis-cli KEYS "agent:checkpoint:*"
```

### 场景三：完整灾难恢复

```bash
# 1. 准备环境
git clone <repository>
cd xyncra-server

# 2. 恢复配置
cp /secure/backup/.env .env

# 3. 恢复数据库
# （见场景一）

# 4. 恢复 Redis
# （见场景二）

# 5. 构建并启动
make docker-build
make docker-up

# 6. 验证
curl http://localhost:8080/health
# 运行 E2E 测试验证功能
make test-e2e
```

### 场景四：Agent 配置丢失

```bash
# Agent 配置存储在 Git 中，直接恢复
git checkout -- agents/
make docker-down
make docker-up
```

## 灾难恢复计划

### 故障等级定义

| 等级 | 描述 | 恢复时间目标（RTO） | 恢复点目标（RPO） |
|------|------|---------------------|--------------------|
| P0 | 服务完全不可用 | 1 小时 | 1 小时 |
| P1 | 主要功能受损 | 4 小时 | 6 小时 |
| P2 | 次要功能受损 | 24 小时 | 24 小时 |
| P3 | 非功能性故障 | 7 天 | 7 天 |

### 恢复优先级

1. **恢复数据库**：消息和会话数据是核心资产
2. **恢复 Redis**：确保 Agent 和消息队列可运行
3. **恢复服务**：启动 WebSocket 服务
4. **验证功能**：运行 E2E 测试确认
5. **通知用户**：告知用户服务已恢复

### 预防措施

1. **定期演练**：每季度执行一次恢复演练
2. **备份验证**：每月随机抽取一个备份进行恢复测试
3. **多地域备份**：关键备份应存储在不同地域
4. **权限控制**：备份文件应加密，仅限运维人员访问

## Docker 数据卷备份

```bash
# 备份 Docker 卷
docker run --rm -v xyncra-data:/data -v /backup:/backup alpine \
  tar czf /backup/xyncra-data-$(date +%Y%m%d).tar.gz -C /data .

# 恢复 Docker 卷
docker run --rm -v xyncra-data:/data -v /backup:/backup alpine \
  tar xzf /backup/xyncra-data-20240101.tar.gz -C /data
```

## 注意事项

1. **SQLite 并发限制**：SQLite 不支持并发写入，备份时应确保服务不进行写操作
2. **Redis 持久化**：生产环境应开启 AOF + RDB 双重持久化
3. **密钥保护**：备份文件中的 API Key 必须加密存储
4. **测试恢复**：定期测试备份的可恢复性，而不只是备份本身
5. **监控告警**：备份失败应有告警通知
