/**
 * React 受控组件值设置工具函数
 *
 * 本模块提供了一组工具函数，用于正确设置 Ant Design 等 React 受控组件的值。
 * 由于 React 受控组件不响应直接的 DOM 值修改（如 `input.value = value`），
 * 我们需要使用 `nativeInputValueSetter` 技术来绕过这个限制。
 *
 * 工作原理：
 * 1. 获取原生 HTML 元素的 value setter（如 HTMLInputElement.prototype.value 的 setter）
 * 2. 先将值设为空字符串，再设为目标值，确保 React 感知到变化
 * 3. 派发 input 和 change 事件，触发 React 的事件处理系统
 *
 * 局限性：
 * - 依赖 React 内部实现，React 版本升级可能影响兼容性
 * - 不适用于所有 Ant Design 组件（如 Select 需要模拟用户操作）
 * - 某些组件可能依赖特定的事件序列或额外的状态更新
 *
 * @module reactValueSetter
 */

import { waitForSelector } from '../dom-engine';

/**
 * 获取原生 HTMLInputElement 的 value setter
 * 使用 Object.getOwnPropertyDescriptor 获取原型链上的原生 setter
 */
const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
  HTMLInputElement.prototype,
  'value',
)?.set;

/**
 * 获取原生 HTMLTextAreaElement 的 value setter
 */
const nativeTextareaValueSetter = Object.getOwnPropertyDescriptor(
  HTMLTextAreaElement.prototype,
  'value',
)?.set;

/**
 * 设置 React 受控 Input 组件的值
 *
 * 使用 nativeInputValueSetter 技术确保 React 感知到值的变化。
 * 该函数会先 focus 元素（某些组件依赖 focus 状态），然后使用原生 setter 设置值，
 * 最后派发 input 和 change 事件。
 *
 * @param el - 目标 Input 元素
 * @param value - 要设置的值
 * @returns boolean - 是否设置成功
 *
 * @example
 * ```typescript
 * const input = document.querySelector('input[name="title"]');
 * if (input) {
 *   const success = setReactInputValue(input as HTMLElement, '新标题');
 *   console.log(success ? '设置成功' : '设置失败');
 * }
 * ```
 */
export function setReactInputValue(el: HTMLElement, value: string): boolean {
  try {
    if (!nativeInputValueSetter) {
      console.warn('[reactValueSetter] HTMLInputElement.prototype.value setter not found');
      return false;
    }

    const inputEl = el as HTMLInputElement;
    if (inputEl.disabled) {
      return false;
    }

    // Focus 元素，某些组件依赖 focus 状态
    inputEl.focus();

    // 先设为空串，再设为目标值，确保 React 感知变化
    nativeInputValueSetter.call(inputEl, '');
    nativeInputValueSetter.call(inputEl, value);

    // 派发事件，触发 React 的事件处理系统
    inputEl.dispatchEvent(new Event('input', { bubbles: true }));
    inputEl.dispatchEvent(new Event('change', { bubbles: true }));

    return true;
  } catch (error) {
    console.error('[reactValueSetter] setReactInputValue failed:', error);
    return false;
  }
}

/**
 * 设置 React 受控 Textarea 组件的值
 *
 * 使用 nativeTextareaValueSetter 技术确保 React 感知到值的变化。
 * 逻辑与 setReactInputValue 类似，但使用 HTMLTextAreaElement 的原生 setter。
 *
 * @param el - 目标 Textarea 元素
 * @param value - 要设置的值
 * @returns boolean - 是否设置成功
 */
export function setReactTextareaValue(el: HTMLElement, value: string): boolean {
  try {
    if (!nativeTextareaValueSetter) {
      console.warn('[reactValueSetter] HTMLTextAreaElement.prototype.value setter not found');
      return false;
    }

    const textareaEl = el as HTMLTextAreaElement;
    if (textareaEl.disabled) {
      return false;
    }

    // Focus 元素
    textareaEl.focus();

    // 先设为空串，再设为目标值
    nativeTextareaValueSetter.call(textareaEl, '');
    nativeTextareaValueSetter.call(textareaEl, value);

    // 派发事件
    textareaEl.dispatchEvent(new Event('input', { bubbles: true }));
    textareaEl.dispatchEvent(new Event('change', { bubbles: true }));

    return true;
  } catch (error) {
    console.error('[reactValueSetter] setReactTextareaValue failed:', error);
    return false;
  }
}

/**
 * 设置 Ant Design Select 组件的值
 *
 * Ant Design Select 不使用原生 <select> 元素，需要模拟用户操作：
 * 1. 点击 Select 触发下拉
 * 2. 等待下拉菜单出现
 * 3. 查找并点击匹配的选项
 *
 * @param containerEl - Select 容器元素（通常是 .ant-select）
 * @param optionValue - 要选择的选项值
 * @param optionText - 可选的选项文本（用于按文本内容匹配）
 * @returns Promise<boolean> - 是否设置成功
 *
 * @example
 * ```typescript
 * const select = document.querySelector('.ant-select');
 * if (select) {
 *   const success = await setReactSelectValue(select, 'option1', '选项一');
 *   console.log(success ? '选择成功' : '选择失败');
 * }
 * ```
 */
export async function setReactSelectValue(
  containerEl: HTMLElement,
  optionValue: string,
  optionText?: string,
): Promise<boolean> {
  try {
    // 1. 点击 Select 触发下拉
    const selector = containerEl.querySelector('.ant-select-selector');
    if (!selector) {
      console.warn('[reactValueSetter] .ant-select-selector not found');
      return false;
    }

    (selector as HTMLElement).click();

    // 2. 等待下拉菜单出现
    const dropdown = await waitForSelector(
      '.ant-select-dropdown:not(.ant-select-dropdown-hidden)',
      5000,
    );

    if (!dropdown) {
      console.warn('[reactValueSetter] Select dropdown not appeared');
      return false;
    }

    // 3. 查找并点击匹配的选项
    const options = dropdown.querySelectorAll('.ant-select-item-option');
    for (const option of Array.from(options)) {
      const title = option.getAttribute('title') || '';
      const text = option.textContent?.trim() || '';
      const value = option.getAttribute('data-value') || '';

      // 按文本内容匹配
      if (optionText && (title.includes(optionText) || text.includes(optionText))) {
        (option as HTMLElement).click();
        return true;
      }

      // 按值匹配
      if (value === optionValue || title === optionValue) {
        (option as HTMLElement).click();
        return true;
      }
    }

    console.warn(`[reactValueSetter] Option not found: ${optionValue}`);
    return false;
  } catch (error) {
    console.error('[reactValueSetter] setReactSelectValue failed:', error);
    return false;
  }
}

/**
 * 设置 Ant Design DateRangePicker 组件的值
 *
 * Ant Design DateRangePicker 有两个 input 元素，需要分别设置开始和结束日期。
 * 如果直接设置失败，会降级到点击打开面板并选择日期。
 *
 * @param containerEl - DateRangePicker 容器元素（通常是 .ant-picker-range）
 * @param startDate - 开始日期，格式 YYYY-MM-DD
 * @param endDate - 结束日期，格式 YYYY-MM-DD
 * @returns Promise<boolean> - 是否设置成功
 */
export async function setReactDateRangePickerValue(
  containerEl: HTMLElement,
  startDate: string,
  endDate: string,
): Promise<boolean> {
  try {
    const inputs = containerEl.querySelectorAll('input');
    if (inputs.length < 2) {
      console.warn('[reactValueSetter] DateRangePicker inputs not found');
      return false;
    }

    const startInput = inputs[0] as HTMLInputElement;
    const endInput = inputs[1] as HTMLInputElement;

    // 尝试直接设置值
    const startSuccess = setReactInputValue(startInput as unknown as HTMLElement, startDate);
    const endSuccess = setReactInputValue(endInput as unknown as HTMLElement, endDate);

    if (startSuccess && endSuccess) {
      return true;
    }

    // 降级：点击打开面板，然后选择日期
    console.warn('[reactValueSetter] Direct value setting failed, falling back to panel selection');

    // 点击开始日期输入框打开面板
    startInput.click();

    // 等待面板出现
    const panel = await waitForSelector('.ant-picker-dropdown:not(.ant-picker-dropdown-hidden)', 3000);
    if (!panel) {
      return false;
    }

    // 解析日期并选择对应的单元格
    const startDateObj = new Date(startDate);
    const endDateObj = new Date(endDate);

    // 选择开始日期
    const startDay = startDateObj.getDate();
    const startCells = panel.querySelectorAll('.ant-picker-cell');
    for (const cell of Array.from(startCells)) {
      const inner = cell.querySelector('.ant-picker-cell-inner');
      if (inner && inner.textContent?.trim() === String(startDay)) {
        (cell as HTMLElement).click();
        break;
      }
    }

    // 等待一下，然后选择结束日期
    await new Promise((resolve) => setTimeout(resolve, 100));

    const endDay = endDateObj.getDate();
    for (const cell of Array.from(startCells)) {
      const inner = cell.querySelector('.ant-picker-cell-inner');
      if (inner && inner.textContent?.trim() === String(endDay)) {
        (cell as HTMLElement).click();
        break;
      }
    }

    return true;
  } catch (error) {
    console.error('[reactValueSetter] setReactDateRangePickerValue failed:', error);
    return false;
  }
}

/**
 * 设置 Ant Design Radio 组件的值
 *
 * Ant Design Radio 渲染为 .ant-radio-group 包含多个 .ant-radio-wrapper，
 * 需要遍历找到匹配的选项并点击。
 *
 * @param containerEl - Radio 容器元素（通常是 .ant-radio-group）
 * @param value - 要选择的值
 * @returns Promise<boolean> - 是否设置成功
 */
export async function setReactRadioValue(
  containerEl: HTMLElement,
  value: string,
): Promise<boolean> {
  try {
    const wrappers = containerEl.querySelectorAll('.ant-radio-wrapper');
    for (const wrapper of Array.from(wrappers)) {
      const input = wrapper.querySelector('input');
      const text = wrapper.textContent?.trim();

      // 按值匹配
      if (input && input.value === value) {
        (wrapper as HTMLElement).click();
        return true;
      }

      // 按文本内容匹配
      if (text && text === value) {
        (wrapper as HTMLElement).click();
        return true;
      }
    }

    console.warn(`[reactValueSetter] Radio option not found: ${value}`);
    return false;
  } catch (error) {
    console.error('[reactValueSetter] setReactRadioValue failed:', error);
    return false;
  }
}