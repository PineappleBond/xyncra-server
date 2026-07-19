import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import {
  createClickFunction,
  createInputFunction,
  createTabFunction,
} from '../utils/factory';

const accountCenterFunctions: FunctionEntry[] = [
  createTabFunction(
    'pg_acc_center_tab_articles',
    '在个人中心切换到「文章」Tab',
    '文章',
    '.ant-tabs',
    ['page:account-center'],
  ),
  createTabFunction(
    'pg_acc_center_tab_applications',
    '在个人中心切换到「应用」Tab',
    '应用',
    '.ant-tabs',
    ['page:account-center'],
  ),
  createTabFunction(
    'pg_acc_center_tab_projects',
    '在个人中心切换到「项目」Tab',
    '项目',
    '.ant-tabs',
    ['page:account-center'],
  ),
  createInputFunction(
    'pg_acc_center_tag_input',
    '在个人中心添加新标签',
    '.ant-tag input',
    ['page:account-center'],
  ),
  createClickFunction(
    'pg_acc_center_add_tag',
    '在个人中心点击添加标签按钮',
    '.ant-tag:contains("+")',
    ['page:account-center'],
  ),
];

export function AccountCenterFunctions() {
  useRegisterFunctions(accountCenterFunctions);
  return null;
}
