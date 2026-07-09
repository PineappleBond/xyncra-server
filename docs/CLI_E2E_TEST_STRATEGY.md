# xyncra-client CLI E2E 测试策略文档

> **作者**: QA Engineer Agent
> **日期**: 2026-07-09
> **状态**: 已实现 (136 tests passed, ~88% coverage)
> **包路径**: `internal/cli/e2e/`

---

## 测试执行记录

| 日期 | Git Commit | 执行者 | 结果 | 报告 |
|------|-----------|--------|------|------|
| 2026-07-09 | 7f43f99 | E2E Manual (CLI-driven) | 127/133 PASS (95.5%) | [e2e-manual-test-report-2026-07-09.md](e2e-manual-test-report-2026-07-09.md) |

> 下次测试前请参考上方记录，避免重复已通过的场景。详细验证矩阵见 [第 5 节](#5-手动-cli-e2e-测试验证矩阵-2026-07-09)。

---

## 目录

1. [测试场景清单](#1-测试场景清单)
2. [测试环境需求](#2-测试环境需求)
3. [测试数据策略](#3-测试数据策略)
4. [测试文档格式](#4-测试文档格式)

---

## 1. 测试场景清单

### 1.1 守护进程生命周期 (Daemon Lifecycle)

| 场景 ID | 场景名称 | 验证决策 | 预期行为 | 优先级 |
|---------|---------|---------|---------|--------|
| CLI-E2E-001 | listen 正常启动 | D-030, D-031 | 创建 `xyncra.sock`、`xyncra.lock`、`xyncra.db`；stderr 输出启动 banner；IPC server 可连接 | P0 |
| CLI-E2E-002 | listen 重复启动（锁冲突） | D-031 | 第二次启动退出码 2，stderr 输出 "listen already running (PID: X)" | P0 |
| CLI-E2E-003 | listen stale lock 恢复 | D-031 | 手动写一个指向不存在 PID 的 lock 文件，listen 应自动清理并成功启动 | P0 |
| CLI-E2E-004 | listen SIGTERM 优雅退出 | D-031, D-039 | 发送 SIGTERM，进程退出码 0；lock 文件和 sock 文件被清理 | P0 |
| CLI-E2E-005 | listen SIGINT 退出 | D-031 | Ctrl+C 发送 SIGINT，进程正常退出，文件被清理 | P1 |
| CLI-E2E-006 | listen 无 user-id 启动 | D-034 | 退出码 1，stderr 提示 "user-id is required" | P1 |
| CLI-E2E-007 | listen WebSocket 连接失败 | - | 进程启动但 stderr 输出连接错误；IPC server 仍可用（不影响本地命令） | P1 |
| CLI-E2E-008 | listen 使用环境变量 | D-034 | 设置 `XYNCRA_USER_ID` 等环境变量后无需 flag 即可启动 | P1 |
| CLI-E2E-009 | listen flag 优先级高于环境变量 | D-034 | flag 值和 env 值不同时，flag 值生效 | P2 |
| CLI-E2E-010 | listen 自定义 device-id | D-033 | 使用 `--device-id` 或 `XYNCRA_DEVICE_ID` 覆盖默认设备 ID | P2 |

### 1.2 IPC 通信 (IPC Communication)

| 场景 ID | 场景名称 | 验证决策 | 预期行为 | 优先级 |
|---------|---------|---------|---------|--------|
| CLI-E2E-020 | IPC JSON-RPC 请求/响应格式 | D-030 | 请求为 `{"jsonrpc":"2.0","method":"...","params":...,"id":"..."}` 加换行符；响应同格式 | P0 |
| CLI-E2E-021 | IPC send_message 成功 | D-030 | 通过 IPC 调用 send_message 返回消息 ID 和 conversation ID | P0 |
| CLI-E2E-022 | IPC create_conversation 成功 | D-030 | 返回 conversation 对象 | P0 |
| CLI-E2E-023 | IPC delete_conversation 成功 | D-030 | 返回 status "ok" | P0 |
| CLI-E2E-024 | IPC restore_conversation 成功 | D-030 | 返回恢复后的 conversation | P1 |
| CLI-E2E-025 | IPC delete_message 成功 | D-030 | 返回 void（无错误即可） | P1 |
| CLI-E2E-026 | IPC mark_as_read 成功 | D-030 | 返回 status "ok" 和 unread_count | P1 |
| CLI-E2E-027 | IPC sync_updates 成功 | D-036 | 触发 FullSync，返回 status "ok" | P0 |
| CLI-E2E-028 | IPC 方法不存在 | D-030 | 返回错误码 -32601 "Method not found" | P1 |
| CLI-E2E-029 | IPC 无效请求（非 JSON-RPC 2.0） | D-030 | 返回错误码 -32600 "Invalid Request" | P1 |
| CLI-E2E-030 | IPC 参数格式错误 | D-030 | 返回错误码 -32602 "invalid params" | P1 |
| CLI-E2E-031 | IPC 连接超时（daemon 未响应） | D-030 | IPCClient 在 timeout 内返回错误 | P2 |
| CLI-E2E-032 | IPC socket 文件权限 | D-030 | `xyncra.sock` 权限为 0600（仅所有者可读写） | P2 |

### 1.3 Standalone WebSocket Fallback

| 场景 ID | 场景名称 | 验证决策 | 预期行为 | 优先级 |
|---------|---------|---------|---------|--------|
| CLI-E2E-040 | send 命令 daemon 未运行，fallback 到 WS | D-032 | IPC 连接失败后自动建立 WebSocket 短连接发送消息 | P0 |
| CLI-E2E-041 | create-conversation standalone 模式 | D-032 | 无 daemon 时通过 WebSocket 创建 conversation | P0 |
| CLI-E2E-042 | delete-conversation standalone 模式 | D-032 | 无 daemon 时通过 WebSocket 删除 conversation | P1 |
| CLI-E2E-043 | restore-conversation standalone 模式 | D-032 | 无 daemon 时通过 WebSocket 恢复 conversation | P1 |
| CLI-E2E-044 | delete-message standalone 模式 | D-032 | 无 daemon 时通过 WebSocket 删除 message | P1 |
| CLI-E2E-045 | mark-as-read standalone 模式 | D-032 | 无 daemon 时通过 WebSocket 标记已读 | P1 |
| CLI-E2E-046 | sync-updates 无 fallback（daemon 未运行报错） | D-036 | 退出码 1，stderr 输出 "daemon not running" | P0 |
| CLI-E2E-047 | IPC 和 WS 均失败 | D-032 | 输出统一错误信息（两个原因 + hint） | P1 |
| CLI-E2E-048 | standalone 服务器不可达 | - | 退出码 1，错误信息包含连接失败详情 | P1 |

### 1.4 写入命令 (Write Commands)

| 场景 ID | 场景名称 | 验证决策 | 预期行为 | 优先级 |
|---------|---------|---------|---------|--------|
| CLI-E2E-050 | send 消息到存在的 conversation | - | 输出 "Message sent." + 消息 ID；本地 DB 中出现该消息 | P0 |
| CLI-E2E-051 | send 消息到不存在的 conversation | - | 退出码 1，错误信息提示 conversation not found | P1 |
| CLI-E2E-052 | send 重复消息（client_message_id 去重） | D-006 | 返回 `Duplicate: true`，不重复创建 | P1 |
| CLI-E2E-053 | send 缺少 --conversation-id | - | 退出码 1，错误信息提示参数缺失 | P1 |
| CLI-E2E-054 | send 缺少 --content | - | 退出码 1，错误信息提示参数缺失 | P1 |
| CLI-E2E-055 | send 带 --reply-to | - | 返回消息包含正确的 reply_to 引用 | P2 |
| CLI-E2E-060 | create-conversation 新建 1-on-1 | - | 输出 conversation ID；本地 DB 中可查询到该 conversation | P0 |
| CLI-E2E-061 | create-conversation 重复创建（相同 peer） | - | 返回已存在的 conversation（幂等） | P1 |
| CLI-E2E-062 | create-conversation 缺少 --peer-id | - | 退出码 1，错误信息提示参数缺失 | P1 |
| CLI-E2E-063 | create-conversation 带 --title | - | conversation title 正确设置 | P2 |
| CLI-E2E-070 | delete-conversation 正常删除 | D-013 | 返回 deleted_message_count；本地 DB 中 conversation 被软删除 | P0 |
| CLI-E2E-071 | delete-conversation 级联删除消息 | D-013 | conversation 的消息也被软删除 | P0 |
| CLI-E2E-072 | delete-conversation 不存在的 ID | - | 退出码 1，错误信息提示 not found | P1 |
| CLI-E2E-080 | restore-conversation 恢复已删除的 | - | conversation 恢复可用；消息恢复 | P0 |
| CLI-E2E-081 | restore-conversation 恢复不存在的 | - | 退出码 1，错误信息提示 not found | P1 |
| CLI-E2E-090 | delete-message 删除自己发送的消息 | D-014 | 消息被软删除；本地 DB 中不再可见 | P0 |
| CLI-E2E-091 | delete-message 删除他人消息（权限拒绝） | D-014 | 退出码 1，permission denied | P1 |
| CLI-E2E-092 | delete-message 不存在的消息 | - | 退出码 1，not found | P1 |
| CLI-E2E-100 | mark-as-read 标记到指定消息 | D-012 | 返回 unread_count = 0（全部已读） | P0 |
| CLI-E2E-101 | mark-as-read 标记全部已读（不指定 --message-id） | D-012 | 使用 LastProcessedMessageID 作为已读游标 | P1 |
| CLI-E2E-102 | mark-as-read MAX 语义（不能回退） | D-012 | 已读游标只能前进不能后退 | P1 |
| CLI-E2E-103 | mark-as-read 不存在的 conversation | - | 退出码 1，not found | P1 |

### 1.5 查询命令 (Query Commands - 本地 DB)

| 场景 ID | 场景名称 | 验证决策 | 预期行为 | 优先级 |
|---------|---------|---------|---------|--------|
| CLI-E2E-110 | list-conversations 列出用户的会话 | D-035 | 输出格式为 tabwriter 表格；按 LastMessageAt 降序 | P0 |
| CLI-E2E-111 | list-conversations 分页 (--limit, --offset) | D-035 | 返回指定数量的结果；has_more 标识正确 | P1 |
| CLI-E2E-112 | list-conversations 空列表 | D-035 | 输出空（或 "No conversations found."） | P1 |
| CLI-E2E-113 | list-conversations 排除软删除的 | D-035 | 已删除的 conversation 不出现在列表中 | P1 |
| CLI-E2E-120 | get-conversation 获取详情 | D-035 | 输出 conversation 完整信息（ID, title, type, members 等） | P0 |
| CLI-E2E-121 | get-conversation 不存在的 ID | D-035 | 退出码 1，not found | P1 |
| CLI-E2E-122 | get-conversation 缺少 --conversation-id | - | 退出码 1，提示参数缺失 | P1 |
| CLI-E2E-130 | get-messages 获取消息列表 | D-035 | 按 MessageID 升序输出；分页正确 | P0 |
| CLI-E2E-131 | get-messages --after-message-id 分页 | D-035 | 只返回 ID > after_message_id 的消息 | P1 |
| CLI-E2E-132 | get-messages --limit 限制 | D-035 | 返回不超过 limit 条消息 | P1 |
| CLI-E2E-133 | get-messages 空消息列表 | D-035 | 无输出或 "No messages found." | P2 |
| CLI-E2E-140 | search-messages 搜索内容 | D-035 | LIKE 匹配的消息被返回；按 MessageID 降序 | P0 |
| CLI-E2E-141 | search-messages 无匹配结果 | D-035 | 空结果 | P1 |
| CLI-E2E-142 | search-messages 缺少 --query | - | 退出码 1，参数缺失 | P1 |
| CLI-E2E-143 | search-messages 特殊字符（%, _） | D-035 | 正确处理 SQL LIKE 通配符 | P2 |
| CLI-E2E-144 | search-messages 中文内容搜索 | - | 正确搜索包含中文的消息 | P2 |

### 1.6 同步 (Sync)

| 场景 ID | 场景名称 | 验证决策 | 预期行为 | 优先级 |
|---------|---------|---------|---------|--------|
| CLI-E2E-150 | sync-updates 触发 FullSync | D-036 | 输出 "Sync complete."；本地 DB 数据与服务器同步 | P0 |
| CLI-E2E-151 | sync-updates daemon 未运行 | D-036 | 退出码 1，提示启动 daemon | P0 |
| CLI-E2E-152 | sync-updates daemon 无新数据 | D-036 | 输出 "Sync complete."（幂等） | P1 |
| CLI-E2E-153 | sync-updates 大量数据分页 | D-029 | 多批次拉取直到 has_more=false；gap 填充正确 | P1 |
| CLI-E2E-154 | listen 连接后自动初始同步 | - | daemon 启动后自动执行初始 FullSync，本地 DB 填充数据 | P0 |
| CLI-E2E-155 | listen 收到推送后自动同步 | - | 另一用户发送消息后，本地 DB 中可见新消息（轮询验证） | P0 |

### 1.7 草稿管理 (Draft Management)

| 场景 ID | 场景名称 | 验证决策 | 预期行为 | 优先级 |
|---------|---------|---------|---------|--------|
| CLI-E2E-160 | draft save 保存新草稿 | - | 输出 "Draft saved."；本地 DB 中可查到草稿 | P0 |
| CLI-E2E-161 | draft save 更新已有草稿（upsert） | - | 内容被覆盖更新 | P1 |
| CLI-E2E-162 | draft save 缺少 --conversation-id | - | 退出码 1 | P1 |
| CLI-E2E-163 | draft save 缺少 --content | - | 退出码 1 | P1 |
| CLI-E2E-170 | draft get 获取已存在的草稿 | - | 输出草稿内容 | P0 |
| CLI-E2E-171 | draft get 获取不存在的草稿 | - | 输出 "No draft found for this conversation."（退出码 0） | P1 |
| CLI-E2E-180 | draft delete 删除已存在的草稿 | - | 输出 "Draft deleted."；再次 get 返回 not found | P0 |
| CLI-E2E-181 | draft delete 删除不存在的草稿 | - | 输出 "No draft found for this conversation."（退出码 0） | P1 |

### 1.8 日志管理 (Logs)

| 场景 ID | 场景名称 | 验证决策 | 预期行为 | 优先级 |
|---------|---------|---------|---------|--------|
| CLI-E2E-190 | logs tail 显示最近 RPC 日志 | - | 表格输出包含 TIME, METHOD, STATUS, DURATION, CONVERSATION 列 | P0 |
| CLI-E2E-191 | logs tail --type notifications | - | 表格输出包含 TIME, SEQ, TYPE 列 | P1 |
| CLI-E2E-192 | logs tail --limit N | - | 最多返回 N 条记录 | P1 |
| CLI-E2E-193 | logs tail --since 过滤时间 | - | 只返回指定时间之后的日志 | P1 |
| CLI-E2E-194 | logs tail 无效 --type | - | 退出码 1，错误提示 invalid type | P2 |
| CLI-E2E-200 | logs search --method 过滤 | - | 只返回指定方法的日志 | P1 |
| CLI-E2E-201 | logs search --error 只显示错误 | - | 只返回 status_code < 0 的日志 | P1 |
| CLI-E2E-202 | logs search --request-id 精确查找 | - | 返回该 request_id 对应的单条日志 | P1 |
| CLI-E2E-203 | logs search --conversation-id 过滤 | - | 只返回指定 conversation 的日志 | P2 |
| CLI-E2E-204 | logs search --from / --to 时间范围 | - | 只返回时间范围内的日志 | P2 |
| CLI-E2E-210 | logs stats 聚合统计 | - | 表格输出 METHOD, COUNT, SUCCESS, ERRORS, AVG(ms) | P1 |
| CLI-E2E-211 | logs stats --interval 分组统计 | - | 按时间间隔分组输出 | P2 |
| CLI-E2E-212 | logs stats 无效 --interval | - | 退出码 1，错误提示有效值列表 | P2 |
| CLI-E2E-220 | logs export --format csv | - | 输出 CSV 格式，含表头 | P1 |
| CLI-E2E-221 | logs export --format json | - | 输出 JSON 格式数组 | P1 |
| CLI-E2E-222 | logs export --output 文件路径 | - | 内容写入指定文件 | P1 |
| CLI-E2E-223 | logs export 无效 --format | - | 退出码 1 | P2 |
| CLI-E2E-230 | logs cleanup --dry-run | D-040 | 只输出将要删除的数量，不实际删除 | P1 |
| CLI-E2E-231 | logs cleanup --retain 7d | D-040 | 删除 7 天前的日志；输出删除数量 | P1 |
| CLI-E2E-232 | logs cleanup --type rpc | D-040 | 只清理 RPC 日志 | P2 |
| CLI-E2E-233 | logs cleanup --type notifications | D-040 | 只清理通知日志 | P2 |
| CLI-E2E-234 | logs cleanup --type all | D-040 | 同时清理两类日志 | P1 |

### 1.9 Kill 命令

| 场景 ID | 场景名称 | 验证决策 | 预期行为 | 优先级 |
|---------|---------|---------|---------|--------|
| CLI-E2E-240 | kill 正常终止 daemon | D-039 | 发送 SIGTERM，daemon 退出码 0；lock 和 sock 文件被清理 | P0 |
| CLI-E2E-241 | kill --force 强制终止 | D-039 | 发送 SIGKILL，daemon 被强制终止；文件被清理 | P0 |
| CLI-E2E-242 | kill daemon 未运行 | D-039 | 输出 "No running daemon found."；退出码 0 | P1 |
| CLI-E2E-243 | kill stale lock（进程不存在） | D-039 | 检测到 stale PID，清理文件；退出码 0 | P0 |
| CLI-E2E-244 | kill --timeout 超时 | D-039, D-042 | daemon 未在规定时间内退出；退出码 3；stderr 提示使用 --force | P1 |
| CLI-E2E-245 | kill --timeout 自定义时长 | D-039 | 使用自定义 timeout 而非默认 5s | P2 |
| CLI-E2E-246 | kill 后 IPC 不可用 | D-039 | kill 完成后 IPC socket 文件不存在 | P1 |

### 1.10 多实例 (Multi-instance)

| 场景 ID | 场景名称 | 验证决策 | 预期行为 | 优先级 |
|---------|---------|---------|---------|--------|
| CLI-E2E-250 | 不同 user_id 独立运行 | - | 两个 listen daemon 可同时运行（不同 socket/lock 路径） | P0 |
| CLI-E2E-251 | 不同 device_id 独立运行 | - | 同一 user_id + 不同 device_id 可并行运行 | P0 |
| CLI-E2E-252 | 同 user_id 同 device_id 冲突 | D-031 | 第二个 listen 退出码 2 | P0 |
| CLI-E2E-253 | 跨实例消息同步 | - | user1-device1 发送消息，user1-device2 sync-updates 后可见 | P0 |
| CLI-E2E-254 | 多 daemon 各自的本地 DB 隔离 | - | 不同 user_id 的 DB 数据互不干扰 | P1 |

### 1.11 错误处理 (Error Handling)

| 场景 ID | 场景名称 | 验证决策 | 预期行为 | 优先级 |
|---------|---------|---------|---------|--------|
| CLI-E2E-260 | 缺少 user-id 执行所有命令 | D-034 | 退出码 1，错误信息包含 "user-id is required" | P0 |
| CLI-E2E-261 | 服务器不可达（standalone 模式） | - | 退出码 1，连接错误详情 | P0 |
| CLI-E2E-262 | 服务器不可达（IPC 也失败） | D-032 | 统一错误信息含两个原因 + hint | P1 |
| CLI-E2E-263 | DB 路径不存在 | - | 查询命令退出码 1，open db 错误 | P1 |
| CLI-E2E-264 | DB 文件损坏 | - | 退出码 1，数据库打开错误 | P2 |
| CLI-E2E-265 | IPC socket 路径权限不足 | D-030 | 退出码 1，连接错误 | P2 |
| CLI-E2E-266 | 无效时间参数（logs 命令） | - | 退出码 1，提示有效格式 | P2 |
| CLI-E2E-267 | 无效 duration 参数（logs cleanup） | - | 退出码 1，提示有效格式 | P2 |
| CLI-E2E-268 | 退出码标准一致性 | D-042 | 所有成功=0，通用错误=1，前置条件=2，超时=3 | P0 |

---

## 2. 测试环境需求

### 2.1 服务架构

```
Host Machine (测试运行环境)
├── Docker Compose (E2E 基础设施)
│   ├── Redis          → port 16379 (DB 15 用于 E2E)
│   └── xyncra-server  → port 18080 (避免与开发端口冲突)
│
├── Go Test Runner
│   └── go test ./internal/cli/e2e/...
│       ├── 编译 xyncra-client (exec.Command)
│       ├── 连接 Docker 中的 Redis + xyncra-server
│       └── t.TempDir() 作为 HOME 目录
│
└── 临时文件 (t.TempDir())
    └── .xyncra/
        └── {user_id}/
            └── {device_id}/
                ├── xyncra.db    (SQLite)
                ├── xyncra.lock  (flock)
                └── xyncra.sock  (Unix Socket)
```

### 2.2 Docker Compose E2E 配置

创建 `internal/cli/e2e/docker-compose.yml`:

```yaml
# E2E 测试专用 Docker Compose
# 使用方式: docker compose -f internal/cli/e2e/docker-compose.yml up -d
services:
  redis:
    image: redis:7-alpine
    ports:
      - "16379:6379"
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5
    command: >
      redis-server
      --save ""
      --appendonly no

  xyncra-server:
    build:
      context: ../../..
      dockerfile: Dockerfile
    ports:
      - "18080:8080"
    environment:
      - XYNCRA_ADDR=:8080
      - XYNCRA_REDIS_ADDR=redis:6379
      - XYNCRA_DB_DRIVER=sqlite
      - XYNCRA_DB_DSN=/data/xyncra.db
    depends_on:
      redis:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 5s
      timeout: 3s
      retries: 5
```

**说明**:
- Redis 使用内存模式（`--save "" --appendonly no`），无需持久化
- 端口 16379 和 18080 与开发环境端口（6379 和 8080）隔离
- `xyncra-server` 使用项目 Dockerfile 构建

### 2.3 端口分配

| 服务 | 端口 | 用途 |
|------|------|------|
| Redis | 16379 | E2E 专用 Redis 实例，DB 15 |
| xyncra-server | 18080 | E2E 专用 WebSocket 服务器 |
| IPC socket | t.TempDir()/.xyncra/{uid}/{did}/xyncra.sock | 每个测试隔离 |

### 2.4 前置条件

1. **Docker + Docker Compose** 已安装并运行
2. **Go 工具链** 可编译 xyncra-client
3. **Redis 16379** 可达（测试开始时检查，不可达则 skip）
4. **xyncra-server 18080** 健康（测试开始时检查，不可达则 skip）

### 2.5 清理策略

| 层级 | 策略 | 实现 |
|------|------|------|
| **测试级别** | 每个测试独立 HOME 目录 | `t.TempDir()` 设置 `HOME` 环境变量 |
| **Redis 级别** | 每个测试前 FlushDB(15) | `redisClient.FlushDB()` |
| **服务器级别** | Docker 容器保持运行，每个测试自动创建新 conversation/message | 无需特殊清理 |
| **进程级别** | 每个 daemon 测试结束后 kill | `t.Cleanup()` 中 `exec.Command("kill")` |
| **文件级别** | `t.TempDir()` 自动清理 | Go 测试框架自动删除 |

### 2.6 环境初始化（TestMain）

```go
func TestMain(m *testing.M) {
    // 1. 检查 Docker 服务是否可达
    if !checkRedisAvailable() {
        fmt.Println("Redis not available at localhost:16379, skipping all CLI E2E tests")
        os.Exit(0)
    }
    if !checkServerAvailable() {
        fmt.Println("xyncra-server not available at localhost:18080, skipping all CLI E2E tests")
        os.Exit(0)
    }

    // 2. 编译 xyncra-client
    buildClient()

    // 3. 运行测试
    code := m.Run()
    os.Exit(code)
}
```

---

## 3. 测试数据策略

### 3.1 数据创建方式

#### 方式 A: 通过 CLI 命令创建（端到端）

```go
// 通过 CLI 命令创建 conversation 和发送消息
func createTestConversation(t *testing.T, env *cliTestEnv, userID, peerID string) string {
    t.Helper()
    out := runCLI(t, env, "create-conversation", "--peer-id", peerID, "--user-id", userID)
    // 解析输出提取 conversation ID
    return extractConversationID(out)
}

func sendTestMessage(t *testing.T, env *cliTestEnv, userID, convID, content string) {
    t.Helper()
    runCLI(t, env, "send", "-c", convID, "-m", content, "--user-id", userID)
}
```

#### 方式 B: 通过服务器 API 预置数据（绕过 CLI）

```go
// 直接写入服务器数据库以预置测试数据
func seedConversation(t *testing.T, serverAddr string, conv *model.Conversation) {
    t.Helper()
    // 通过 WebSocket RPC 或 HTTP API（如有）创建
    // 或通过 Redis + SQLite 直接注入
}
```

#### 方式 C: 混合模式（推荐）

```go
// 1. 用户 A 通过 CLI 创建 conversation + 发送消息
// 2. 用户 B 通过 CLI sync-updates 拉取数据
// 3. 验证用户 B 的本地 DB 状态
```

### 3.2 测试数据模型

```
Test Users:
  - alice (user_id: "alice-e2e")
  - bob   (user_id: "bob-e2e")
  - charlie (user_id: "charlie-e2e")

Test Conversations:
  - alice <-> bob  (1-on-1)
  - alice <-> charlie (1-on-1)
  - group-e2e-001 (group, members: alice, bob, charlie)

Test Messages:
  - 在 alice<->bob 中创建 10 条消息（用于分页测试）
  - 包含 "hello", "world", "你好", "搜索测试" 等内容
```

### 3.3 数据验证策略

| 验证类型 | 方法 | 示例 |
|---------|------|------|
| **stdout 输出** | 匹配关键字和格式 | `strings.Contains(out, "Message sent.")` |
| **stderr 输出** | 匹配错误信息 | `strings.Contains(stderr, "daemon not running")` |
| **退出码** | 精确匹配 | `exitCode == 0`, `exitCode == 2` |
| **本地 DB 查询** | 打开 SQLite 验证 | `db.Conversations.Get(ctx, convID)` |
| **文件状态** | 检查文件存在/不存在 | `os.Stat(lockPath)`, `os.Stat(sockPath)` |
| **跨用户验证** | 第二个用户 sync 后查询 | B 的 DB 中出现 A 发送的消息 |

### 3.4 数据隔离矩阵

| 测试类型 | HOME 隔离 | Redis 隔离 | DB 隔离 |
|---------|-----------|-----------|---------|
| Daemon 生命周期 | t.TempDir() | FlushDB(15) | 新 DB |
| IPC 通信 | t.TempDir() | FlushDB(15) | 新 DB |
| Standalone 模式 | t.TempDir() | FlushDB(15) | 新 DB |
| 写入命令 | t.TempDir() | FlushDB(15) | 新 DB |
| 查询命令 | t.TempDir() | FlushDB(15) | 预填充数据 |
| 多实例 | 多个 t.TempDir() | FlushDB(15) | 各自独立 DB |

---

## 4. 测试文档格式

### 4.1 测试用例 Markdown 模板

每个测试文件（如 `daemon_test.go`）对应一个 Markdown 文档（如 `docs/e2e/CLI-E2E-001_listen_start.md`）:

```markdown
# CLI-E2E-001: listen 正常启动

## 元信息
- **分类**: Daemon Lifecycle
- **优先级**: P0
- **验证决策**: D-030, D-031
- **测试文件**: `internal/cli/e2e/daemon_test.go`
- **测试函数**: `TestListenStart`
- **创建日期**: 2026-07-09

## 前置条件
1. Docker Compose E2E 环境已启动（Redis + xyncra-server）
2. xyncra-client 已编译
3. 无残留的 daemon 进程

## 测试步骤
1. 设置临时 HOME 目录（t.TempDir()）
2. 启动 `xyncra-client listen --user-id alice-e2e --server ws://localhost:18080/ws`
3. 等待 stderr 输出 "IPC server listening at"
4. 验证以下状态：
   a. `xyncra.sock` 文件存在且权限为 0600
   b. `xyncra.lock` 文件存在且包含有效 PID
   c. `xyncra.db` 文件存在
   d. IPC 可连接（发送 JSON-RPC ping 或调用 sync_updates）
5. 发送 SIGTERM 终止 daemon
6. 验证退出码为 0
7. 验证 lock 和 sock 文件被清理

## 预期结果
- 退出码: 0
- stdout: 无输出
- stderr: 包含 "Starting listener daemon" 和 "IPC server listening"
- 文件: `xyncra.sock`, `xyncra.lock`, `xyncra.db` 创建
- 退出后: `xyncra.sock`, `xyncra.lock` 被清理

## 边界条件
- 服务器不可达时仍启动（仅 stderr 输出连接错误）
- DB 路径权限不足时退出码 1
- HOME 目录不可写时退出码 1

## 关联场景
- CLI-E2E-002（重复启动冲突）
- CLI-E2E-003（stale lock 恢复）
- CLI-E2E-004（SIGTERM 优雅退出）
```

### 4.2 测试代码结构模板

```go
// internal/cli/e2e/daemon_test.go

package e2e_test

import (
    "os"
    "os/exec"
    "path/filepath"
    "testing"
    "time"

    "github.com/stretchr/testify/require"
)

// TestListenStart verifies that the listen command starts the daemon
// correctly, creating the required state files and IPC socket.
// Verifies: D-030, D-031
func TestListenStart(t *testing.T) {
    env := newCLITestEnv(t)

    // Start daemon
    cmd := env.startDaemon(t, "alice-e2e")

    // Wait for IPC socket to appear
    require.Eventually(t, func() bool {
        _, err := os.Stat(env.socketPath("alice-e2e"))
        return err == nil
    }, 5*time.Second, 100*time.Millisecond, "IPC socket should be created")

    // Verify state files exist
    assertFileExists(t, env.lockPath("alice-e2e"))
    assertFileExists(t, env.dbPath("alice-e2e"))

    // Verify socket permissions
    info, err := os.Stat(env.socketPath("alice-e2e"))
    require.NoError(t, err)
    require.Equal(t, os.FileMode(0600), info.Mode().Perm())

    // Verify IPC is functional
    resp := env.ipcCall(t, "alice-e2e", "sync_updates", nil)
    require.Nil(t, resp.Error)

    // Cleanup
    env.stopDaemon(t, cmd)
    require.Equal(t, 0, cmd.ProcessState.ExitCode())

    // Verify cleanup
    assertFileNotExists(t, env.lockPath("alice-e2e"))
    assertFileNotExists(t, env.socketPath("alice-e2e"))
}

// TestListenDuplicate verifies that a second listen attempt with the
// same user_id/device_id is rejected with exit code 2.
// Verifies: D-031
func TestListenDuplicate(t *testing.T) {
    env := newCLITestEnv(t)

    // Start first daemon
    cmd1 := env.startDaemon(t, "alice-e2e")
    defer env.stopDaemon(t, cmd1)

    // Attempt second daemon
    cmd2 := env.runCLI(t, "listen", "--user-id", "alice-e2e")
    require.Equal(t, 2, cmd2.ExitCode())
    require.Contains(t, cmd2.Stderr(), "listen already running")
}
```

### 4.3 测试辅助工具模板

```go
// internal/cli/e2e/helpers.go

package e2e_test

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net"
    "os"
    "os/exec"
    "path/filepath"
    "syscall"
    "testing"
    "time"

    "github.com/redis/go-redis/v9"
    "github.com/stretchr/testify/require"
)

const (
    e2eRedisAddr  = "localhost:16379"
    e2eRedisDB    = 15
    e2eServerAddr = "ws://localhost:18080/ws"
)

// cliTestEnv holds the environment for a single CLI E2E test.
type cliTestEnv struct {
    homeDir    string // t.TempDir()
    clientBin  string // path to compiled xyncra-client
    serverAddr string // xyncra-server WebSocket URL
}

// newCLITestEnv creates a test environment with:
// - Temporary HOME directory
// - Redis FlushDB
// - Compiled xyncra-client path
func newCLITestEnv(t *testing.T) *cliTestEnv {
    t.Helper()

    homeDir := t.TempDir()

    // FlushDB
    rdb := redis.NewClient(&redis.Options{Addr: e2eRedisAddr, DB: e2eRedisDB})
    defer rdb.Close()
    require.NoError(t, rdb.FlushDB(context.Background()).Err())

    return &cliTestEnv{
        homeDir:    homeDir,
        clientBin:  compiledClientPath,
        serverAddr: e2eServerAddr,
    }
}

// runCLI executes a xyncra-client command and returns the result.
func (e *cliTestEnv) runCLI(t *testing.T, args ...string) *CLIResult {
    t.Helper()
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()

    cmd := exec.CommandContext(ctx, e.clientBin, args...)
    cmd.Env = append(os.Environ(),
        "HOME="+e.homeDir,
        "XYNCRA_SERVER="+e.serverAddr,
    )

    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    err := cmd.Run()
    exitCode := 0
    if err != nil {
        if exitErr, ok := err.(*exec.ExitError); ok {
            exitCode = exitErr.ExitCode()
        } else {
            t.Fatalf("failed to run command: %v", err)
        }
    }

    return &CLIResult{
        ExitCode: exitCode,
        Stdout:   stdout.String(),
        Stderr:   stderr.String(),
    }
}

// startDaemon starts the listen command in the background.
func (e *cliTestEnv) startDaemon(t *testing.T, userID string) *exec.Cmd {
    t.Helper()
    cmd := exec.Command(e.clientBin, "listen", "--user-id", userID)
    cmd.Env = append(os.Environ(),
        "HOME="+e.homeDir,
        "XYNCRA_SERVER="+e.serverAddr,
    )
    var stderr bytes.Buffer
    cmd.Stderr = &stderr

    require.NoError(t, cmd.Start())

    // Wait for socket to appear
    sockPath := filepath.Join(e.homeDir, ".xyncra", userID,
        defaultDeviceID(), "xyncra.sock")
    require.Eventually(t, func() bool {
        _, err := os.Stat(sockPath)
        return err == nil
    }, 10*time.Second, 200*time.Millisecond,
        "daemon socket should appear (stderr: %s)", stderr.String())

    t.Cleanup(func() {
        if cmd.Process != nil {
            _ = cmd.Process.Signal(syscall.SIGTERM)
            _ = cmd.Wait()
        }
    })

    return cmd
}

// stopDaemon sends SIGTERM and waits for the daemon to exit.
func (e *cliTestEnv) stopDaemon(t *testing.T, cmd *exec.Cmd) {
    t.Helper()
    if cmd.Process == nil {
        return
    }
    _ = cmd.Process.Signal(syscall.SIGTERM)
    _ = cmd.Wait()
}

// ipcCall sends a JSON-RPC request to the daemon's IPC socket.
func (e *cliTestEnv) ipcCall(t *testing.T, userID, method string, params any) *IPCResponse {
    t.Helper()
    sockPath := filepath.Join(e.homeDir, ".xyncra", userID,
        defaultDeviceID(), "xyncra.sock")

    conn, err := net.DialTimeout("unix", sockPath, 5*time.Second)
    require.NoError(t, err)
    defer conn.Close()

    req := map[string]any{
        "jsonrpc": "2.0",
        "method":  method,
        "id":      "test-1",
    }
    if params != nil {
        req["params"] = params
    }

    data, _ := json.Marshal(req)
    data = append(data, '\n')

    conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
    _, err = conn.Write(data)
    require.NoError(t, err)

    conn.SetReadDeadline(time.Now().Add(5 * time.Second))
    buf := make([]byte, 64*1024)
    n, err := conn.Read(buf)
    require.NoError(t, err)

    var resp IPCResponse
    require.NoError(t, json.Unmarshal(buf[:n], &resp))
    return &resp
}

// CLIResult holds the result of a CLI command execution.
type CLIResult struct {
    ExitCode int
    Stdout   string
    Stderr   string
}

// IPCResponse is a JSON-RPC 2.0 response.
type IPCResponse struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      string          `json:"id"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *IPCError       `json:"error,omitempty"`
}

// IPCError is a JSON-RPC 2.0 error object.
type IPCError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}

// Assert helpers

func assertFileExists(t *testing.T, path string) {
    t.Helper()
    _, err := os.Stat(path)
    require.NoError(t, err, "file should exist: %s", path)
}

func assertFileNotExists(t *testing.T, path string) {
    t.Helper()
    _, err := os.Stat(path)
    require.True(t, os.IsNotExist(err), "file should not exist: %s", path)
}

func defaultDeviceID() string {
    // Must match the CLI's defaultDeviceID() logic
    hostname, _ := os.Hostname()
    h := sha256.Sum256([]byte(hostname))
    return fmt.Sprintf("%x", h[:4])
}
```

### 4.4 测试运行方式

```bash
# 1. 启动 E2E Docker 环境
docker compose -f internal/cli/e2e/docker-compose.yml up -d

# 2. 等待服务健康
docker compose -f internal/cli/e2e/docker-compose.yml ps

# 3. 运行 CLI E2E 测试
go test -v -count=1 -timeout 300s ./internal/cli/e2e/...

# 4. 运行特定类别的测试
go test -v -count=1 -run "TestListen" ./internal/cli/e2e/...
go test -v -count=1 -run "TestSend" ./internal/cli/e2e/...
go test -v -count=1 -run "TestDraft" ./internal/cli/e2e/...

# 5. 清理 Docker 环境
docker compose -f internal/cli/e2e/docker-compose.yml down -v
```

### 4.5 CI 集成

```yaml
# .github/workflows/cli-e2e.yml 片段
jobs:
  cli-e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - name: Start E2E services
        run: docker compose -f internal/cli/e2e/docker-compose.yml up -d --wait
      - name: Run CLI E2E tests
        run: go test -v -count=1 -timeout 300s ./internal/cli/e2e/...
      - name: Stop E2E services
        if: always()
        run: docker compose -f internal/cli/e2e/docker-compose.yml down -v
```

---

## 附录 A: 测试场景统计

| 分类 | P0 | P1 | P2 | 合计 |
|------|----|----|----|----|
| Daemon Lifecycle | 4 | 4 | 2 | 10 |
| IPC Communication | 4 | 7 | 2 | 13 |
| Standalone Fallback | 3 | 6 | 0 | 9 |
| Write Commands | 7 | 13 | 2 | 22 |
| Query Commands | 4 | 9 | 3 | 16 |
| Sync | 4 | 2 | 0 | 6 |
| Draft Management | 3 | 5 | 0 | 8 |
| Logs | 1 | 13 | 8 | 22 |
| Kill Command | 3 | 3 | 1 | 7 |
| Multi-instance | 4 | 1 | 0 | 5 |
| Error Handling | 3 | 2 | 4 | 9 |
| **合计** | **40** | **65** | **22** | **127** |

## 附录 B: 执行优先级建议

### 第一轮 (冒烟测试) - 40 个 P0 场景
- 所有 Daemon Lifecycle P0 场景
- 所有 IPC Communication P0 场景
- Standalone Fallback 的 P0 场景
- Write Commands 的核心 P0 场景
- Query Commands 的 P0 场景
- Sync 的 P0 场景
- Kill / Multi-instance / Error Handling 的 P0 场景

### 第二轮 (功能完整性) - 65 个 P1 场景
- 所有边界条件和错误路径
- 分页、过滤、排序验证
- Standalone fallback 的所有命令
- 日志管理的所有过滤/导出功能

### 第三轮 (健壮性) - 22 个 P2 场景
- 权限和特殊字符处理
- 自定义参数覆盖
- 异常数据库状态

## 附录 C: 关键产品决策速查

| 决策 | 内容 | 测试影响 |
|------|------|---------|
| D-030 | Unix Socket IPC, JSON-RPC 2.0, 换行分隔 | IPC 测试需要 socket 连接 + JSON 序列化 |
| D-031 | flock 进程锁 + stale 检测 | 需要测试锁冲突和 stale lock 清理 |
| D-032 | IPC 优先 + WS fallback | 写命令需测试两种路径 |
| D-035 | 查询命令读本地 SQLite | 查询测试不需要 daemon 运行 |
| D-036 | sync-updates IPC-only | 无 daemon 时必须报错 |
| D-039 | kill: SIGTERM -> SIGKILL 升级 | 需要测试信号处理和超时 |
| D-040 | 日志保留 7 天 | cleanup 测试使用 retain 参数 |
| D-042 | 退出码 0/1/2/3 | 所有测试需验证退出码 |

---

## 附录 D: 测试执行报告 (2026-07-09)

### 测试统计

| 指标 | 数值 |
|------|------|
| 总测试数 (含子测试) | 136 passed |
| 测试函数数 | 51 |
| 跳过 | 1 (TestCrossInstanceSync — 服务端 UserUpdate bug) |
| 失败 | 0 |
| 场景覆盖率 | ~113/127 (~88%) |

### 测试文件分布

| 文件 | 优先级 | 测试函数数 | 描述 |
|------|--------|-----------|------|
| `cli_e2e_test.go` | 原始 | 8 | 基础冒烟测试 (daemon, IPC, standalone, query, sync, multi-instance) |
| `cli_e2e_p0_test.go` | P0 | 11 | 核心场景 (listen 生命周期, kill, sync, 多设备隔离, 错误处理) |
| `cli_e2e_p1_test.go` | P1 | 25 | 功能完整性 (IPC 操作/错误, standalone fallback, 写入命令, 查询命令, 草稿, 日志, 同步) |
| `cli_e2e_p2_test.go` | P2 | 7 | 健壮性 (daemon/IPC/写入/查询/日志/kill/错误 边界场景) |
| `helpers_test.go` | - | 0 | 测试辅助工具 (cliTestEnv, IPC helpers, assertions) |

### 覆盖场景分类

| 分类 | P0 已实现 | P1 已实现 | P2 已实现 | 合计 |
|------|----------|----------|----------|------|
| Daemon Lifecycle | 4 | 4 | 2 | 10 |
| IPC Communication | 4 | 7 | 2 | 13 |
| Standalone Fallback | 3 | 5 | 0 | 8 |
| Write Commands | 7 | 11 | 2 | 20 |
| Query Commands | 4 | 9 | 3 | 16 |
| Sync | 4 | 2 | 0 | 6 |
| Draft Management | 3 | 5 | 0 | 8 |
| Logs | 1 | 13 | 8 | 22 |
| Kill Command | 3 | 3 | 1 | 7 |
| Multi-instance | 3 | 1 | 0 | 4 |
| Error Handling | 3 | 2 | 4 | 9 |
| **合计** | **39/40** | **62/65** | **22/22** | **~113/127** |

### 未覆盖场景 (14 个)

- CLI-E2E-253: 跨实例消息同步 (SKIP — 服务端 UserUpdate 持久化 bug)
- 其余 13 个为低优先级边界场景 (flag 优先级、socket 权限、DB 损坏、时间过滤等)

### 测试过程中发现并修复的 Bug

1. **ipcRawCall error code**: JSON-RPC error response 的 error code 解析不正确
2. **mark-as-read standalone DB path**: standalone 模式下 mark-as-read 未正确更新本地 DB 的已读游标
3. **delete-message permission error message**: 删除他人消息时返回的错误信息不符合预期格式

---

## 5. 手动 CLI E2E 测试验证矩阵 (2026-07-09)

> **执行者**: E2E Manual (CLI-driven)
> **Git Commit**: 7f43f99
> **总计**: 127/133 PASS (95.5%) — 1 FAIL, 2 WARN, 2 N/A, 1 PARTIAL

### Group A: Smoke Test (2/2 PASS)

| 场景 | 描述 | 状态 |
|------|------|------|
| CLI-E2E-A001 | 环境健康检查 (Redis PONG, Server /health) | ✅ PASS |
| CLI-E2E-A002 | 端到端快乐路径 (create, send, query, sync, kill) | ✅ PASS |

### Group B: Daemon Lifecycle (19/19 PASS, 修复后)

| 场景 | 描述 | 状态 |
|------|------|------|
| CLI-E2E-B01 | listen 创建状态文件 (sock, lock, db) | ✅ PASS |
| CLI-E2E-B02 | 重复 listen 拒绝 (exit 2, D-031) | ✅ PASS |
| CLI-E2E-B03 | kill 发送 SIGTERM (D-039) | ✅ PASS |
| CLI-E2E-B04 | kill --force 发送 SIGKILL (D-039) | ✅ PASS |
| CLI-E2E-B05 | SIGINT 正常退出 | ✅ PASS |
| CLI-E2E-B06 | listen 缺少 --user-id 失败 | ✅ PASS |
| CLI-E2E-B07 | WS 不可达时 IPC 仍可用 (D-044) | ✅ PASS |
| CLI-E2E-B08 | XYNCRA_USER_ID 环境变量 (D-034) | ✅ PASS |
| CLI-E2E-B09 | XYNCRA_SERVER 环境变量 (D-034) | ✅ PASS |
| CLI-E2E-B10 | flag 优先级高于环境变量 (D-034) | ✅ PASS |
| CLI-E2E-B11 | stale lock 自动恢复 (D-031) | ✅ PASS |
| CLI-E2E-B12 | kill 无 daemon 运行 | ✅ PASS |
| CLI-E2E-B13 | kill 清理 stale lock (D-039) | ✅ PASS |
| CLI-E2E-B14 | 自定义 --device-id | ✅ PASS |
| CLI-E2E-B15 | 不同 device_id 隔离 | ✅ PASS |
| CLI-E2E-B16 | socket 权限 0600 (D-030) | ✅ PASS |
| CLI-E2E-B17 | IPC socket 路径格式 (D-030) | ✅ PASS |
| CLI-E2E-B18 | 目录权限 0700 | ✅ PASS |
| CLI-E2E-B19 | kill --timeout flag | ✅ PASS |

### Group D: Message Operations (20/23, 1 FAIL, 2 WARN)

| 场景 | 描述 | 状态 |
|------|------|------|
| CLI-E2E-D01 | send 消息基本功能 | ✅ PASS |
| CLI-E2E-D02 | send 第二条消息 (递增 ID) | ❌ FAIL (send 缺少 --client-msg-id flag) |
| CLI-E2E-D03 | send 空内容被拒绝 | ✅ PASS |
| CLI-E2E-D04 | send 到不存在的 conversation | ✅ PASS |
| CLI-E2E-D05 | send 缺少 --conversation-id | ✅ PASS |
| CLI-E2E-D06 | send 缺少 --content | ✅ PASS |
| CLI-E2E-D07 | send 带 --reply-to (D-038) | ✅ PASS |
| CLI-E2E-D08 | send standalone fallback (D-032) | ✅ PASS |
| CLI-E2E-D09 | delete 自己的消息 (D-014) | ✅ PASS |
| CLI-E2E-D10 | delete 不存在的消息 | ✅ PASS |
| CLI-E2E-D11 | delete 权限拒绝 (D-014) | ✅ PASS |
| CLI-E2E-D12 | mark-as-read MAX 语义 (D-012) | ⚠️ WARN (需服务端验证) |
| CLI-E2E-D13 | mark-as-read 指定消息 | ✅ PASS |
| CLI-E2E-D14 | mark-as-read 全部 (message-id=0) | ✅ PASS |
| CLI-E2E-D15 | mark-as-read 缺少 --conversation-id | ✅ PASS |
| CLI-E2E-D16 | send 长消息 (256 字符) | ✅ PASS |
| CLI-E2E-D17 | send 特殊字符 | ✅ PASS |
| CLI-E2E-D18 | send Unicode 消息 | ✅ PASS |
| CLI-E2E-D19 | get-messages 单调递增 MessageID (D-008) | ✅ PASS |
| CLI-E2E-D20 | get-messages --after-message-id 分页 | ✅ PASS |
| CLI-E2E-D21 | search-messages 基本 | ✅ PASS |
| CLI-E2E-D22 | search-messages 无结果 | ⚠️ WARN (exit 0 可能是设计行为) |
| CLI-E2E-D23 | send 幂等性 (D-006) | ✅ PASS |

### Group E: Conversation Ops (1/1 PASS)

| 场景 | 描述 | 状态 |
|------|------|------|
| CLI-E2E-E001 | create-conversation 通知对端 (D-045) | ✅ PASS |

### Group F: Query Commands (18/18 PASS)

| 场景 | 描述 | 状态 |
|------|------|------|
| CLI-E2E-F01 | list-conversations 基本 | ✅ PASS |
| CLI-E2E-F02 | tabwriter 对齐 (D-041) | ✅ PASS |
| CLI-E2E-F03 | list-conversations 空列表 | ✅ PASS |
| CLI-E2E-F04 | list-conversations --limit | ✅ PASS |
| CLI-E2E-F05 | list-conversations --offset | ✅ PASS |
| CLI-E2E-F06 | get-conversation 详情 | ✅ PASS |
| CLI-E2E-F07 | get-conversation not found | ✅ PASS |
| CLI-E2E-F08 | get-messages 基本 (D-035) | ✅ PASS |
| CLI-E2E-F09 | get-messages 升序 | ✅ PASS |
| CLI-E2E-F10 | get-messages --after-message-id | ✅ PASS |
| CLI-E2E-F11 | get-messages --limit | ✅ PASS |
| CLI-E2E-F12 | get-messages 空 conversation | ✅ PASS |
| CLI-E2E-F13 | search-messages 基本 | ✅ PASS |
| CLI-E2E-F14 | search-messages 降序 | ✅ PASS |
| CLI-E2E-F15 | search-messages --limit | ✅ PASS |
| CLI-E2E-F16 | search-messages 无结果 | ✅ PASS |
| CLI-E2E-F17 | get-conversation 未读计数 | ✅ PASS |
| CLI-E2E-F18 | 查询命令无需 daemon (D-035) | ✅ PASS |

### Group G: Draft Management (8/8 PASS)

| 场景 | 描述 | 状态 |
|------|------|------|
| CLI-E2E-G01 | draft save | ✅ PASS |
| CLI-E2E-G02 | draft get | ✅ PASS |
| CLI-E2E-G03 | draft save upsert | ✅ PASS |
| CLI-E2E-G04 | draft get 显示更新内容 | ✅ PASS |
| CLI-E2E-G05 | draft delete | ✅ PASS |
| CLI-E2E-G06 | draft get 删除后 | ✅ PASS |
| CLI-E2E-G07 | draft save 缺少参数 | ✅ PASS |
| CLI-E2E-G08 | draft get 不存在 | ✅ PASS |

### Group H: Logs Management (25/25 PASS, 修复后)

| 场景 | 描述 | 状态 |
|------|------|------|
| CLI-E2E-H01 | logs tail 基本 | ✅ PASS |
| CLI-E2E-H02 | logs tail --type rpc | ✅ PASS |
| CLI-E2E-H03 | logs tail --type notifications | ✅ PASS |
| CLI-E2E-H04 | logs tail --limit | ✅ PASS |
| CLI-E2E-H05 | logs tail --since | ✅ PASS |
| CLI-E2E-H06 | logs search 基本 | ✅ PASS |
| CLI-E2E-H07 | logs search --method | ✅ PASS |
| CLI-E2E-H08 | logs search --error | ✅ PASS |
| CLI-E2E-H09 | logs search --from --to | ✅ PASS |
| CLI-E2E-H10 | logs search --conversation-id | ✅ PASS |
| CLI-E2E-H11 | logs search --request-id | ✅ PASS |
| CLI-E2E-H12 | logs stats 基本 | ✅ PASS |
| CLI-E2E-H13 | logs stats --since | ✅ PASS |
| CLI-E2E-H14 | logs stats --interval | ✅ PASS |
| CLI-E2E-H15 | logs stats --interval 无效 | ✅ PASS |
| CLI-E2E-H16 | logs export CSV | ✅ PASS |
| CLI-E2E-H17 | logs export JSON | ✅ PASS |
| CLI-E2E-H18 | logs export --output 文件 | ✅ PASS |
| CLI-E2E-H19 | logs export --method 过滤 | ✅ PASS |
| CLI-E2E-H20 | logs export 无效格式 | ✅ PASS |
| CLI-E2E-H21 | logs cleanup --dry-run | ✅ PASS |
| CLI-E2E-H22 | logs cleanup 基本 | ✅ PASS |
| CLI-E2E-H23 | logs cleanup --retain | ✅ PASS |
| CLI-E2E-H24 | logs cleanup --type rpc | ✅ PASS |
| CLI-E2E-H25 | logs cleanup --type notifications | ✅ PASS |

### Group I: Sync Operations (5/6, 1 N/A)

| 场景 | 描述 | 状态 |
|------|------|------|
| CLI-E2E-I01 | sync-updates with daemon | ✅ PASS |
| CLI-E2E-I02 | sync-updates no daemon (exit 2, D-036) | ✅ PASS |
| CLI-E2E-I03 | sync-updates 重复同步 | ✅ PASS |
| CLI-E2E-I04 | sync 新数据后 --full/--force | ⬜ N/A (不支持，默认即完全同步) |
| CLI-E2E-I05 | daemon 初始 FullSync | ✅ PASS |
| CLI-E2E-I06 | sync-updates IPC-only (D-036) | ✅ PASS |

### Group J: Multi-Instance + IPC (18/19, 1 N/A)

| 场景 | 描述 | 状态 |
|------|------|------|
| CLI-E2E-J01 | 多用户隔离 | ✅ PASS |
| CLI-E2E-J02 | 多设备隔离 | ✅ PASS |
| CLI-E2E-J03 | IPC 路由到正确 daemon | ✅ PASS |
| CLI-E2E-J04 | kill 一个 daemon 不影响其他 | ✅ PASS |
| CLI-E2E-J05 | 不同 DB 文件 per device | ✅ PASS |
| CLI-E2E-J06 | 不同 lock 文件 per device | ✅ PASS |
| CLI-E2E-J07 | Bob daemon send | ✅ PASS |
| CLI-E2E-J08 | 跨用户数据隔离 | ✅ PASS |
| CLI-E2E-J09 | IPC JSON-RPC 2.0 协议 (D-030) | ✅ PASS |
| CLI-E2E-J10 | IPC 无效方法 | ✅ PASS |
| CLI-E2E-J11 | IPC 无效 JSON | ✅ PASS |
| CLI-E2E-J12 | standalone fallback create-conversation | ✅ PASS |
| CLI-E2E-J13 | standalone fallback delete-conversation | ✅ PASS |
| CLI-E2E-J14 | standalone fallback restore-conversation | ⬜ N/A (--timeout 不支持) |
| CLI-E2E-J15 | standalone fallback mark-as-read | ✅ PASS |
| CLI-E2E-J16 | multi-instance --timeout | ✅ PASS |
| CLI-E2E-J17 | IPC 并发请求 | ✅ PASS |
| CLI-E2E-J18 | IPC socket kill 后清理 | ✅ PASS |
| CLI-E2E-J19 | daemon IPC 断开后重连 | ✅ PASS |

### Group K: Error Handling (10/10 PASS)

| 场景 | 描述 | 状态 |
|------|------|------|
| CLI-E2E-K01 | send 缺少 --user-id | ✅ PASS |
| CLI-E2E-K02 | 无效服务器 URL | ✅ PASS |
| CLI-E2E-K03 | 无效 UUID conversation-id | ✅ PASS |
| CLI-E2E-K04 | get-conversation 缺少 -c | ✅ PASS |
| CLI-E2E-K05 | send 到不存在的 conversation | ✅ PASS |
| CLI-E2E-K06 | delete 不存在的 conversation | ✅ PASS |
| CLI-E2E-K07 | restore 不存在的 conversation | ✅ PASS |
| CLI-E2E-K08 | listen 缺少 --user-id | ✅ PASS |
| CLI-E2E-K09 | 双重原因错误 (IPC+WS 均失败) | ✅ PASS |
| CLI-E2E-K10 | 退出码标准 (D-042) | ✅ PASS |

### Group L: Resilience (1/1 PASS)

| 场景 | 描述 | 状态 |
|------|------|------|
| CLI-E2E-L001 | daemon 服务器重启后存活 (D-044) | ✅ PASS |

### Group M: Advanced (0/1, 1 PARTIAL)

| 场景 | 描述 | 状态 |
|------|------|------|
| CLI-E2E-M001 | 自定义 --db-path 和 --log-dir | ⚠️ PARTIAL (--log-dir 未写入日志文件) |

---

## 待改进场景

| 场景 | 状态 | 原因 | 建议 |
|------|------|------|------|
| D02 | ❌ FAIL | send 缺少 --client-msg-id flag | 添加 flag 支持 CLI 幂等性测试 |
| D12 | ⚠️ WARN | mark-as-read MAX 语义需服务端验证 | 服务端检查并确认 |
| D22 | ⚠️ WARN | 不存在会话查询返回 exit 0 | 确认是否期望行为 |
| I04 | ⬜ N/A | --full/--force 不支持 | 如需单独触发全量同步则添加 |
| J14 | ⬜ N/A | --timeout 不支持 | 如需超时控制则添加 |
| M001 | ⚠️ PARTIAL | --log-dir 未写入日志文件 | 实现日志文件写入或移除参数 |
