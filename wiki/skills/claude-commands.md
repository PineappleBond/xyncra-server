# Claude Code Commands

## 概述

Claude Code 提供了一套自定义命令体系，用于快速执行常见的开发操作。这些命令通过斜杠（`/`）触发，比完整的自然语言描述更高效。

## 可用命令

### 开发命令

| 命令 | 说明 | 使用时机 |
|------|------|----------|
| `/skill` | 加载指定 Skill | 需要特定领域知识时 |
| `/plan` | 进入规划模式 | 复杂功能实施前 |
| `/review` | 代码审查 | 提交前检查代码质量 |

### 测试命令

| 命令 | 说明 | 使用时机 |
|------|------|----------|
| `make test` | 运行单元测试 | 修改代码后验证 |
| `make test-all` | 运行全部测试 | 全面回归验证 |
| `make test-e2e` | 运行 E2E 测试 | 端到端功能验证 |

### 构建命令

| 命令 | 说明 | 使用时机 |
|------|------|----------|
| `make build` | 编译服务器和客户端 | 验证代码可编译 |
| `make docker-build` | 构建 Docker 镜像 | 发布前构建 |
| `make release` | 交叉编译多平台 | 准备发布包 |

### 代码质量命令

| 命令 | 说明 | 使用时机 |
|------|------|----------|
| `make fmt` | 格式化代码 | 提交前格式化 |
| `make vet` | 静态分析 | 发现潜在问题 |
| `make tidy` | 整理依赖 | 修改依赖后 |

## 如何添加新的 Command

### 通过 Makefile 添加

项目中的大部分开发命令通过 Makefile 暴露。添加新命令的步骤：

1. 在 `Makefile` 中添加目标：
   ```makefile
   ## my-command: 描述这个命令做什么
   my-command:
       @echo "Running my command..."
       ./scripts/my-script.sh
   ```

2. 使用 `##` 注释格式，这样 `make help` 会自动发现它

3. 更新相关文档，使团队知晓新命令

### 通过 AGENTS.md 添加

对于 AI 特定的命令或提示词，可在 AGENTS.md 中定义操作模式：

```markdown
## 自定义操作模式

当我说 "/do-something" 时，请执行以下步骤：
1. 步骤一
2. 步骤二
3. 步骤三
```

### 通过 Shell 脚本添加

对于复杂的多步骤操作，创建 Shell 脚本：

1. 在项目根目录或 `scripts/` 下创建脚本文件
2. 添加执行权限：`chmod +x scripts/my-command.sh`
3. 可以在 Makefile 中添加对应的目标来调用

## 命令设计原则

### 一致性

- 命名风格统一（使用小写字母和连字符）
- 参数顺序一致
- 输出格式一致

### 安全

- 破坏性操作（删除、重置）必须有确认机制
- 默认不执行危险操作
- 提供 `--dry-run` 选项预览效果

### 可发现

- `make help` 能列出所有命令
- 每个命令都有清晰的描述
- 复杂命令有使用示例

## 命令使用示例

### 日常开发

```bash
# 修改代码后验证
make fmt && make vet && make test

# 编译并运行
make build
./bin/xyncra-server -addr :8080

# 在 E2E 环境中测试
make docker-e2e-up
make test-e2e
make docker-e2e-down
```

### 发布流程

```bash
# 1. 运行所有测试
make test-all

# 2. 构建 Docker 镜像
make docker-build

# 3. 交叉编译发布
make release

# 4. 产出的二进制在 dist/ 目录
ls dist/
```

## 与外部工具的集成

### Docker Compose

通过 Makefile 封装的 Docker 命令：

```bash
make docker-up       # 启动生产环境
make docker-down     # 停止生产环境
make docker-e2e-up   # 启动 E2E 环境
make docker-e2e-down # 停止 E2E 环境
```

### CLI 客户端

编译后的 `xyncra-client` 二进制提供了命令行交互能力：

```bash
make build-client
./bin/xyncra-client --help
```

## 故障排除

### 命令找不到

```bash
# 检查 Makefile 中是否有该目标
make help | grep <command>

# 检查 PATH 中是否有对应脚本
which <command>
```

### 命令执行失败

1. 检查前置条件（如 Docker 是否运行、Redis 是否可达）
2. 查看 `make` 输出的错误信息
3. 运行 `make <target> -d` 获取调试信息

## Command 与 Script 的选择

### 何时用 Makefile Target

| 优势 | 劣势 |
|------|------|
| 自带依赖管理 | 语法限制 |
| `make help` 自动发现 | 复杂逻辑用 Shell 实现 |
| 项目中广泛认可 | - |

### 何时用 Shell Script

| 优势 | 劣势 |
|------|------|
| 完整编程能力 | 需手动注册到 Makefile |
| 可测试 | 跨平台兼容性需注意 |
| 可复用 | - |

### 选择标准

```
简单任务（单步操作，无复杂逻辑）？
├── 是 → Makefile Target
└── 否 → Shell Script + Makefile Target

每次构建都需要执行？
├── 是 → Makefile Target（作为依赖）
└── 否 → 独立 Shell Script
```

## 命令的版本控制

### Makefile 的版本管理

```makefile
# 版本信息，通过 git describe 注入
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME  ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS     := -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)"
```

### 发布相关的命令

```bash
# 查看当前版本信息
grep "^VERSION" Makefile

# 构建带版本号的二进制
make build-server
./bin/xyncra-server --version  # 如果有 --version 标志

# 查看构建信息
strings ./bin/xyncra-server | grep "version\|commit\|buildTime"
```

## 命令的测试策略

### 如何测试 Makefile 命令

```bash
# 测试命令是否存在
make <command> --just-print  # 仅打印而不执行

# 测试命令的依赖是否正确
make <command> -d  # 调试模式，显示依赖解析

# 测试命令的错误处理
make <command> || echo "Expected failure: $?"
```

### 测试命令的自动化

在 CI 中验证关键命令：

```yaml
# CI 验证命令可用性
jobs:
  verify-commands:
    runs-on: ubuntu-latest
    steps:
      - name: Check build
        run: make build
      - name: Check format
        run: make fmt && git diff --exit-code
      - name: Check vet
        run: make vet
```

## 跨平台命令注意事项

### macOS vs Linux

| 命令 | macOS | Linux | 注意事项 |
|------|-------|-------|----------|
| `nc` | BSD nc | netcat | 参数略有差异 |
| `date` | BSD date | GNU date | `-u` 参数相同 |
| `sed` | BSD sed | GNU sed | `-i` 参数不同 |

### Makefile 中的跨平台处理

```makefile
# 使用跨平台兼容的方式获取时间
BUILD_TIME  ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# 使用 go 而不是系统命令
# go build 本身是跨平台的
```

## Command 的文档化

### 自文档化 Makefile

每个 Makefile target 使用 `##` 注释即可被 `make help` 发现：

```makefile
## my-command: 简明扼要的描述
my-command:
    @echo "Executing..."
```

`make help` 会解析这些注释并生成帮助信息：

```
Xyncra Server — Available targets:
  build            Compile server and client binaries into ./bin/
  build-server     Compile the xyncra-server binary
  build-client     Compile the xyncra-client binary
  clean            Remove all build artifacts (bin/ and dist/)
  fmt              Format all Go source files
  help             Show this help message
  release          Cross-compile server and client for all supported platforms
  test             Run unit tests only
  test-all         Run all tests (unit + e2e + cli-e2e)
  test-e2e         Run server-side E2E tests
  tidy             Tidy go.mod and go.sum
  vet              Run Go static analysis
```

### 文档引用

在 Wiki 中引用命令时，使用 `make <command>` 格式，并注明前置条件：

```
1. 确保 Docker 在运行
2. 执行 `make docker-e2e-up`
3. 执行 `make test-e2e`
4. 执行 `make docker-e2e-down`
```
