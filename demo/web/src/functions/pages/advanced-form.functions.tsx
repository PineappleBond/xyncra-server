import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import {
  createClickFunction,
  createInputFunction,
  createSelectFunction,
  createDateRangeFunction,
} from '../utils/factory';

const advancedFormFunctions: FunctionEntry[] = [
  createInputFunction(
    'pg_adv_form_name_input',
    '在高级表单页填写仓库名',
    'input[name="name"]',
    ['page:advanced-form'],
  ),
  createInputFunction(
    'pg_adv_form_url_input',
    '在高级表单页填写仓库域名',
    'input[name="url"]',
    ['page:advanced-form'],
  ),
  createSelectFunction(
    'pg_adv_form_owner_select',
    '在高级表单页选择仓库管理员',
    '.ant-select',
    ['page:advanced-form'],
  ),
  createSelectFunction(
    'pg_adv_form_approver_select',
    '在高级表单页选择审批人',
    '.ant-select',
    ['page:advanced-form'],
  ),
  createDateRangeFunction(
    'pg_adv_form_date_range',
    '在高级表单页选择生效日期',
    '.ant-picker',
    ['page:advanced-form'],
  ),
  createSelectFunction(
    'pg_adv_form_type_select',
    '在高级表单页选择仓库类型',
    '.ant-select',
    ['page:advanced-form'],
  ),
  createInputFunction(
    'pg_adv_form_task_name',
    '在高级表单页填写任务名',
    'input[name="name2"]',
    ['page:advanced-form'],
  ),
  createInputFunction(
    'pg_adv_form_task_desc',
    '在高级表单页填写任务描述',
    'input[name="url2"]',
    ['page:advanced-form'],
  ),
  createSelectFunction(
    'pg_adv_form_task_owner',
    '在高级表单页选择任务执行人',
    '.ant-select',
    ['page:advanced-form'],
  ),
  createSelectFunction(
    'pg_adv_form_task_approver',
    '在高级表单页选择任务责任人',
    '.ant-select',
    ['page:advanced-form'],
  ),
  createClickFunction(
    'pg_adv_form_submit',
    '在高级表单页提交表单',
    'button[type="submit"]',
    ['page:advanced-form'],
  ),
];

export function AdvancedFormFunctions() {
  useRegisterFunctions(advancedFormFunctions);
  return null;
}
