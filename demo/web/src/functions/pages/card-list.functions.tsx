import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import {
  createClickFunction,
  createLinkFunction,
} from '../utils/factory';

const cardListFunctions: FunctionEntry[] = [
  createLinkFunction(
    'pg_card_list_quick_start',
    '在卡片列表页点击「快速开始」链接',
    'a:contains("快速开始")',
    ['page:card-list'],
  ),
  createLinkFunction(
    'pg_card_list_product_intro',
    '在卡片列表页点击「产品简介」链接',
    'a:contains("产品简介")',
    ['page:card-list'],
  ),
  createLinkFunction(
    'pg_card_list_product_docs',
    '在卡片列表页点击「产品文档」链接',
    'a:contains("产品文档")',
    ['page:card-list'],
  ),
  createClickFunction(
    'pg_card_list_add_product',
    '在卡片列表页点击「新增产品」按钮',
    'button:contains("新增产品")',
    ['page:card-list'],
  ),
];

export function CardListFunctions() {
  useRegisterFunctions(cardListFunctions);
  return null;
}
