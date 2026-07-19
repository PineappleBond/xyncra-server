import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import {
  createClickFunction,
  createInputFunction,
  createTabFunction,
} from '../utils/factory';

const listSearchFunctions: FunctionEntry[] = [
  createTabFunction(
    'pg_search_tab_articles',
    '在搜索列表页切换到「文章」Tab',
    '文章',
    '.ant-tabs',
    ['page:list-search'],
  ),
  createTabFunction(
    'pg_search_tab_projects',
    '在搜索列表页切换到「项目」Tab',
    '项目',
    '.ant-tabs',
    ['page:list-search'],
  ),
  createTabFunction(
    'pg_search_tab_applications',
    '在搜索列表页切换到「应用」Tab',
    '应用',
    '.ant-tabs',
    ['page:list-search'],
  ),
  createInputFunction(
    'pg_search_input',
    '在搜索列表页输入搜索关键词',
    'input.ant-input-search',
    ['page:list-search'],
  ),
  createClickFunction(
    'pg_search_btn',
    '在搜索列表页点击搜索按钮',
    '.ant-input-search .ant-input-search-button',
    ['page:list-search'],
  ),
];

export function ListSearchFunctions() {
  useRegisterFunctions(listSearchFunctions);
  return null;
}
