import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';
import { waitForSelector } from './dom-engine';

const selectOptionInfo: FunctionInfo = {
  name: 'select_option',
  description: '选择下拉框(Select)、单选框(Radio)或复选框(Checkbox)的选项',
  parameters: {
    type: 'object',
    properties: {
      selector: { type: 'string', description: '元素 CSS 选择器' },
      value: { type: 'string', description: '要选择的值' },
      option_text: { type: 'string', description: '选项显示文本（Select 下拉时使用）' },
    },
    required: ['selector'],
  },
  tags: ['dom', 'generic'],
  timeout_ms: 10000,
};

export function SelectOptionFunction() {
  useRegisterFunction(
    selectOptionInfo,
    async (params) => {
      const selector = params.selector as string;
      const value = params.value as string | undefined;
      const optionText = params.option_text as string | undefined;

      const el = await waitForSelector(selector, 5000);
      if (!el) {
        return { success: false, error: `元素未找到: ${selector}` };
      }

      let selected = '';

      if (el.classList.contains('ant-select')) {
        const trigger = el.querySelector<HTMLElement>('.ant-select-selector');
        if (!trigger) return { success: false, error: '未找到 Select 触发器' };
        trigger.click();

        const dropdown = await waitForSelector('.ant-select-dropdown:not(.ant-select-dropdown-hidden)', 3000);
        if (!dropdown) {
          return { success: false, error: '下拉菜单未打开' };
        }

        const options = dropdown.querySelectorAll<HTMLElement>('.ant-select-item-option');
        let targetOption: HTMLElement | null = null;

        for (const opt of options) {
          const title = opt.getAttribute('title');
          const content = opt.querySelector('.ant-select-item-option-content')?.textContent?.trim();
          if (optionText) {
            if (title === optionText || content === optionText) {
              targetOption = opt;
              break;
            }
          } else if (value) {
            if (opt.getAttribute('value') === value || title === value || content === value) {
              targetOption = opt;
              break;
            }
          }
        }

        if (!targetOption) {
          return { success: false, error: `未找到匹配的选项: ${optionText || value}` };
        }

        targetOption.click();
        selected = targetOption.textContent?.trim() || value || '';
      } else if (el.classList.contains('ant-radio-wrapper')) {
        const input = el.querySelector('input[type="radio"]') as HTMLInputElement;
        if (value && input) {
          const radioValue = input.value || el.textContent?.trim() || '';
          if (radioValue !== value && el.textContent?.trim() !== value) {
            return { success: false, error: `未找到匹配的 Radio 选项: ${value}` };
          }
        }
        (el as HTMLElement).click();
        selected = el.textContent?.trim() || value || '';
      } else if (el.classList.contains('ant-checkbox-wrapper')) {
        const input = el.querySelector('input[type="checkbox"]') as HTMLInputElement;
        if (input?.checked) {
          return { success: true, selected: el.textContent?.trim() || '' };
        }
        (el as HTMLElement).click();
        selected = el.textContent?.trim() || value || '';
      } else {
        return { success: false, error: '不支持的元素类型' };
      }

      return { success: true, selected };
    },
  );

  return null;
}
