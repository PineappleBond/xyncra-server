# xyncra-client CLI E2E 测试策略文档 - 第二轮

> 基于第一轮结果 (2026-07-09, commit 7f43f99, 127/133 PASS, 95.5%)
> 创建日期: 2026-07-10
> 覆盖：6 个未解决场景 + 32 个极端场景
> 总计 38 场景（3 SKIP，35 可执行）

---

## 测试执行记录

| 日期 | Commit | 执行者 | 结果 |
|------|--------|--------|------|
| 2026-07-10 | 523e866 | Claude | 38 场景: 30 PASS, 3 WARN, 2 SKIP, 3 fixed (33/35 可执行 = 94.3%) |

---

## 目录

1. [场景清单](#1-场景清单)
   - [Category A: Previously Unresolved (6)](#category-a-previously-unresolved-6)
   - [Category B: Message Extremes (8)](#category-b-message-extremes-8)
   - [Category C: Concurrency & Race Conditions (6)](#category-c-concurrency--race-conditions-6)
   - [Category D: SQLite / Database Extremes (5)](#category-d-sqlite--database-extremes-5)
   - [Category E: IPC / Daemon Extremes (4)](#category-e-ipc--daemon-extremes-4)
   - [Category F: Sync / Network Extremes (4)](#category-f-sync--network-extremes-4)
   - [Category G: Validation Edge Cases (5)](#category-g-validation-edge-cases-5)
2. [测试环境需求](#2-测试环境需求)
3. [执行策略](#3-执行策略)
4. [通过标准](#4-通过标准)
5. [产品决策验证矩阵](#5-产品决策验证矩阵)

---

## 1. 场景清单

### Category A: Previously Unresolved (6)

> 第一轮中 6 个未完全解决的场景，需重新验证或确认为已知限制。

---

#### EXT-001: mark-as-read MAX 语义 CLI 显示问题

| 字段 | 值 |
|------|-----|
| **ID** | EXT-001 |
| **原始编号** | D12 |
| **第一轮结果** | WARN |
| **优先级** | P1 |
| **验证决策** | D-012 (mark_as_read MAX 语义：只进不退) |

**背景**: 第一轮测试发现 CLI 在执行 `mark-as-read --message-id 1`（低于当前游标 #3）后显示 "Marked as read up to message #1"，但 server 端 MAX 语义应保持游标在 #3。这是 CLI 显示 bug（显示请求值而非 server 实际游标）。

**前置设置**:
```bash
# 创建会话并发送 5 条消息
$CLI create-conversation --peer-id bob --user-id alice
CONV_ID=$(...)  # 提取 conversation ID

for i in 1 2 3 4 5; do
  $CLI send --conversation-id $CONV_ID -m "Message $i" --user-id alice
done

# 同步到本地 DB
$CLI listen --user-id alice &
sleep 5
$CLI sync-updates --user-id alice
```

**步骤**:
```bash
# 步骤 1: 标记到 #3
$CLI mark-as-read --conversation-id $CONV_ID --message-id 3 --user-id alice
echo "Exit code: $?"
# 预期: exit 0, 输出 "Marked as read up to message #3"

# 步骤 2: 尝试回退到 #1（应该被 MAX 语义阻止）
$CLI mark-as-read --conversation-id $CONV_ID --message-id 1 --user-id alice
echo "Exit code: $?"
# CLI 可能显示: "Marked as read up to message #1" (BUG)
```

**验证方法**:
```bash
# 直接查 server 端数据库确认 MAX 语义正确执行
# 通过 IPC 或 WS 查询实际游标值
# 或检查 alice 本地 DB 中 last_read_message_id 字段
sqlite3 $E2E_HOME/.xyncra/alice/<device_id>/xyncra.db \
  "SELECT last_read_message_id1, last_read_message_id2 FROM conversations WHERE id = '$CONV_ID';"
# 预期: 值 >= 3（MAX 语义生效）
```

**预期结果**: CLI 输出 "Marked as read up to message #1"（显示 bug），但 sqlite3 验证确认游标仍在 >= 3。此场景标记为 **KNOWN BUG**，需修复 CLI 显示逻辑（应显示 server 返回的实际游标值而非请求值）。

---

#### EXT-002: 空搜索 exit 0

| 字段 | 值 |
|------|-----|
| **ID** | EXT-002 |
| **原始编号** | D22 |
| **第一轮结果** | WARN |
| **优先级** | P2 |
| **验证决策** | D-035 (查询命令读本地 SQLite) |

**背景**: 第一轮中 `search-messages --query "nonexistent"` 返回 exit 0，测试者标记为 WARN。实际上空结果返回 exit 0 是正确的 UNIX 惯例（grep 无匹配返回 1 但 database query 工具通常返回 0）。

**步骤**:
```bash
# 确保 daemon 已运行且有同步数据
$CLI listen --user-id alice &
wait_for_socket

# 搜索不存在的消息
$CLI search-messages --query "zzz_nonexistent_zzz" --user-id alice 2>&1
echo "Exit code: $?"
```

**验证方法**:
```bash
# 验证 exit code 为 0
# 验证输出包含 "No messages found." 或类似空结果提示
# 验证 stderr 无错误信息
```

**预期结果**: exit 0 + 输出空结果提示（如 "No messages found."）。标记 **PASS** — 正确的 UNIX 惯例。

---

#### EXT-003: send 幂等性 (--client-msg-id)

| 字段 | 值 |
|------|-----|
| **ID** | EXT-003 |
| **原始编号** | D23 |
| **第一轮结果** | N/A |
| **优先级** | P0 |
| **验证决策** | D-006 (client_message_id 幂等性) |

**背景**: 第一轮因 `send` 命令缺少 `--client-msg-id` flag 而无法测试。需先添加该 flag 才能执行。

**前置条件**: `send` 命令需新增 `--client-msg-id` flag（代码变更）。

**步骤**:
```bash
# 步骤 1: 使用指定 client-msg-id 发送消息
UUID_1=$(python3 -c "import uuid; print(uuid.uuid4())")
$CLI send --conversation-id $CONV_ID -m "Idempotent message" \
  --client-msg-id "$UUID_1" --user-id alice
echo "Exit code: $?"
# 预期: exit 0, 输出中 Duplicate: false 或无 Duplicate 标记

# 步骤 2: 使用相同 client-msg-id 再次发送
$CLI send --conversation-id $CONV_ID -m "Idempotent message" \
  --client-msg-id "$UUID_1" --user-id alice
echo "Exit code: $?"
# 预期: exit 0, 输出中 Duplicate: true

# 步骤 3: 验证数据库中没有重复消息
sqlite3 $E2E_HOME/.xyncra/alice/<device_id>/xyncra.db \
  "SELECT COUNT(*) FROM messages WHERE client_message_id = '$UUID_1';"
# 预期: 1（只有一条记录）
```

**预期结果**: 首次发送 `duplicate=false`，重复发送 `duplicate=true`，数据库仅一条记录。需代码变更后执行。标记 **N/A (pending code change)**。

---

#### EXT-004: sync-updates --full/--force 不支持

| 字段 | 值 |
|------|-----|
| **ID** | EXT-004 |
| **原始编号** | I04 |
| **第一轮结果** | N/A |
| **优先级** | P2 |
| **验证决策** | D-036 (sync-updates IPC-only) |

**步骤**:
```bash
# 尝试使用不存在的 --full flag
$CLI sync-updates --full --user-id alice 2>&1
echo "Exit code: $?"
# 预期: exit 1, 输出 "unknown flag: --full"

# 尝试使用不存在的 --force flag
$CLI sync-updates --force --user-id alice 2>&1
echo "Exit code: $?"
# 预期: exit 1, 输出 "unknown flag: --force"
```

**预期结果**: "unknown flag" + exit 1。设计行为，sync-updates 默认执行 full sync。标记 **NOT PLANNED**。

---

#### EXT-005: standalone --timeout 不支持

| 字段 | 值 |
|------|-----|
| **ID** | EXT-005 |
| **原始编号** | J14 |
| **第一轮结果** | N/A |
| **优先级** | P2 |

**步骤**:
```bash
# 确保 daemon 未运行（standalone 模式）
$CLI kill --user-id alice 2>/dev/null

# 尝试使用 --timeout flag
$CLI send --conversation-id $CONV_ID -m "test" --timeout 30s --user-id alice 2>&1
echo "Exit code: $?"
# 预期: exit 1, 输出 "unknown flag: --timeout"
```

**预期结果**: "unknown flag" + exit 1。设计行为，当前 standalone 模式使用硬编码 5s 超时。标记 **NOT PLANNED**。

---

#### EXT-006: --log-dir 不写文件

| 字段 | 值 |
|------|-----|
| **ID** | EXT-006 |
| **原始编号** | M01 |
| **第一轮结果** | PARTIAL |
| **优先级** | P2 |
| **验证决策** | D-040 (CLI logs 数据保留策略) |

**步骤**:
```bash
# 创建自定义日志目录
mkdir -p /tmp/custom-logs

# 启动 daemon 使用 --log-dir
$CLI listen --user-id alice --log-dir /tmp/custom-logs &
DAEMON_PID=$!
sleep 5

# 执行一些操作产生日志
$CLI send --conversation-id $CONV_ID -m "test log" --user-id alice
sleep 2

# 检查日志目录
ls -la /tmp/custom-logs/
# 预期: 目录为空（Phase 1 限制）

# 清理
kill $DAEMON_PID 2>/dev/null
wait $DAEMON_PID 2>/dev/null
```

**验证方法**:
```bash
# 确认目录为空（或仅有 .gitkeep）
file_count=$(find /tmp/custom-logs -type f | wc -l)
echo "Files in log dir: $file_count"
# 预期: 0（Phase 1 不实现文件写入，cliLogger 仅写 stderr）
```

**预期结果**: 目录为空。Phase 1 限制，cliLogger 当前仅写 stderr。标记 **KNOWN LIMITATION**。

---

### Category B: Message Extremes (8)

> 测试消息内容、长度、编码的极端情况。

---

#### EXT-007: 接近 64KiB WS 消息限制

| 字段 | 值 |
|------|-----|
| **ID** | EXT-007 |
| **优先级** | P1 |

**步骤**:
```bash
# 生成 ~60KiB 内容
LARGE_CONTENT=$(python3 -c "print('X' * 61440)")  # 60 * 1024 = 61440

# 发送大消息
$CLI send --conversation-id $CONV_ID -m "$LARGE_CONTENT" --user-id alice
echo "Exit code: $?"
# 预期: exit 0, "Message sent."

# 同步并验证内容完整性
$CLI sync-updates --user-id bob
$CLI get-messages --conversation-id $CONV_ID --user-id bob --limit 1 > /tmp/msg_output.txt

# 验证内容长度
python3 -c "
content = open('/tmp/msg_output.txt').read()
# 提取消息内容部分
assert len(content) >= 61440, f'Content truncated: {len(content)}'
print(f'Content length: {len(content)} - OK')
"
```

**预期结果**: 消息成功发送和接收，内容完整无截断。

---

#### EXT-008: 超 64KiB WS 消息

| 字段 | 值 |
|------|-----|
| **ID** | EXT-008 |
| **优先级** | P1 |

**步骤**:
```bash
# 生成 ~70KiB 内容（超过常见 WS 消息限制）
HUGE_CONTENT=$(python3 -c "print('Y' * 71680)")  # 70 * 1024 = 71680

# 尝试发送超大消息
$CLI send --conversation-id $CONV_ID -m "$HUGE_CONTENT" --user-id alice 2>&1
echo "Exit code: $?"
# 预期: exit 非 0 或错误消息（被 server 或 WS 层限制拒绝）
```

**验证方法**:
```bash
# 如果被拒绝，验证 exit code 非 0
# 如果意外成功，验证 get-messages 能正确取回
# 无论结果如何，不应 panic/crash
```

**预期结果**: 被拒绝（exit code 非 0 或错误消息），或成功但内容完整。系统不应崩溃。

---

#### EXT-009: LIKE 搜索 `%` 特殊字符

| 字段 | 值 |
|------|-----|
| **ID** | EXT-009 |
| **优先级** | P1 |
| **验证决策** | D-035 (查询命令读本地 SQLite) |

**步骤**:
```bash
# 发送包含 % 的消息
$CLI send --conversation-id $CONV_ID -m "100% discount today" --user-id alice
echo "Exit code: $?"

# 等待同步
sleep 2
$CLI sync-updates --user-id alice

# 搜索包含 % 的消息
$CLI search-messages --query "100%" --user-id alice 2>&1
echo "Exit code: $?"
# 预期: 找到 "100% discount today" 消息

# 反向验证：搜索不应匹配的消息
$CLI search-messages --query "1000%" --user-id alice 2>&1
# 预期: 无结果（% 被正确转义，不匹配任意字符）
```

**验证方法**:
```bash
# 确认搜索 "100%" 只匹配字面 "100%"，不匹配 "1000" 等
# 验证 SQL LIKE 中 % 被正确转义
```

**预期结果**: `%` 被正确转义为字面字符，搜索精确匹配。

---

#### EXT-010: LIKE 搜索 `_` 特殊字符

| 字段 | 值 |
|------|-----|
| **ID** | EXT-010 |
| **优先级** | P1 |
| **验证决策** | D-035 |

**步骤**:
```bash
# 发送包含 _ 的消息
$CLI send --conversation-id $CONV_ID -m "test_value here" --user-id alice
$CLI send --conversation-id $CONV_ID -m "testXvalue here" --user-id alice

# 等待同步
sleep 2
$CLI sync-updates --user-id alice

# 搜索包含 _ 的消息
$CLI search-messages --query "test_value" --user-id alice 2>&1
echo "Exit code: $?"
# 预期: 只找到 "test_value here"，不匹配 "testXvalue here"
```

**验证方法**:
```bash
# 确认 _ 不被当作 SQL LIKE 通配符（匹配任意单字符）
# 如果 _ 未转义，"test_value" 会同时匹配 "test_value" 和 "testXvalue"
result_count=$($CLI search-messages --query "test_value" --user-id alice 2>&1 | grep -c "test")
echo "Match count: $result_count"
# 预期: 1（仅精确匹配 _ 为字面下划线）
```

**预期结果**: `_` 被正确转义为字面下划线，不匹配任意字符。

---

#### EXT-011: 超长 conversation title

| 字段 | 值 |
|------|-----|
| **ID** | EXT-011 |
| **优先级** | P2 |

**步骤**:
```bash
# 生成 1000 字符的 title
LONG_TITLE=$(python3 -c "print('A' * 1000)")

# 创建带超长 title 的会话
$CLI create-conversation --peer-id bob --title "$LONG_TITLE" --user-id alice 2>&1
echo "Exit code: $?"
# 预期: 成功（无校验则通过）或被截断/拒绝

# 如果成功，验证 title 存储
$CLI get-conversation --conversation-id $CONV_ID --user-id alice 2>&1
# 验证 title 完整或已被截断
```

**预期结果**: 成功创建（无校验限制）或被告知 title 过长。系统不应崩溃。

---

#### EXT-012: 空 content 消息

| 字段 | 值 |
|------|-----|
| **ID** | EXT-012 |
| **优先级** | P1 |

**步骤**:
```bash
# 尝试发送空内容
$CLI send --conversation-id $CONV_ID -m "" --user-id alice 2>&1
echo "Exit code: $?"
# 预期: exit 1, cobra required flag 拦截
# 输出: 'required flag(s) "content", "m" not set' 或类似
```

**验证方法**:
```bash
# 确认 exit code 为 1
# 确认错误消息提到缺少 required flag
# 确认消息未被发送（DB 无变化）
```

**预期结果**: exit 1 + cobra 拦截空 content。

---

#### EXT-013: Unicode/emoji 内容

| 字段 | 值 |
|------|-----|
| **ID** | EXT-013 |
| **优先级** | P1 |

**步骤**:
```bash
# 发送包含中文和 emoji 的消息
$CLI send --conversation-id $CONV_ID -m "Hello 世界 🌍🎉" --user-id alice
echo "Exit code: $?"

# 同步到 bob 端
$CLI sync-updates --user-id bob

# 获取消息验证内容完整
$CLI get-messages --conversation-id $CONV_ID --user-id bob --limit 1 2>&1
# 预期: 输出包含 "Hello 世界 🌍🎉"，内容完整无损
```

**验证方法**:
```bash
# 验证 Unicode 字符未被损坏
$CLI get-messages --conversation-id $CONV_ID --user-id bob --limit 1 2>&1 | grep -c "🌍"
# 预期: 1（emoji 完整保留）
```

**预期结果**: Unicode/emoji 内容完整无损地存储和检索。

---

#### EXT-014: RTL 内容

| 字段 | 值 |
|------|-----|
| **ID** | EXT-014 |
| **优先级** | P2 |

**步骤**:
```bash
# 发送阿拉伯语（RTL）消息
$CLI send --conversation-id $CONV_ID -m "مرحبا بالعالم" --user-id alice
echo "Exit code: $?"

# 同步并获取
$CLI sync-updates --user-id bob
$CLI get-messages --conversation-id $CONV_ID --user-id bob --limit 1 2>&1
```

**验证方法**:
```bash
# 验证字节级别内容完整（显示方向是终端渲染问题）
# 使用 xxd 或 hexdump 验证 UTF-8 编码正确
$CLI get-messages --conversation-id $CONV_ID --user-id bob --limit 1 2>&1 | \
  grep -c "مرحبا"
# 预期: 1
```

**预期结果**: RTL 内容存储完整（字节级别），显示方向是终端问题不影响数据完整性。

---

### Category C: Concurrency & Race Conditions (6)

> 测试并发操作、竞态条件、资源竞争。

---

#### EXT-015: 50 并行 send（MessageID 唯一性）

| 字段 | 值 |
|------|-----|
| **ID** | EXT-015 |
| **优先级** | P0 |
| **验证决策** | D-008 (MessageID uint32 单调递增，事务内分配保证不重复) |
| **风险** | Top 1 — 并发写入可能导致 MessageID 重复 |

**步骤**:
```bash
# 确保 daemon 运行
$CLI listen --user-id alice &
wait_for_socket

# 并行发送 50 条消息
for i in $(seq 1 50); do
  $CLI send --conversation-id $CONV_ID -m "Parallel msg $i" --user-id alice &
done
wait

# 同步到 bob 端
$CLI sync-updates --user-id bob

# 获取所有消息并验证 MessageID 唯一性
$CLI get-messages --conversation-id $CONV_ID --user-id bob --limit 200 > /tmp/all_msgs.txt

# 提取 MessageID 列并检查唯一性
total=$(grep -c "^[0-9]" /tmp/all_msgs.txt || true)
unique=$(awk '{print $1}' /tmp/all_msgs.txt | sort -nu | wc -l | tr -d ' ')
echo "Total messages: $total, Unique IDs: $unique"
# 预期: total == unique == 50
```

**验证方法**:
```bash
# 直接查 SQLite 验证
sqlite3 $E2E_HOME/.xyncra/bob/<device_id>/xyncra.db \
  "SELECT message_id, COUNT(*) as cnt FROM messages \
   WHERE conversation_id = '$CONV_ID' \
   GROUP BY message_id HAVING cnt > 1;"
# 预期: 空结果（无重复）

# 验证 MessageID 单调递增
sqlite3 $E2E_HOME/.xyncra/bob/<device_id>/xyncra.db \
  "SELECT message_id FROM messages \
   WHERE conversation_id = '$CONV_ID' \
   ORDER BY created_at ASC;"
# 预期: 严格递增序列
```

**预期结果**: 50 条消息全部发送成功，所有 MessageID 唯一且单调递增。

---

#### EXT-016: 100+ 会话

| 字段 | 值 |
|------|-----|
| **ID** | EXT-016 |
| **优先级** | P1 |

**步骤**:
```bash
# 循环创建 100 个会话（使用不同 peer）
for i in $(seq 1 100); do
  peer="user_$i"
  $CLI create-conversation --peer-id "$peer" --title "Conv $i" --user-id alice >/dev/null 2>&1 &
  # 限制并行数避免过载
  if (( i % 10 == 0 )); then wait; fi
done
wait

# 查询所有会话
$CLI list-conversations --user-id alice --limit 200 > /tmp/convs.txt
count=$(grep -c "^" /tmp/convs.txt || true)
echo "Conversation count: $count"
# 预期: 100

# 边界测试：1000 会话 — SKIP（时间不可行）
```

**预期结果**: 100 个会话全部创建成功，list-conversations 返回正确计数。1000 会话 SKIP（耗时不可行）。

---

#### EXT-017: 并发 mark-as-read（MAX 语义）

| 字段 | 值 |
|------|-----|
| **ID** | EXT-017 |
| **优先级** | P0 |
| **验证决策** | D-012 (MAX 语义：只进不退) |

**步骤**:
```bash
# 前置: alice 和 bob 有会话，会话中有 10 条消息
# 设备 A (alice-dev1) 和设备 B (alice-dev2) 同时操作

# 设备 A: 标记到 #5
$CLI mark-as-read --conversation-id $CONV_ID --message-id 5 \
  --user-id alice --device-id dev-A &
PID_A=$!

# 设备 B: 标记到 #3（较低值）
$CLI mark-as-read --conversation-id $CONV_ID --message-id 3 \
  --user-id alice --device-id dev-B &
PID_B=$!

wait $PID_A $PID_B

# 同步两个设备
$CLI sync-updates --user-id alice --device-id dev-A
$CLI sync-updates --user-id alice --device-id dev-B
```

**验证方法**:
```bash
# 验证设备 A 的游标
sqlite3 $E2E_HOME/.xyncra/alice/dev-A/xyncra.db \
  "SELECT last_read_message_id1 FROM conversations WHERE id = '$CONV_ID';"
# 预期: >= 5

# 验证设备 B 的游标
sqlite3 $E2E_HOME/.xyncra/alice/dev-B/xyncra.db \
  "SELECT last_read_message_id1 FROM conversations WHERE id = '$CONV_ID';"
# 预期: >= 5（MAX 语义：B 的 mark-as-read 3 不会将游标从 5 降到 3）
```

**预期结果**: 最终游标在 #5（MAX），设备 B 的 mark-as-read 3 不会回退游标。

---

#### EXT-018: 并发 delete + send

| 字段 | 值 |
|------|-----|
| **ID** | EXT-018 |
| **优先级** | P1 |

**步骤**:
```bash
# 前置: 创建一些消息
for i in $(seq 1 5); do
  $CLI send --conversation-id $CONV_ID -m "Msg $i" --user-id alice
done

# 获取一个消息 ID 用于删除
MSG_ID=$(sqlite3 $E2E_HOME/.xyncra/alice/<device_id>/xyncra.db \
  "SELECT id FROM messages WHERE conversation_id = '$CONV_ID' LIMIT 1;")

# 并发执行: 删除 + 发送
$CLI delete-message --message-id "$MSG_ID" --user-id alice &
PID_DEL=$!

$CLI send --conversation-id $CONV_ID -m "Concurrent send" --user-id alice &
PID_SEND=$!

wait $PID_DEL $PID_SEND
echo "Delete exit: $?, Send exit: $?"
# 预期: 无 panic/crash/deadlock

# 验证 daemon 仍运行
$CLI sync-updates --user-id alice
echo "Sync exit: $?"
# 预期: 正常完成
```

**预期结果**: 并发 delete 和 send 均完成，无 panic/crash/deadlock，daemon 保持运行。

---

#### EXT-019: 10 次并行 create-conversation（find-or-create 幂等）

| 字段 | 值 |
|------|-----|
| **ID** | EXT-019 |
| **优先级** | P0 |
| **验证决策** | D-011 (create_conversation find-or-create 幂等模型) |

**步骤**:
```bash
# 10 个并行 create-conversation（相同 peer）
for i in $(seq 1 10); do
  $CLI create-conversation --peer-id bob --user-id alice &
done
wait

# 查询 alice 的会话列表
$CLI list-conversations --user-id alice > /tmp/convs_after.txt

# 统计 alice-bob 会话数量
bob_conv_count=$(grep -c "bob" /tmp/convs_after.txt || true)
echo "Alice-Bob conversations: $bob_conv_count"
# 预期: 1（find-or-create 幂等）
```

**验证方法**:
```bash
# 通过 sqlite3 直接验证
sqlite3 $E2E_HOME/.xyncra/alice/<device_id>/xyncra.db \
  "SELECT COUNT(*) FROM conversations \
   WHERE (user_id1 = 'alice' AND user_id2 = 'bob') \
   OR (user_id1 = 'bob' AND user_id2 = 'alice');"
# 预期: 1
```

**预期结果**: 只创建 1 个 conversation（幂等命中 9 次），无重复会话。

---

#### EXT-020: 5 次并行 sync-updates

| 字段 | 值 |
|------|-----|
| **ID** | EXT-020 |
| **优先级** | P1 |

**步骤**:
```bash
# 前置: bob 发送了一些消息
$CLI send --conversation-id $CONV_ID -m "test" --user-id bob

# 5 个并行 sync-updates
for i in $(seq 1 5); do
  $CLI sync-updates --user-id alice &
done
wait
echo "All sync exit codes: $?"
# 预期: 全部成功

# 验证数据一致性
$CLI get-messages --conversation-id $CONV_ID --user-id alice --limit 200 > /tmp/after_sync.txt
```

**预期结果**: 5 个并行 sync-updates 全部成功，无冲突，数据一致。

---

### Category D: SQLite / Database Extremes (5)

> 测试 SQLite 数据库在极端条件下的行为。

---

#### EXT-021: daemon 写操作中被 kill -9

| 字段 | 值 |
|------|-----|
| **ID** | EXT-021 |
| **优先级** | P0 |
| **风险** | Top 4 — 强制杀进程可能导致 DB 损坏 |

**步骤**:
```bash
# 启动 daemon
$CLI listen --user-id alice &
DAEMON_PID=$!
wait_for_socket

# 发送消息（确保有写入操作）
$CLI send --conversation-id $CONV_ID -m "Before kill" --user-id alice
sleep 1

# 在写入过程中 kill -9
$CLI send --conversation-id $CONV_ID -m "During kill" --user-id alice &
sleep 0.1  # 短暂等待让 send 开始
kill -9 $DAEMON_PID
wait $DAEMON_PID 2>/dev/null

# 重启 daemon
$CLI listen --user-id alice &
NEW_PID=$!
wait_for_socket

# 验证数据库完整性
DB_PATH="$E2E_HOME/.xyncra/alice/<device_id>/xyncra.db"
sqlite3 "$DB_PATH" "PRAGMA integrity_check;"
# 预期: "ok"

# 验证消息不丢失
$CLI get-messages --conversation-id $CONV_ID --user-id alice --limit 200 2>&1
# 预期: "Before kill" 消息存在

# 清理
kill $NEW_PID 2>/dev/null
wait $NEW_PID 2>/dev/null
```

**预期结果**: DB integrity_check = "ok"，kill -9 前的消息不丢失，daemon 可正常重启。SQLite WAL 模式保证崩溃恢复。

---

#### EXT-022: 100+ 消息

| 字段 | 值 |
|------|-----|
| **ID** | EXT-022 |
| **优先级** | P1 |

**步骤**:
```bash
$CLI listen --user-id alice &
wait_for_socket

# 发送 100 条消息
for i in $(seq 1 100); do
  $CLI send --conversation-id $CONV_ID -m "Message number $i" --user-id alice
done

# 同步
$CLI sync-updates --user-id bob

# 查询所有消息
$CLI get-messages --conversation-id $CONV_ID --user-id bob --limit 200 > /tmp/msgs_100.txt
count=$(grep -c "Message number" /tmp/msgs_100.txt || true)
echo "Message count: $count"
# 预期: 100

# 边界：10000 消息 — SKIP（时间不可行）
```

**验证方法**:
```bash
sqlite3 $E2E_HOME/.xyncra/bob/<device_id>/xyncra.db \
  "SELECT COUNT(*) FROM messages WHERE conversation_id = '$CONV_ID';"
# 预期: 100
```

**预期结果**: 100 条消息全部成功发送和接收。10000 SKIP（耗时不可行）。

---

#### EXT-023: DB 文件被删 while daemon 运行

| 字段 | 值 |
|------|-----|
| **ID** | EXT-023 |
| **优先级** | P1 |
| **验证决策** | D-044 (daemon 连接韧性) |

**步骤**:
```bash
DB_PATH="$E2E_HOME/.xyncra/alice/<device_id>/xyncra.db"

# 启动 daemon
$CLI listen --user-id alice &
DAEMON_PID=$!
wait_for_socket

# 发送消息确认正常
$CLI send --conversation-id $CONV_ID -m "Before DB delete" --user-id alice

# 删除 DB 文件（daemon 正在运行）
rm -f "$DB_PATH"
# macOS 上已打开的文件句柄仍然有效，但文件名已删除

# 尝试操作
$CLI send --conversation-id $CONV_ID -m "After DB delete" --user-id alice 2>&1
echo "Exit code: $?"
# 预期: daemon 继续运行（D-044）; 操作可能失败或成功（取决于 SQLite 行为）

# 验证 daemon 仍然运行
kill -0 $DAEMON_PID 2>/dev/null && echo "Daemon still running" || echo "Daemon dead"
# 预期: "Daemon still running"

# 清理
$CLI kill --user-id alice
```

**预期结果**: daemon 不因 DB 文件被删而退出（D-044），但后续写操作可能失败。

---

#### EXT-024: DB 文件权限改只读

| 字段 | 值 |
|------|-----|
| **ID** | EXT-024 |
| **优先级** | P1 |

**步骤**:
```bash
DB_PATH="$E2E_HOME/.xyncra/alice/<device_id>/xyncra.db"

# 启动 daemon 并同步一些数据
$CLI listen --user-id alice &
DAEMON_PID=$!
wait_for_socket
$CLI send --conversation-id $CONV_ID -m "Before readonly" --user-id alice
sleep 2

# 修改 DB 文件权限为只读
chmod 444 "$DB_PATH"

# 尝试读操作（list-conversations 读本地 DB，D-035）
$CLI list-conversations --user-id alice 2>&1
echo "Read exit code: $?"
# 预期: 可能成功（SQLite WAL 模式支持并发读）

# 尝试写操作
$CLI send --conversation-id $CONV_ID -m "After readonly" --user-id alice 2>&1
echo "Write exit code: $?"
# 预期: 失败（无法写入）

# 恢复权限
chmod 644 "$DB_PATH"

# 清理
$CLI kill --user-id alice
```

**预期结果**: 读操作可能成功（WAL 模式），写操作失败。daemon 不崩溃。

---

#### EXT-025: mark-as-read message_id 溢出

| 字段 | 值 |
|------|-----|
| **ID** | EXT-025 |
| **优先级** | P0 |
| **验证决策** | D-012 (MAX 语义) |

**步骤**:
```bash
# 前置: 会话中有 5 条消息（MessageID 1-5）

# 标记到远超最大 ID 的值
$CLI mark-as-read --conversation-id $CONV_ID --message-id 99999 --user-id alice 2>&1
echo "Exit code: $?"
# 预期: exit 0（server 应 clamp 到实际最大值 5）
```

**验证方法**:
```bash
# 验证实际游标值
sqlite3 $E2E_HOME/.xyncra/alice/<device_id>/xyncra.db \
  "SELECT last_read_message_id1 FROM conversations WHERE id = '$CONV_ID';"
# 预期: <= 实际最大 MessageID（被 clamp）

# 验证未读计数正确
$CLI get-conversation --conversation-id $CONV_ID --user-id alice 2>&1 | grep -i "unread"
# 预期: 0（所有消息已读）
```

**预期结果**: server 将 99999 clamp 到实际最大 MessageID，不报错，未读计数为 0。

---

### Category E: IPC / Daemon Extremes (4)

> 测试 IPC 机制和 daemon 进程在极端条件下的行为。

---

#### EXT-026: socket 文件被删 while daemon 运行

| 字段 | 值 |
|------|-----|
| **ID** | EXT-026 |
| **优先级** | P1 |
| **验证决策** | D-030 (Unix Socket + JSON-RPC 2.0) |

**步骤**:
```bash
SOCK_PATH="$E2E_HOME/.xyncra/alice/<device_id>/xyncra.sock"

# 启动 daemon
$CLI listen --user-id alice &
DAEMON_PID=$!
wait_for_socket

# 确认 socket 存在
ls -la "$SOCK_PATH"

# 删除 socket 文件
rm -f "$SOCK_PATH"

# 尝试 CLI 操作（通过 IPC）
$CLI send --conversation-id $CONV_ID -m "After socket delete" --user-id alice 2>&1
echo "Exit code: $?"
# 预期: IPC 失败，可能 fallback 到 standalone WS（D-032）

# 验证 daemon 仍运行
kill -0 $DAEMON_PID 2>/dev/null && echo "Daemon still running" || echo "Daemon dead"
# 预期: daemon 仍运行（D-044）

# 清理
$CLI kill --user-id alice 2>/dev/null || kill $DAEMON_PID
```

**预期结果**: IPC 失败后 fallback 到 standalone WS（D-032），daemon 保持运行（D-044）。

---

#### EXT-027: lock 文件删除后重启 daemon

| 字段 | 值 |
|------|-----|
| **ID** | EXT-027 |
| **优先级** | P1 |
| **验证决策** | D-031 (fcntl 进程锁) |

**步骤**:
```bash
LOCK_PATH="$E2E_HOME/.xyncra/alice/<device_id>/xyncra.lock"

# 启动 daemon
$CLI listen --user-id alice &
DAEMON_PID=$!
wait_for_socket

# 正常停止 daemon
$CLI kill --user-id alice
sleep 2

# 确认锁文件可能残留
ls -la "$LOCK_PATH" 2>/dev/null

# 手动删除锁文件
rm -f "$LOCK_PATH"

# 启动新 daemon
$CLI listen --user-id alice &
NEW_PID=$!
wait_for_socket

# 验证新 daemon 正常运行
$CLI sync-updates --user-id alice
echo "Exit code: $?"
# 预期: 新 daemon 正常启动和工作

# 清理
$CLI kill --user-id alice
```

**预期结果**: 新 daemon 正常启动（锁文件已删除，无冲突）。验证 fcntl 锁 + stale lock 检测正确工作。

---

#### EXT-028: IPC socket 目录权限 0500

| 字段 | 值 |
|------|-----|
| **ID** | EXT-028 |
| **优先级** | P2 |

**步骤**:
```bash
USER_DIR="$E2E_HOME/.xyncra/alice/<device_id>"

# 确保目录存在
mkdir -p "$USER_DIR"

# 修改目录权限为 0500（r-x，不可写）
chmod 0500 "$USER_DIR"

# 尝试启动 daemon
$CLI listen --user-id alice 2>&1
echo "Exit code: $?"
# 预期: socket 创建失败（无法在只读目录中创建 socket 文件）

# 恢复权限
chmod 0700 "$USER_DIR"
```

**预期结果**: daemon 启动失败，报告 socket 创建错误（权限不足）。

---

#### EXT-029: 双重删除消息幂等性

| 字段 | 值 |
|------|-----|
| **ID** | EXT-029 |
| **优先级** | P0 |
| **验证决策** | D-014 (delete_message 发送者权限) |

**步骤**:
```bash
# 发送一条消息
$CLI send --conversation-id $CONV_ID -m "Delete me" --user-id alice
MSG_ID=$($CLI get-messages --conversation-id $CONV_ID --user-id alice --limit 1 | \
  awk 'NR==1{print $NF}')  # 提取消息 UUID

# 第一次删除
$CLI delete-message --message-id "$MSG_ID" --user-id alice 2>&1
echo "First delete exit: $?"
# 预期: exit 0, 成功删除

# 第二次删除（同一消息）
$CLI delete-message --message-id "$MSG_ID" --user-id alice 2>&1
echo "Second delete exit: $?"
# 预期: 成功（幂等）或明确 "not found" 错误
```

**验证方法**:
```bash
# 验证消息确实被删除
sqlite3 $E2E_HOME/.xyncra/alice/<device_id>/xyncra.db \
  "SELECT COUNT(*) FROM messages WHERE id = '$MSG_ID' AND deleted_at IS NULL;"
# 预期: 0

# 如果第二次删除成功，说明是幂等操作
# 如果第二次删除报错 "not found"，也是可接受的行为
```

**预期结果**: 第二次删除成功（幂等）或返回明确的 "not found" 错误。系统不应崩溃或产生不一致状态。

---

### Category F: Sync / Network Extremes (4)

> 测试同步和网络异常场景。

---

#### EXT-030: 10000+ updates 同步 — SKIP

| 字段 | 值 |
|------|-----|
| **ID** | EXT-030 |
| **优先级** | P2 |
| **状态** | **SKIP**（时间不可行） |

**说明**: 生成并同步 10000+ UserUpdates 在 E2E 环境中耗时过长（估计 >30 分钟），不适合手动测试。此类场景应由性能测试或自动化 CI 覆盖。

---

#### EXT-031: 网络分区中 sync

| 字段 | 值 |
|------|-----|
| **ID** | EXT-031 |
| **优先级** | P1 |
| **验证决策** | D-044 (daemon 不因 WS 失败退出) |

**步骤**:
```bash
# 启动 E2E 环境
docker compose -f docker-compose.e2e.yml up -d
# 等待服务就绪

# 启动 daemon
$CLI listen --user-id alice &
DAEMON_PID=$!
wait_for_socket

# 停止 server（模拟网络分区）
docker compose -f docker-compose.e2e.yml stop xyncra-server-e2e
sleep 3

# 尝试 sync-updates
$CLI sync-updates --user-id alice 2>&1
echo "Sync exit code: $?"
# 预期: sync 失败（无法连接 server）

# 验证 daemon 仍然运行
kill -0 $DAEMON_PID 2>/dev/null && echo "Daemon still running" || echo "Daemon dead"
# 预期: daemon 仍运行（D-044）

# 恢复 server
docker compose -f docker-compose.e2e.yml start xyncra-server-e2e
sleep 10  # 等待 daemon 重连

# 验证 sync 恢复正常
$CLI sync-updates --user-id alice 2>&1
echo "Recovery sync exit code: $?"
# 预期: 成功
```

**预期结果**: 网络分区期间 sync 失败但 daemon 保持运行（D-044），网络恢复后 sync 正常工作。

---

#### EXT-032: server 重启期间 sync

| 字段 | 值 |
|------|-----|
| **ID** | EXT-032 |
| **优先级** | P1 |

**步骤**:
```bash
# 启动 daemon 并发送消息
$CLI listen --user-id alice &
DAEMON_PID=$!
wait_for_socket
$CLI send --conversation-id $CONV_ID -m "Before restart" --user-id alice

# 停止 server
docker compose -f docker-compose.e2e.yml stop xyncra-server-e2e

# 在 server 停止时发送消息（通过 standalone fallback）
$CLI send --conversation-id $CONV_ID -m "During restart" --user-id alice 2>&1
# 预期: standalone WS 连接失败

# 重启 server
docker compose -f docker-compose.e2e.yml start xyncra-server-e2e
sleep 10  # 等待 daemon 自动重连

# 验证 daemon 已重连
$CLI sync-updates --user-id alice 2>&1
echo "Exit code: $?"
# 预期: sync 成功，所有消息同步完成

# 清理
$CLI kill --user-id alice
```

**预期结果**: server 重启后 daemon 自动重连，sync-updates 成功恢复数据同步。

---

#### EXT-033: 并发 sync + send

| 字段 | 值 |
|------|-----|
| **ID** | EXT-033 |
| **优先级** | P0 |

**步骤**:
```bash
$CLI listen --user-id alice &
wait_for_socket

# 前置: bob 发送了一些消息
$CLI send --conversation-id $CONV_ID -m "From bob" --user-id bob

# 并发执行 sync 和 send
$CLI sync-updates --user-id alice &
PID_SYNC=$!

$CLI send --conversation-id $CONV_ID -m "Concurrent send" --user-id alice &
PID_SEND=$!

wait $PID_SYNC
SYNC_EXIT=$?
wait $PID_SEND
SEND_EXIT=$?

echo "Sync exit: $SYNC_EXIT, Send exit: $SEND_EXIT"
# 预期: 两者都成功（exit 0）
```

**验证方法**:
```bash
# 验证数据一致性
$CLI get-messages --conversation-id $CONV_ID --user-id alice --limit 200 2>&1 | \
  grep -c "Concurrent send"
# 预期: 1

# 验证 daemon 仍正常
$CLI sync-updates --user-id alice
echo "Post-test sync exit: $?"
# 预期: 0
```

**预期结果**: 并发 sync 和 send 均成功，无冲突，数据一致。

---

### Category G: Validation Edge Cases (5)

> 测试输入验证和边界条件。

---

#### EXT-034: 与自己创建会话

| 字段 | 值 |
|------|-----|
| **ID** | EXT-034 |
| **优先级** | P0 |

**步骤**:
```bash
# 尝试与自己创建会话
$CLI create-conversation --peer-id alice --user-id alice 2>&1
echo "Exit code: $?"
# 预期: 被拒绝（自己不能与自己对话）或特殊处理
```

**验证方法**:
```bash
# 如果被拒绝，验证错误消息
# 如果被允许，验证会话是否可以正常使用
sqlite3 $E2E_HOME/.xyncra/alice/<device_id>/xyncra.db \
  "SELECT * FROM conversations WHERE user_id1 = 'alice' AND user_id2 = 'alice';"
```

**预期结果**: 被拒绝（返回错误），或特殊处理（自对话模式）。系统不应崩溃。

---

#### EXT-035: 空 query 搜索

| 字段 | 值 |
|------|-----|
| **ID** | EXT-035 |
| **优先级** | P1 |
| **验证决策** | D-035 |

**步骤**:
```bash
# 搜索空字符串
$CLI search-messages --query "" --user-id alice 2>&1
echo "Exit code: $?"
# 预期: exit 1, cobra required flag 拦截空字符串
```

**验证方法**:
```bash
# 确认 exit code 为 1
# 确认错误消息提到 required flag
```

**预期结果**: cobra 拦截空 `--query`，exit 1。

---

#### EXT-036: restore 未删除的会话

| 字段 | 值 |
|------|-----|
| **ID** | EXT-036 |
| **优先级** | P1 |
| **验证决策** | D-015 (restore_conversation 幂等：对未删除的会话调用不报错) |

**步骤**:
```bash
# 创建一个活跃会话
$CLI create-conversation --peer-id bob --user-id alice
CONV_ID=$(...)

# 对活跃会话执行 restore（未删除的）
$CLI restore-conversation --conversation-id $CONV_ID --user-id alice 2>&1
echo "Exit code: $?"
# 预期: exit 0（幂等操作，D-015）
```

**验证方法**:
```bash
# 验证会话仍然正常
$CLI get-conversation --conversation-id $CONV_ID --user-id alice 2>&1
# 预期: 返回正常会话信息，deleted_at 为 NULL
```

**预期结果**: restore 未删除的会话是幂等操作，返回当前会话，不报错。

---

#### EXT-037: MessageID uint32 overflow — SKIP

| 字段 | 值 |
|------|-----|
| **ID** | EXT-037 |
| **优先级** | P2 |
| **状态** | **SKIP**（uint32 上限约 42 亿条消息/会话，不可行） |

**说明**: uint32 上限为 4,294,967,295。在 E2E 环境中发送 42 亿条消息不现实。此类溢出场景应由单元测试覆盖边界逻辑。

---

#### EXT-038: 特殊字符 conversation title

| 字段 | 值 |
|------|-----|
| **ID** | EXT-038 |
| **优先级** | P1 |

**步骤**:
```bash
# 使用包含 HTML/特殊字符的 title
SPECIAL_TITLE='Test <script>&quot;alert&quot;</script>'
$CLI create-conversation --peer-id bob --title "$SPECIAL_TITLE" --user-id alice 2>&1
echo "Exit code: $?"

# 获取会话信息
$CLI get-conversation --conversation-id $CONV_ID --user-id alice 2>&1
# 预期: title 正确显示为纯文本，无 XSS 风险
```

**验证方法**:
```bash
# 验证 title 存储完整
sqlite3 $E2E_HOME/.xyncra/alice/<device_id>/xyncra.db \
  "SELECT title FROM conversations WHERE id = '$CONV_ID';"
# 预期: 原始字符串完整保留

# 确认 CLI 输出中不含 HTML 标签被解析（纯文本显示）
```

**预期结果**: 特殊字符 title 正确存储和显示为纯文本，无 XSS 风险（CLI 是纯文本终端，无 HTML 渲染）。

---

## 2. 测试环境需求

### 基础设施

| 组件 | 配置 | 用途 |
|------|------|------|
| Redis | Docker, 端口 16379→6379, DB 15 | E2E 专用 Redis（D-043） |
| Xyncra Server | Docker, 端口 18080→8080 | E2E 专用 Server |
| docker-compose | `docker-compose.e2e.yml` | 一键启动 E2E 环境 |

### 启动命令

```bash
# 启动 E2E 环境
docker compose -f docker-compose.e2e.yml up -d

# 验证环境就绪
redis-cli -p 16379 ping           # 预期: PONG
curl -s http://localhost:18080/health  # 预期: 200 OK
```

### macOS 环境特殊处理

```bash
# socket 路径限制：使用隔离的 HOME 目录
export E2E_HOME="/tmp/xe2e-$(date +%s)"
mkdir -p "$E2E_HOME"
export HOME="$E2E_HOME"

# CLI 二进制
CLI="./xyncra-client"  # go build -o xyncra-client ./cmd/xyncra-client
```

### 工具依赖

| 工具 | 用途 |
|------|------|
| `redis-cli` | Redis 操作、FLUSHDB |
| `sqlite3` | 本地 DB 验证 |
| `curl` | Server health check |
| `socat` | IPC 调试（可选） |
| `python3` | UUID 生成、大文本生成 |
| `docker compose` | E2E 环境管理 |

---

## 3. 执行策略

### 执行顺序

按优先级从高到低执行：**P0 → P1 → P2**

每个 Category 内按 EXT-ID 顺序执行。

### 环境清理（每个 Category 前）

```bash
# 清空 Redis
redis-cli -p 16379 -n 15 FLUSHDB

# 清理本地状态
rm -rf "$E2E_HOME/.xyncra"

# 重启 E2E 服务（如需完全隔离）
docker compose -f docker-compose.e2e.yml down
docker compose -f docker-compose.e2e.yml up -d
```

### daemon 管理

```bash
# 启动 daemon 并等待 socket 就绪
start_daemon() {
  local user=$1
  local device=${2:-}
  local opts="--user-id $user"
  [ -n "$device" ] && opts="$opts --device-id $device"

  $CLI listen $opts &
  local pid=$!

  # 轮询等待 socket 出现（10s 超时，200ms 间隔）
  local sock_path="$E2E_HOME/.xyncra/$user/${device:-<default>}/xyncra.sock"
  local waited=0
  while [ ! -S "$sock_path" ] && [ $waited -lt 10000 ]; do
    sleep 0.2
    waited=$((waited + 200))
  done

  if [ ! -S "$sock_path" ]; then
    echo "ERROR: daemon socket not ready after 10s" >&2
    kill $pid 2>/dev/null
    return 1
  fi
  echo $pid
}

# 停止 daemon
stop_daemon() {
  local user=$1
  $CLI kill --user-id $user 2>/dev/null
  sleep 2
  # SIGKILL 兜底
  local pid=$(pgrep -f "xyncra-client listen.*--user-id $user")
  if [ -n "$pid" ]; then
    kill -9 $pid 2>/dev/null
    sleep 1
  fi
}
```

### 并行测试模式

```bash
# bash 后台并行
for i in $(seq 1 N); do
  command &
done
wait

# xargs 并行（控制并行度）
seq 1 N | xargs -P 10 -I {} command {}
```

### 结果记录格式

每个场景执行后记录：
```
| EXT-XXX | 描述 | PASS/FAIL/WARN/SKIP/N/A | 备注 |
```

---

## 4. 通过标准

| 等级 | 条件 | 说明 |
|------|------|------|
| **PASS** | >= 90% P0/P1 通过，且所有 P0 通过 | 可进入下一阶段 |
| **WARN** | 80-89% P0/P1 通过 | 需修复后重测 |
| **FAIL** | < 80% P0/P1 通过，或任何 P0 失败 | 需重大修复后全面重测 |

### P0 场景清单（必须全部通过）

| ID | 场景 |
|----|------|
| EXT-015 | 50 并行 send（MessageID 唯一性） |
| EXT-017 | 并发 mark-as-read（MAX 语义） |
| EXT-019 | 10 次并行 create-conversation（幂等） |
| EXT-021 | daemon 写操作中被 kill -9 |
| EXT-025 | mark-as-read message_id 溢出 |
| EXT-029 | 双重删除消息幂等性 |
| EXT-033 | 并发 sync + send |
| EXT-034 | 与自己创建会话 |

### SKIP 场景说明

| ID | 原因 |
|----|------|
| EXT-030 | 10000+ updates 同步，时间不可行（>30min） |
| EXT-037 | MessageID uint32 overflow，42 亿条不可行 |

### 最终结果汇总（38 场景）

| 场景 | 初始状态 | 修复后状态 | 说明 |
|------|----------|------------|------|
| EXT-001 | WARN | **PASS** | CLI 显示 bug 已修复（三层修复：server handler + IPC handler + CLI display） |
| EXT-002 | WARN | **PASS** | 正确设计行为，空搜索 exit 0 |
| EXT-003 | N/A | **PASS** | --client-msg-id flag 已添加，幂等性验证通过 |
| EXT-004 | N/A | **PASS** | 确认 not planned，sync-updates 默认 full sync |
| EXT-005 | N/A | **PASS** | 确认 not planned，standalone 使用硬编码 5s 超时 |
| EXT-006 | PARTIAL | **WARN** | Phase 1 known limitation：--log-dir 不写文件 |
| EXT-007~014 | -- | **8/8 PASS** | 消息极端场景全通过 |
| EXT-015~020 | -- | **6/6 PASS** | 并发竞争场景全通过 |
| EXT-021~025 | -- | **4 PASS, 1 WARN** | EXT-024 WARN：只读 DB fallback |
| EXT-026~029 | -- | **3 PASS, 1 WARN** | EXT-029 WARN：双重删除返回 error 而非静默成功 |
| EXT-030 | -- | **SKIP** | 时间不可行 |
| EXT-031~033 | -- | **3/3 PASS** | 网络极端场景全通过 |
| EXT-034~036 | -- | **3/3 PASS** | 验证边缘场景 |
| EXT-037 | -- | **SKIP** | 42 亿条不可行 |
| EXT-038 | -- | **PASS** | 特殊字符安全 |

**总计：30 PASS, 3 WARN, 2 SKIP, 3 fixed（最终 33/35 可执行场景 PASS = 94.3%）**

---

## 5. 产品决策验证矩阵

> 第二轮测试对产品决策的覆盖情况。

| 决策 | 描述 | 测试场景 | 第一轮状态 | 第二轮结果 |
|------|------|----------|-----------|-----------|
| D-006 | client_message_id 幂等性 | EXT-003 | WARN (缺 flag) | **PASS** (--client-msg-id flag 已添加) |
| D-008 | MessageID uint32 单调递增 | EXT-015 | PASS | **PASS** (并发压测验证) |
| D-011 | create_conversation find-or-create | EXT-019 | PASS | **PASS** (并发幂等验证) |
| D-012 | mark_as_read MAX 语义 | EXT-001 + EXT-017 + EXT-025 | WARN | **PASS** (CLI 显示 bug 已修复 + 并发 MAX + 溢出 clamp) |
| D-014 | delete_message 发送者权限 | EXT-029 | PASS | **WARN** (双重删除返回 error 而非静默成功) |
| D-015 | restore_conversation 幂等 | EXT-036 | -- | **PASS** (首次验证) |
| D-030 | Unix Socket + JSON-RPC 2.0 | EXT-026 + EXT-028 | PASS | **PASS** (socket 删除 + 目录权限) |
| D-031 | fcntl 进程锁 | EXT-027 | PASS | **PASS** (lock 删除后重启) |
| D-035 | 查询命令读本地 SQLite | EXT-002 + EXT-035 | PASS | **PASS** (空搜索 + 空 query) |
| D-036 | sync-updates IPC-only | EXT-004 | PASS | **PASS** (--full/--force 确认) |
| D-040 | CLI logs 7 天保留 | EXT-006 | PARTIAL | **WARN** (--log-dir Phase 1 不写文件) |
| D-044 | daemon 不因 WS 失败退出 | EXT-031 + EXT-032 | PASS | **PASS** (网络分区 + server 重启) |
| D-046 | CLI send --client-msg-id flag | EXT-003 | -- | **PASS** (新增决策) |
| D-047 | mark-as-read 显示实际游标 | EXT-001 | -- | **PASS** (新增决策) |

---

## 附录 A: 场景优先级分布

| 优先级 | 数量 | 占比 |
|--------|------|------|
| P0 | 8 | 21% |
| P1 | 19 | 50% |
| P2 | 9 | 24% |
| SKIP | 2 | 5% |
| **总计** | **38** | **100%** |

## 附录 B: 第一轮未解决场景映射

| 第一轮编号 | 第一轮结果 | 第二轮 EXT-ID | 最终状态 |
|-----------|-----------|---------------|---------|
| D12 | WARN | EXT-001 | **PASS** (CLI 显示 bug 已修复) |
| D22 | WARN | EXT-002 | **PASS** (正确设计行为) |
| D23 | N/A | EXT-003 | **PASS** (--client-msg-id flag 已添加) |
| I04 | N/A | EXT-004 | **PASS** (确认设计行为) |
| J14 | N/A | EXT-005 | **PASS** (确认设计行为) |
| M01 | PARTIAL | EXT-006 | **WARN** (Phase 1 限制，--log-dir 不写文件) |

---

> 下次测试前请参考本文档的测试执行记录表，避免重复已通过的场景。
> 测试完成后更新 [CLI_E2E_TEST_STRATEGY.md](CLI_E2E_TEST_STRATEGY.md) 的测试执行记录。
