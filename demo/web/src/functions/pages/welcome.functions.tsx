import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import { createLinkFunction } from '../utils/factory';

const welcomeFunctions: FunctionEntry[] = [
  createLinkFunction(
    'pg_welcome_card_umi',
    '在欢迎页点击「Learn umi」信息卡片',
    'a[aria-label="Learn umi"]',
    ['page:welcome'],
  ),
  createLinkFunction(
    'pg_welcome_card_antd',
    '在欢迎页点击「Learn Ant Design」信息卡片',
    'a[aria-label="Learn Ant Design"]',
    ['page:welcome'],
  ),
  createLinkFunction(
    'pg_welcome_card_procomponents',
    '在欢迎页点击「Learn Pro Components」信息卡片',
    'a[aria-label="Learn Pro Components"]',
    ['page:welcome'],
  ),
];

export function WelcomeFunctions() {
  useRegisterFunctions(welcomeFunctions);
  return null;
}
