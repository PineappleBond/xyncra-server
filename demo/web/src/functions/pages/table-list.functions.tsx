import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import {
  createClickFunction,
  createInputFunction,
  createLinkFunction,
} from '../utils/factory';

const tableListFunctions: FunctionEntry[] = [
  createClickFunction(
    'pg_table_list_new_btn',
    '在表格列表页点击「新建」按钮',
    '.ant-pro-table-list-toolbar-right button',
    ['page:table-list'],
  ),
  createInputFunction(
    'pg_table_list_search_input',
    '在表格列表页填写搜索输入框',
    '.ant-input-search input',
    ['page:table-list'],
  ),
  createClickFunction(
    'pg_table_list_refresh',
    '在表格列表页刷新表格数据',
    '.ant-pro-table-list-toolbar-setting-item',
    ['page:table-list'],
  ),
  createClickFunction(
    'pg_table_list_batch_delete',
    '在表格列表页点击「批量删除」按钮',
    'button:contains("Batch deletion")',
    ['page:table-list'],
  ),
  createClickFunction(
    'pg_table_list_batch_approve',
    '在表格列表页点击「批量审批」按钮',
    'button:contains("Batch approval")',
    ['page:table-list'],
  ),
  createLinkFunction(
    'pg_table_list_row_config',
    '在表格列表页点击某行「配置」操作链接',
    'a:contains("Configuration")',
    ['page:table-list'],
  ),
  createLinkFunction(
    'pg_table_list_row_subscribe',
    '在表格列表页点击某行「订阅」操作链接',
    'a:contains("Subscribe to alerts")',
    ['page:table-list'],
  ),
  createClickFunction(
    'pg_table_list_row_select',
    '在表格列表页勾选某行复选框',
    '.ant-checkbox-input',
    ['page:table-list'],
  ),
  createClickFunction(
    'pg_table_list_next_page',
    '在表格列表页翻到下一页',
    '.ant-pagination-next',
    ['page:table-list'],
  ),
  createClickFunction(
    'pg_table_list_page_size',
    '在表格列表页切换每页条数',
    '.ant-pagination-options .ant-select',
    ['page:table-list'],
  ),
  createClickFunction(
    'pg_table_list_drawer_close',
    '在表格列表页关闭详情抽屉',
    '.ant-drawer-close',
    ['page:table-list'],
  ),
];

export function TableListFunctions() {
  useRegisterFunctions(tableListFunctions);
  return null;
}
