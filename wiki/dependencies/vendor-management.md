# Vendor 管理

## 概述

本文档描述 Xyncra Server 的 Go Vendor 管理策略，包括何时使用 Vendor、目录结构、更新流程和与 Module Cache 的对比。

## 当前 Vendoring 状态

Xyncra Server 当前**未启用** Vendor 目录（`vendor/` 不在项目根目录中）。项目使用 Go Modules 的标准依赖管理方式。

```bash
# 当前项目根目录中不存在 vendor 目录
ls vendor/  # 如果执行会提示 "No such file or directory"
```

## Vendor 目录结构

如果需要引入 Vendor，标准的 Vendor 目录结构如下：

```
vendor/
├── github.com/
│   ├── cloudwego/
│   │   └── eino/
│   ├── gorilla/
│   │   └── websocket/
│   └── ...
├── go.mod              # 由 go mod vendor 自动生成
├── go.sum              # 由 go mod vendor 自动生成
└── modules.txt         # 由 go mod vendor 自动生成
```

## 何时使用 Vendor

### 推荐使用 Vendor 的场景

1. **离线构建环境**：CI/CD 环境无法访问互联网
2. **依赖可靠性要求高**：需要确保每次构建使用完全相同的依赖版本
3. **内部网络受限**：无法访问 go module proxy
4. **合规要求**：需要对所有依赖进行代码审查

### 不推荐使用 Vendor 的场景

1. **快速迭代阶段**：频繁变更依赖时，Vendor 会增加管理负担
2. **无需离线构建**：CI/CD 可以正常访问互联网
3. **磁盘空间受限**：Vendor 目录会增加仓库体积

### 当前阶段的建议

推荐在以下时间点引入 Vendor：
- 项目进入维护期（非快速迭代阶段）
- CI/CD 环境被限制访问外网
- 发布了正式版本（v1.0+）后

## Vendor 操作指南

### 初始化 Vendor

```bash
# 在项目根目录执行
go mod vendor

# 这会创建 vendor/ 目录并复制所有依赖
```

### 更新 Vendor

```bash
# 1. 更新依赖版本
go get github.com/cloudwego/eino@v0.10.0

# 2. 重新生成 vendor
go mod vendor

# 3. 验证
go build ./...
```

### 使用 Vendor 构建

```bash
# 使用 vendor 目录构建（需传递 -mod=vendor）
GOFLAGS=-mod=vendor go build ./...

# 或者在 go.mod 中设置
# go 1.26
```

当项目中存在 `vendor/modules.txt` 且 Go 版本 >= 1.14 时，Go 会自动使用 vendor 目录。

### 清理 Vendor

```bash
# 如果不打算继续使用 vendor
rm -rf vendor/

# 确保 go.mod 中的 require 块正常
go mod tidy
```

## Vendoring 与 Module Cache 对比

| 维度 | Vendor 目录 | Module Cache |
|------|-------------|--------------|
| 存储位置 | 项目内 `vendor/` | `$GOPATH/pkg/mod` |
| 版本控制 | ✅ 可提交到 Git | ❌ 不提交到 Git |
| 构建可重现性 | ✅ 完全可控 | ⚠️ 依赖 proxy 可用性 |
| 磁盘空间 | 项目内占用 | 全局共享缓存 |
| 更新流程 | 手动 `go mod vendor` | 自动从 proxy 下载 |
| 离线构建 | ✅ 支持 | ❌ 需要预缓存 |
| 代码审查 | ✅ 可在 PR 中审查 | ❌ 不包含在仓库中 |
| Go 版本要求 | Go 1.11+ | Go 1.11+ |

### 构建速度对比

```
Module Cache（首次）: 慢（需要下载所有依赖）
Module Cache（后续）: 快（本地缓存）
Vendor（首次）: 中等（git clone 包含 vendor）
Vendor（后续）: 快（本地文件）
```

### 磁盘空间影响

```bash
# 预估 vendor 目录大小
# 22 个直接依赖 + ~55 个间接依赖
# 预估 vendor 目录约 100-200MB

# go module cache 大小（全部项目共享）
du -sh $GOPATH/pkg/mod
# 通常 1-5GB（取决于有多少 Go 项目）
```

## Vendor 最佳实践

### 如果启用 Vendor

1. **提交 vendor 目录**：确保构建可重现
2. **使用 .gitignore 排除**：不要排除 vendor（除非有特殊原因）
3. **定期更新**：每次依赖变更后运行 `go mod vendor`
4. **PR 审查 vendor**：在 Code Review 中关注 vendor 变更（可以使用 GitHub 的 "Hide whitespace" 功能）

### Vendor 的 .gitignore 配置

```gitignore
# 不要排除 vendor 目录
# !vendor/
```

### 减小 Vendor 体积的技巧

```bash
# 移除不必要的测试文件
find vendor -name "*_test.go" -delete

# 移除示例目录
rm -rf vendor/github.com/example/examples

# 注意：这些操作需要每次 go mod vendor 后重新执行
```

## Vendor 与 CI/CD

### 使用 Vendor 的 CI 配置

```yaml
# GitHub Actions 示例
jobs:
  build:
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
          cache: true
          cache-dependency-path: "**/go.sum"

      - name: Build with vendor
        run: |
          GOFLAGS=-mod=vendor go build ./...

      - name: Test with vendor
        run: |
          GOFLAGS=-mod=vendor go test -short ./...
```

### 不使用 Vendor 的 CI 配置

```yaml
# 当前项目使用的配置（无 vendor）
jobs:
  build:
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
          cache: true

      - name: Download dependencies
        run: go mod download

      - name: Build
        run: go build ./...

      - name: Test
        run: go test -short ./...
```

## 依赖验证

无论是否使用 Vendor，都应验证依赖的完整性：

```bash
# 验证 go.sum 与依赖的一致性
go mod verify

# 检查是否有未使用的依赖
go mod tidy -v
```

## 常见问题

### Vendor 目录和 go.sum 不一致

```bash
# 现象：构建时报 checksum mismatch
# 解决方案：重新生成 vendor
go mod vendor
```

### Vendor 目录包含敏感文件

Vendor 目录是 Go 自动生成的，不应手动修改。如果依赖包含敏感信息（如测试用的 API Key），应向上游仓库报告。

### Go 版本变更后的 Vendor

```bash
# 升级 Go 版本后，需要重新 vendor（如果启用）
go mod vendor
```

## 决策记录

### 当前选择：不使用 Vendor

**决策时间**：项目初始阶段
**理由**：
1. 项目处于快速迭代阶段，依赖变更频繁
2. CI/CD 环境可正常访问 Go Module Proxy
3. 减少仓库体积，加快 git clone 速度
4. 团队熟悉 go module 的标准工作流

**何时重新评估**：
- 发布 v1.0 版本
- CI/CD 环境被限制访问外部网络
- 需要离线构建支持
