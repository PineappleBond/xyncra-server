import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import {
  createClickFunction,
  createInputFunction,
} from '../utils/factory';

const basicListFunctions: FunctionEntry[] = [
  createClickFunction(
    'pg_basic_list_segment_all',
    '在基础列表页切换到「全部」筛选',
    '.ant-segmented-item:contains("全部")',
    ['page:basic-list'],
  ),
  createClickFunction(
    'pg_basic_list_segment_progress',
    '在基础列表页切换到「进行中」筛选',
    '.ant-segmented-item:contains("进行中")',
    ['page:basic-list'],
  ),
  createClickFunction(
    'pg_basic_list_segment_waiting',
    '在基础列表页切换到「等待中」筛选',
    '.ant-segmented-item:contains("等待中")',
    ['page:basic-list'],
  ),
  createInputFunction(
    'pg_basic_list_search_input',
    '在基础列表页填写搜索输入框',
    '.ant-input-search input',
    ['page:basic-list'],
  ),
  createClickFunction(
    'pg_basic_list_row_edit',
    '在基础列表页点击某行「编辑」操作',
    'a:contains("编辑")',
    ['page:basic-list'],
  ),
  createClickFunction(
    'pg_basic_list_row_delete',
    '在基础列表页点击某行「删除」按钮',
    '.ant-dropdown-trigger',
    ['page:basic-list'],
  ),
  createClickFunction(
    'pg_basic_list_add_btn',
    '在基础列表页点击「添加」按钮',
    'button:contains("添加")',
    ['page:basic-list'],
  ),
  createClickFunction(
    'pg_basic_list_confirm_delete',
    '在基础列表页确认删除弹窗的「确认」按钮',
    '.ant-modal-confirm-btns .ant-btn-primary',
    ['page:basic-list'],
  ),
  createClickFunction(
    'pg_basic_list_confirm_cancel',
    '在基础列表页取消删除弹窗的「取消」按钮',
    '.ant-modal-confirm-btns .ant-btn:not(.ant-btn-primary)',
    ['page:basic-list'],
  ),
  createClickFunction(
    'pg_basic_list_next_page',
    '在基础列表页翻到下一页',
    '.ant-pagination-next',
    ['page:basic-list'],
  ),
];

export function BasicListFunctions() {
  useRegisterFunctions(basicListFunctions);
  return null;
}
