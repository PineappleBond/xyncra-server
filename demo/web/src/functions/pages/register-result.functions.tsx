import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import { createClickFunction } from '../utils/factory';

const registerResultFunctions: FunctionEntry[] = [
  createClickFunction(
    'pg_register_result_check_email',
    '在注册结果页点击「查看邮箱」按钮',
    'button:contains("查看邮箱")',
    ['page:register-result'],
  ),
  createClickFunction(
    'pg_register_result_back_home',
    '在注册结果页点击「返回首页」按钮',
    'button:contains("返回首页")',
    ['page:register-result'],
  ),
];

export function RegisterResultFunctions() {
  useRegisterFunctions(registerResultFunctions);
  return null;
}
