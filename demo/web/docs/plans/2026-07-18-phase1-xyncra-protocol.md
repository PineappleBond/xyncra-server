# Phase 1: xyncra-protocol 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**目标**: 创建 `xyncra-protocol` 包——纯 TypeScript 类型定义和协议常量，1:1 映射 Go `pkg/protocol/` 包。零运行时依赖。

**架构**: 作为 npm workspace 子包放在 `demo/web/packages/xyncra-protocol/`。纯类型 + const，只生成 `.d.ts`，不生成运行时代码。需要先在 `demo/web/package.json` 添加 workspaces 配置。

**技术栈**: TypeScript 5.x, npm workspaces

## 全局约束

- Node.js >= 20
- TypeScript strict mode
- 所有类型和常量 1:1 映射 Go `pkg/protocol/`
- 包名使用 `@xyncra/protocol`（workspace scope）
- 零运行时依赖（`dependencies: {}`）

## 参考文件

| 文件 | 内容 |
|------|------|
| `pkg/protocol/protocol.go` | Package, PackageType, PackageDataRequest/Response/Updates |
| `pkg/protocol/function.go` | FunctionInfo, ReturnInfo |
| `pkg/protocol/errors.go` | ResponseCode 常量, HandlerError, 错误工厂函数 |
| `demo/web/package.json` | 需要添加 workspaces 配置 |
| `demo/web/tsconfig.json` | 基础 tsconfig 参考 |

---

## 文件结构

```
demo/web/
├── package.json                          # 修改：添加 workspaces
├── tsconfig.base.json                    # 新建：共享 tsconfig 基础配置
└── packages/
    └── xyncra-protocol/
        ├── package.json                  # 包定义
        ├── tsconfig.json                 # 包级 tsconfig（继承 base）
        └── src/
            ├── package.ts                # Package 类型、PackageType 枚举、请求/响应/更新类型
            ├── function.ts               # FunctionInfo, ReturnInfo
            ├── errors.ts                 # ResponseCode 常量、ProtocolError、工厂函数
            ├── index.ts                  # 公共导出汇总
            └── __tests__/
                └── protocol.test.ts      # 类型编译验证 + 常量值验证
```

---

### Task 1: Workspace 基础设施

**Files:**
- Modify: `demo/web/package.json`
- Create: `demo/web/tsconfig.base.json`
- Create: `demo/web/packages/xyncra-protocol/package.json`
- Create: `demo/web/packages/xyncra-protocol/tsconfig.json`

- [ ] **Step 1: 在 demo/web/package.json 中添加 workspaces 配置**

在 `package.json` 顶层添加 `workspaces` 字段：

```json
{
  "workspaces": ["packages/*"],
  ...
}
```

- [ ] **Step 2: 创建共享 tsconfig 基础配置**

创建 `demo/web/tsconfig.base.json`，供所有 workspace 包继承：

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "declaration": true,
    "declarationMap": true,
    "sourceMap": true,
    "strict": true,
    "esModuleInterop": true,
    "forceConsistentCasingInFileNames": true,
    "skipLibCheck": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "outDir": "./dist",
    "rootDir": "./src"
  }
}
```

- [ ] **Step 3: 创建 xyncra-protocol 包定义**

创建 `demo/web/packages/xyncra-protocol/package.json`：

```json
{
  "name": "@xyncra/protocol",
  "version": "0.1.0",
  "description": "Xyncra WebSocket protocol types and constants",
  "type": "module",
  "main": "./dist/index.js",
  "types": "./dist/index.d.ts",
  "exports": {
    ".": {
      "types": "./dist/index.d.ts",
      "import": "./dist/index.js"
    }
  },
  "scripts": {
    "build": "tsc",
    "test": "tsc --noEmit"
  },
  "dependencies": {},
  "devDependencies": {
    "typescript": "^5.5.0"
  }
}
```

- [ ] **Step 4: 创建 xyncra-protocol tsconfig**

创建 `demo/web/packages/xyncra-protocol/tsconfig.json`：

```json
{
  "extends": "../../tsconfig.base.json",
  "compilerOptions": {
    "outDir": "./dist",
    "rootDir": "./src"
  },
  "include": ["src/**/*.ts"],
  "exclude": ["src/**/__tests__/**"]
}
```

- [ ] **Step 5: 创建目录结构并验证**

```bash
mkdir -p demo/web/packages/xyncra-protocol/src/__tests__
cd demo/web && npm install
```

预期：`npm install` 成功，识别到 workspace 包。

- [ ] **Step 6: Commit**

```bash
git add demo/web/package.json demo/web/tsconfig.base.json demo/web/packages/xyncra-protocol/
git commit -m "chore: set up npm workspace and xyncra-protocol package skeleton"
```

---

### Task 2: 协议类型定义 (package.ts)

**Files:**
- Create: `packages/xyncra-protocol/src/package.ts`
- Reference: `pkg/protocol/protocol.go`

- [ ] **Step 1: 创建 package.ts**

参考 `pkg/protocol/protocol.go`，1:1 映射以下类型：

- `PackageType` — enum（Request=0, Response=1, Updates=2）
- `Package` — interface（version?, type, data）
- `PackageDataRequest` — interface（id, method, params?, idempotency_key?, seq?）
- `ResponseCode` — type alias（number），基础常量（OK=0, Error=-1）
- `PackageDataResponse` — interface（id, code, msg, data?）
- `PackageDataUpdates` — interface（updates 数组）
- `PackageDataUpdate` — interface（seq, type, payload, created_at?）
- Update type 常量对象（`UpdateType`）：
  - message, delete_message, mark_read, conversation, gap, typing, streaming, agent_status, agent_timeout

注意：Go 的 `json.RawMessage` 映射为 TypeScript 的 `unknown`（运行时按需解析）。

- [ ] **Step 2: 编译验证**

```bash
cd demo/web/packages/xyncra-protocol && npx tsc --noEmit
```

预期：编译通过，无错误。

- [ ] **Step 3: Commit**

```bash
git add packages/xyncra-protocol/src/package.ts
git commit -m "feat(protocol): add Package types and update type constants"
```

---

### Task 3: Function 类型定义 (function.ts)

**Files:**
- Create: `packages/xyncra-protocol/src/function.ts`
- Reference: `pkg/protocol/function.go`

- [ ] **Step 1: 创建 function.ts**

参考 `pkg/protocol/function.go`，1:1 映射：

- `FunctionInfo` — interface
  - name: string
  - description?: string
  - parameters?: Record<string, unknown>
  - returns?: ReturnInfo
  - tags?: string[]
  - timeout_ms?: number
- `ReturnInfo` — interface
  - type: string
  - description?: string

- [ ] **Step 2: 编译验证**

```bash
cd demo/web/packages/xyncra-protocol && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add packages/xyncra-protocol/src/function.ts
git commit -m "feat(protocol): add FunctionInfo and ReturnInfo types"
```

---

### Task 4: 错误类型定义 (errors.ts)

**Files:**
- Create: `packages/xyncra-protocol/src/errors.ts`
- Reference: `pkg/protocol/errors.go`

- [ ] **Step 1: 创建 errors.ts**

参考 `pkg/protocol/errors.go`，1:1 映射：

**ResponseCode 常量**（扩展 Task 2 中的基础定义）：
- Client errors (-100s): ValidationError=-100, NotFound=-101, Duplicate=-102
- Permission errors (-200s): PermissionDenied=-200, Forbidden=-201
- Server errors (-300s): InternalError=-300, Unavailable=-301

**HandlerError 类**：
- 属性: code (ResponseCode), message (string), cause? (Error)
- 方法: `Error()` 方法（实现 Error interface），`unwrap()` 方法
- 工厂函数: `newHandlerError()`, `wrapError()`, `newValidationError()`, `newNotFoundError()`, `newDuplicateError()`, `newPermissionDeniedError()`, `newInternalError()`

- [ ] **Step 2: 编译验证**

```bash
cd demo/web/packages/xyncra-protocol && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add packages/xyncra-protocol/src/errors.ts
git commit -m "feat(protocol): add ResponseCode constants and HandlerError class"
```

---

### Task 5: 公共导出 + 测试 + 最终验证

**Files:**
- Create: `packages/xyncra-protocol/src/index.ts`
- Create: `packages/xyncra-protocol/src/__tests__/protocol.test.ts`

- [ ] **Step 1: 创建 index.ts 汇总导出**

```typescript
export * from './package.js';
export * from './function.js';
export * from './errors.js';
```

- [ ] **Step 2: 创建类型编译验证测试**

创建 `src/__tests__/protocol.test.ts`，验证：
1. PackageType 枚举值正确（Request=0, Response=1, Updates=2）
2. UpdateType 常量值正确（所有 9 种类型）
3. ResponseCode 常量值正确（OK=0, Error=-1, ValidationError=-100 等）
4. HandlerError 类行为正确（Error() 返回消息，cause 可 unwrap）
5. FunctionInfo 对象可正常创建

使用 Node.js 内置 `node:test` 或项目现有的 Jest。

- [ ] **Step 3: 运行测试**

```bash
cd demo/web/packages/xyncra-protocol && npm test
```

预期：所有测试通过。

- [ ] **Step 4: 构建验证**

```bash
cd demo/web/packages/xyncra-protocol && npm run build
```

预期：`dist/` 目录生成 `.js` + `.d.ts` 文件。

- [ ] **Step 5: Commit**

```bash
git add packages/xyncra-protocol/src/
git commit -m "feat(protocol): add index exports and type verification tests"
```

---

### Task 6: Phase 1 验收

- [ ] **Step 1: 完整构建 + 测试**

```bash
cd demo/web/packages/xyncra-protocol
npm run build
npm test
```

预期：构建成功，测试全通过。

- [ ] **Step 2: 确认包可被 workspace 其他包引用**

在 `demo/web` 根目录验证：

```bash
cd demo/web
node -e "const p = require('@xyncra/protocol'); console.log(Object.keys(p))"
```

预期：输出所有导出的类型和常量名称。

- [ ] **Step 3: 最终 Commit**

```bash
git commit --allow-empty -m "feat: Phase 1 complete - xyncra-protocol package"
```

---

## Phase 1 完成标准

- [x] `@xyncra/protocol` 包存在于 `demo/web/packages/xyncra-protocol/`
- [x] npm workspace 配置正确
- [x] 所有 Go `pkg/protocol/` 类型 1:1 映射为 TypeScript
- [x] 编译通过（`tsc --noEmit`）
- [x] 测试通过
- [x] 构建产物（`dist/`）包含 `.js` + `.d.ts`
- [x] 零运行时依赖
