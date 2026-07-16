# 升级策略

## 概述

本文档定义 Xyncra Server 的依赖升级策略，确保在有新功能和安全性修复时及时更新依赖，同时降低破坏性变更的风险。

## 升级原则

1. **安全优先**：安全相关的升级优先处理，无延迟
2. **保持兼容**：升级不应破坏已有功能
3. **渐进升级**：避免跳跃式升级（跳过多个大版本）
4. **全量验证**：每次升级后运行完整测试套件
5. **记录变更**：每次升级记录变更原因和影响范围

## 依赖分类

### 按变更风险分类

| 类别 | 风险等级 | 升级频率 | 示例 |
|------|----------|----------|------|
| 框架核心 | 高 | 季度 | `cloudwego/eino` |
| 数据存储 | 高 | 季度 | `gorm.io/gorm`, `redis/go-redis` |
| LLM Provider | 中 | 月度 | `eino-ext/*` |
| 通信协议 | 中 | 半年度 | `gorilla/websocket`, `mark3labs/mcp-go` |
| 工具库 | 低 | 年度 | `google/uuid`, `spf13/cobra` |
| 测试工具 | 低 | 按需 | `stretchr/testify` |

### 按更新源分类

| 类别 | 说明 | 处理方式 |
|------|------|----------|
| 直接依赖 | go.mod 中显式列出的依赖 | 主动管理，有计划升级 |
| 间接依赖 | 通过直接依赖引入的传递依赖 | 随直接依赖一起升级 |
| 开发依赖 | 测试和开发工具 | 按需升级 |

## 升级流程

### 日常升级流程

```bash
# 1. 检查可升级的依赖
go list -u -m all | grep '\['

# 2. 升级指定依赖（小版本）
go get github.com/cloudwego/eino@v0.9.13

# 3. 整理依赖
go mod tidy

# 4. 运行测试
make test

# 5. 如果通过，提交变更
git add go.mod go.sum
git commit -m "chore(deps): upgrade eino to v0.9.13"
```

### 大版本升级流程

```bash
# 1. 阅读上游的 CHANGELOG/Release Notes
#    识别破坏性变更

# 2. 创建独立的升级分支
git checkout -b upgrade/eino-v1.0

# 3. 升级依赖
go get github.com/cloudwego/eino@v1.0.0
go mod tidy

# 4. 修复编译错误
make build

# 5. 修复测试
make test

# 6. 运行 E2E 测试
make docker-e2e-up
make test-e2e
make docker-e2e-down

# 7. 代码审查
#    提交 PR，标注为"大版本升级"

# 8. 合并
git checkout main
git merge upgrade/eino-v1.0
```

### 安全漏洞紧急升级

```bash
# 1. 评估影响范围
#    - 哪些版本受影响
#    - 是否被利用

# 2. 直接升级到修复版本
go get github.com/redis/go-redis/v9@v9.14.2

# 3. 运行关键测试
make test

# 4. 快速合并
git add go.mod go.sum
git commit -m "fix(security): upgrade go-redis to v9.14.2 (CVE-2024-XXXX)"
```

## 版本固定策略

### go.mod 中的版本

所有直接依赖都指定了精确版本（而非前缀匹配）：

```
require (
    github.com/gorilla/websocket v1.5.3    // 精确版本
    github.com/redis/go-redis/v9 v9.14.1   // 精确版本
)
```

### 何时固定版本

- 重大功能依赖（Eino、GORM、go-redis）固定小版本号
- 工具类依赖可以接受 patch 版本自动升级
- 测试依赖可以保持最新

### 间接依赖版本

间接依赖版本由 `go mod tidy` 自动管理，不手动修改。仅在以下情况干预：
- 安全漏洞修复需要提前升级
- 直接依赖需要特定的间接依赖版本
- 解决版本冲突

## 破坏性变更管理

### 识别破坏性变更

升级前检查上游的变更日志：
- 重构的接口
- 重命名或删除的函数/类型
- 配置格式变更
- 行为语义变更
- 最低 Go 版本要求变更

### 破坏性变更的影响分析

| 影响范围 | 示例 | 处理方式 |
|----------|------|----------|
| 仅内部实现 | 内部函数重命名 | 直接修改调用代码 |
| 接口变更 | Logger 接口新增方法 | 更新所有实现 |
| 配置变更 | 环境变量名变更 | 增加兼容层 |
| 行为变更 | 默认超时时间变化 | 显式设置参数 |
| 编译环境变更 | Go 版本要求提升 | 更新 CI 和开发环境 |

### 破坏性变更的迁移策略

```go
// 兼容层示例：新版本删除了旧函数
// 旧代码：
// store.FindByID(ctx, id)

// 新代码使用 Query 接口：
store.Query().Where("id = ?", id).First()

// 兼容层
// Deprecated: 使用 store.Query().Where() 替代
func (s *Store) FindByID(ctx context.Context, id string) (*Model, error) {
    return s.Query().Where("id = ?", id).First()
}
```

## 安全补丁优先级

| 优先级 | 响应时间 | 场景 |
|--------|----------|------|
| 紧急 | 24 小时内 | 存在已知利用的安全漏洞 |
| 高 | 1 周内 | 高危漏洞，无已知利用 |
| 中 | 1 个月内 | 中等风险漏洞 |
| 低 | 下次常规升级 | 低风险或需要认证的攻击面 |

### 安全补丁升级示例

```bash
# 1. 检查 CVE 影响
# CVE-2024-XXXX affects go-redis < v9.14.2

# 2. 检查当前版本
grep "redis/go-redis" go.mod
# => github.com/redis/go-redis/v9 v9.14.0

# 3. 升级
go get github.com/redis/go-redis/v9@v9.14.2
go mod tidy

# 4. 运行测试
make test

# 5. 提交
git add go.mod go.sum
git commit -m "fix(security): upgrade go-redis to v9.14.2

CVE-2024-XXXX: description of the vulnerability
Impact: all versions prior to 9.14.2
Fix: upgraded to patched version"
```

## 定期升级计划

### 季度升级

每季度执行一次常规依赖升级：

```bash
# 1. 升级所有直接依赖到最新 patch 版本
go get -u ./...

# 2. 审查破坏性变更
go build ./... 2>&1 | grep "undefined\|not used"

# 3. 运行完整测试
make test-all

# 4. 记录结果
# - 成功升级的依赖列表
# - 未升级的依赖及其原因
# - 发现的兼容性问题
```

### 升级检查清单

- [ ] 阅读 CHANGELOG / Release Notes
- [ ] 识别破坏性变更
- [ ] 更新 go.mod
- [ ] 运行 `go mod tidy`
- [ ] 运行 `make build`
- [ ] 运行 `make vet`
- [ ] 运行 `make test`
- [ ] 运行 `make test-all`（需要 Docker 环境）
- [ ] 检查 vendor 目录（如果使用 vendor）
- [ ] 提交变更记录

## 版本兼容性矩阵

### Go 版本兼容性

| Go 版本 | 项目兼容性 | 说明 |
|---------|------------|------|
| 1.26 | ✅ 完全支持 | 当前使用版本 |
| 1.25 | ⚠️ 可能兼容 | 未测试 |
| < 1.25 | ❌ 不兼容 | 依赖新特性 |

### 数据库版本兼容性

| 数据库 | 版本 | 兼容性 |
|--------|------|--------|
| SQLite | 3.x | ✅ |
| PostgreSQL | 12+ | ✅ |
| MySQL | 8.0+ | ✅ |

## 依赖废弃处理

当依赖被废弃时：

1. 寻找替代方案
2. 制定迁移计划
3. 逐步替换使用点
4. 运行完整测试验证
5. 移除旧依赖
6. 更新文档

### 废弃依赖的识别

- `go vet` 报告废弃函数使用
- 上游仓库标记为归档（archived）
- 超过 1 年无活跃维护
- 存在已知未修复的安全漏洞
