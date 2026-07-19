import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';
import { waitForSelector } from './dom-engine';

const datePickerInfo: FunctionInfo = {
  name: 'date_picker',
  description: '在 antd DatePicker 中选择日期',
  parameters: {
    type: 'object',
    properties: {
      selector: { type: 'string', description: 'DatePicker 的 CSS 选择器' },
      date: { type: 'string', description: '目标日期，格式 YYYY-MM-DD' },
    },
    required: ['selector', 'date'],
  },
  tags: ['dom', 'generic'],
  timeout_ms: 15000,
};

export function DatePickerFunction() {
  useRegisterFunction(
    datePickerInfo,
    async (params) => {
      const selector = params.selector as string;
      const date = params.date as string;

      const container = await waitForSelector(selector, 5000);
      if (!container) {
        return { success: false, error: `DatePicker 未找到: ${selector}` };
      }

      if (container.querySelector('.ant-picker-range')) {
        return { success: false, error: '暂不支持 RangePicker' };
      }

      const input = container.querySelector<HTMLElement>('input, .ant-picker-input');
      if (!input) return { success: false, error: '未找到 DatePicker 输入框' };
      (input as HTMLElement).click();

      const dropdown = await waitForSelector('.ant-picker-dropdown', 3000);
      if (!dropdown) {
        return { success: false, error: '日期面板未打开' };
      }

      const cells = dropdown.querySelectorAll<HTMLElement>('.ant-picker-cell');
      let targetCell: HTMLElement | null = null;

      for (const cell of cells) {
        const title = cell.getAttribute('title');
        if (title === date) {
          targetCell = cell;
          break;
        }
        if (cell.textContent?.trim() === date.split('-')[2]) {
          const cellDate = cell.getAttribute('title') || cell.getAttribute('data-date');
          if (cellDate === date) {
            targetCell = cell;
            break;
          }
        }
      }

      if (!targetCell) {
        return { success: false, error: `未找到日期单元格: ${date}` };
      }

      (targetCell as HTMLElement).click();

      return { success: true, date };
    },
  );

  return null;
}