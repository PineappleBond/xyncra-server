import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import {
  createClickFunction,
  createInputFunction,
  createSelectFunction,
} from '../utils/factory';

const stepFormFunctions: FunctionEntry[] = [
  createSelectFunction(
    'pg_step_form_pay_account',
    '在分步表单页第一步选择付款账户',
    '.ant-select',
    ['page:step-form'],
  ),
  createSelectFunction(
    'pg_step_form_receive_account',
    '在分步表单页第一步选择收款账户',
    '.ant-select',
    ['page:step-form'],
  ),
  createInputFunction(
    'pg_step_form_receiver_name',
    '在分步表单页第一步填写收款人姓名',
    'input[name="receiverName"]',
    ['page:step-form'],
  ),
  createInputFunction(
    'pg_step_form_amount',
    '在分步表单页第一步填写转账金额',
    'input[name="amount"]',
    ['page:step-form'],
  ),
  createClickFunction(
    'pg_step_form_next',
    '在分步表单页点击「下一步」按钮',
    'button[type="submit"]',
    ['page:step-form'],
  ),
  createClickFunction(
    'pg_step_form_confirm',
    '在分步表单页第二步确认并提交',
    'button[type="primary"]',
    ['page:step-form'],
  ),
  createClickFunction(
    'pg_step_form_transfer_again',
    '在分步表单页完成后点击「再转一笔」',
    'button:contains("再转一笔")',
    ['page:step-form'],
  ),
];

export function StepFormFunctions() {
  useRegisterFunctions(stepFormFunctions);
  return null;
}
