import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';

const formResetInfo: FunctionInfo = {
  name: 'form_reset',
  description: '重置表单到初始状态',
  parameters: {
    type: 'object',
    properties: {
      form_selector: { type: 'string', description: '表单 CSS 选择器（可选）' },
    },
  },
  tags: ['form', 'page-specific'],
  timeout_ms: 10000,
};

function resetInput(el: Element) {
  const input = el as HTMLInputElement;
  const tag = el.tagName.toLowerCase();
  if (tag === 'input' || tag === 'textarea') {
    input.value = '';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    input.dispatchEvent(new Event('change', { bubbles: true }));
  } else if (tag === 'select') {
    input.value = '';
    input.dispatchEvent(new Event('change', { bubbles: true }));
  }
}

export function FormResetFunction() {
  useRegisterFunction(
    formResetInfo,
    async (params) => {
      const formSelector = params.form_selector as string | undefined;
      let form: Element | null = null;

      if (formSelector) {
        form = document.querySelector(formSelector);
      } else {
        form = document.querySelector('.ant-form') || document.querySelector('form');
      }

      if (!form) {
        return { success: false, error: '未找到表单' };
      }

      const buttons = Array.from(form.querySelectorAll('button'));
      const resetBtn = buttons.find(
        (b) => b.getAttribute('type') === 'reset' || b.textContent?.trim() === '重置',
      ) as HTMLElement | undefined;
      if (resetBtn) {
        resetBtn.click();
        return { success: true };
      }

      const inputs = form.querySelectorAll('input, select, textarea');
      inputs.forEach(resetInput);

      return { success: true };
    },
  );

  return null;
}
