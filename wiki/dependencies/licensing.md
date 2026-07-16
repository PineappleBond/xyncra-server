# Licensing

## 项目 License

Xyncra Server 使用 **MIT License**。

### MIT 核心要求

| 要求 | 说明 |
|------|------|
| 保留版权声明 | 在修改后的文件中保留原始版权声明和许可声明 |
| 包含许可证副本 | 分发时包含 `LICENSE` 文件 |
| 免责声明 | 不提供任何形式的保证 |

## 第三方依赖 License 兼容性

### 直接依赖 License 分析

| 依赖 | License | 与 MIT 兼容 | 备注 |
|------|---------|-------------|------|
| `cloudwego/eino` | Apache 2.0 | ✅ | 宽松许可证 |
| `cloudwego/eino-ext` | Apache 2.0 | ✅ | 宽松许可证 |
| `gorilla/websocket` | BSD 2-Clause | ✅ | 宽松许可证 |
| `gorm.io/gorm` | MIT | ✅ | 相同许可证 |
| `redis/go-redis/v9` | BSD 2-Clause | ✅ | 宽松许可证 |
| `hibiken/asynq` | MIT | ✅ | 相同许可证 |
| `spf13/cobra` | Apache 2.0 | ✅ | 宽松许可证 |
| `google/uuid` | BSD 3-Clause | ✅ | 宽松许可证 |
| `stretchr/testify` | MIT | ✅ | 相同许可证 |
| `glebarez/sqlite` | MIT | ✅ | 相同许可证 |
| `gofrs/flock` | BSD 3-Clause | ✅ | 宽松许可证 |
| `mark3labs/mcp-go` | MIT | ✅ | 相同许可证 |
| `alicebob/miniredis/v2` | MIT | ✅ | 相同许可证 |
| `nikolalohinski/gonja` | MIT | ✅ | 相同许可证 |
| `eino-contrib/jsonschema` | Apache 2.0 | ✅ | 宽松许可证 |
| `robfig/cron/v3` | MIT | ✅ | 相同许可证 |

### 间接依赖 License 分析

| 间接依赖 | License | 与 MIT 兼容 |
|----------|---------|-------------|
| `bytedance/sonic` | Apache 2.0 | ✅ |
| `anthropics/anthropic-sdk-go` | Apache 2.0 | ✅ |
| `aws/aws-sdk-go-v2` | Apache 2.0 | ✅ |
| `sirupsen/logrus` | MIT | ✅ |

### License 风险判断

| 风险等级 | 含义 | 本项目情况 |
|----------|------|------------|
| 低风险 | MIT / Apache 2.0 / BSD / ISC 兼容许可证 | 绝大多数依赖 |
| 中风险 | GPL 类弱传染性许可证 | 不存在 |
| 高风险 | AGPL 类强传染性许可证 | 不存在 |

## License 合规检查清单

### 发布前检查

- [ ] 项目根目录包含 `LICENSE` 文件（MIT）
- [ ] 所有依赖的许可证与 MIT 兼容
- [ ] Docker 镜像中不包含 GPL 代码
- [ ] 第三方版权声明文件已包含（如需）
- [ ] CLI 客户端输出包含版权信息

### 每年检查

- [ ] 更新依赖版本后重新审查许可证
- [ ] 检查是否有新增依赖使用了不同的许可证
- [ ] 确认所有依赖的许可证仍然兼容

### Docker 构建注意事项

Docker 多阶段构建中，运行时镜像使用 `alpine:3.20`，其许可证为 MIT。构建阶段使用 `golang:1.26-alpine`，均为兼容许可证。

```dockerfile
# 运行时镜像
FROM alpine:3.20  # MIT License

# 构建阶段
FROM golang:1.26-alpine  # BSD 3-Clause + Go license
```

## 许可证合规性维护

### 工具支持

推荐使用以下工具自动检查许可证合规性：

```bash
# 安装
go install github.com/google/go-licenses@latest

# 检查所有依赖的许可证
go-licenses check ./...

# 列出所有依赖的许可证
go-licenses csv ./...
```

### 依赖许可证记录文件

建议在项目根目录维护 `DEPENDENCIES_LICENSES.md` 或使用 `go-licenses` 自动生成：

```bash
go-licenses csv ./... > DEPENDENCIES_LICENSES.csv
```

### 修改依赖时的合规检查

1. 在 `go.mod` 中添加新依赖时，检查其许可证
2. 确认许可证与 MIT 兼容
3. 如果使用 AGPL/GPL 许可证的库，需要特殊处理（本项目目前没有）

## 项目自身 License 声明

MIT 许可证要求：
- 所有源代码文件头部应包含许可证声明
- 衍生产品必须保留原始版权声明和许可声明

建议的文件头格式：

```go
// Copyright (c) 2026 PineappleBond
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
```

## License 合规工具

### go-licenses

```bash
# 安装
go install github.com/google/go-licenses@latest

# 检查所有依赖的 License
go-licenses check ./...

# 导出 License CSV
go-licenses csv ./... > DEPENDENCIES_LICENSES.csv

# 保存所有 License 文件副本
go-licenses save ./... --save_path third_party/licenses/
```

### 集成到 CI

```yaml
# GitHub Actions 中检查 License
jobs:
  license-check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'

      - name: Check licenses
        run: |
          go install github.com/google/go-licenses@latest
          go-licenses check ./...
```

## Redistribution

### 二进制分发

MIT 许可证要求：

1. 包含 `LICENSE` 文件
2. 保留版权声明和许可声明
3. 不歪曲原始作者的贡献

### Docker 镜像分发

Docker 镜像中需要包含：

```dockerfile
# Runtime stage
FROM alpine:3.20

# 包含许可证文件
COPY LICENSE /usr/share/licenses/xyncra/
COPY third_party/licenses/ /usr/share/licenses/xyncra-third-party/
```

### Go Module 发布

如果 Xyncra 作为 Go Module 发布（供其他项目 import）：

```go
// 模块的 LICENSE 文件在根目录
// 引入 Xyncra 的项目会自动遵循 MIT License
```

## 第三方声明文件

如果是商业分发，可能需要包含 Third-Party Notices：

```text
Xyncra Server
Copyright (c) 2026 PineappleBond

This software includes the following third-party components:

1. github.com/gorilla/websocket (BSD 2-Clause)
   Copyright (c) 2013 The Gorilla WebSocket Authors

2. gorm.io/gorm (MIT)
   Copyright (c) 2013-NOW Jinzhu

3. github.com/redis/go-redis/v9 (BSD 2-Clause)
   Copyright (c) 2013 The go-redis Authors

...（完整列表请参考 DEPENDENCIES_LICENSES.csv）
```

## 常见问题

### 问：我可以将 Xyncra Server 用于商业项目吗？

答：可以。MIT 许可证允许商业使用，无需支付版税，只需保留版权声明和免责声明。

### 问：我需要公开我的修改吗？

答：不需要。MIT 许可证不要求公开修改，也不需要标记修改过的文件。这是 MIT 比 Apache 2.0 更简洁的方面之一。

### 问：我可以重新分发包含 Xyncra 的 Docker 镜像吗？

答：可以，但需要包含原始许可证文件和版权声明。

### 问：如果依赖更新了许可证怎么办？

答：每次更新依赖时，应审查新的许可证是否仍然兼容。如果不兼容，需要寻找替代方案。

### 问：MIT 和 Apache 2.0 有什么区别？

答：两者都是宽松许可证，允许商业使用、修改和再分发。主要区别在于 Apache 2.0 提供了明确的专利授权条款，而 MIT 更简洁、约束更少。MIT 只需保留版权声明和许可声明，而 Apache 2.0 还需要标记修改文件并包含 NOTICE 文件。

### 问：我需要为每个依赖单独声明吗？

答：对于 MIT、Apache 2.0 和 BSD 许可证的库，在 NOTICE 文件或文档中列出即可。部分 BSD 变体要求在文档中附加声明。

## 合规风险应对

### 如果发现不兼容的依赖

1. **标记问题**：记录该依赖及其使用的许可证
2. **寻找替代**：寻找功能相似但许可证兼容的库
3. **分离依赖**：如果无法替代，考虑将其作为独立的可选服务
4. **法律咨询**：对复杂情况寻求法律建议

### 许可证冲突处理优先级

```
1. 寻找兼容替代品（首选）
2. 升级到兼容版本
3. 隔离为独立模块（通过 IPC 通信）
4. 申请上游许可证变更
5. 法律咨询后决定是否保留
```
