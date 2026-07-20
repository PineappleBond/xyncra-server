import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import {
  createClickFunction,
  createInputFunction,
  createTabFunction,
  createTextareaFunction,
  createDateRangeFunction,
  createRadioFunction,
  createSelectFunction,
  buildFunctionEntry,
} from '../utils/factory';
import { waitForSelector } from '../dom-engine';
import {
  setReactInputValue,
  setReactTextareaValue,
  setReactSelectValue,
  setReactDateRangePickerValue,
  setReactRadioValue,
} from '../utils/reactValueSetter';

const fillAllFunction: FunctionEntry = buildFunctionEntry(
  {
    name: 'pg_basic_form_fill_all',
    description: '一键填满基础表单的所有字段（标题、起止日期、目标描述、衡量标准、客户、邀评人、权重、公开选项、公开用户）',
    parameters: {
      type: 'object',
      properties: {
        title:       { type: 'string',  description: '标题' },
        startDate:   { type: 'string',  description: '开始日期，格式 YYYY-MM-DD' },
        endDate:     { type: 'string',  description: '结束日期，格式 YYYY-MM-DD' },
        goal:        { type: 'string',  description: '目标描述' },
        standard:    { type: 'string',  description: '衡量标准' },
        client:      { type: 'string',  description: '客户（选填）' },
        invites:     { type: 'string',  description: '邀评人（选填）' },
        weight:      { type: 'string',  description: '权重，纯数字如 50' },
        publicType:  { type: 'string',  description: '公开选项：1=公开, 2=部分公开, 3=不公开' },
        publicUsers: { type: 'string',  description: '公开用户（publicType=2 时必填）' },
      },
      required: ['title', 'startDate', 'endDate', 'goal', 'standard'],
    },
    tags: ['page:basic-form', 'type:fill-all'],
    timeout_ms: 30000,
  },
  async (params) => {
    const errors: string[] = [];
    const filled: string[] = [];
    const failed: { field: string; error: string }[] = [];

    const form = await waitForSelector('.ant-form', 10000);
    if (!form) return { success: false, error: '未找到表单' };

    // 1. 标题 — ProFormText → input[name="title"]
    const titleEl = await waitForSelector('input[name="title"]', 5000);
    if (titleEl) {
      setReactInputValue(titleEl as unknown as HTMLElement, params.title as string);
      filled.push('title');
    } else {
      failed.push({ field: 'title', error: '标题输入框未找到' });
    }

    // 2. 起止日期 — ProFormDateRangePicker
    const datePicker = await waitForSelector('.ant-picker-range', 5000);
    if (datePicker) {
      await setReactDateRangePickerValue(
        datePicker as unknown as HTMLElement,
        params.startDate as string,
        params.endDate as string,
      );
      filled.push('date');
    } else {
      failed.push({ field: 'date', error: '日期选择器未找到' });
    }

    // 3. 目标描述 — ProFormTextArea → textarea[name="goal"]
    const goalEl = await waitForSelector('textarea[name="goal"]', 5000);
    if (goalEl) {
      setReactTextareaValue(goalEl as unknown as HTMLElement, params.goal as string);
      filled.push('goal');
    } else {
      failed.push({ field: 'goal', error: '目标描述文本框未找到' });
    }

    // 4. 衡量标准 — ProFormTextArea → textarea[name="standard"]
    const standardEl = await waitForSelector('textarea[name="standard"]', 5000);
    if (standardEl) {
      setReactTextareaValue(standardEl as unknown as HTMLElement, params.standard as string);
      filled.push('standard');
    } else {
      failed.push({ field: 'standard', error: '衡量标准文本框未找到' });
    }

    // 5. 客户（选填）
    if (params.client) {
      const clientEl = await waitForSelector('input[name="client"]', 5000);
      if (clientEl) {
        setReactInputValue(clientEl as HTMLElement, params.client as string);
        filled.push('client');
      }
    }

    // 6. 邀评人（选填）
    if (params.invites) {
      const invitesEl = await waitForSelector('input[name="invites"]', 5000);
      if (invitesEl) {
        setReactInputValue(invitesEl as HTMLElement, params.invites as string);
        filled.push('invites');
      }
    }

    // 7. 权重 — ProFormDigit
    if (params.weight) {
      const weightEl = await waitForSelector('.ant-input-number-input', 5000);
      if (weightEl) {
        setReactInputValue(weightEl as HTMLElement, params.weight as string);
        filled.push('weight');
      }
    }

    // 8. 公开选项 — ProFormRadio.Group
    if (params.publicType) {
      const radioGroup = await waitForSelector('.ant-radio-group', 5000);
      if (radioGroup) {
        await setReactRadioValue(radioGroup as unknown as HTMLElement, params.publicType as string);
        filled.push('publicType');
      }
    }

    // 9. 公开用户（仅 publicType=2 时显示）
    if (params.publicUsers && params.publicType === '2') {
      const selectEl = await waitForSelector('.ant-select', 5000);
      if (selectEl) {
        await setReactSelectValue(selectEl as unknown as HTMLElement, params.publicUsers as string);
        filled.push('publicUsers');
      }
    }

    if (failed.length > 0) {
      return { success: false, error: failed.map(f => `${f.field}: ${f.error}`).join('; '), filled, failed };
    }
    return { success: true, filled };
  },
);

const basicFormFunctions: FunctionEntry[] = [
  fillAllFunction,
  createInputFunction(
    'pg_basic_form_title_input',
    '在基础表单页填写标题输入框',
    'input[name="title"]',
    ['page:basic-form'],
  ),
  createDateRangeFunction(
    'pg_basic_form_date_range',
    '在基础表单页选择起止日期',
    '.ant-picker',
    ['page:basic-form'],
  ),
  createTextareaFunction(
    'pg_basic_form_desc_textarea',
    '在基础表单页填写目标描述文本框',
    'textarea[name="goal"]',
    ['page:basic-form'],
  ),
  createTextareaFunction(
    'pg_basic_form_standard_textarea',
    '在基础表单页填写衡量标准文本框',
    'textarea[name="standard"]',
    ['page:basic-form'],
  ),
  createInputFunction(
    'pg_basic_form_client_input',
    '在基础表单页填写客户输入框',
    'input[name="client"]',
    ['page:basic-form'],
  ),
  createInputFunction(
    'pg_basic_form_reviewer_input',
    '在基础表单页填写邀评人输入框',
    'input[name="invites"]',
    ['page:basic-form'],
  ),
  createInputFunction(
    'pg_basic_form_weight_input',
    '在基础表单页填写权重输入框',
    'input[name="weight"]',
    ['page:basic-form'],
  ),
  createRadioFunction(
    'pg_basic_form_public_radio',
    '在基础表单页选择公开选项（公开/部分公开/不公开）',
    'input[name="publicType"]',
    ['page:basic-form'],
  ),
  createSelectFunction(
    'pg_basic_form_public_user_select',
    '在基础表单页选择公开用户',
    '.ant-select',
    ['page:basic-form'],
  ),
  createClickFunction(
    'pg_basic_form_submit',
    '在基础表单页提交表单',
    'button[type="submit"]',
    ['page:basic-form'],
  ),
];

export function BasicFormFunctions() {
  useRegisterFunctions(basicFormFunctions);
  return null;
}
