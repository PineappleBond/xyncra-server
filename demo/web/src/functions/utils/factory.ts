import type { FunctionInfo } from '@xyncra/protocol';
import type { FunctionEntry } from '@xyncra/client-web';
import { waitForSelector, waitForLoadingComplete } from '../dom-engine';
import {
  setReactInputValue,
  setReactTextareaValue,
  setReactSelectValue,
  setReactDateRangePickerValue,
  setReactRadioValue,
} from './reactValueSetter';

export function buildFunctionEntry(
  info: FunctionInfo,
  handler: (params: Record<string, unknown>) => Promise<{ success: boolean; error?: string }>,
): FunctionEntry {
  return { info, handler };
}

export function createClickFunction(
  name: string,
  description: string,
  selector: string,
  tags: string[] = [],
): FunctionEntry {
  return buildFunctionEntry(
    {
      name,
      description,
      parameters: {},
      tags: [...tags, 'type:click'],
      timeout_ms: 15000,
    },
    async () => {
      const el = await waitForSelector(selector, 15000);
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
}

export function createInputFunction(
  name: string,
  description: string,
  selector: string,
  tags: string[] = [],
): FunctionEntry {
  return buildFunctionEntry(
    {
      name,
      description,
      parameters: {
        type: 'object',
        properties: {
          value: { type: 'string', description: '要填入的文本内容' },
        },
        required: ['value'],
      },
      tags: [...tags, 'type:input'],
      timeout_ms: 10000,
    },
    async (params) => {
      const value = params.value as string;
      const el = await waitForSelector(selector, 10000);
      if (!el) {
        return { success: false, error: `输入框未找到: ${selector}` };
      }
      if (el.hasAttribute('disabled')) {
        return { success: false, error: '输入框已禁用' };
      }
      const success = setReactInputValue(el as HTMLElement, value);
      if (!success) {
        return { success: false, error: '值设置失败，React 组件未响应' };
      }
      return { success: true };
    },
  );
}

export function createSelectFunction(
  name: string,
  description: string,
  selector: string,
  tags: string[] = [],
): FunctionEntry {
  return buildFunctionEntry(
    {
      name,
      description,
      parameters: {
        type: 'object',
        properties: {
          option: { type: 'string', description: '要选择的选项值' },
        },
        required: ['option'],
      },
      tags: [...tags, 'type:select'],
      timeout_ms: 10000,
    },
    async (params) => {
      const option = params.option as string;
      const el = await waitForSelector(selector, 10000);
      if (!el) {
        return { success: false, error: `选择器未找到: ${selector}` };
      }
      const success = await setReactSelectValue(el as HTMLElement, option);
      if (!success) {
        return { success: false, error: `选项未找到: ${option}` };
      }
      return { success: true };
    },
  );
}

export function createTabFunction(
  name: string,
  description: string,
  tabLabel: string,
  containerSelector?: string,
  tags: string[] = [],
): FunctionEntry {
  const selector = containerSelector
    ? `${containerSelector} [role="tab"][aria-label="${tabLabel}"], ${containerSelector} .ant-tabs-tab:contains("${tabLabel}")`
    : `[role="tab"][aria-label="${tabLabel}"], .ant-tabs-tab:contains("${tabLabel}")`;
  return createClickFunction(name, description, selector, [...tags, 'type:tab']);
}

export function createSubmitFunction(
  name: string,
  description: string,
  formSelector: string,
  tags: string[] = [],
): FunctionEntry {
  return buildFunctionEntry(
    {
      name,
      description,
      parameters: {},
      tags: [...tags, 'type:submit'],
      timeout_ms: 20000,
    },
    async () => {
      const form = await waitForSelector(formSelector, 10000);
      if (!form) {
        return { success: false, error: `表单未找到: ${formSelector}` };
      }
      const submitBtn = form.querySelector('button[type="submit"], .ant-btn-primary');
      if (submitBtn) {
        (submitBtn as HTMLElement).click();
      }
      await waitForLoadingComplete();
      return { success: true };
    },
  );
}

export function createNavigateFunction(
  name: string,
  description: string,
  path: string,
  tags: string[] = [],
): FunctionEntry {
  return buildFunctionEntry(
    {
      name,
      description,
      parameters: {},
      tags: [...tags, 'type:navigate'],
      timeout_ms: 15000,
    },
    async () => {
      const link = await waitForSelector(`a[href="${path}"]`, 10000);
      if (link) {
        (link as HTMLElement).click();
        await waitForLoadingComplete();
        return { success: true };
      }
      window.location.href = path;
      return { success: true };
    },
  );
}

export function createCheckboxFunction(
  name: string,
  description: string,
  selector: string,
  tags: string[] = [],
): FunctionEntry {
  return buildFunctionEntry(
    {
      name,
      description,
      parameters: {
        type: 'object',
        properties: {
          checked: { type: 'boolean', description: '是否选中' },
        },
      },
      tags: [...tags, 'type:checkbox'],
      timeout_ms: 10000,
    },
    async (params) => {
      const el = await waitForSelector(selector, 10000);
      if (!el) {
        return { success: false, error: `复选框未找到: ${selector}` };
      }
      const inputEl = el as HTMLInputElement;
      const checked = params.checked as boolean | undefined;
      if (checked !== undefined && inputEl.checked !== checked) {
        inputEl.checked = checked;
        inputEl.dispatchEvent(new Event('change', { bubbles: true }));
      }
      return { success: true };
    },
  );
}

export function createLinkFunction(
  name: string,
  description: string,
  selector: string,
  tags: string[] = [],
): FunctionEntry {
  return createClickFunction(name, description, selector, [...tags, 'type:link']);
}

export function createRadioFunction(
  name: string,
  description: string,
  selector: string,
  tags: string[] = [],
): FunctionEntry {
  return buildFunctionEntry(
    {
      name,
      description,
      parameters: {
        type: 'object',
        properties: {
          value: { type: 'string', description: '要选择的值' },
        },
        required: ['value'],
      },
      tags: [...tags, 'type:radio'],
      timeout_ms: 10000,
    },
    async (params) => {
      const value = params.value as string;
      const el = await waitForSelector(selector, 10000);
      if (!el) {
        return { success: false, error: `单选按钮组未找到: ${selector}` };
      }
      const success = await setReactRadioValue(el as HTMLElement, value);
      if (!success) {
        return { success: false, error: `选项未找到: ${value}` };
      }
      return { success: true };
    },
  );
}

export function createSegmentFunction(
  name: string,
  description: string,
  segmentLabel: string,
  containerSelector?: string,
  tags: string[] = [],
): FunctionEntry {
  const selector = containerSelector
    ? `${containerSelector} .ant-segmented-item[aria-label="${segmentLabel}"], ${containerSelector} .ant-segmented-item:contains("${segmentLabel}")`
    : `.ant-segmented-item[aria-label="${segmentLabel}"], .ant-segmented-item:contains("${segmentLabel}")`;
  return createClickFunction(name, description, selector, [...tags, 'type:segment']);
}

export function createDateRangeFunction(
  name: string,
  description: string,
  selector: string,
  tags: string[] = [],
): FunctionEntry {
  return buildFunctionEntry(
    {
      name,
      description,
      parameters: {
        type: 'object',
        properties: {
          startDate: { type: 'string', description: '开始日期，格式 YYYY-MM-DD' },
          endDate: { type: 'string', description: '结束日期，格式 YYYY-MM-DD' },
        },
        required: ['startDate', 'endDate'],
      },
      tags: [...tags, 'type:datepicker'],
      timeout_ms: 15000,
    },
    async (params) => {
      const startDate = params.startDate as string;
      const endDate = params.endDate as string;
      const el = await waitForSelector(selector, 10000);
      if (!el) {
        return { success: false, error: `日期选择器未找到: ${selector}` };
      }
      const success = await setReactDateRangePickerValue(el as HTMLElement, startDate, endDate);
      if (!success) {
        return { success: false, error: '日期设置失败' };
      }
      return { success: true };
    },
  );
}

export function createDropdownFunction(
  name: string,
  description: string,
  selector: string,
  tags: string[] = [],
): FunctionEntry {
  return createClickFunction(name, description, selector, [...tags, 'type:dropdown']);
}

export function createTextareaFunction(
  name: string,
  description: string,
  selector: string,
  tags: string[] = [],
): FunctionEntry {
  return buildFunctionEntry(
    {
      name,
      description,
      parameters: {
        type: 'object',
        properties: {
          value: { type: 'string', description: '要填入的文本内容' },
        },
        required: ['value'],
      },
      tags: [...tags, 'type:textarea'],
      timeout_ms: 10000,
    },
    async (params) => {
      const value = params.value as string;
      const el = await waitForSelector(selector, 10000);
      if (!el) {
        return { success: false, error: `文本框未找到: ${selector}` };
      }
      const success = setReactTextareaValue(el as HTMLElement, value);
      if (!success) {
        return { success: false, error: '值设置失败，React 组件未响应' };
      }
      return { success: true };
    },
  );
}

export function createToggleFunction(
  name: string,
  description: string,
  selector: string,
  tags: string[] = [],
): FunctionEntry {
  return createClickFunction(name, description, selector, [...tags, 'type:toggle']);
}
