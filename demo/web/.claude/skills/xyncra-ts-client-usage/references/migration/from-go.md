# Go 客户端迁移到 TypeScript 客户端指南

本文档详细说明从 Go 版 `xyncra-client` 迁移到 TypeScript 版的差异和注意事项。

---

## 1. 存储不共享

**最重要的差异**：Go 版和 TS 版使用完全不同的存储机制，数据不互通。

| 特性 | Go 客户端 | TS 客户端 |
|------|----------|----------|
| 存储引擎 | SQLite (WAL 模式) | IndexedDB (Dexie.js) |
| 数据持久化 | 磁盘文件 (`xyncra.db`) | 进程内存 (`fake-indexeddb`) |
| 跨进程访问 | 查询命令可直接读 SQLite 文件 | 必须通过 IPC 连接 daemon |
| 重启后数据 | 保留（文件持久化） | 丢失（内存存储），需 FullSync 重建 |

**影响**：
- Go 版的 `xyncra.db` 文件不会被 TS 版读取
- TS 版启动后需要重新同步所有数据
- 两者不能共享本地数据

---

## 2. `--db-path` 语义变更 (TS-D-012)

这是最容易混淆的差异。

| | Go 客户端 | TS 客户端 |
|---|----------|----------|
| `--db-path` 含义 | SQLite 数据库文件路径 | IndexedDB 数据库名称 |
| 默认值 | `~/.xyncra/{uid}/{did}/xyncra.db` | 同左字符串，但仅作名称使用 |
| 是否创建文件 | 是（磁盘上的 SQLite 文件） | 否（仅内存中的 Dexie 实例） |

**示例**：

```bash
# Go 版：--db-path 是文件路径
xyncra-client listen --user-id alice --device-id dev1 --db-path /data/xyncra.db
# → 创建 /data/xyncra.db 文件

# TS 版：--db-path 是 IndexedDB 名称
xyncra-client listen --user-id alice --device-id dev1 --db-path /data/xyncra.db
# → 不创建任何文件，"/data/xyncra.db" 仅作为 Dexie 数据库名称
```

**注意**：虽然默认值看起来像文件路径（`~/.xyncra/{uid}/{did}/xyncra.db`），但在 TS 版中它不会创建任何文件。

---

## 3. 相同的 socket/lock 路径

Go 和 TS daemon 使用相同的文件路径约定：

```
~/.xyncra/{user_id}/{device_id}/
├── xyncra.sock     # Unix Socket IPC (D-030)
├── xyncra.lock     # 进程锁文件 (D-031)
└── logs/           # 日志目录
```

**路径相同，但实现不同**：
- Socket 文件格式一致（Unix Domain Socket + JSON-RPC 2.0）
- Lock 文件 JSON 结构一致
- 但 Go 使用 `flock`，TS 使用 `fs-ext`

---

## 4. 不能同时运行

Go 和 TS daemon **不能同时运行**同一个 `(user_id, device_id)` 组合，因为：

1. 它们竞争同一个 `xyncra.lock` 文件
2. 它们竞争同一个 `xyncra.sock` 路径
3. 后启动的会报 `EADDRINUSE` 或 `listen already running` 错误

**迁移步骤**：

```bash
# 1. 先停止 Go 版 daemon
./xyncra-client-old kill --user-id alice --device-id dev1

# 2. 清理残留文件
rm ~/.xyncra/alice/dev1/xyncra.sock  # 如果残留

# 3. 启动 TS 版 daemon
xyncra-client listen --user-id alice --device-id dev1
```

---

## 5. 旧 `xyncra.db` 文件处理

Go 版的 SQLite 文件在迁移后可以保留，不会影响 TS 版：

```bash
# Go 版遗留的文件
ls ~/.xyncra/alice/dev1/
# xyncra.db        ← 不再被使用，可以删除
# xyncra.sock      ← TS 版会重新创建
# xyncra.lock      ← TS 版会重新创建
# logs/            ← 日志文件保留
```

**建议**：
- 确认 TS 版正常工作后再删除旧的 `xyncra.db`
- 如果需要保留历史数据，可以先导出：
  ```bash
  # 在停止 TS 版之前，用 Go 版导出旧数据
  ./xyncra-client-old logs export --user-id alice --device-id dev1 --format json --output old-logs.json
  ```

---

## 6. 命令完全一致 (TS-D-005)

TS 版完全替代了 Go 版的所有命令。命令名、flag、输出格式完全一致。

**相同的部分**：
- 所有命令名（listen, send, create-conversation, etc.）
- 所有 flag（--user-id, --device-id, --server, -c, -m, etc.）
- 所有输出格式（表格、键值对、CSV、JSON）
- 退出码（0/1/2/3）
- 环境变量（XYNCRA_*）

**迁移时不需要修改**：
- Shell 脚本中的命令调用
- CI/CD 中的命令
- 自动化脚本

---

## 7. 环境要求变更

| | Go 客户端 | TS 客户端 |
|---|----------|----------|
| 运行时 | Go binary（编译后无依赖） | Node.js >= 20 |
| 安装方式 | `go build` 编译 | `npm install` + `npm link` |
| 分发方式 | 单个二进制文件 | npm 包 + node_modules |
| 内存占用 | 较低（Go runtime） | 较高（V8 + fake-indexeddb） |

**安装步骤**：

```bash
# 确保 Node.js >= 20
node --version  # 应为 v20.x.x 或更高

# 构建 TS 客户端
cd /path/to/xyncra-server
npm install
cd packages/xyncra-client-cli
npm run build
npm link

# 验证安装
xyncra-client --help
```

---

## 8. 构建方式变更

| | Go 客户端 | TS 客户端 |
|---|----------|----------|
| 构建命令 | `go build -o xyncra-client ./cmd/xyncra-client` | `npm run build` |
| 输出产物 | `xyncra-client` 二进制 | `dist/` 目录（JS 文件） |
| 全局安装 | `cp xyncra-client /usr/local/bin/` | `npm link` |
| 交叉编译 | `GOOS=linux GOARCH=amd64 go build` | 不需要（JS 跨平台） |

---

## 9. 新增命令

TS 版新增了以下 Go 版没有的命令：

| 命令 | 模式 | 说明 |
|------|------|------|
| `set-typing` | IPC-only | 发送打字指示器（D-050，fire-and-forget） |
| `stream-text` | IPC-only | 发送流式文本（D-051，fire-and-forget） |

**使用示例**：

```bash
# 发送打字指示器
xyncra-client set-typing --user-id alice --device-id dev1 -c <conv-uuid> --typing

# 发送流式文本
echo "Streaming response..." | xyncra-client stream-text --user-id alice --device-id dev1 -c <conv-uuid>
```

---

## 10. 查询命令模式变更

这是最影响使用习惯的差异。

### Go 版：查询命令可直接读本地 SQLite

```bash
# Go 版：即使 daemon 没运行，查询命令也能读本地文件
./xyncra-client list-conversations --user-id alice --device-id dev1
# → 直接读取 ~/.xyncra/alice/dev1/xyncra.db
```

### TS 版：查询命令必须通过 IPC 连接 daemon

```bash
# TS 版：查询命令必须通过 daemon 的 IndexedDB
xyncra-client list-conversations --user-id alice --device-id dev1
# → 通过 IPC 连接到 daemon，daemon 从 IndexedDB 查询数据
# → 如果 daemon 没有运行，命令会失败
```

**影响**：
- 使用 TS 版时，daemon **必须保持运行**才能执行查询
- 不能像 Go 版那样在无 daemon 时离线查询
- 脚本中需要先确保 daemon 已启动

**解决方案**：

```bash
# 在脚本中自动启动 daemon
if ! xyncra-client sync-updates --user-id alice --device-id dev1 2>/dev/null; then
  xyncra-client listen --user-id alice --device-id dev1 &
  sleep 3
fi

# 现在可以安全查询
xyncra-client list-conversations --user-id alice --device-id dev1
```

---

## 迁移检查清单

1. [ ] 确认 Node.js >= 20 已安装
2. [ ] 构建 TS 客户端（`npm run build && npm link`）
3. [ ] 停止 Go 版 daemon
4. [ ] 清理残留的 `xyncra.sock` 文件
5. [ ] 启动 TS 版 daemon
6. [ ] 执行 `sync-updates` 重建本地数据
7. [ ] 验证查询命令正常工作
8. [ ] 更新自动化脚本中的二进制路径（`./xyncra-client` → `xyncra-client`）
9. [ ] 确认 `XYNCRA_DB_PATH` 的语义理解（是名称，不是路径）
10. [ ] （可选）删除旧的 `xyncra.db` 文件

---

## 快速对比表

| 特性 | Go 客户端 | TS 客户端 | 迁移影响 |
|------|----------|----------|---------|
| 命令名 | 一致 | 一致 | 无 |
| Flag | 一致 | 一致 | 无 |
| 输出格式 | 一致 | 一致 | 无 |
| 退出码 | 一致 | 一致 | 无 |
| 环境变量 | 一致 | 一致 | 无 |
| 存储 | SQLite 文件 | IndexedDB 内存 | 数据不共享 |
| `--db-path` | 文件路径 | DB 名称 | 语义变更 |
| 查询命令 | 直接读文件 | IPC-only | 需要 daemon 运行 |
| 锁机制 | flock | fs-ext | 不兼容 |
| 构建 | `go build` | `npm run build` | 流程变更 |
| 运行时 | Go binary | Node.js 20+ | 环境要求变更 |
| 新增命令 | 无 | set-typing, stream-text | 新功能可用 |
| 数据持久化 | 磁盘文件 | 进程内存 | 重启后需 FullSync |
