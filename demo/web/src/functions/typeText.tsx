import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';
import { waitForSelector } from './dom-engine';

const typeTextInfo: FunctionInfo = {
  name: 'type_text',
  description: '在输入框中填入文本（先清空再输入）',
  parameters: {
    type: 'object',
    properties: {
      selector: { type: 'string', description: 'CSS 选择器' },
      value: { type: 'string', description: '要填入的文本' },
      clear_first: { type: 'boolean', description: '是否先清空，默认 true' },
    },
    required: ['selector', 'value'],
  },
  tags: ['dom', 'generic'],
};

export function TypeTextFunction() {
  useRegisterFunction(
    typeTextInfo,
    async (params) => {
      const selector = params.selector as string;
      const value = params.value as string;
      const clearFirst = params.clear_first !== false;

      const el = await waitForSelector(selector, 5000);
      if (!el) {
        return { success: false, error: `输入框未找到: ${selector}` };
      }

      const input = el as HTMLInputElement;
      input.focus();

      if (clearFirst) {
        input.value = '';
      }

      input.value = value;
      input.dispatchEvent(new Event('input', { bubbles: true }));
      input.dispatchEvent(new Event('change', { bubbles: true }));

      return { success: true };
    },
  );

  return null;
}
