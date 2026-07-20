/**
 * reactValueSetter.ts 单元测试
 *
 * 测试 React 受控组件值设置工具函数的正常路径、边界场景和错误处理。
 */

import {
  setReactInputValue,
  setReactTextareaValue,
  setReactSelectValue,
  setReactDateRangePickerValue,
  setReactRadioValue,
} from '../../functions/utils/reactValueSetter';

// Mock waitForSelector
jest.mock('../../functions/dom-engine', () => ({
  waitForSelector: jest.fn(),
}));

// Mock nativeInputValueSetter
const mockNativeInputValueSetter = jest.fn();
Object.defineProperty(HTMLInputElement.prototype, 'value', {
  get: function () {
    return this.getAttribute('data-mock-value') || '';
  },
  set: function (value) {
    this.setAttribute('data-mock-value', value);
    mockNativeInputValueSetter.call(this, value);
  },
  configurable: true,
});

// Mock nativeTextareaValueSetter
const mockNativeTextareaValueSetter = jest.fn();
Object.defineProperty(HTMLTextAreaElement.prototype, 'value', {
  get: function () {
    return this.getAttribute('data-mock-value') || '';
  },
  set: function (value) {
    this.setAttribute('data-mock-value', value);
    mockNativeTextareaValueSetter.call(this, value);
  },
  configurable: true,
});

describe('reactValueSetter', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockNativeInputValueSetter.mockClear();
    mockNativeTextareaValueSetter.mockClear();
  });

  describe('setReactInputValue', () => {
    it('IN-01: 正常设置 input 值', () => {
      const input = document.createElement('input');
      document.body.appendChild(input);

      const result = setReactInputValue(input, 'test value');

      expect(result).toBe(true);

      document.body.removeChild(input);
    });

    it('IN-02: 设置空值', () => {
      const input = document.createElement('input');
      document.body.appendChild(input);

      const result = setReactInputValue(input, '');

      expect(result).toBe(true);

      document.body.removeChild(input);
    });

    it('IN-03: 设置包含特殊字符的值', () => {
      const input = document.createElement('input');
      document.body.appendChild(input);

      const specialValue = 'Test <script>alert("xss")</script> & "quotes" & \'single quotes\'';
      const result = setReactInputValue(input, specialValue);

      expect(result).toBe(true);

      document.body.removeChild(input);
    });

    it('IN-04: 禁用的 input 返回 false', () => {
      const input = document.createElement('input');
      input.disabled = true;
      document.body.appendChild(input);

      const result = setReactInputValue(input, 'test');

      expect(result).toBe(false);

      document.body.removeChild(input);
    });

    it('IN-05: focus 元素', () => {
      const input = document.createElement('input');
      document.body.appendChild(input);
      const focusSpy = jest.spyOn(input, 'focus');

      setReactInputValue(input, 'test');

      expect(focusSpy).toHaveBeenCalled();

      document.body.removeChild(input);
    });

    it('IN-06: 派发 input 事件', () => {
      const input = document.createElement('input');
      document.body.appendChild(input);
      const inputEventHandler = jest.fn();
      input.addEventListener('input', inputEventHandler);

      setReactInputValue(input, 'test');

      expect(inputEventHandler).toHaveBeenCalled();

      document.body.removeChild(input);
    });

    it('IN-07: 派发 change 事件', () => {
      const input = document.createElement('input');
      document.body.appendChild(input);
      const changeEventHandler = jest.fn();
      input.addEventListener('change', changeEventHandler);

      setReactInputValue(input, 'test');

      expect(changeEventHandler).toHaveBeenCalled();

      document.body.removeChild(input);
    });

    it('IN-08: 事件冒泡', () => {
      const parent = document.createElement('div');
      const input = document.createElement('input');
      parent.appendChild(input);
      document.body.appendChild(parent);

      const parentInputHandler = jest.fn();
      parent.addEventListener('input', parentInputHandler);

      setReactInputValue(input, 'test');

      expect(parentInputHandler).toHaveBeenCalled();

      document.body.removeChild(parent);
    });

    it('IN-09: 长文本设置', () => {
      const input = document.createElement('input');
      document.body.appendChild(input);

      const longText = 'a'.repeat(10000);
      const result = setReactInputValue(input, longText);

      expect(result).toBe(true);

      document.body.removeChild(input);
    });

    it('IN-10: 包含换行符的值', () => {
      const input = document.createElement('input');
      document.body.appendChild(input);

      const multiLineText = 'Line 1\nLine 2\rLine 3\r\nLine 4';
      const result = setReactInputValue(input, multiLineText);

      expect(result).toBe(true);

      document.body.removeChild(input);
    });

    it('IN-11: shouldFocus 参数为 false 时不调用 focus', () => {
      const input = document.createElement('input');
      document.body.appendChild(input);
      const focusSpy = jest.spyOn(input, 'focus');

      setReactInputValue(input, 'test', false);

      expect(focusSpy).not.toHaveBeenCalled();

      document.body.removeChild(input);
    });

    it('IN-12: shouldFocus 参数为 true 时调用 focus', () => {
      const input = document.createElement('input');
      document.body.appendChild(input);
      const focusSpy = jest.spyOn(input, 'focus');

      setReactInputValue(input, 'test', true);

      expect(focusSpy).toHaveBeenCalled();

      document.body.removeChild(input);
    });
  });

  describe('setReactTextareaValue', () => {
    it('TA-01: 正常设置 textarea 值', () => {
      const textarea = document.createElement('textarea');
      document.body.appendChild(textarea);

      const result = setReactTextareaValue(textarea, 'test value');

      expect(result).toBe(true);

      document.body.removeChild(textarea);
    });

    it('TA-02: 设置空值', () => {
      const textarea = document.createElement('textarea');
      document.body.appendChild(textarea);

      const result = setReactTextareaValue(textarea, '');

      expect(result).toBe(true);

      document.body.removeChild(textarea);
    });

    it('TA-03: 禁用的 textarea 返回 false', () => {
      const textarea = document.createElement('textarea');
      textarea.disabled = true;
      document.body.appendChild(textarea);

      const result = setReactTextareaValue(textarea, 'test');

      expect(result).toBe(false);

      document.body.removeChild(textarea);
    });

    it('TA-04: 多行文本设置', () => {
      const textarea = document.createElement('textarea');
      document.body.appendChild(textarea);

      const multiLineText = 'Line 1\nLine 2\nLine 3';
      const result = setReactTextareaValue(textarea, multiLineText);

      expect(result).toBe(true);

      document.body.removeChild(textarea);
    });

    it('TA-05: focus 元素', () => {
      const textarea = document.createElement('textarea');
      document.body.appendChild(textarea);
      const focusSpy = jest.spyOn(textarea, 'focus');

      setReactTextareaValue(textarea, 'test');

      expect(focusSpy).toHaveBeenCalled();

      document.body.removeChild(textarea);
    });
  });

  describe('setReactSelectValue', () => {
    it('SEL-01: 通过 value 匹配选项', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');

      const container = document.createElement('div');
      container.className = 'ant-select';
      document.body.appendChild(container);

      const selector = document.createElement('div');
      selector.className = 'ant-select-selector';
      container.appendChild(selector);

      const dropdown = document.createElement('div');
      dropdown.className = 'ant-select-dropdown';
      document.body.appendChild(dropdown);

      const option = document.createElement('div');
      option.className = 'ant-select-item-option';
      option.setAttribute('data-value', 'option1');
      option.setAttribute('title', 'Option 1');
      dropdown.appendChild(option);

      waitForSelector.mockResolvedValueOnce(dropdown);

      const result = await setReactSelectValue(container, 'option1');

      expect(result).toBe(true);

      document.body.removeChild(container);
      document.body.removeChild(dropdown);
    });

    it('SEL-02: 通过文本匹配选项', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');

      const container = document.createElement('div');
      container.className = 'ant-select';
      document.body.appendChild(container);

      const selector = document.createElement('div');
      selector.className = 'ant-select-selector';
      container.appendChild(selector);

      const dropdown = document.createElement('div');
      dropdown.className = 'ant-select-dropdown';
      document.body.appendChild(dropdown);

      const option = document.createElement('div');
      option.className = 'ant-select-item-option';
      option.setAttribute('title', 'Option 1');
      option.textContent = 'Option 1';
      dropdown.appendChild(option);

      waitForSelector.mockResolvedValueOnce(dropdown);

      const result = await setReactSelectValue(container, 'any', 'Option 1');

      expect(result).toBe(true);

      document.body.removeChild(container);
      document.body.removeChild(dropdown);
    });

    it('SEL-03: 选项不存在返回 false', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');

      const container = document.createElement('div');
      container.className = 'ant-select';
      document.body.appendChild(container);

      const selector = document.createElement('div');
      selector.className = 'ant-select-selector';
      container.appendChild(selector);

      const dropdown = document.createElement('div');
      dropdown.className = 'ant-select-dropdown';
      document.body.appendChild(dropdown);

      waitForSelector.mockResolvedValueOnce(dropdown);

      const result = await setReactSelectValue(container, 'nonexistent');

      expect(result).toBe(false);

      document.body.removeChild(container);
      document.body.removeChild(dropdown);
    });

    it('SEL-04: 下拉菜单未出现返回 false', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');

      const container = document.createElement('div');
      container.className = 'ant-select';
      document.body.appendChild(container);

      const selector = document.createElement('div');
      selector.className = 'ant-select-selector';
      container.appendChild(selector);

      waitForSelector.mockResolvedValueOnce(null);

      const result = await setReactSelectValue(container, 'option1');

      expect(result).toBe(false);

      document.body.removeChild(container);
    });

    it('SEL-05: selector 元素不存在返回 false', async () => {
      const container = document.createElement('div');
      container.className = 'ant-select';
      document.body.appendChild(container);

      const result = await setReactSelectValue(container, 'option1');

      expect(result).toBe(false);

      document.body.removeChild(container);
    });

    it('SEL-06: 多个选项匹配第一个', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');

      const container = document.createElement('div');
      container.className = 'ant-select';
      document.body.appendChild(container);

      const selector = document.createElement('div');
      selector.className = 'ant-select-selector';
      container.appendChild(selector);

      const dropdown = document.createElement('div');
      dropdown.className = 'ant-select-dropdown';
      document.body.appendChild(dropdown);

      const option1 = document.createElement('div');
      option1.className = 'ant-select-item-option';
      option1.setAttribute('data-value', 'option1');
      dropdown.appendChild(option1);

      const option2 = document.createElement('div');
      option2.className = 'ant-select-item-option';
      option2.setAttribute('data-value', 'option1');
      dropdown.appendChild(option2);

      waitForSelector.mockResolvedValueOnce(dropdown);

      const result = await setReactSelectValue(container, 'option1');

      expect(result).toBe(true);

      document.body.removeChild(container);
      document.body.removeChild(dropdown);
    });

    it('SEL-07: 点击 selector 触发下拉', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');

      const container = document.createElement('div');
      container.className = 'ant-select';
      document.body.appendChild(container);

      const selector = document.createElement('div');
      selector.className = 'ant-select-selector';
      container.appendChild(selector);

      const dropdown = document.createElement('div');
      dropdown.className = 'ant-select-dropdown';
      document.body.appendChild(dropdown);

      waitForSelector.mockResolvedValueOnce(dropdown);

      await setReactSelectValue(container, 'option1');

      document.body.removeChild(container);
      document.body.removeChild(dropdown);
    });

    it('SEL-08: 超时场景', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');

      const container = document.createElement('div');
      container.className = 'ant-select';
      document.body.appendChild(container);

      const selector = document.createElement('div');
      selector.className = 'ant-select-selector';
      container.appendChild(selector);

      waitForSelector.mockResolvedValueOnce(null);

      const result = await setReactSelectValue(container, 'option1');

      expect(result).toBe(false);

      document.body.removeChild(container);
    });
  });

  describe('setReactDateRangePickerValue', () => {
    it('DR-01: 正常设置日期范围', async () => {
      const container = document.createElement('div');
      container.className = 'ant-picker-range';
      document.body.appendChild(container);

      const startInput = document.createElement('input');
      container.appendChild(startInput);

      const endInput = document.createElement('input');
      container.appendChild(endInput);

      const result = await setReactDateRangePickerValue(container, '2024-01-01', '2024-01-31');

      expect(result).toBe(true);

      document.body.removeChild(container);
    });

    it('DR-02: input 数量不足返回 false', async () => {
      const container = document.createElement('div');
      container.className = 'ant-picker-range';
      document.body.appendChild(container);

      const startInput = document.createElement('input');
      container.appendChild(startInput);

      const result = await setReactDateRangePickerValue(container, '2024-01-01', '2024-01-31');

      expect(result).toBe(false);

      document.body.removeChild(container);
    });

    it('DR-03: 直接设置失败时降级到面板选择', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');

      const container = document.createElement('div');
      container.className = 'ant-picker-range';
      document.body.appendChild(container);

      const startInput = document.createElement('input');
      container.appendChild(startInput);

      const endInput = document.createElement('input');
      container.appendChild(endInput);

      // Mock setReactInputValue to fail
      jest.spyOn(require('../../functions/utils/reactValueSetter'), 'setReactInputValue').mockReturnValue(false);

      const panel = document.createElement('div');
      panel.className = 'ant-picker-dropdown';
      document.body.appendChild(panel);

      const startCell = document.createElement('div');
      startCell.className = 'ant-picker-cell';
      const startInner = document.createElement('div');
      startInner.className = 'ant-picker-cell-inner';
      startInner.textContent = '1';
      startCell.appendChild(startInner);
      panel.appendChild(startCell);

      const endCell = document.createElement('div');
      endCell.className = 'ant-picker-cell';
      const endInner = document.createElement('div');
      endInner.className = 'ant-picker-cell-inner';
      endInner.textContent = '31';
      endCell.appendChild(endInner);
      panel.appendChild(endCell);

      waitForSelector.mockResolvedValueOnce(panel);

      const startCellClickSpy = jest.spyOn(startCell, 'click');
      const endCellClickSpy = jest.spyOn(endCell, 'click');

      const result = await setReactDateRangePickerValue(container, '2024-01-01', '2024-01-31');

      expect(result).toBe(true);

      document.body.removeChild(container);
      document.body.removeChild(panel);
      jest.restoreAllMocks();
    });

    it('DR-04: 正常输入框直接成功返回 true（不走降级）', async () => {
      const container = document.createElement('div');
      container.className = 'ant-picker-range';
      document.body.appendChild(container);

      const startInput = document.createElement('input');
      container.appendChild(startInput);

      const endInput = document.createElement('input');
      container.appendChild(endInput);

      const result = await setReactDateRangePickerValue(container, '2024-01-01', '2024-01-31');

      expect(result).toBe(true);

      document.body.removeChild(container);
    });

    it('DR-05: 日期单元格不存在时仍返回 true', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');

      const container = document.createElement('div');
      container.className = 'ant-picker-range';
      document.body.appendChild(container);

      const startInput = document.createElement('input');
      container.appendChild(startInput);

      const endInput = document.createElement('input');
      container.appendChild(endInput);

      jest.spyOn(require('../../functions/utils/reactValueSetter'), 'setReactInputValue').mockReturnValue(false);

      const panel = document.createElement('div');
      panel.className = 'ant-picker-dropdown';
      document.body.appendChild(panel);

      waitForSelector.mockResolvedValueOnce(panel);

      const result = await setReactDateRangePickerValue(container, '2024-01-01', '2024-01-31');

      expect(result).toBe(true);

      document.body.removeChild(container);
      document.body.removeChild(panel);
      jest.restoreAllMocks();
    });

    it('DR-06: 不同月份的日期范围', async () => {
      const container = document.createElement('div');
      container.className = 'ant-picker-range';
      document.body.appendChild(container);

      const startInput = document.createElement('input');
      container.appendChild(startInput);

      const endInput = document.createElement('input');
      container.appendChild(endInput);

      const result = await setReactDateRangePickerValue(container, '2024-01-15', '2024-02-15');

      expect(result).toBe(true);

      document.body.removeChild(container);
    });

    it('DR-07: 同一天的日期范围', async () => {
      const container = document.createElement('div');
      container.className = 'ant-picker-range';
      document.body.appendChild(container);

      const startInput = document.createElement('input');
      container.appendChild(startInput);

      const endInput = document.createElement('input');
      container.appendChild(endInput);

      const result = await setReactDateRangePickerValue(container, '2024-01-01', '2024-01-01');

      expect(result).toBe(true);

      document.body.removeChild(container);
    });
  });

  describe('setReactRadioValue', () => {
    it('RD-01: 通过 value 匹配选项', async () => {
      const container = document.createElement('div');
      container.className = 'ant-radio-group';
      document.body.appendChild(container);

      const wrapper = document.createElement('div');
      wrapper.className = 'ant-radio-wrapper';
      container.appendChild(wrapper);

      const input = document.createElement('input');
      input.type = 'radio';
      input.value = 'option1';
      wrapper.appendChild(input);

      const wrapperClickSpy = jest.spyOn(wrapper, 'click');

      const result = await setReactRadioValue(container, 'option1');

      expect(result).toBe(true);
      expect(wrapperClickSpy).toHaveBeenCalled();

      document.body.removeChild(container);
    });

    it('RD-02: 通过文本匹配选项', async () => {
      const container = document.createElement('div');
      container.className = 'ant-radio-group';
      document.body.appendChild(container);

      const wrapper = document.createElement('div');
      wrapper.className = 'ant-radio-wrapper';
      wrapper.textContent = 'Option 1';
      container.appendChild(wrapper);

      const input = document.createElement('input');
      input.type = 'radio';
      input.value = 'different';
      wrapper.appendChild(input);

      const wrapperClickSpy = jest.spyOn(wrapper, 'click');

      const result = await setReactRadioValue(container, 'Option 1');

      expect(result).toBe(true);
      expect(wrapperClickSpy).toHaveBeenCalled();

      document.body.removeChild(container);
    });

    it('RD-03: 选项不存在返回 false', async () => {
      const container = document.createElement('div');
      container.className = 'ant-radio-group';
      document.body.appendChild(container);

      const wrapper = document.createElement('div');
      wrapper.className = 'ant-radio-wrapper';
      container.appendChild(wrapper);

      const input = document.createElement('input');
      input.type = 'radio';
      input.value = 'option1';
      wrapper.appendChild(input);

      const result = await setReactRadioValue(container, 'nonexistent');

      expect(result).toBe(false);

      document.body.removeChild(container);
    });

    it('RD-04: 多个选项匹配第一个', async () => {
      const container = document.createElement('div');
      container.className = 'ant-radio-group';
      document.body.appendChild(container);

      const wrapper1 = document.createElement('div');
      wrapper1.className = 'ant-radio-wrapper';
      container.appendChild(wrapper1);

      const input1 = document.createElement('input');
      input1.type = 'radio';
      input1.value = 'option1';
      wrapper1.appendChild(input1);

      const wrapper2 = document.createElement('div');
      wrapper2.className = 'ant-radio-wrapper';
      container.appendChild(wrapper2);

      const input2 = document.createElement('input');
      input2.type = 'radio';
      input2.value = 'option1';
      wrapper2.appendChild(input2);

      const wrapper1ClickSpy = jest.spyOn(wrapper1, 'click');
      const wrapper2ClickSpy = jest.spyOn(wrapper2, 'click');

      const result = await setReactRadioValue(container, 'option1');

      expect(result).toBe(true);
      expect(wrapper1ClickSpy).toHaveBeenCalled();
      expect(wrapper2ClickSpy).not.toHaveBeenCalled();

      document.body.removeChild(container);
    });

    it('RD-05: 空容器返回 false', async () => {
      const container = document.createElement('div');
      container.className = 'ant-radio-group';
      document.body.appendChild(container);

      const result = await setReactRadioValue(container, 'option1');

      expect(result).toBe(false);

      document.body.removeChild(container);
    });
  });
});