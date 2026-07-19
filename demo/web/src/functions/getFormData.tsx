import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';
import { generateSelector } from './dom-engine';

interface FormField {
  label: string
  name: string | null
  type: string
  value: string | boolean | undefined
  disabled: boolean
  required: boolean
  options?: string[]
  errors?: string[]
}

function detectFieldType(item: Element): { type: string; value: string | boolean | undefined; options?: string[] } {
  const select = item.querySelector('.ant-select');
  if (select) {
    const value = select.querySelector('.ant-select-selection-item')?.textContent?.trim() || '';
    const options = Array.from(select.querySelectorAll('.ant-select-item-option')).map(
      (o) => o.textContent?.trim() || '',
    ).filter(Boolean);
    return { type: 'select', value, options };
  }

  const picker = item.querySelector('.ant-picker');
  if (picker) {
    const input = picker.querySelector('input') as HTMLInputElement;
    return { type: 'datepicker', value: input?.value || '' };
  }

  const checkbox = item.querySelector('.ant-checkbox-wrapper');
  if (checkbox) {
    const input = checkbox.querySelector('input[type="checkbox"]') as HTMLInputElement;
    return { type: 'checkbox', value: input?.checked || false };
  }

  const radio = item.querySelector('.ant-radio-wrapper');
  if (radio) {
    const input = radio.querySelector('input[type="radio"]') as HTMLInputElement;
    return { type: 'radio', value: input?.checked || false };
  }

  const textInput = item.querySelector('input, textarea') as HTMLInputElement | HTMLTextAreaElement;
  if (textInput) {
    return { type: textInput.tagName.toLowerCase() === 'textarea' ? 'textarea' : 'input', value: textInput.value || '' };
  }

  return { type: 'unknown', value: '' };
}

const getFormDataInfo: FunctionInfo = {
  name: 'get_form_data',
  description: '获取指定表单或当前表单的所有字段值和校验状态',
  parameters: {
    type: 'object',
    properties: {
      form_selector: {
        type: 'string',
        description: '表单 CSS 选择器。不传则自动检测当前页面主表单',
      },
    },
  },
  tags: ['dom', 'generic'],
};

export function GetFormDataFunction() {
  useRegisterFunction(
    getFormDataInfo,
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

      const items = form.querySelectorAll('.ant-form-item');
      const fields: FormField[] = [];

      items.forEach((item) => {
        const label = item.querySelector('.ant-form-item-label label')?.textContent?.trim() || '';
        const name = item.getAttribute('name') || (item.querySelector('input[name], select[name], textarea[name]')?.getAttribute('name')) || null;
        const { type, value, options } = detectFieldType(item);
        const disabled = item.querySelector('[disabled], .ant-select-disabled, .ant-btn-disabled') !== null;
        const required = item.classList.contains('ant-form-item-required');
        const errors = Array.from(item.querySelectorAll('.ant-form-item-explain-error')).map(
          (e) => e.textContent?.trim() || '',
        ).filter(Boolean);

        fields.push({ label, name, type, value, disabled, required, options: options?.length ? options : undefined, errors: errors.length ? errors : undefined });
      });

      return {
        success: true,
        form_selector: generateSelector(form),
        fields,
      };
    },
  );

  return null;
}
