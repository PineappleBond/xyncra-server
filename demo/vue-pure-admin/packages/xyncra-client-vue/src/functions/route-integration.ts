import type { Router } from 'vue-router'

/**
 * 设置路由整合：在 window 上暴露 router 引用供通用函数使用，
 * 并在路由变化时记录日志。
 */
export function setupRouteIntegration(router: Router): void {
  window.__vue_router = router

  router.afterEach((to) => {
    const pageKey = routeNameToPageKey(to.name)
    console.log('[xyncra] Route changed:', pageKey, to.path)
  })
}

function routeNameToPageKey(name: string | symbol | null | undefined): string {
  if (!name) return 'unknown'
  const strName = String(name)
  return strName
    .replace(/([A-Z])/g, '-$1')
    .toLowerCase()
    .replace(/^-/, '')
}
