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
 * 获取 React 内部 fiber 对象
 * 用于直接操作 React 组件状态
 */
function getReactFiber(element: HTMLElement): any {
  const key = Object.keys(element).find(
    (key) => key.startsWith('__reactFiber$') || key.startsWith('__reactInternalInstance$'),
  );
  return key ? (element as any)[key] : null;
}

/**
 * 获取 React props 对象
 */
function getReactProps(element: HTMLElement): any {
  const key = Object.keys(element).find(
    (key) => key.startsWith('__reactProps$') || key.startsWith('__reactEventHandlers$'),
  );
  return key ? (element as any)[key] : null;
}

/**
 * 通过 React 内部机制触发事件
 * 这比直接派发原生事件更可靠，因为 React 有自己的事件系统
 */
function triggerReactEvent(element: HTMLElement, eventType: string, value: string): boolean {
  try {
    const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
      HTMLInputElement.prototype,
      'value',
    )?.set;

    if (!nativeInputValueSetter) {
      return false;
    }

    const inputEl = element as HTMLInputElement;
    nativeInputValueSetter.call(inputEl, value);

    // 使用 React 的事件系统
    const event = new Event(eventType, { bubbles: true });
    Object.defineProperty(event, 'target', { writable: false, value: inputEl });
    inputEl.dispatchEvent(event);

    return true;
  } catch (error) {
    console.error('[reactValueSetter] triggerReactEvent failed:', error);
    return false;
  }
}

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
export function setReactInputValue(el: HTMLElement, value: string, shouldFocus: boolean = true): boolean {
  try {
    if (!nativeInputValueSetter) {
      console.warn('[reactValueSetter] HTMLInputElement.prototype.value setter not found');
      return false;
    }

    const inputEl = el as HTMLInputElement;
    if (inputEl.disabled) {
      return false;
    }

    // Focus 元素，某些组件依赖 focus 状态（可通过参数控制）
    if (shouldFocus) {
      inputEl.focus();
    }

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
 * 从 DOM 元素向上遍历 React fiber 树，查找包含 onChange 属性的组件
 * 用于直接访问 React 受控组件的 onChange 回调
 *
 * @param element - 起始 DOM 元素
 * @returns 包含 onChange 回调和 fiber 的对象，或 null
 */
function findReactOnChange(element: HTMLElement): { onChange: Function; fiber: any } | null {
  const key = Object.keys(element).find(
    (k) => k.startsWith('__reactFiber$') || k.startsWith('__reactInternalInstance$'),
  );
  if (!key) return null;

  let fiber = (element as any)[key];
  while (fiber) {
    if (fiber.memoizedProps && fiber.memoizedProps.onChange) {
      return { onChange: fiber.memoizedProps.onChange, fiber };
    }
    fiber = fiber.return;
  }
  return null;
}

/**
 * 设置 Ant Design DateRangePicker 组件的值
 *
 * Ant Design DateRangePicker 是受控组件，直接修改 input 值会触发下拉面板弹出。
 * 本函数优先通过 React fiber 直接调用 onChange 回调，绕过 DOM 模拟，
 * 避免面板弹出阻塞后续操作。
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
    // 方法一：通过 React fiber 直接调用 onChange（推荐，不触发下拉面板）
    const result = findReactOnChange(containerEl);
    if (result) {
      try {
        const dayjs = (await import('dayjs')).default;
        const startDayjs = dayjs(startDate);
        const endDayjs = dayjs(endDate);

        // 直调 onChange，绕过 DOM，不触发 focus/下拉
        result.onChange([startDayjs, endDayjs]);
        return true;
      } catch (e) {
        console.warn('[reactValueSetter] React fiber onChange approach failed, falling back', e);
      }
    }

    // 方法二：降级到键盘模拟（现有实现，作为 fallback）
    const inputs = containerEl.querySelectorAll('input');
    if (inputs.length < 2) {
      console.warn('[reactValueSetter] DateRangePicker inputs not found');
      return false;
    }

    const startInput = inputs[0] as HTMLInputElement;
    const endInput = inputs[1] as HTMLInputElement;

    await setInputValueWithKeyboard(startInput, startDate);
    await new Promise((resolve) => setTimeout(resolve, 100));
    await setInputValueWithKeyboard(endInput, endDate);

    return true;
  } catch (error) {
    console.error('[reactValueSetter] setReactDateRangePickerValue failed:', error);
    return false;
  }
}

/**
 * 模拟键盘输入设置 input 值
 * 通过模拟完整的键盘事件序列，确保 React 正确处理值的变化
 */
async function setInputValueWithKeyboard(input: HTMLInputElement, value: string): Promise<void> {
  // 1. Focus 输入框
  input.focus();

  // 2. 清空现有值
  input.value = '';
  triggerReactEvent(input, 'input', '');

  // 3. 逐字符输入，模拟真实键盘输入
  for (const char of value) {
    await new Promise((resolve) => setTimeout(resolve, 10));

    // 触发 keydown 事件
    const keydownEvent = new KeyboardEvent('keydown', {
      key: char,
      code: `Key${char.toUpperCase()}`,
      bubbles: true,
    });
    input.dispatchEvent(keydownEvent);

    // 更新值
    input.value += char;
    triggerReactEvent(input, 'input', input.value);

    // 触发 keyup 事件
    const keyupEvent = new KeyboardEvent('keyup', {
      key: char,
      code: `Key${char.toUpperCase()}`,
      bubbles: true,
    });
    input.dispatchEvent(keyupEvent);
  }

  // 4. 按回车键确认
  await new Promise((resolve) => setTimeout(resolve, 50));
  const enterEvent = new KeyboardEvent('keydown', {
    key: 'Enter',
    code: 'Enter',
    bubbles: true,
  });
  input.dispatchEvent(enterEvent);

  // 5. 触发 change 事件
  triggerReactEvent(input, 'change', input.value);

  // 6. Blur 输入框
  input.blur();
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