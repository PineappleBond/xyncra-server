import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import { createClickFunction } from '../utils/factory';

const exceptionFunctions: FunctionEntry[] = [
  createClickFunction(
    'pg_exception_back_home',
    '在 404/403/500 异常页点击「Back to home」按钮',
    'button[type="primary"]',
    ['page:exception'],
  ),
];

export function ExceptionFunctions() {
  useRegisterFunctions(exceptionFunctions);
  return null;
}
