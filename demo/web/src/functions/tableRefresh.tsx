import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';
import { waitForLoadingComplete } from './dom-engine';

const tableRefreshInfo: FunctionInfo = {
  name: 'table_refresh',
  description: '刷新当前表格数据',
  parameters: {
    type: 'object',
    properties: {
      table_selector: { type: 'string', description: '表格 CSS 选择器（可选）' },
    },
  },
  tags: ['table', 'page-specific'],
  timeout_ms: 15000,
};

export function TableRefreshFunction() {
  useRegisterFunction(
    tableRefreshInfo,
    async (params) => {
      const tableSelector = params.table_selector as string | undefined;
      const container = tableSelector
        ? document.querySelector(tableSelector)
        : document.querySelector('.ant-table-wrapper');

      if (!container) {
        return { success: false, error: '未找到表格区域' };
      }

      const refreshBtn = container.querySelector<HTMLElement>(
        '.ant-table-toolbar .ant-btn, button[title="刷新"], button[aria-label="刷新"]',
      );
      if (!refreshBtn) {
        return { success: false, error: '未找到刷新按钮' };
      }

      refreshBtn.click();
      await waitForLoadingComplete();

      return { success: true };
    },
  );

  return null;
}
