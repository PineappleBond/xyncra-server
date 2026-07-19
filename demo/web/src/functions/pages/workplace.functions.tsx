import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import {
  createClickFunction,
  createLinkFunction,
} from '../utils/factory';

const workplaceFunctions: FunctionEntry[] = [
  createLinkFunction(
    'pg_workplace_link_op1',
    '在工作台页点击「操作一」链接',
    'a:contains("操作一")',
    ['page:workplace'],
  ),
  createLinkFunction(
    'pg_workplace_link_op2',
    '在工作台页点击「操作二」链接',
    'a:contains("操作二")',
    ['page:workplace'],
  ),
  createLinkFunction(
    'pg_workplace_link_op3',
    '在工作台页点击「操作三」链接',
    'a:contains("操作三")',
    ['page:workplace'],
  ),
  createLinkFunction(
    'pg_workplace_link_op4',
    '在工作台页点击「操作四」链接',
    'a:contains("操作四")',
    ['page:workplace'],
  ),
  createLinkFunction(
    'pg_workplace_link_op5',
    '在工作台页点击「操作五」链接',
    'a:contains("操作五")',
    ['page:workplace'],
  ),
  createLinkFunction(
    'pg_workplace_link_op6',
    '在工作台页点击「操作六」链接',
    'a:contains("操作六")',
    ['page:workplace'],
  ),
];

export function WorkplaceFunctions() {
  useRegisterFunctions(workplaceFunctions);
  return null;
}
