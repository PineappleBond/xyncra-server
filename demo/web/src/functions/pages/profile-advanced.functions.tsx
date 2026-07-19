import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import {
  createClickFunction,
  createTabFunction,
  createDropdownFunction,
} from '../utils/factory';

const profileAdvancedFunctions: FunctionEntry[] = [
  createClickFunction(
    'pg_profile_adv_action_1',
    '在高级详情页执行「操作一」',
    'button:contains("操作一")',
    ['page:profile-advanced'],
  ),
  createClickFunction(
    'pg_profile_adv_action_2',
    '在高级详情页执行「操作二」',
    'button:contains("操作二")',
    ['page:profile-advanced'],
  ),
  createDropdownFunction(
    'pg_profile_adv_dropdown',
    '在高级详情页点击下拉菜单',
    '.ant-dropdown-trigger',
    ['page:profile-advanced'],
  ),
  createTabFunction(
    'pg_profile_adv_tab_detail',
    '在高级详情页切换到「详情」Tab',
    '详情',
    '.ant-tabs',
    ['page:profile-advanced'],
  ),
  createTabFunction(
    'pg_profile_adv_tab_rule',
    '在高级详情页切换到「规则」Tab',
    '规则',
    '.ant-tabs',
    ['page:profile-advanced'],
  ),
];

export function ProfileAdvancedFunctions() {
  useRegisterFunctions(profileAdvancedFunctions);
  return null;
}
