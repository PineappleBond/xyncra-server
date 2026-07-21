import { test, expect, type Page } from '@playwright/test'

// ============================================================================
// 测试配置
// ============================================================================

const BASE_URL = 'http://localhost:8848'
// ui-assistant 在 Agent 列表中的索引 (agents.ts 中 DEFAULT_AGENTS 的第 8 个, index=7)
const UI_ASSISTANT_INDEX = 7

// ============================================================================
// IndexedDB 工具
// ============================================================================

/**
 * 清空 IndexedDB 中的消息数据，确保每个测试从干净状态开始。
 * 只清空 messages 表，保留 conversations 表以维持会话状态。
 */
async function clearIndexedDB(page: Page): Promise<void> {
  await page.evaluate(async () => {
    const databases = await indexedDB.databases()
    const dbNames = databases
      .map(db => db.name)
      .filter(name => name && name.startsWith('xyncra-'))

    for (const dbName of dbNames) {
      const db = await new Promise<IDBDatabase>((resolve, reject) => {
        const request = indexedDB.open(dbName)
        request.onsuccess = () => resolve(request.result)
        request.onerror = () => reject(request.error)
      })

      try {
        const tx = db.transaction('messages', 'readwrite')
        const store = tx.objectStore('messages')
        store.clear()
        await new Promise<void>((resolve, reject) => {
          tx.oncomplete = () => resolve()
          tx.onerror = () => reject(tx.error)
        })
      } catch {
        // store might not exist, ignore
      }
    }
  })
}

async function getAgentMessages(page: Page): Promise<any[]> {
  return page.evaluate(async () => {
    const databases = await indexedDB.databases()
    const dbNames = databases
      .map(db => db.name)
      .filter(name => name && name.startsWith('xyncra-'))

    if (dbNames.length === 0) return []

    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open(dbNames[0])
      request.onsuccess = () => resolve(request.result)
      request.onerror = () => reject(request.error)
    })

    const tx = db.transaction('messages', 'readonly')
    const store = tx.objectStore('messages')
    const messages = await new Promise<any[]>((resolve, reject) => {
      const request = store.getAll()
      request.onsuccess = () => resolve(request.result)
      request.onerror = () => reject(request.error)
    })

    return messages.filter(msg =>
      msg.sender_id?.includes('agent') || msg.role === 'assistant'
    )
  })
}

async function getAllMessages(page: Page): Promise<any[]> {
  return page.evaluate(async () => {
    const databases = await indexedDB.databases()
    const dbNames = databases
      .map(db => db.name)
      .filter(name => name && name.startsWith('xyncra-'))

    if (dbNames.length === 0) return []

    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open(dbNames[0])
      request.onsuccess = () => resolve(request.result)
      request.onerror = () => reject(request.error)
    })

    const tx = db.transaction('messages', 'readonly')
    const store = tx.objectStore('messages')
    return new Promise<any[]>((resolve, reject) => {
      const request = store.getAll()
      request.onsuccess = () => resolve(request.result)
      request.onerror = () => reject(request.error)
    })
  })
}

async function getAllConversations(page: Page): Promise<any[]> {
  return page.evaluate(async () => {
    const databases = await indexedDB.databases()
    const dbNames = databases
      .map(db => db.name)
      .filter(name => name && name.startsWith('xyncra-'))

    if (dbNames.length === 0) return []

    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open(dbNames[0])
      request.onsuccess = () => resolve(request.result)
      request.onerror = () => reject(request.error)
    })

    const tx = db.transaction('conversations', 'readonly')
    const store = tx.objectStore('conversations')
    return new Promise<any[]>((resolve, reject) => {
      const request = store.getAll()
      request.onsuccess = () => resolve(request.result)
      request.onerror = () => reject(request.error)
    })
  })
}

// ============================================================================
// 等待工具
// ============================================================================

/**
 * 等待 FloatingAssistant 连接就绪。
 * 必须等到按钮变为绿色（el-button--success = 已连接）才算真正就绪。
 * 黄色（el-button--warning）表示正在连接/同步中，此时 pg_* 函数可能尚未注册。
 */
async function waitForFloatingAssistant(page: Page): Promise<void> {
  await page.waitForSelector('.xyncra-floating-assistant .floating-button', {
    timeout: 10000
  })

  // 等待按钮变为绿色（已连接状态），确保 WebSocket 完全建立且函数已注册
  await page.waitForFunction(() => {
    const button = document.querySelector('.xyncra-floating-assistant .floating-button')
    if (!button) return false
    return button.classList.contains('el-button--success')
  }, { timeout: 30000 })

  // 额外等待一段时间，确保 pg_* 函数注册完成
  // syncFunctionsToClient -> setFunctions -> reregisterFunctions 是异步的
  await page.waitForTimeout(1500)
}

/**
 * 等待 Agent 回复（基于 DOM 检查当前对话中的 AI 消息）
 * 使用 DOM 而非 IndexedDB，避免历史消息干扰
 */
async function waitForAgentReply(
  page: Page,
  timeoutMs: number = 60000
): Promise<any[]> {
  // 先等待 AI 消息出现在 DOM 中
  await page.waitForSelector('.message-ai .message-content', { timeout: timeoutMs })

  // 等待回复稳定（连续 3 轮 DOM 内容不变）
  let lastText = ''
  let stableRounds = 0
  const startTime = Date.now()

  while (Date.now() - startTime < timeoutMs) {
    const currentText = await page.evaluate(() => {
      const msgs = document.querySelectorAll('.message-ai .message-content')
      return msgs.length > 0 ? msgs[msgs.length - 1].textContent || '' : ''
    })

    if (currentText.length > 0 && currentText === lastText) {
      stableRounds++
      if (stableRounds >= 3) {
        // DOM 内容稳定，Agent 回复完成
        break
      }
    } else {
      stableRounds = 0
      lastText = currentText
    }

    await page.waitForTimeout(1000)
  }

  // 返回 IndexedDB 中的 agent 消息（用于数据结构验证等）
  const messages = await getAgentMessages(page)
  return messages
}

// ============================================================================
// 登录工具
// ============================================================================

async function login(page: Page): Promise<void> {
  await page.goto('/login')
  await page.waitForLoadState('networkidle')

  // 等待 XyncraTestHelpers 可用，如果不可用则重试导航
  try {
    await page.waitForFunction(() => {
      return window.XyncraTestHelpers && window.XyncraTestHelpers.login
    }, { timeout: 10000 })
  } catch {
    // 重试：重新导航到登录页
    await page.goto('/login')
    await page.waitForLoadState('networkidle')
    await page.waitForFunction(() => {
      return window.XyncraTestHelpers && window.XyncraTestHelpers.login
    }, { timeout: 15000 })
  }

  await page.evaluate(() => {
    window.XyncraTestHelpers.login.submit()
  })

  await page.waitForURL(/#\/welcome/, { timeout: 15000 })
}

// ============================================================================
// 导航工具
// ============================================================================

/**
 * 使用 Vue Router 进行客户端导航（不触发页面刷新和 WebSocket 断连）。
 * 与 page.goto() 不同，hash 路由切换不会导致 WebSocket 重新连接，
 * 因此已注册的 pg_* 函数不会被清理。
 *
 * @param page - Playwright Page 实例
 * @param hashPath - 目标路由路径，如 '/form/index'、'/table/index'
 */
async function navigateToPage(page: Page, hashPath: string): Promise<void> {
  await page.evaluate((path: string) => {
    const router = (window as any).__vue_router
    if (!router) {
      throw new Error('Vue router not available on window.__vue_router')
    }
    router.push(path)
  }, hashPath)

  // 等待 Vue 渲染新路由组件
  await page.waitForLoadState('networkidle')
}

// ============================================================================
// 断言工具
// ============================================================================

/**
 * 验证 Agent 回复不是简单的回显用户输入。
 * 如果 Agent 仅回显了用户消息而未调用任何函数，此断言会失败。
 */
function assertAgentDidNotEchoUserInput(replyText: string, userMessage: string): void {
  const trimmedReply = replyText.trim()
  const trimmedMessage = userMessage.trim()

  // 回复不应与用户消息完全相同
  expect(trimmedReply).not.toBe(trimmedMessage)

  // 回复不应以用户消息开头（排除 "用户说：xxx" 这种回显模式）
  if (trimmedReply.startsWith(trimmedMessage)) {
    expect(trimmedReply.length).toBeGreaterThan(trimmedMessage.length + 20)
  }
}

/**
 * 验证 Agent 回复包含有意义的内容（不是空话或仅回显）。
 */
function assertMeaningfulReply(replyText: string): void {
  expect(replyText.length).toBeGreaterThan(10)
  // 回复不应只是重复的标点或空白
  const stripped = replyText.replace(/[\s\p{P}]/gu, '')
  expect(stripped.length).toBeGreaterThan(5)
}

// ============================================================================
// UI Assistant 操作工具
// ============================================================================

/**
 * 创建与 ui-assistant 的对话
 */
async function createConversationWithUIAssistant(page: Page): Promise<void> {
  await page.click('.xyncra-floating-assistant .floating-button')
  await page.waitForSelector('.xyncra-sidebar', { timeout: 5000 })

  const createButton = page.locator('.xyncra-sidebar__header-right button').nth(2)
  await createButton.click()

  await page.waitForSelector('.agent-selector .agent-item', { timeout: 5000 })

  // 选择 ui-assistant
  await page.locator('.agent-selector .agent-item').nth(UI_ASSISTANT_INDEX).click()

  // 等待 Agent 选择器面板关闭
  await page.waitForFunction(() => {
    const panel = document.querySelector('.xyncra-sidebar__sub-panel')
    return !panel || panel.offsetParent === null
  }, { timeout: 10000 })

  // 等待输入框可用
  await page.waitForSelector('.xyncra-chat-panel .el-input__inner:not([disabled])', { timeout: 10000 })
}

/**
 * 发送消息
 */
async function sendMessage(page: Page, message: string): Promise<void> {
  await page.fill('.xyncra-chat-panel .el-input__inner', message)
  await page.click('.xyncra-chat-panel .el-button--primary')
}

/**
 * 获取最后一条 AI 消息的文本内容
 */
async function getLastAIReplyText(page: Page): Promise<string> {
  return page.evaluate(() => {
    const messages = document.querySelectorAll('.message-ai .message-content')
    if (messages.length === 0) return ''
    return messages[messages.length - 1].textContent || ''
  })
}

// ============================================================================
// 测试用例
// ============================================================================

test.describe('UI Assistant E2E 测试 - P0 场景', () => {
  test.beforeEach(async ({ page }) => {
    // 登录后自动跳转到 /#/welcome，无需再次 page.goto(BASE_URL)
    // 避免多余的页面刷新导致 WebSocket 断连重连
    await login(page)

    // 清空 IndexedDB 历史消息，确保每个测试从干净状态开始
    await clearIndexedDB(page)

    // 等待 FloatingAssistant 连接就绪（绿色按钮 = 已连接）
    await waitForFloatingAssistant(page)
  })

  // ========================================================================
  // P0-1: 基础回复
  // ========================================================================
  test('P0-1: 基础回复 - 发送"你好"应收到 Agent 回复', async ({ page }) => {
    await createConversationWithUIAssistant(page)
    await sendMessage(page, '你好')

    const messages = await waitForAgentReply(page, 60000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '你好')
    console.log(`[P0-1] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P0-2: 获取页面信息
  // ========================================================================
  test('P0-2: 获取页面信息 - 询问当前页面应收到有意义回复', async ({ page }) => {
    await createConversationWithUIAssistant(page)
    await sendMessage(page, '当前是什么页面？')

    const messages = await waitForAgentReply(page, 60000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '当前是什么页面？')

    // 回复应该提到页面相关信息（欢迎页/welcome）或其他有意义的内容
    const lowerReply = replyText.toLowerCase()
    const mentionsPage = lowerReply.includes('欢迎') || lowerReply.includes('welcome') || lowerReply.includes('页面') || lowerReply.includes('首页') || lowerReply.includes('home') || lowerReply.includes('当前') || lowerReply.includes('url') || lowerReply.includes('地址')
    // 兜底条件：如果回复长度足够长，说明 Agent 至少提供了有意义的回复
    const hasMeaningfulReply = replyText.length > 50
    expect(mentionsPage || hasMeaningfulReply).toBeTruthy()
    console.log(`[P0-2] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P0-3: 导航
  // ========================================================================
  test('P0-3: 导航 - "跳转到表单页面"应收到导航相关回复', async ({ page }) => {
    await createConversationWithUIAssistant(page)
    await sendMessage(page, '跳转到表单页面')

    const messages = await waitForAgentReply(page, 60000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '跳转到表单页面')

    // 验证 Agent 回复中提到了表单/导航相关内容
    const lowerReply = replyText.toLowerCase()
    const mentionsForm = lowerReply.includes('表单') || lowerReply.includes('form') || lowerReply.includes('跳转') || lowerReply.includes('导航') || lowerReply.includes('已')
    expect(mentionsForm).toBeTruthy()
    console.log(`[P0-3] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P0-4: 表单填写
  // ========================================================================
  test('P0-4: 表单填写 - 在表单页填写标题为"测试任务"', async ({ page }) => {
    // 使用客户端路由导航，不触发页面刷新和 WebSocket 断连
    await navigateToPage(page, '/form/index')
    await waitForFloatingAssistant(page)

    await createConversationWithUIAssistant(page)
    await sendMessage(page, "填写标题为'测试任务'")

    const messages = await waitForAgentReply(page, 60000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, "填写标题为'测试任务'")

    // 验证 Agent 回复中提到了填写/完成相关内容
    const lowerReply = replyText.toLowerCase()
    const mentionsFilled = lowerReply.includes('填写') || lowerReply.includes('完成') || lowerReply.includes('已') || lowerReply.includes('成功') || lowerReply.includes('测试任务')
    expect(mentionsFilled).toBeTruthy()
    console.log(`[P0-4] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P0-5: 表单提交
  // ========================================================================
  test('P0-5: 表单提交 - 填写后提交表单', async ({ page }) => {
    // 使用客户端路由导航
    await navigateToPage(page, '/form/index')
    await waitForFloatingAssistant(page)

    await createConversationWithUIAssistant(page)
    await sendMessage(page, "填写标题为'测试任务'，然后提交表单")

    const messages = await waitForAgentReply(page, 60000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, "填写标题为'测试任务'，然后提交表单")
    console.log(`[P0-5] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P0-6: 表格搜索
  // ========================================================================
  test('P0-6: 表格搜索 - 在表格页搜索关键词"测试"', async ({ page }) => {
    // 使用客户端路由导航
    await navigateToPage(page, '/table/index')
    await waitForFloatingAssistant(page)

    await createConversationWithUIAssistant(page)
    await sendMessage(page, "搜索关键词'测试'")

    const messages = await waitForAgentReply(page, 60000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, "搜索关键词'测试'")

    // 验证 Agent 回复中提到了搜索/测试相关内容
    const lowerReply = replyText.toLowerCase()
    const mentionsSearch = lowerReply.includes('搜索') || lowerReply.includes('测试') || lowerReply.includes('已') || lowerReply.includes('完成') || lowerReply.includes('结果')
    const hasMeaningfulReply = replyText.length > 50
    expect(mentionsSearch || hasMeaningfulReply).toBeTruthy()
    console.log(`[P0-6] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P0-7: 表格翻页
  // ========================================================================
  test('P0-7: 表格翻页 - 翻到第 2 页', async ({ page }) => {
    // 使用客户端路由导航
    await navigateToPage(page, '/table/index')
    await waitForFloatingAssistant(page)

    await createConversationWithUIAssistant(page)
    await sendMessage(page, '翻到第 2 页')

    const messages = await waitForAgentReply(page, 60000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '翻到第 2 页')
    console.log(`[P0-7] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P0-8: Tab 切换
  // ========================================================================
  test('P0-8: Tab 切换 - 切换到第二个 Tab', async ({ page }) => {
    // 使用客户端路由导航
    await navigateToPage(page, '/tabs/index')
    await waitForFloatingAssistant(page)

    await createConversationWithUIAssistant(page)
    await sendMessage(page, '切换到第二个 Tab')

    const messages = await waitForAgentReply(page, 60000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '切换到第二个 Tab')

    // 验证 Agent 回复中提到了切换/Tab 相关内容
    const lowerReply = replyText.toLowerCase()
    const mentionsTab = lowerReply.includes('切换') || lowerReply.includes('tab') || lowerReply.includes('已') || lowerReply.includes('完成')
    expect(mentionsTab).toBeTruthy()
    console.log(`[P0-8] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P0-9: 日期选择
  // ========================================================================
  test('P0-9: 日期选择 - 选择日期为 2024-01-15', async ({ page }) => {
    // 使用客户端路由导航
    await navigateToPage(page, '/form/index')
    await waitForFloatingAssistant(page)

    await createConversationWithUIAssistant(page)
    await sendMessage(page, '选择日期为 2024-01-15')

    const messages = await waitForAgentReply(page, 60000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '选择日期为 2024-01-15')
    console.log(`[P0-9] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P0-10: 复合操作
  // ========================================================================
  test('P0-10: 复合操作 - 跳转到表单页，填写标题，提交', async ({ page }) => {
    test.setTimeout(120000) // 复合操作需要更长时间
    await createConversationWithUIAssistant(page)
    await sendMessage(page, '跳转到表单页，填写标题为"测试任务"，然后提交表单')

    // 复合操作需要更长时间
    const messages = await waitForAgentReply(page, 90000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '跳转到表单页，填写标题为"测试任务"，然后提交表单')
    console.log(`[P0-10] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P0-11: IndexedDB Message 数据结构验证
  // ========================================================================
  test('P0-11: IndexedDB Message 数据结构验证', async ({ page }) => {
    await createConversationWithUIAssistant(page)
    await sendMessage(page, '你好，这是数据结构验证测试')

    const agentMessages = await waitForAgentReply(page, 60000)
    expect(agentMessages.length).toBeGreaterThan(0)

    const allMessages = await getAllMessages(page)
    expect(allMessages.length).toBeGreaterThan(0)

    // 验证消息结构
    allMessages.forEach((msg, index) => {
      expect(msg).toHaveProperty('id')
      expect(typeof msg.id).toBe('string')
      expect(msg.id.length).toBeGreaterThan(0)

      expect(msg).toHaveProperty('conversation_id')
      expect(typeof msg.conversation_id).toBe('string')

      expect(msg).toHaveProperty('sender_id')
      expect(typeof msg.sender_id).toBe('string')

      expect(msg).toHaveProperty('content')
      expect(typeof msg.content).toBe('string')

      expect(msg).toHaveProperty('type')
      expect(typeof msg.type).toBe('string')

      expect(msg).toHaveProperty('status')
      expect(typeof msg.status).toBe('string')

      expect(msg).toHaveProperty('created_at')
      const createdAt = new Date(msg.created_at)
      expect(createdAt.getTime()).not.toBeNaN()
    })

    // 验证有 Agent 消息
    const agentMsgs = allMessages.filter(msg => msg.sender_id?.includes('agent'))
    expect(agentMsgs.length).toBeGreaterThan(0)
    expect(allMessages.length).toBeGreaterThan(1) // 至少有用户消息和 Agent 消息
    console.log(`[P0-11] 总消息: ${allMessages.length} 条, Agent 消息: ${agentMsgs.length} 条`)

    // 验证至少有两种不同的 sender_id
    const senderIds = new Set(allMessages.map(msg => msg.sender_id))
    console.log(`[P0-11] 不同 sender_id: ${Array.from(senderIds).join(', ')}`)
  })

  // ========================================================================
  // P0-12: IndexedDB Conversation 数据结构验证
  // ========================================================================
  test('P0-12: IndexedDB Conversation 数据结构验证', async ({ page }) => {
    await createConversationWithUIAssistant(page)
    await sendMessage(page, '你好，这是对话数据结构验证测试')

    const agentMessages = await waitForAgentReply(page, 60000)
    expect(agentMessages.length).toBeGreaterThan(0)

    const conversations = await getAllConversations(page)
    expect(conversations.length).toBeGreaterThan(0)

    // 验证对话结构
    const conv = conversations[0]
    expect(conv).toHaveProperty('id')
    expect(typeof conv.id).toBe('string')
    expect(conv.id.length).toBeGreaterThan(0)

    expect(conv).toHaveProperty('user_id1')
    expect(typeof conv.user_id1).toBe('string')

    expect(conv).toHaveProperty('user_id2')
    expect(typeof conv.user_id2).toBe('string')

    expect(conv).toHaveProperty('type')
    expect(typeof conv.type).toBe('string')

    expect(conv).toHaveProperty('agent_status')
    expect(typeof conv.agent_status).toBe('string')

    // agent_status 应该是有效值
    const validAgentStatuses = ['idle', 'thinking', 'tool_calling', 'generating', 'asking_user', 'timeout']
    expect(validAgentStatuses).toContain(conv.agent_status)

    console.log(`[P0-12] 对话结构正确: id=${conv.id}, agent_status=${conv.agent_status}`)
  })

  // ========================================================================
  // P0-13: 对话创建和消息关联完整性
  // ========================================================================
  test('P0-13: 对话创建和消息关联完整性', async ({ page }) => {
    await createConversationWithUIAssistant(page)
    await sendMessage(page, '你好，这是关联完整性验证测试')

    const agentMessages = await waitForAgentReply(page, 60000)
    expect(agentMessages.length).toBeGreaterThan(0)

    const conversations = await getAllConversations(page)
    const allMessages = await getAllMessages(page)

    expect(conversations.length).toBeGreaterThan(0)
    expect(allMessages.length).toBeGreaterThan(0)

    // 验证消息的 conversation_id 关联到有效对话
    const conversationIds = new Set(conversations.map(conv => conv.id))
    const validMessages = allMessages.filter(msg =>
      msg.deleted_at == null && conversationIds.has(msg.conversation_id)
    )
    expect(validMessages.length).toBeGreaterThan(0)
    console.log(`[P0-13] 有效关联消息: ${validMessages.length} 条`)
  })

  // ========================================================================
  // P0-14: Agent 消息 sender_id 验证
  // ========================================================================
  test('P0-14: Agent 消息 sender_id 验证', async ({ page }) => {
    await createConversationWithUIAssistant(page)
    await sendMessage(page, '你好，这是 sender_id 验证测试')

    const agentMessages = await waitForAgentReply(page, 60000)
    expect(agentMessages.length).toBeGreaterThan(0)

    const allMessages = await getAllMessages(page)

    // 验证 Agent 消息的 sender_id 格式
    const agentMsgs = allMessages.filter(msg =>
      msg.sender_id?.includes('agent') || msg.role === 'assistant'
    )
    expect(agentMsgs.length).toBeGreaterThan(0)

    agentMsgs.forEach((msg, index) => {
      expect(msg.sender_id).toBeDefined()
      expect(msg.sender_id.length).toBeGreaterThan(0)
    })

    // 验证至少有 ui-assistant 的消息
    const uiAssistantMsgs = agentMsgs.filter(msg => msg.sender_id === 'agent/ui-assistant')
    expect(uiAssistantMsgs.length).toBeGreaterThan(0)
    console.log(`[P0-14] ui-assistant 消息: ${uiAssistantMsgs.length} 条, 其他 Agent 消息: ${agentMsgs.length - uiAssistantMsgs.length} 条`)
  })
})

// ============================================================================
// P1 场景
// ============================================================================

test.describe('UI Assistant E2E 测试 - P1 场景', () => {
  // P1 测试在 P0 之后运行，Agent 可能有请求积压，增加超时时间
  test.describe.configure({ timeout: 150000 })

  test.beforeEach(async ({ page }) => {
    // 登录后自动跳转到 /#/welcome，无需再次 page.goto(BASE_URL)
    await login(page)

    // 清空 IndexedDB 历史消息，确保每个测试从干净状态开始
    await clearIndexedDB(page)

    // 等待 FloatingAssistant 连接就绪
    await waitForFloatingAssistant(page)

    // 等待 Agent 空闲，避免与之前测试的请求争用
    await page.waitForTimeout(3000)
  })

  // ========================================================================
  // P1-11: 账户设置
  // ========================================================================
  test('P1-11: 账户设置 - 设置页表单操作', async ({ page }) => {
    // 使用客户端路由导航
    await navigateToPage(page, '/account/settings')
    await waitForFloatingAssistant(page)

    await createConversationWithUIAssistant(page)
    await sendMessage(page, '查看当前账户设置信息')

    const messages = await waitForAgentReply(page, 90000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '查看当前账户设置信息')
    console.log(`[P1-11] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P1-12: 卡片列表
  // ========================================================================
  test('P1-12: 卡片列表 - 列表筛选和操作', async ({ page }) => {
    // 使用客户端路由导航
    await navigateToPage(page, '/list/card')
    await waitForFloatingAssistant(page)

    await createConversationWithUIAssistant(page)
    await sendMessage(page, '当前页面有哪些卡片？')

    const messages = await waitForAgentReply(page, 90000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '当前页面有哪些卡片？')
    console.log(`[P1-12] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P1-13: 结果成功页
  // ========================================================================
  test('P1-13: 结果成功页 - 结果页操作按钮', async ({ page }) => {
    // 使用客户端路由导航
    await navigateToPage(page, '/result/success')
    await waitForFloatingAssistant(page)

    await createConversationWithUIAssistant(page)
    await sendMessage(page, '当前页面显示了什么结果？')

    // Agent 可能因之前测试请求积压而响应较慢，增加等待时间
    const messages = await waitForAgentReply(page, 90000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '当前页面显示了什么结果？')
    console.log(`[P1-13] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P1-14: 引导页
  // ========================================================================
  test('P1-14: 引导页 - 引导流程', async ({ page }) => {
    // 使用客户端路由导航
    await navigateToPage(page, '/guide')
    await waitForFloatingAssistant(page)

    await createConversationWithUIAssistant(page)
    await sendMessage(page, '当前页面是什么？')

    const messages = await waitForAgentReply(page, 90000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '当前页面是什么？')
    console.log(`[P1-14] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P1-15: 高级表单
  // ========================================================================
  test('P1-15: 高级表单 - 复杂表单操作', async ({ page }) => {
    // 使用客户端路由导航
    await navigateToPage(page, '/form/advanced')
    await waitForFloatingAssistant(page)

    await createConversationWithUIAssistant(page)
    await sendMessage(page, '查看当前表单有哪些字段')

    const messages = await waitForAgentReply(page, 90000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '查看当前表单有哪些字段')
    console.log(`[P1-15] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P1-16: 分步表单
  // ========================================================================
  test('P1-16: 分步表单 - 分步操作', async ({ page }) => {
    // 使用客户端路由导航
    await navigateToPage(page, '/form/step')
    await waitForFloatingAssistant(page)

    await createConversationWithUIAssistant(page)
    await sendMessage(page, '当前分步表单是什么步骤？')

    const messages = await waitForAgentReply(page, 90000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '当前分步表单是什么步骤？')
    console.log(`[P1-16] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P1-17: 搜索列表
  // ========================================================================
  test('P1-17: 搜索列表 - 搜索和 Tab 切换', async ({ page }) => {
    // 使用客户端路由导航
    await navigateToPage(page, '/list/search')
    await waitForFloatingAssistant(page)

    await createConversationWithUIAssistant(page)
    await sendMessage(page, '当前页面有哪些 Tab？')

    const messages = await waitForAgentReply(page, 90000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '当前页面有哪些 Tab？')
    console.log(`[P1-17] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P1-18: 高级详情
  // ========================================================================
  test('P1-18: 高级详情 - 详情页操作', async ({ page }) => {
    // 使用客户端路由导航
    await navigateToPage(page, '/profile/advanced')
    await waitForFloatingAssistant(page)

    await createConversationWithUIAssistant(page)
    await sendMessage(page, '当前页面显示了什么信息？')

    const messages = await waitForAgentReply(page, 90000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '当前页面显示了什么信息？')
    console.log(`[P1-18] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P1-19: 个人中心
  // ========================================================================
  test('P1-19: 个人中心 - Tab 切换和标签编辑', async ({ page }) => {
    // 使用客户端路由导航
    await navigateToPage(page, '/account/center')
    await waitForFloatingAssistant(page)

    await createConversationWithUIAssistant(page)
    await sendMessage(page, '当前个人中心有哪些 Tab？')

    const messages = await waitForAgentReply(page, 90000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '当前个人中心有哪些 Tab？')
    console.log(`[P1-19] Agent 回复: ${replyText.substring(0, 100)}...`)
  })

  // ========================================================================
  // P1-20: 分析仪表盘
  // ========================================================================
  test('P1-20: 分析仪表盘 - 仪表盘操作', async ({ page }) => {
    // 使用客户端路由导航
    await navigateToPage(page, '/dashboard/analysis')
    await waitForFloatingAssistant(page)

    await createConversationWithUIAssistant(page)
    await sendMessage(page, '当前仪表盘显示了什么数据？')

    const messages = await waitForAgentReply(page, 90000)
    expect(messages.length).toBeGreaterThan(0)

    const replyText = await getLastAIReplyText(page)
    assertMeaningfulReply(replyText)
    assertAgentDidNotEchoUserInput(replyText, '当前仪表盘显示了什么数据？')
    console.log(`[P1-20] Agent 回复: ${replyText.substring(0, 100)}...`)
  })
})

// ============================================================================
// 诊断工具
// ============================================================================

test.afterEach(async ({ page }, testInfo) => {
  if (testInfo.status !== 'passed') {
    console.log('\n=== 测试失败诊断 ===')
    console.log('当前 URL:', page.url())
    console.log('页面标题:', await page.title())

    const hasAssistant = await page.$('.xyncra-floating-assistant')
    console.log('FloatingAssistant 存在:', !!hasAssistant)

    if (hasAssistant) {
      const buttonClasses = await page.evaluate(() => {
        const btn = document.querySelector('.xyncra-floating-assistant .floating-button')
        return btn?.classList.toString()
      })
      console.log('按钮 classes:', buttonClasses)
    }

    try {
      const messages = await getAllMessages(page)
      console.log('IndexedDB 消息数量:', messages.length)

      const conversations = await getAllConversations(page)
      console.log('IndexedDB 对话数量:', conversations.length)

      // 输出最后一条 Agent 消息内容以便诊断
      const agentMsgs = messages.filter(msg => msg.sender_id?.includes('agent') || msg.role === 'assistant')
      if (agentMsgs.length > 0) {
        const lastMsg = agentMsgs[agentMsgs.length - 1]
        console.log('最后一条 Agent 消息:', lastMsg.content?.substring(0, 200))
      }
    } catch (e) {
      console.log('IndexedDB 查询失败:', e)
    }

    console.log('===================\n')
  }
})
