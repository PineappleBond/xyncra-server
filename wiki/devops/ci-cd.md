---
last_updated: 2026-07-17
---

# CI/CD

## 概述

Xyncra Server 使用 Makefile 驱动的构建和测试流程。本文档描述当前的持续集成能力和建议的持续部署方案。

## 当前 CI 能力

Xyncra 目前没有 GitHub Actions 工作流文件，所有的构建和测试通过 Makefile 进行本地化执行。以下是可用的自动化步骤：

### 构建流水线

```
源代码 → 编译 → 单元测试 → 构建 Docker 镜像
```

### Makefile 定义的阶段

| 阶段 | 命令 | 前置条件 | 说明 |
|------|------|----------|------|
| 格式化 | `make fmt` | 无 | `gofmt -w -s .` |
| 静态分析 | `make vet` | 无 | `go vet ./...` |
| 依赖整理 | `make tidy` | 无 | `go mod tidy` |
| 单元测试 | `make test` | 无 | 使用 `-short` 标记，不依赖外部服务 |
| 构建服务器 | `make build-server` | 无 | 编译 `cmd/xyncra-server/` |
| 构建客户端 | `make build-client` | 无 | 编译 `cmd/xyncra-client/` |
| 构建全部 | `make build` | 无 | 同时编译 server 和 client |
| E2E 测试 | `make test-e2e` | Redis（16379） | 需要 Docker E2E 环境 |
| CLI E2E | `make test-cli-e2e` | Redis + Server | 需要完整 E2E 环境 |
| 全部测试 | `make test-all` | 全部 | 运行所有测试 |
| Docker 构建 | `make docker-build` | Docker | 构建生产镜像 |
| 交叉编译 | `make release` | 无 | 多平台发布 |

## 推荐的 CI 配置

### GitHub Actions 示例

```yaml
name: CI

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]

jobs:
  lint-and-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
          cache: true

      - name: Format check
        run: |
          gofmt -l . | tee /dev/stderr
          test -z "$(gofmt -l .)"

      - name: Vet
        run: go vet ./...

      - name: Unit tests
        run: go test -short ./...

      - name: Build
        run: make build

  e2e-tests:
    runs-on: ubuntu-latest
    needs: lint-and-test
    services:
      redis:
        image: redis:7-alpine
        ports:
          - 16379:6379
        options: >-
          --health-cmd "redis-cli ping"
          --health-interval 5s
          --health-timeout 3s
          --health-retries 5
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
          cache: true

      - name: E2E tests
        env:
          XYNCRA_TEST_REAL_LLM_ENABLED: ${{ secrets.XYNCRA_TEST_REAL_LLM_ENABLED }}
          XYNCRA_TEST_LLM_API_KEY: ${{ secrets.XYNCRA_TEST_LLM_API_KEY }}
          XYNCRA_TEST_LLM_BASE_URL: ${{ secrets.XYNCRA_TEST_LLM_BASE_URL }}
          XYNCRA_TEST_LLM_MODEL: ${{ secrets.XYNCRA_TEST_LLM_MODEL }}
          XYNCRA_TEST_LLM_PROVIDER: ${{ secrets.XYNCRA_TEST_LLM_PROVIDER }}
        run: make test-e2e

  docker:
    runs-on: ubuntu-latest
    needs: lint-and-test
    steps:
      - uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Login to Container Registry
        uses: docker/login-action@v3
        with:
          registry: ${{ secrets.REGISTRY_URL }}
          username: ${{ secrets.REGISTRY_USERNAME }}
          password: ${{ secrets.REGISTRY_PASSWORD }}

      - name: Build and push
        uses: docker/build-push-action@v5
        with:
          context: .
          push: true
          tags: |
            ${{ secrets.REGISTRY_URL }}/xyncra-server:latest
            ${{ secrets.REGISTRY_URL }}/xyncra-server:${{ github.sha }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

### 本地 CI 流程

在配置 CI 服务器之前，开发者应在本地完成以下流程：

```bash
# 步骤 1：代码质量
make fmt
make vet

# 步骤 2：单元测试（快速）
make test

# 步骤 3：Docker E2E 测试（较慢）
make docker-e2e-up
make test-e2e
make docker-e2e-down

# 步骤 4：构建
make build

# 步骤 5：Docker 镜像构建
make docker-build

# 步骤 6：清理
make clean
```

## Docker 镜像构建

### 多阶段构建

Docker 构建使用两阶段策略（参考 `Dockerfile`）：

```
阶段 1: golang:1.26-alpine (构建)
  ├── 下载依赖 (go mod download) → 缓存层
  └── 编译二进制 (CGO_ENABLED=0)

阶段 2: alpine:3.20 (运行)
  ├── 安装 ca-certificates, curl
  ├── 创建 xyncra 用户 (UID 1000)
  ├── 复制二进制和 agents 目录
  └── 设置 HEALTHCHECK
```

```bash
# 构建镜像
docker build -t xyncra-server:latest .

# 带版本标签
docker build -t xyncra-server:$(git describe --tags) .
```

### 镜像优化

- **基础镜像**：使用 Alpine 保持镜像小巧
- **CGO 禁用**：`CGO_ENABLED=0` 避免 C 依赖
- **剥离符号**：`-ldflags="-s -w"` 减小二进制体积
- **构建缓存**：利用 Docker 层缓存加速构建
- **非 root 运行**：使用 `xyncra` 用户运行

## 部署自动化

### 当前部署方式

```bash
# 方式一：直接二进制部署
rsync bin/xyncra-server user@host:/opt/xyncra/
ssh user@host 'systemctl restart xyncra'

# 方式二：Docker Compose 部署
scp docker-compose.yml user@host:/opt/xyncra/
ssh user@host 'cd /opt/xyncra && docker compose pull && docker compose up -d'

# 方式三：Docker 单容器部署
docker run -d \
  --name xyncra-server \
  -p 8080:8080 \
  -v xyncra-data:/data \
  -e XYNCRA_REDIS_ADDR=redis:6379 \
  xyncra-server:latest
```

### 推荐的 CD 流程

```
CI 通过 → 构建镜像 → 推送镜像仓库 → 部署到环境 → 健康检查 → 完成
```

```yaml
# GitHub Actions Deploy 示例
deploy:
  runs-on: ubuntu-latest
  needs: [lint-and-test, docker]
  environment: production
  steps:
    - name: Deploy to production
      uses: appleboy/ssh-action@v1
      with:
        host: ${{ secrets.DEPLOY_HOST }}
        username: ${{ secrets.DEPLOY_USER }}
        key: ${{ secrets.DEPLOY_KEY }}
        script: |
          cd /opt/xyncra
          docker compose pull
          docker compose up -d --wait
          # 健康检查
          if curl -f http://localhost:8080/health; then
            echo "Deploy successful"
          else
            echo "Health check failed"
            exit 1
          fi
```

## 版本发布流程

### 版本号规范

使用 Semantic Versioning：

```
vMAJOR.MINOR.PATCH
```

- MAJOR：不兼容的 API 变更
- MINOR：向后兼容的功能新增
- PATCH：向后兼容的 bug 修复

### 发布检查清单

```bash
# 1. 运行完整测试
make test-all

# 2. 更新 CHANGELOG

# 3. 创建 Tag
git tag -a v1.0.0 -m "v1.0.0: 发布说明"
git push origin v1.0.0

# 4. 交叉编译发布
make release

# 5. 构建 Docker 镜像
docker build -t xyncra-server:v1.0.0 .
docker tag xyncra-server:v1.0.0 registry.example.com/xyncra-server:v1.0.0
docker push registry.example.com/xyncra-server:v1.0.0

# 6. 部署
# （按生产环境部署流程执行）
```

## 环境管理

### 环境划分

| 环境 | 用途 | 数据库 | LLM 调用 |
|------|------|--------|----------|
| 开发 | 本地开发测试 | SQLite | 可选 |
| 测试 | PR 验证 | SQLite | Mock/真实 |
| 预发布 | 发布前验证 | PostgreSQL | 真实 |
| 生产 | 线上服务 | PostgreSQL/MySQL | 真实 |

### 环境配置管理

- 使用 `.env.{environment}` 文件管理环境特定配置
- 密钥通过 CI/CD Secrets 或云厂商 Secret Manager 注入
- 数据库连接字符串通过环境变量配置
