import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';
import { waitForSelector } from './dom-engine';

const waitForElementInfo: FunctionInfo = {
  name: 'wait_for_element',
  description: '等待元素出现在 DOM 中并可交互',
  parameters: {
    type: 'object',
    properties: {
      selector: { type: 'string', description: 'CSS 选择器' },
      timeout_ms: { type: 'number', description: '超时(ms)，默认 10000' },
      visible: { type: 'boolean', description: '要求元素可见，默认 true' },
    },
    required: ['selector'],
  },
  tags: ['dom', 'generic'],
  timeout_ms: 20000,
};

export function WaitForElementFunction() {
  useRegisterFunction(
    waitForElementInfo,
    async (params) => {
      const selector = params.selector as string;
      const timeoutMs = (params.timeout_ms as number) || 10000;
      const visible = params.visible !== false;

      const el = await waitForSelector(selector, timeoutMs, visible);
      if (!el) {
        return { success: false, error: '等待超时' };
      }

      return { success: true, found: true };
    },
  );

  return null;
}
