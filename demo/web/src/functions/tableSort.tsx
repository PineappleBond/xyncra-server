import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';
import { waitForLoadingComplete } from './dom-engine';

const tableSortInfo: FunctionInfo = {
  name: 'table_sort',
  description: '按指定列对表格进行排序',
  parameters: {
    type: 'object',
    properties: {
      column_index: { type: 'number', description: '列索引（从 0 开始）' },
      order: { type: 'string', enum: ['ascend', 'descend'], description: '排序方向' },
    },
    required: ['column_index', 'order'],
  },
  tags: ['table', 'page-specific'],
  timeout_ms: 15000,
};

function getSortState(header: Element): string | null {
  const sorter = header.querySelector('.ant-table-column-sorter');
  if (!sorter) return null;
  if (sorter.classList.contains('ant-table-column-sorter-active')) {
    const up = sorter.querySelector('.ant-table-column-sorter-up.active');
    const down = sorter.querySelector('.ant-table-column-sorter-down.active');
    if (up) return 'ascend';
    if (down) return 'descend';
  }
  return null;
}

export function TableSortFunction() {
  useRegisterFunction(
    tableSortInfo,
    async (params) => {
      const columnIndex = params.column_index as number;
      const order = params.order as 'ascend' | 'descend';

      const headers = document.querySelectorAll('.ant-table-thead > tr > th, .ant-table-cell');
      const header = headers[columnIndex] as HTMLElement | undefined;

      if (!header) {
        return { success: false, error: `未找到列索引 ${columnIndex} 对应的表头` };
      }

      const sorter = header.querySelector<HTMLElement>(
        '.ant-table-column-sorter, .ant-table-column-has-sorters',
      );
      if (!sorter) {
        return { success: false, error: `列 ${columnIndex} 不支持排序` };
      }

      const maxClicks = 3;
      for (let i = 0; i < maxClicks; i++) {
        const currentState = getSortState(header);
        if (currentState === order) break;
        sorter.click();
      }

      await waitForLoadingComplete();

      return { success: true, column_index: columnIndex, order };
    },
  );

  return null;
}
