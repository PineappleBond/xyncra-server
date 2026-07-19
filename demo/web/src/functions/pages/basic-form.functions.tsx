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
} from '../utils/factory';

const basicFormFunctions: FunctionEntry[] = [
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
