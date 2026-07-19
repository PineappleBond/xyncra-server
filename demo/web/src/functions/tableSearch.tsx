import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';
import { waitForLoadingComplete } from './dom-engine';

const tableSearchInfo: FunctionInfo = {
  name: 'table_search',
  description: '在表格搜索栏中输入关键词并触发查询',
  parameters: {
    type: 'object',
    properties: {
      keyword: { type: 'string', description: '搜索关键词' },
      field_selector: { type: 'string', description: '搜索输入框的选择器（可选）' },
    },
    required: ['keyword'],
  },
  tags: ['table', 'page-specific'],
  timeout_ms: 15000,
};

export function TableSearchFunction() {
  useRegisterFunction(
    tableSearchInfo,
    async (params) => {
      const keyword = params.keyword as string;
      const fieldSelector = params.field_selector as string | undefined;

      let input: HTMLInputElement | HTMLTextAreaElement | null = null;

      if (fieldSelector) {
        input = document.querySelector(fieldSelector) as HTMLInputElement | null;
      } else {
        input = document.querySelector(
          '.ant-input-search input, .ant-table-filter-dropdown input',
        ) as HTMLInputElement | null;
      }

      if (!input) {
        return { success: false, error: '未找到搜索输入框' };
      }

      input.focus();
      input.value = keyword;
      input.dispatchEvent(new Event('input', { bubbles: true }));
      input.dispatchEvent(new Event('change', { bubbles: true }));

      const searchBtn = document.querySelector<HTMLElement>(
        '.ant-input-search-button, button[type="submit"]',
      );
      if (searchBtn) {
        searchBtn.click();
      } else {
        input.dispatchEvent(
          new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }),
        );
      }

      await waitForLoadingComplete();

      return { success: true, keyword };
    },
  );

  return null;
}
