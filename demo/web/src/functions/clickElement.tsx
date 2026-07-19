import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';
import { waitForSelector, waitForLoadingComplete } from './dom-engine';

const clickElementInfo: FunctionInfo = {
  name: 'click_element',
  description: '点击页面中的指定元素',
  parameters: {
    type: 'object',
    properties: {
      selector: { type: 'string', description: 'CSS 选择器' },
      timeout_ms: { type: 'number', description: '等待元素超时(ms)，默认 5000' },
    },
    required: ['selector'],
  },
  tags: ['dom', 'generic'],
  timeout_ms: 15000,
};

export function ClickElementFunction() {
  useRegisterFunction(
    clickElementInfo,
    async (params) => {
      const selector = params.selector as string;
      const timeoutMs = (params.timeout_ms as number) || 5000;

      const el = await waitForSelector(selector, timeoutMs);
      if (!el) {
        return { success: false, error: `元素未找到: ${selector}` };
      }

      if (el.hasAttribute('disabled') || el.classList.contains('ant-btn-disabled')) {
        return { success: false, error: '元素已禁用' };
      }

      (el as HTMLElement).click();

      await waitForLoadingComplete();

      return { success: true };
    },
  );

  return null;
}
