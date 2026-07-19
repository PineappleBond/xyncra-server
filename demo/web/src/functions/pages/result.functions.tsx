import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import {
  createClickFunction,
  createLinkFunction,
} from '../utils/factory';

const resultFunctions: FunctionEntry[] = [
  createClickFunction(
    'pg_result_back_list',
    '在结果页点击「返回列表」按钮',
    'button:contains("返回列表")',
    ['page:result'],
  ),
  createClickFunction(
    'pg_result_view_project',
    '在结果页点击「查看项目」按钮',
    'button:contains("查看项目")',
    ['page:result'],
  ),
  createClickFunction(
    'pg_result_print',
    '在结果页点击「打印」按钮',
    'button:contains("打印")',
    ['page:result'],
  ),
  createLinkFunction(
    'pg_result_urge',
    '在结果页点击「催一下」链接',
    'a:contains("催一下")',
    ['page:result'],
  ),
];

export function ResultFunctions() {
  useRegisterFunctions(resultFunctions);
  return null;
}
