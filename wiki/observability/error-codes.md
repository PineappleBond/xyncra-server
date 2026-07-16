# 错误码体系

## 概述

Xyncra Server 定义了层次化的错误码体系，涵盖协议层、服务器内部和客户端错误，确保错误信息的结构化和可操作性。

## 错误码分类

```
错误码范围：
  -100 到 -199: 客户端错误（参数验证、资源未找到、重复）
  -200 到 -299: 权限错误（无权限、禁止访问）
  -300 到 -399: 服务器错误（内部错误、服务不可用）
  -400 到 -499: 客户端内部错误（连接、超时、同步）
```

## 错误码表

### 协议层错误码（pkg/protocol）

定义在 `pkg/protocol/protocol.go`：

| 错误码 | 常量名 | HTTP 类比 | 说明 |
|--------|--------|-----------|------|
| `0` | `ResponseCodeOK` | 200 | 请求成功 |
| `-1` | `ResponseCodeError` | 500 | 通用错误 |
| `-100` | `ResponseCodeValidationError` | 400 | 参数验证失败 |
| `-101` | `ResponseCodeNotFound` | 404 | 资源未找到 |
| `-102` | `ResponseCodeDuplicate` | 409 | 资源重复 |
| `-200` | `ResponseCodePermissionDenied` | 403 | 权限不足 |
| `-201` | `ResponseCodeForbidden` | 403 | 禁止访问 |
| `-300` | `ResponseCodeInternalError` | 500 | 服务器内部错误 |
| `-301` | `ResponseCodeUnavailable` | 503 | 服务暂时不可用 |

### 服务器内部错误码（internal/server）

定义在 `internal/server/server.go`：

| 错误 | Go 类型 | 说明 |
|------|---------|------|
| `ErrServerNotStarted` | `errors.New("server: not started")` | 服务器未启动时执行操作 |
| `ErrServerAlreadyRunning` | `errors.New("server: already running")` | 重复启动服务 |
| `ErrServerStopped` | `errors.New("server: stopped")` | 服务器已停止 |
| `ErrConnectionNotFound` | `errors.New("server: connection not found")` | 请求的连接不存在 |
| `ErrMaxConnectionsExceeded` | `errors.New("server: max connections per user exceeded")` | 超过每用户连接上限 |

### WebSocket 服务器错误码（internal/server）

| 错误 | Go 类型 | 说明 |
|------|---------|------|
| `ErrWebSocketServerClosed` | `errors.New("websocket: server closed")` | 服务器已关闭 |
| `ErrAuthenticationFailed` | `errors.New("websocket: authentication failed")` | 认证失败 |
| `ErrDeviceOffline` | `errors.New("reverse_rpc: device is offline")` | 目标设备不在线 |

### 数据库存储错误码（internal/store）

定义在 `internal/store/errors.go`：

| 错误 | Go 类型 | 说明 |
|------|---------|------|
| `ErrNotFound` | `errors.New("store: record not found")` | 记录不存在 |
| `ErrDuplicateKey` | `errors.New("store: duplicate key")` | 唯一约束冲突 |
| `ErrForeignKeyViolation` | `errors.New("store: foreign key violation")` | 外键约束冲突 |
| `ErrConnectionFailed` | `errors.New("store: connection failed")` | 数据库连接失败 |
| `ErrContextDeadlineExceeded` | `errors.New("store: context deadline exceeded")` | 上下文超时 |
| `ErrConflict` | `errors.New("store: conflict")` | 资源状态冲突 |

### 客户端错误码（pkg/client/options.go）

| 错误码 | 常量名 | 说明 |
|--------|--------|------|
| `-400` | `ConnectionError` | WebSocket 连接失败 |
| `-401` | `TimeoutError` | RPC 调用超时 |
| `-402` | `SyncError` | 数据同步失败 |

## HandlerError 使用

### 创建带错误码的错误

```go
// 参数验证失败
return protocol.NewValidationError("message_id is required")

// 资源未找到
return protocol.NewNotFoundError("conversation not found")

// 重复资源
return protocol.NewDuplicateError("device already registered")

// 权限不足
return protocol.NewPermissionDeniedError("insufficient permissions")

// 内部错误
return protocol.NewInternalError(err)
```

### 错误码在响应中的格式

```json
{
    "id": "req-001",
    "code": -100,
    "msg": "message_id is required",
    "data": null
}
```

### 错误包装

```go
// 包装底层错误
if err := store.Save(ctx, record); err != nil {
    return protocol.WrapError(protocol.ResponseCodeInternalError, err)
}

// 解包
var handlerErr *protocol.HandlerError
if errors.As(err, &handlerErr) {
    // 处理特定错误码
    switch handlerErr.Code {
    case protocol.ResponseCodeValidationError:
        // 返回 400
    case protocol.ResponseCodeNotFound:
        // 返回 404
    }
}
```

## 数据库错误分类

`internal/store/errors.go` 中的 `classifyError` 函数将 GORM 和驱动级别的错误映射为存储层错误：

```go
func classifyError(err error) error {
    // GORM 记录未找到 → ErrNotFound
    // 唯一约束冲突 → ErrDuplicateKey
    // 外键约束冲突 → ErrForeignKeyViolation
    // 连接异常 → ErrConnectionFailed
    // 超时 → ErrContextDeadlineExceeded
}
```

支持的数据库方言：
- PostgreSQL 错误模式
- SQLite 错误模式
- MySQL 错误模式

## 错误处理最佳实践

### 1. 尽早失败

```go
// 不好：深层嵌套的错误检查
if result, err := doSomething(); err == nil {
    if result, err := doNext(result); err == nil {
        // ...
    }
}

// 好：尽早返回错误
result, err := doSomething()
if err != nil {
    return nil, fmt.Errorf("step 1 failed: %w", err)
}
result2, err := doNext(result)
if err != nil {
    return nil, fmt.Errorf("step 2 failed: %w", err)
}
```

### 2. 错误包装链

```go
// 包装错误上下文
func (s *Store) SendMessage(ctx context.Context, msg *model.Message) error {
    if err := s.db.Create(msg).Error; err != nil {
        return fmt.Errorf("store: send message: %w", classifyError(err))
    }
    return nil
}
```

### 3. 敏感信息过滤

```go
// 不好：暴露内部错误详情
return protocol.NewInternalError(err) // 客户端可能看到内部错误

// 好：记录内部错误，返回通用消息
logger.Error("database write failed", "error", err)
return protocol.NewHandlerError(protocol.ResponseCodeInternalError, "internal error")
```

### 4. 错误日志级别

```go
// 预期中的错误用 Info
if errors.Is(err, store.ErrNotFound) {
    logger.Info("conversation not found", "conversation_id", id)
    return protocol.NewNotFoundError("conversation not found")
}

// 未预期的错误用 Error
if err != nil && !errors.Is(err, store.ErrNotFound) {
    logger.Error("unexpected database error", "error", err)
    return protocol.NewInternalError(err)
}
```

### 5. 错误恢复

```go
// 连接失败后重试
for i := 0; i < 3; i++ {
    err := store.Ping(ctx)
    if err == nil {
        return nil
    }
    if errors.Is(err, store.ErrConnectionFailed) {
        time.Sleep(time.Second * time.Duration(i+1))
        continue
    }
    return err
}
```

## 错误码使用规范

1. **客户端错误（-100 系列）**：返回给调用者，调用者可据此调整请求
2. **权限错误（-200 系列）**：表明调用者无权执行操作
3. **服务器错误（-300 系列）**：表明服务器端出现问题，重试可能有效
4. **内部 sentinel 错误**：不返回给客户端，用于内部错误处理和重试逻辑
5. **数据库错误**：通过 `classifyError` 统一分类后再处理
6. **客户端内部错误（-400 系列）**：仅在客户端内部使用

## 错误码扩展指南

添加新的错误码：
1. 确定错误码范围（客户端/权限/服务器）
2. 在对应文件添加常量定义
3. 创建便捷构造函数（如 `NewValidationError`）
4. 添加 HandlerError 类型便于包装
5. 更新本文档的错误码表
6. 更新客户端文档（如果暴露给客户端）
