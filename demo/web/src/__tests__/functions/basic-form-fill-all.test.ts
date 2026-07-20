/**
 * basic-form-fill-all.test.ts 集成测试
 *
 * 测试 pg_basic_form_fill_all 函数的正常路径、边界场景和错误处理。
 */

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
}));

describe('pg_basic_form_fill_all', () => {
  let fillAllFunction: any;

  beforeAll(async () => {
    // 动态导入以确保 mocks 已设置
    const module = await import('../../functions/pages/basic-form.functions');
    // 获取 basicFormFunctions 数组中的第一个函数
    const { useRegisterFunctions } = require('@xyncra/client-web');
    const registeredFunctions = useRegisterFunctions.mock.calls[0]?.[0];
    if (registeredFunctions) {
      fillAllFunction = registeredFunctions.find(
        (f: any) => f.info.name === 'pg_basic_form_fill_all'
      );
    }
  });

  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('正常路径', () => {
    it('FF-01: 填充所有必填字段', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');
      const {
        setReactInputValue,
        setReactTextareaValue,
        setReactDateRangePickerValue,
      } = require('../../functions/utils/reactValueSetter');

      // Mock 表单元素
      const form = document.createElement('form');
      form.className = 'ant-form';
      document.body.appendChild(form);

      const titleInput = document.createElement('input');
      titleInput.name = 'title';
      document.body.appendChild(titleInput);

      const datePicker = document.createElement('div');
      datePicker.className = 'ant-picker-range';
      document.body.appendChild(datePicker);

      const goalTextarea = document.createElement('textarea');
      goalTextarea.name = 'goal';
      document.body.appendChild(goalTextarea);

      const standardTextarea = document.createElement('textarea');
      standardTextarea.name = 'standard';
      document.body.appendChild(standardTextarea);

      waitForSelector
        .mockResolvedValueOnce(form) // .ant-form
        .mockResolvedValueOnce(titleInput) // input[name="title"]
        .mockResolvedValueOnce(datePicker) // .ant-picker-range
        .mockResolvedValueOnce(goalTextarea) // textarea[name="goal"]
        .mockResolvedValueOnce(standardTextarea); // textarea[name="standard"]

      const params = {
        title: '测试标题',
        startDate: '2024-01-01',
        endDate: '2024-01-31',
        goal: '测试目标描述',
        standard: '测试衡量标准',
      };

      const result = await fillAllFunction.handler(params);

      expect(result.success).toBe(true);
      expect(result.filled).toContain('title');
      expect(result.filled).toContain('date');
      expect(result.filled).toContain('goal');
      expect(result.filled).toContain('standard');

      expect(setReactInputValue).toHaveBeenCalledWith(titleInput, '测试标题');
      expect(setReactDateRangePickerValue).toHaveBeenCalledWith(
        datePicker,
        '2024-01-01',
        '2024-01-31',
      );
      expect(setReactTextareaValue).toHaveBeenCalledWith(goalTextarea, '测试目标描述');
      expect(setReactTextareaValue).toHaveBeenCalledWith(standardTextarea, '测试衡量标准');

      document.body.removeChild(form);
      document.body.removeChild(titleInput);
      document.body.removeChild(datePicker);
      document.body.removeChild(goalTextarea);
      document.body.removeChild(standardTextarea);
    });

    it('FF-02: 仅填充必填字段', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');

      const form = document.createElement('form');
      form.className = 'ant-form';
      document.body.appendChild(form);

      const titleInput = document.createElement('input');
      titleInput.name = 'title';
      document.body.appendChild(titleInput);

      const datePicker = document.createElement('div');
      datePicker.className = 'ant-picker-range';
      document.body.appendChild(datePicker);

      const goalTextarea = document.createElement('textarea');
      goalTextarea.name = 'goal';
      document.body.appendChild(goalTextarea);

      const standardTextarea = document.createElement('textarea');
      standardTextarea.name = 'standard';
      document.body.appendChild(standardTextarea);

      waitForSelector
        .mockResolvedValueOnce(form)
        .mockResolvedValueOnce(titleInput)
        .mockResolvedValueOnce(datePicker)
        .mockResolvedValueOnce(goalTextarea)
        .mockResolvedValueOnce(standardTextarea);

      const params = {
        title: '测试标题',
        startDate: '2024-01-01',
        endDate: '2024-01-31',
        goal: '测试目标描述',
        standard: '测试衡量标准',
      };

      const result = await fillAllFunction.handler(params);

      expect(result.success).toBe(true);
      expect(result.filled).toHaveLength(4);

      document.body.removeChild(form);
      document.body.removeChild(titleInput);
      document.body.removeChild(datePicker);
      document.body.removeChild(goalTextarea);
      document.body.removeChild(standardTextarea);
    });

    it('FF-03: 填充所有字段（包括选填）', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');
      const {
        setReactInputValue,
        setReactTextareaValue,
        setReactDateRangePickerValue,
        setReactSelectValue,
        setReactRadioValue,
      } = require('../../functions/utils/reactValueSetter');

      const form = document.createElement('form');
      form.className = 'ant-form';
      document.body.appendChild(form);

      const titleInput = document.createElement('input');
      titleInput.name = 'title';
      document.body.appendChild(titleInput);

      const datePicker = document.createElement('div');
      datePicker.className = 'ant-picker-range';
      document.body.appendChild(datePicker);

      const goalTextarea = document.createElement('textarea');
      goalTextarea.name = 'goal';
      document.body.appendChild(goalTextarea);

      const standardTextarea = document.createElement('textarea');
      standardTextarea.name = 'standard';
      document.body.appendChild(standardTextarea);

      const clientInput = document.createElement('input');
      clientInput.name = 'client';
      document.body.appendChild(clientInput);

      const invitesInput = document.createElement('input');
      invitesInput.name = 'invites';
      document.body.appendChild(invitesInput);

      const weightInput = document.createElement('input');
      weightInput.className = 'ant-input-number-input';
      document.body.appendChild(weightInput);

      const radioGroup = document.createElement('div');
      radioGroup.className = 'ant-radio-group';
      document.body.appendChild(radioGroup);

      const selectEl = document.createElement('div');
      selectEl.className = 'ant-select';
      document.body.appendChild(selectEl);

      waitForSelector
        .mockResolvedValueOnce(form)
        .mockResolvedValueOnce(titleInput)
        .mockResolvedValueOnce(datePicker)
        .mockResolvedValueOnce(goalTextarea)
        .mockResolvedValueOnce(standardTextarea)
        .mockResolvedValueOnce(clientInput)
        .mockResolvedValueOnce(invitesInput)
        .mockResolvedValueOnce(weightInput)
        .mockResolvedValueOnce(radioGroup)
        .mockResolvedValueOnce(selectEl);

      const params = {
        title: '测试标题',
        startDate: '2024-01-01',
        endDate: '2024-01-31',
        goal: '测试目标描述',
        standard: '测试衡量标准',
        client: '测试客户',
        invites: '测试邀评人',
        weight: '50',
        publicType: '1',
        publicUsers: 'user1',
      };

      const result = await fillAllFunction.handler(params);

      expect(result.success).toBe(true);
      expect(result.filled).toContain('title');
      expect(result.filled).toContain('date');
      expect(result.filled).toContain('goal');
      expect(result.filled).toContain('standard');
      expect(result.filled).toContain('client');
      expect(result.filled).toContain('invites');
      expect(result.filled).toContain('weight');
      expect(result.filled).toContain('publicType');

      expect(setReactInputValue).toHaveBeenCalledWith(clientInput, '测试客户');
      expect(setReactInputValue).toHaveBeenCalledWith(invitesInput, '测试邀评人');
      expect(setReactInputValue).toHaveBeenCalledWith(weightInput, '50');
      expect(setReactRadioValue).toHaveBeenCalledWith(radioGroup, '1');

      document.body.removeChild(form);
      document.body.removeChild(titleInput);
      document.body.removeChild(datePicker);
      document.body.removeChild(goalTextarea);
      document.body.removeChild(standardTextarea);
      document.body.removeChild(clientInput);
      document.body.removeChild(invitesInput);
      document.body.removeChild(weightInput);
      document.body.removeChild(radioGroup);
      document.body.removeChild(selectEl);
    });

    it('FF-04: publicType=2 时填充公开用户', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');
      const { setReactSelectValue } = require('../../functions/utils/reactValueSetter');

      const form = document.createElement('form');
      form.className = 'ant-form';
      document.body.appendChild(form);

      const titleInput = document.createElement('input');
      titleInput.name = 'title';
      document.body.appendChild(titleInput);

      const datePicker = document.createElement('div');
      datePicker.className = 'ant-picker-range';
      document.body.appendChild(datePicker);

      const goalTextarea = document.createElement('textarea');
      goalTextarea.name = 'goal';
      document.body.appendChild(goalTextarea);

      const standardTextarea = document.createElement('textarea');
      standardTextarea.name = 'standard';
      document.body.appendChild(standardTextarea);

      const radioGroup = document.createElement('div');
      radioGroup.className = 'ant-radio-group';
      document.body.appendChild(radioGroup);

      const selectEl = document.createElement('div');
      selectEl.className = 'ant-select';
      document.body.appendChild(selectEl);

      waitForSelector
        .mockResolvedValueOnce(form)
        .mockResolvedValueOnce(titleInput)
        .mockResolvedValueOnce(datePicker)
        .mockResolvedValueOnce(goalTextarea)
        .mockResolvedValueOnce(standardTextarea)
        .mockResolvedValueOnce(radioGroup)
        .mockResolvedValueOnce(selectEl);

      const params = {
        title: '测试标题',
        startDate: '2024-01-01',
        endDate: '2024-01-31',
        goal: '测试目标描述',
        standard: '测试衡量标准',
        publicType: '2',
        publicUsers: 'user1',
      };

      const result = await fillAllFunction.handler(params);

      expect(result.success).toBe(true);
      expect(result.filled).toContain('publicType');
      expect(result.filled).toContain('publicUsers');
      expect(setReactSelectValue).toHaveBeenCalledWith(selectEl, 'user1');

      document.body.removeChild(form);
      document.body.removeChild(titleInput);
      document.body.removeChild(datePicker);
      document.body.removeChild(goalTextarea);
      document.body.removeChild(standardTextarea);
      document.body.removeChild(radioGroup);
      document.body.removeChild(selectEl);
    });

    it('FF-05: publicType=1 时不填充公开用户', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');
      const { setReactSelectValue } = require('../../functions/utils/reactValueSetter');

      const form = document.createElement('form');
      form.className = 'ant-form';
      document.body.appendChild(form);

      const titleInput = document.createElement('input');
      titleInput.name = 'title';
      document.body.appendChild(titleInput);

      const datePicker = document.createElement('div');
      datePicker.className = 'ant-picker-range';
      document.body.appendChild(datePicker);

      const goalTextarea = document.createElement('textarea');
      goalTextarea.name = 'goal';
      document.body.appendChild(goalTextarea);

      const standardTextarea = document.createElement('textarea');
      standardTextarea.name = 'standard';
      document.body.appendChild(standardTextarea);

      const radioGroup = document.createElement('div');
      radioGroup.className = 'ant-radio-group';
      document.body.appendChild(radioGroup);

      waitForSelector
        .mockResolvedValueOnce(form)
        .mockResolvedValueOnce(titleInput)
        .mockResolvedValueOnce(datePicker)
        .mockResolvedValueOnce(goalTextarea)
        .mockResolvedValueOnce(standardTextarea)
        .mockResolvedValueOnce(radioGroup);

      const params = {
        title: '测试标题',
        startDate: '2024-01-01',
        endDate: '2024-01-31',
        goal: '测试目标描述',
        standard: '测试衡量标准',
        publicType: '1',
        publicUsers: 'user1',
      };

      const result = await fillAllFunction.handler(params);

      expect(result.success).toBe(true);
      expect(result.filled).not.toContain('publicUsers');
      expect(setReactSelectValue).not.toHaveBeenCalled();

      document.body.removeChild(form);
      document.body.removeChild(titleInput);
      document.body.removeChild(datePicker);
      document.body.removeChild(goalTextarea);
      document.body.removeChild(standardTextarea);
      document.body.removeChild(radioGroup);
    });
  });

  describe('边界场景', () => {
    it('FF-06: weight 为 0', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');
      const { setReactInputValue } = require('../../functions/utils/reactValueSetter');

      const form = document.createElement('form');
      form.className = 'ant-form';
      document.body.appendChild(form);

      const titleInput = document.createElement('input');
      titleInput.name = 'title';
      document.body.appendChild(titleInput);

      const datePicker = document.createElement('div');
      datePicker.className = 'ant-picker-range';
      document.body.appendChild(datePicker);

      const goalTextarea = document.createElement('textarea');
      goalTextarea.name = 'goal';
      document.body.appendChild(goalTextarea);

      const standardTextarea = document.createElement('textarea');
      standardTextarea.name = 'standard';
      document.body.appendChild(standardTextarea);

      const weightInput = document.createElement('input');
      weightInput.className = 'ant-input-number-input';
      document.body.appendChild(weightInput);

      waitForSelector
        .mockResolvedValueOnce(form)
        .mockResolvedValueOnce(titleInput)
        .mockResolvedValueOnce(datePicker)
        .mockResolvedValueOnce(goalTextarea)
        .mockResolvedValueOnce(standardTextarea)
        .mockResolvedValueOnce(weightInput);

      const params = {
        title: '测试标题',
        startDate: '2024-01-01',
        endDate: '2024-01-31',
        goal: '测试目标描述',
        standard: '测试衡量标准',
        weight: '0',
      };

      const result = await fillAllFunction.handler(params);

      expect(result.success).toBe(true);
      expect(result.filled).toContain('weight');
      expect(setReactInputValue).toHaveBeenCalledWith(weightInput, '0');

      document.body.removeChild(form);
      document.body.removeChild(titleInput);
      document.body.removeChild(datePicker);
      document.body.removeChild(goalTextarea);
      document.body.removeChild(standardTextarea);
      document.body.removeChild(weightInput);
    });

    it('FF-07: weight 为 100', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');
      const { setReactInputValue } = require('../../functions/utils/reactValueSetter');

      const form = document.createElement('form');
      form.className = 'ant-form';
      document.body.appendChild(form);

      const titleInput = document.createElement('input');
      titleInput.name = 'title';
      document.body.appendChild(titleInput);

      const datePicker = document.createElement('div');
      datePicker.className = 'ant-picker-range';
      document.body.appendChild(datePicker);

      const goalTextarea = document.createElement('textarea');
      goalTextarea.name = 'goal';
      document.body.appendChild(goalTextarea);

      const standardTextarea = document.createElement('textarea');
      standardTextarea.name = 'standard';
      document.body.appendChild(standardTextarea);

      const weightInput = document.createElement('input');
      weightInput.className = 'ant-input-number-input';
      document.body.appendChild(weightInput);

      waitForSelector
        .mockResolvedValueOnce(form)
        .mockResolvedValueOnce(titleInput)
        .mockResolvedValueOnce(datePicker)
        .mockResolvedValueOnce(goalTextarea)
        .mockResolvedValueOnce(standardTextarea)
        .mockResolvedValueOnce(weightInput);

      const params = {
        title: '测试标题',
        startDate: '2024-01-01',
        endDate: '2024-01-31',
        goal: '测试目标描述',
        standard: '测试衡量标准',
        weight: '100',
      };

      const result = await fillAllFunction.handler(params);

      expect(result.success).toBe(true);
      expect(result.filled).toContain('weight');
      expect(setReactInputValue).toHaveBeenCalledWith(weightInput, '100');

      document.body.removeChild(form);
      document.body.removeChild(titleInput);
      document.body.removeChild(datePicker);
      document.body.removeChild(goalTextarea);
      document.body.removeChild(standardTextarea);
      document.body.removeChild(weightInput);
    });

    it('FF-08: 超长文本', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');
      const { setReactTextareaValue } = require('../../functions/utils/reactValueSetter');

      const form = document.createElement('form');
      form.className = 'ant-form';
      document.body.appendChild(form);

      const titleInput = document.createElement('input');
      titleInput.name = 'title';
      document.body.appendChild(titleInput);

      const datePicker = document.createElement('div');
      datePicker.className = 'ant-picker-range';
      document.body.appendChild(datePicker);

      const goalTextarea = document.createElement('textarea');
      goalTextarea.name = 'goal';
      document.body.appendChild(goalTextarea);

      const standardTextarea = document.createElement('textarea');
      standardTextarea.name = 'standard';
      document.body.appendChild(standardTextarea);

      waitForSelector
        .mockResolvedValueOnce(form)
        .mockResolvedValueOnce(titleInput)
        .mockResolvedValueOnce(datePicker)
        .mockResolvedValueOnce(goalTextarea)
        .mockResolvedValueOnce(standardTextarea);

      const longText = 'a'.repeat(10000);
      const params = {
        title: '测试标题',
        startDate: '2024-01-01',
        endDate: '2024-01-31',
        goal: longText,
        standard: longText,
      };

      const result = await fillAllFunction.handler(params);

      expect(result.success).toBe(true);
      expect(setReactTextareaValue).toHaveBeenCalledWith(goalTextarea, longText);
      expect(setReactTextareaValue).toHaveBeenCalledWith(standardTextarea, longText);

      document.body.removeChild(form);
      document.body.removeChild(titleInput);
      document.body.removeChild(datePicker);
      document.body.removeChild(goalTextarea);
      document.body.removeChild(standardTextarea);
    });

    it('FF-09: 特殊字符', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');
      const { setReactInputValue } = require('../../functions/utils/reactValueSetter');

      const form = document.createElement('form');
      form.className = 'ant-form';
      document.body.appendChild(form);

      const titleInput = document.createElement('input');
      titleInput.name = 'title';
      document.body.appendChild(titleInput);

      const datePicker = document.createElement('div');
      datePicker.className = 'ant-picker-range';
      document.body.appendChild(datePicker);

      const goalTextarea = document.createElement('textarea');
      goalTextarea.name = 'goal';
      document.body.appendChild(goalTextarea);

      const standardTextarea = document.createElement('textarea');
      standardTextarea.name = 'standard';
      document.body.appendChild(standardTextarea);

      waitForSelector
        .mockResolvedValueOnce(form)
        .mockResolvedValueOnce(titleInput)
        .mockResolvedValueOnce(datePicker)
        .mockResolvedValueOnce(goalTextarea)
        .mockResolvedValueOnce(standardTextarea);

      const specialTitle = 'Test <script>alert("xss")</script> & "quotes"';
      const params = {
        title: specialTitle,
        startDate: '2024-01-01',
        endDate: '2024-01-31',
        goal: '测试目标描述',
        standard: '测试衡量标准',
      };

      const result = await fillAllFunction.handler(params);

      expect(result.success).toBe(true);
      expect(setReactInputValue).toHaveBeenCalledWith(titleInput, specialTitle);

      document.body.removeChild(form);
      document.body.removeChild(titleInput);
      document.body.removeChild(datePicker);
      document.body.removeChild(goalTextarea);
      document.body.removeChild(standardTextarea);
    });

    it('FF-10: 日期边界值', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');
      const { setReactDateRangePickerValue } = require('../../functions/utils/reactValueSetter');

      const form = document.createElement('form');
      form.className = 'ant-form';
      document.body.appendChild(form);

      const titleInput = document.createElement('input');
      titleInput.name = 'title';
      document.body.appendChild(titleInput);

      const datePicker = document.createElement('div');
      datePicker.className = 'ant-picker-range';
      document.body.appendChild(datePicker);

      const goalTextarea = document.createElement('textarea');
      goalTextarea.name = 'goal';
      document.body.appendChild(goalTextarea);

      const standardTextarea = document.createElement('textarea');
      standardTextarea.name = 'standard';
      document.body.appendChild(standardTextarea);

      waitForSelector
        .mockResolvedValueOnce(form)
        .mockResolvedValueOnce(titleInput)
        .mockResolvedValueOnce(datePicker)
        .mockResolvedValueOnce(goalTextarea)
        .mockResolvedValueOnce(standardTextarea);

      const params = {
        title: '测试标题',
        startDate: '2000-01-01',
        endDate: '2099-12-31',
        goal: '测试目标描述',
        standard: '测试衡量标准',
      };

      const result = await fillAllFunction.handler(params);

      expect(result.success).toBe(true);
      expect(setReactDateRangePickerValue).toHaveBeenCalledWith(
        datePicker,
        '2000-01-01',
        '2099-12-31',
      );

      document.body.removeChild(form);
      document.body.removeChild(titleInput);
      document.body.removeChild(datePicker);
      document.body.removeChild(goalTextarea);
      document.body.removeChild(standardTextarea);
    });

    it('FF-11: 空字符串可选字段', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');
      const { setReactInputValue } = require('../../functions/utils/reactValueSetter');

      const form = document.createElement('form');
      form.className = 'ant-form';
      document.body.appendChild(form);

      const titleInput = document.createElement('input');
      titleInput.name = 'title';
      document.body.appendChild(titleInput);

      const datePicker = document.createElement('div');
      datePicker.className = 'ant-picker-range';
      document.body.appendChild(datePicker);

      const goalTextarea = document.createElement('textarea');
      goalTextarea.name = 'goal';
      document.body.appendChild(goalTextarea);

      const standardTextarea = document.createElement('textarea');
      standardTextarea.name = 'standard';
      document.body.appendChild(standardTextarea);

      waitForSelector
        .mockResolvedValueOnce(form)
        .mockResolvedValueOnce(titleInput)
        .mockResolvedValueOnce(datePicker)
        .mockResolvedValueOnce(goalTextarea)
        .mockResolvedValueOnce(standardTextarea);

      const params = {
        title: '测试标题',
        startDate: '2024-01-01',
        endDate: '2024-01-31',
        goal: '测试目标描述',
        standard: '测试衡量标准',
        client: '',
        invites: '',
        weight: '',
      };

      const result = await fillAllFunction.handler(params);

      expect(result.success).toBe(true);
      expect(setReactInputValue).not.toHaveBeenCalledWith(expect.anything(), '');

      document.body.removeChild(form);
      document.body.removeChild(titleInput);
      document.body.removeChild(datePicker);
      document.body.removeChild(goalTextarea);
      document.body.removeChild(standardTextarea);
    });
  });

  describe('错误路径', () => {
    it('FF-12: 表单不存在', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');
      waitForSelector.mockResolvedValue(null);

      const params = {
        title: '测试标题',
        startDate: '2024-01-01',
        endDate: '2024-01-31',
        goal: '测试目标描述',
        standard: '测试衡量标准',
      };

      const result = await fillAllFunction.handler(params);

      expect(result.success).toBe(false);
      expect(result.error).toBe('未找到表单');
    });

    it('FF-13: 标题输入框不存在', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');

      const form = document.createElement('form');
      form.className = 'ant-form';
      document.body.appendChild(form);

      const datePicker = document.createElement('div');
      datePicker.className = 'ant-picker-range';
      document.body.appendChild(datePicker);

      const goalTextarea = document.createElement('textarea');
      goalTextarea.name = 'goal';
      document.body.appendChild(goalTextarea);

      const standardTextarea = document.createElement('textarea');
      standardTextarea.name = 'standard';
      document.body.appendChild(standardTextarea);

      waitForSelector
        .mockResolvedValueOnce(form)
        .mockResolvedValueOnce(null) // titleInput
        .mockResolvedValueOnce(datePicker)
        .mockResolvedValueOnce(goalTextarea)
        .mockResolvedValueOnce(standardTextarea);

      const params = {
        title: '测试标题',
        startDate: '2024-01-01',
        endDate: '2024-01-31',
        goal: '测试目标描述',
        standard: '测试衡量标准',
      };

      const result = await fillAllFunction.handler(params);

      expect(result.success).toBe(false);
      expect(result.failed).toContainEqual({
        field: 'title',
        error: '标题输入框未找到',
      });

      document.body.removeChild(form);
      document.body.removeChild(datePicker);
      document.body.removeChild(goalTextarea);
      document.body.removeChild(standardTextarea);
    });

    it('FF-14: 日期选择器不存在', async () => {
      const { waitForSelector } = require('../../functions/dom-engine');

      const form = document.createElement('form');
      form.className = 'ant-form';
      document.body.appendChild(form);

      const titleInput = document.createElement('input');
      titleInput.name = 'title';
      document.body.appendChild(titleInput);

      const goalTextarea = document.createElement('textarea');
      goalTextarea.name = 'goal';
      document.body.appendChild(goalTextarea);

      const standardTextarea = document.createElement('textarea');
      standardTextarea.name = 'standard';
      document.body.appendChild(standardTextarea);

      waitForSelector
        .mockResolvedValueOnce(form)
        .mockResolvedValueOnce(titleInput)
        .mockResolvedValueOnce(null) // datePicker
        .mockResolvedValueOnce(goalTextarea)
        .mockResolvedValueOnce(standardTextarea);

      const params = {
        title: '测试标题',
        startDate: '2024-01-01',
        endDate: '2024-01-31',
        goal: '测试目标描述',
        standard: '测试衡量标准',
      };

      const result = await fillAllFunction.handler(params);

      expect(result.success).toBe(false);
      expect(result.failed).toContainEqual({
        field: 'date',
        error: '日期选择器未找到',
      });

      document.body.removeChild(form);
      document.body.removeChild(titleInput);
      document.body.removeChild(goalTextarea);
      document.body.removeChild(standardTextarea);
    });
  });

  describe('函数签名验证', () => {
    it('FF-15: 函数名称正确', () => {
      expect(fillAllFunction.info.name).toBe('pg_basic_form_fill_all');
    });

    it('FF-16: 函数描述存在', () => {
      expect(fillAllFunction.info.description).toBeDefined();
      expect(fillAllFunction.info.description.length).toBeGreaterThan(0);
    });

    it('FF-17: 必填参数正确', () => {
      const required = fillAllFunction.info.parameters.required;
      expect(required).toContain('title');
      expect(required).toContain('startDate');
      expect(required).toContain('endDate');
      expect(required).toContain('goal');
      expect(required).toContain('standard');
    });

    it('FF-18: 超时时间设置', () => {
      expect(fillAllFunction.info.timeout_ms).toBe(30000);
    });
  });
});