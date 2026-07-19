/**
 * highlightElement — Demo function that highlights a DOM element via CSS.
 *
 * Registered via useRegisterFunction so the Xyncra server can invoke it
 * through the reverse-RPC mechanism.
 *
 * @module
 */

import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';

const highlightElementInfo: FunctionInfo = {
  name: 'highlight_element',
  description: '高亮显示页面中的指定元素',
  parameters: {
    type: 'object',
    properties: {
      selector: {
        type: 'string',
        description: 'CSS 选择器，例如 .my-class 或 #my-id',
      },
      duration_ms: {
        type: 'number',
        description: '高亮持续时间（毫秒），默认 2000',
      },
    },
    required: ['selector'],
  },
};

export function HighlightElementFunction() {
  useRegisterFunction(
    highlightElementInfo,
    async (params) => {
      const selector = params.selector as string;
      const durationMs = (params.duration_ms as number) ?? 2000;
      const element = document.querySelector(selector);
      if (!element) {
        return { success: false, error: `未找到元素: ${selector}` };
      }

      const htmlElement = element as HTMLElement;
      const originalTransition = htmlElement.style.transition;
      const originalBoxShadow = htmlElement.style.boxShadow;

      htmlElement.style.transition = 'box-shadow 0.3s ease';
      htmlElement.style.boxShadow = '0 0 0 4px #1890ff';

      setTimeout(() => {
        htmlElement.style.transition = originalTransition;
        htmlElement.style.boxShadow = originalBoxShadow;
      }, durationMs);

      return { success: true, message: `已高亮元素: ${selector}` };
    },
  );

  return null;
}
