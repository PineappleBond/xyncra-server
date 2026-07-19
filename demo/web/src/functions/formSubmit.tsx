import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';
import { waitForLoadingComplete } from './dom-engine';

const formSubmitInfo: FunctionInfo = {
  name: 'form_submit',
  description: '提交当前表单（含校验等待）',
  parameters: {
    type: 'object',
    properties: {
      form_selector: { type: 'string', description: '表单 CSS 选择器（可选）' },
    },
  },
  tags: ['form', 'page-specific'],
  timeout_ms: 20000,
};

export function FormSubmitFunction() {
  useRegisterFunction(
    formSubmitInfo,
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

      const submitBtn = form.querySelector<HTMLElement>(
        '.ant-btn-primary, button[type="submit"]',
      );
      if (!submitBtn) {
        return { success: false, error: '未找到提交按钮' };
      }

      submitBtn.click();

      await new Promise((resolve) => setTimeout(resolve, 500));

      const errorItems = form.querySelectorAll('.ant-form-item-has-error');
      if (errorItems.length > 0) {
        const errors: { field: string; message: string }[] = [];
        errorItems.forEach((item) => {
          const label =
            item.querySelector('.ant-form-item-label label')?.textContent?.trim() || '';
          const messages = item.querySelectorAll('.ant-form-item-explain-error');
          messages.forEach((msg) => {
            const text = msg.textContent?.trim();
            if (text) errors.push({ field: label, message: text });
          });
        });
        return { success: false, errors };
      }

      await waitForLoadingComplete();

      return { success: true };
    },
  );

  return null;
}
