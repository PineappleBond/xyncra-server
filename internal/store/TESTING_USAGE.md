# Store 包测试使用说明

## 概述

`store` 包的测试覆盖三种数据库：**PostgreSQL**、**MySQL** 和 **SQLite**。

- SQLite 使用内存数据库（`github.com/glebarez/sqlite`，无 CGO）
- PostgreSQL 和 MySQL 使用本地 Docker 容器

## 前置条件

### Docker 容器

测试需要以下 Docker 容器运行：

**PostgreSQL**（端口 5432）：
```bash
docker run -d --name test-postgres \
  -e POSTGRES_USER=sequify \
  -e POSTGRES_PASSWORD=sequify \
  -e POSTGRES_DB=sequify \
  -p 5432:5432 \
  postgres:16-alpine
```

**MySQL**（端口 3306）：
```bash
docker run -d --name test-mysql \
  -e MYSQL_ROOT_PASSWORD=root \
  -e MYSQL_USER=sequify \
  -e MYSQL_PASSWORD=sequify \
  -e MYSQL_DATABASE=sequify \
  -p 3306:3306 \
  mysql:8
```

如果容器已经存在，可以先删除再重建：
```bash
docker rm -f test-postgres test-mysql
```

### 等待容器就绪

容器启动后需要等待数据库完全就绪（通常 3-5 秒）：
```bash
# 检查 PostgreSQL 是否就绪
docker exec test-postgres pg_isready

# 检查 MySQL 是否就绪
docker exec test-mysql mysqladmin ping -h localhost
```

## 运行测试

### 运行所有测试（三种数据库）
```bash
go test -v -count=1 ./internal/store/
```

### 只运行 SQLite 测试（无需 Docker）
```bash
go test -v -count=1 -run "SQLite" ./internal/store/
```

### 只运行 PostgreSQL 测试
```bash
go test -v -count=1 -run "PostgreSQL" ./internal/store/
```

### 只运行 MySQL 测试
```bash
go test -v -count=1 -run "MySQL" ./internal/store/
```

### 运行特定测试函数
```bash
go test -v -count=1 -run "TestSendMessage" ./internal/store/
go test -v -count=1 -run "TestConversationCRUD/PostgreSQL" ./internal/store/
```

## 测试覆盖

| 测试函数 | 覆盖功能 |
|---------|---------|
| `TestNewDatabase` | 数据库连接（无效驱动、成功连接） |
| `TestConversationCRUD` | 会话创建、查询、更新、软删除 |
| `TestConversationGetByUser` | 按用户查询会话列表（排序、分页） |
| `TestMessageCRUD` | 消息创建、查询、唯一约束、软删除 |
| `TestMessageListByConversation` | 会话消息列表（增量查询） |
| `TestUserUpdateCRUD` | 用户更新批量创建、增量查询、最新序号 |
| `TestSendMessage` | 事务操作（INSERT Message + Batch INSERT UserUpdate + UPDATE Conversation） |
| `TestTransactionCommit` | 事务提交 |
| `TestTransactionRollback` | 事务回滚 |
| `TestBeginTx` | 手动事务（BeginTx / Commit） |
| `TestAutoMigrate` | 自动迁移幂等性 |

## 注意事项

- 如果 PostgreSQL 或 MySQL 容器不可用，对应的测试会自动 **skip**（不会失败）
- SQLite 测试使用独立内存数据库，无需外部依赖
- 每次测试前会自动执行 `AutoMigrate`，PostgreSQL/MySQL 测试前会清理数据
- 测试数据使用固定的时间点（2026-07-07），避免 MySQL strict mode 对零值时间的报错
