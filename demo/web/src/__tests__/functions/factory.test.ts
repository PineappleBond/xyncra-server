/**
 * factory.ts 单元测试
 *
 * 测试工厂函数的重构，验证它们使用新的 React 值设置工具。
 */

import {
  createInputFunction,
  createTextareaFunction,
  createSelectFunction,
  createRadioFunction,
  createDateRangeFunction,
  buildFunctionEntry,
} from '../../functions/utils/factory';

// Mock reactValueSetter
jest.mock('../../functions/utils/reactValueSetter', () => ({
  setReactInputValue: jest.fn().mockReturnValue(true),
  setReactTextareaValue: jest.fn().mockReturnValue(true),
  setReactSelectValue: jest.fn().mockResolvedValue(true),
  setReactDateRangePickerValue: jest.fn().mockResolvedValue(true),
  setReactRadioValue: jest.fn().mockResolvedValue(true),
}));

// Mock dom-engine
jest.mock('../../functions/dom-engine', () => ({
  waitForSelector: jest.fn(),
  waitForLoadingComplete: jest.fn(),
}));

describe('factory', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('buildFunctionEntry', () => {
    it('F-01: 正确创建 FunctionEntry', () => {
      const info = {
        name: 'test-function',
        description: 'Test function',
        parameters: { type: 'object', properties: {} },
      };
      const handler = jest.fn().mockResolvedValue({ success: true });

      const entry = buildFunctionEntry(info, handler);

      expect(entry.info).toEqual(info);
      expect(entry.handler).toBe(handler);
    });

    it('F-02: handler 可以被调用', async () => {
      const info = {
        name: 'test-function',
        description: 'Test function',
      };
      const handler = jest.fn().mockResolvedValue({ success: true });

      const entry = buildFunctionEntry(info, handler);
      const result = await entry.handler({});

      expect(handler).toHaveBeenCalled();
      expect(result).toEqual({ success: true });
    });
  });

  describe('createInputFunction', () => {
    it('F-03: 使用 setReactInputValue 设置值', async () => {
      const { setReactInputValue } = require('../../functions/utils/reactValueSetter');
      const { waitForSelector } = require('../../functions/dom-engine');

      const mockElement = document.createElement('input');
      waitForSelector.mockResolvedValue(mockElement);

      const entry = createInputFunction(
        'test_input',
        'Test input',
        'input[name="test"]',
      );

      await entry.handler({ value: 'test value' });

      expect(setReactInputValue).toHaveBeenCalledWith(mockElement, 'test value');
    });

    it('F-04: 元素未找到返回错误', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');
      waitForSelector.mockResolvedValue(null);

      const entry = createInputFunction(
        'test_input',
        'Test input',
        'input[name="test"]',
      );

      const result = await entry.handler({ value: 'test' });

      expect(result).toEqual({
        success: false,
        error: '输入框未找到: input[name="test"]',
      });
    });

    it('F-05: 禁用元素返回错误', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');

      const mockElement = document.createElement('input');
      mockElement.disabled = true;
      waitForSelector.mockResolvedValue(mockElement);

      const entry = createInputFunction(
        'test_input',
        'Test input',
        'input[name="test"]',
      );

      const result = await entry.handler({ value: 'test' });

      expect(result).toEqual({
        success: false,
        error: '输入框已禁用',
      });
    });

    it('F-06: setReactInputValue 失败返回错误', async () => {
      const { setReactInputValue } = require('../../functions/utils/reactValueSetter');
      const { waitForSelector } = require('../../functions/dom-engine');

      setReactInputValue.mockReturnValue(false);

      const mockElement = document.createElement('input');
      waitForSelector.mockResolvedValue(mockElement);

      const entry = createInputFunction(
        'test_input',
        'Test input',
        'input[name="test"]',
      );

      const result = await entry.handler({ value: 'test' });

      expect(result).toEqual({
        success: false,
        error: '值设置失败，React 组件未响应',
      });
    });
  });

  describe('createTextareaFunction', () => {
    it('F-07: 使用 setReactTextareaValue 设置值', async () => {
      const { setReactTextareaValue } = require('../../functions/utils/reactValueSetter');
      const { waitForSelector } = require('../../functions/dom-engine');

      const mockElement = document.createElement('textarea');
      waitForSelector.mockResolvedValue(mockElement);

      const entry = createTextareaFunction(
        'test_textarea',
        'Test textarea',
        'textarea[name="test"]',
      );

      await entry.handler({ value: 'test value' });

      expect(setReactTextareaValue).toHaveBeenCalledWith(mockElement, 'test value');
    });

    it('F-08: 元素未找到返回错误', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');
      waitForSelector.mockResolvedValue(null);

      const entry = createTextareaFunction(
        'test_textarea',
        'Test textarea',
        'textarea[name="test"]',
      );

      const result = await entry.handler({ value: 'test' });

      expect(result).toEqual({
        success: false,
        error: '文本框未找到: textarea[name="test"]',
      });
    });

    it('F-09: setReactTextareaValue 失败返回错误', async () => {
      const { setReactTextareaValue } = require('../../functions/utils/reactValueSetter');
      const { waitForSelector } = require('../../functions/dom-engine');

      setReactTextareaValue.mockReturnValue(false);

      const mockElement = document.createElement('textarea');
      waitForSelector.mockResolvedValue(mockElement);

      const entry = createTextareaFunction(
        'test_textarea',
        'Test textarea',
        'textarea[name="test"]',
      );

      const result = await entry.handler({ value: 'test' });

      expect(result).toEqual({
        success: false,
        error: '值设置失败，React 组件未响应',
      });
    });
  });

  describe('createSelectFunction', () => {
    it('F-10: 使用 setReactSelectValue 设置值', async () => {
      const { setReactSelectValue } = require('../../functions/utils/reactValueSetter');
      const { waitForSelector } = require('../../functions/dom-engine');

      const mockElement = document.createElement('div');
      waitForSelector.mockResolvedValue(mockElement);

      const entry = createSelectFunction(
        'test_select',
        'Test select',
        '.ant-select',
      );

      await entry.handler({ option: 'option1' });

      expect(setReactSelectValue).toHaveBeenCalledWith(mockElement, 'option1');
    });

    it('F-11: 元素未找到返回错误', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');
      waitForSelector.mockResolvedValue(null);

      const entry = createSelectFunction(
        'test_select',
        'Test select',
        '.ant-select',
      );

      const result = await entry.handler({ option: 'option1' });

      expect(result).toEqual({
        success: false,
        error: '选择器未找到: .ant-select',
      });
    });

    it('F-12: setReactSelectValue 失败返回错误', async () => {
      const { setReactSelectValue } = require('../../functions/utils/reactValueSetter');
      const { waitForSelector } = require('../../functions/dom-engine');

      setReactSelectValue.mockResolvedValue(false);

      const mockElement = document.createElement('div');
      waitForSelector.mockResolvedValue(mockElement);

      const entry = createSelectFunction(
        'test_select',
        'Test select',
        '.ant-select',
      );

      const result = await entry.handler({ option: 'option1' });

      expect(result).toEqual({
        success: false,
        error: '选项未找到: option1',
      });
    });
  });

  describe('createRadioFunction', () => {
    it('F-13: 使用 setReactRadioValue 设置值', async () => {
      const { setReactRadioValue } = require('../../functions/utils/reactValueSetter');
      const { waitForSelector } = require('../../functions/dom-engine');

      const mockElement = document.createElement('div');
      waitForSelector.mockResolvedValue(mockElement);

      const entry = createRadioFunction(
        'test_radio',
        'Test radio',
        '.ant-radio-group',
      );

      await entry.handler({ value: 'option1' });

      expect(setReactRadioValue).toHaveBeenCalledWith(mockElement, 'option1');
    });

    it('F-14: 元素未找到返回错误', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');
      waitForSelector.mockResolvedValue(null);

      const entry = createRadioFunction(
        'test_radio',
        'Test radio',
        '.ant-radio-group',
      );

      const result = await entry.handler({ value: 'option1' });

      expect(result).toEqual({
        success: false,
        error: '单选按钮组未找到: .ant-radio-group',
      });
    });

    it('F-15: setReactRadioValue 失败返回错误', async () => {
      const { setReactRadioValue } = require('../../functions/utils/reactValueSetter');
      const { waitForSelector } = require('../../functions/dom-engine');

      setReactRadioValue.mockResolvedValue(false);

      const mockElement = document.createElement('div');
      waitForSelector.mockResolvedValue(mockElement);

      const entry = createRadioFunction(
        'test_radio',
        'Test radio',
        '.ant-radio-group',
      );

      const result = await entry.handler({ value: 'option1' });

      expect(result).toEqual({
        success: false,
        error: '选项未找到: option1',
      });
    });
  });

  describe('createDateRangeFunction', () => {
    it('F-16: 使用 setReactDateRangePickerValue 设置值', async () => {
      const { setReactDateRangePickerValue } = require('../../functions/utils/reactValueSetter');
      const { waitForSelector } = require('../../functions/dom-engine');

      const mockElement = document.createElement('div');
      waitForSelector.mockResolvedValue(mockElement);

      const entry = createDateRangeFunction(
        'test_date_range',
        'Test date range',
        '.ant-picker-range',
      );

      await entry.handler({ startDate: '2024-01-01', endDate: '2024-01-31' });

      expect(setReactDateRangePickerValue).toHaveBeenCalledWith(
        mockElement,
        '2024-01-01',
        '2024-01-31',
      );
    });

    it('F-17: 元素未找到返回错误', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');
      waitForSelector.mockResolvedValue(null);

      const entry = createDateRangeFunction(
        'test_date_range',
        'Test date range',
        '.ant-picker-range',
      );

      const result = await entry.handler({ startDate: '2024-01-01', endDate: '2024-01-31' });

      expect(result).toEqual({
        success: false,
        error: '日期选择器未找到: .ant-picker-range',
      });
    });

    it('F-18: setReactDateRangePickerValue 失败返回错误', async () => {
      const { setReactDateRangePickerValue } = require('../../functions/utils/reactValueSetter');
      const { waitForSelector } = require('../../functions/dom-engine');

      setReactDateRangePickerValue.mockResolvedValue(false);

      const mockElement = document.createElement('div');
      waitForSelector.mockResolvedValue(mockElement);

      const entry = createDateRangeFunction(
        'test_date_range',
        'Test date range',
        '.ant-picker-range',
      );

      const result = await entry.handler({ startDate: '2024-01-01', endDate: '2024-01-31' });

      expect(result).toEqual({
        success: false,
        error: '日期设置失败',
      });
    });
  });
});