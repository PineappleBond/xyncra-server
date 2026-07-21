import { chromium, FullConfig } from '@playwright/test'

/**
 * Playwright 全局设置
 *
 * 功能：
 * 1. 启动单个浏览器实例，所有测试共享
 * 2. 避免每个测试文件都打开/关闭浏览器
 * 3. 保持 WebSocket 连接稳定
 *
 * 解决的问题：
 * - 浏览器频繁开关导致 WebSocket 断开
 * - Agent 正在处理时浏览器关闭，导致函数调用失败
 * - 测试间状态污染
 */

// 全局浏览器实例
let browser: any = null

async function globalSetup(config: FullConfig) {
  console.log('🚀 启动 Playwright 全局设置...')

  // 启动浏览器实例
  browser = await chromium.launch({
    args: [
      // 禁用自动化检测
      '--disable-blink-features=AutomationControlled',
      // 禁用 GPU 加速（提高稳定性）
      '--disable-gpu',
      // 禁用沙盒模式（某些环境需要）
      '--no-sandbox',
      // 禁用开发者工具
      '--disable-dev-shm-usage',
    ],
  })

  // 保存浏览器实例到环境变量，供测试使用
  process.env.PLAYWRIGHT_BROWSER_INSTANCE = 'launched'

  console.log('✅ 浏览器实例已启动')

  // 返回清理函数
  return async () => {
    console.log('🛑 关闭 Playwright 全局设置...')
    if (browser) {
      await browser.close()
      browser = null
      console.log('✅ 浏览器实例已关闭')
    }
  }
}

export default globalSetup
