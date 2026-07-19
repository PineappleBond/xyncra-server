import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';

const getTableDataInfo: FunctionInfo = {
  name: 'get_table_data',
  description: '获取表格的列定义、当前页行数据和分页信息',
  parameters: {
    type: 'object',
    properties: {
      table_selector: {
        type: 'string',
        description: '表格 CSS 选择器。不传则自动检测',
      },
      max_rows: {
        type: 'number',
        description: '最多返回行数，默认 50',
      },
    },
  },
  tags: ['dom', 'generic'],
  timeout_ms: 10000,
};

export function GetTableDataFunction() {
  useRegisterFunction(
    getTableDataInfo,
    async (params) => {
      const tableSelector = params.table_selector as string | undefined;
      const maxRows = (params.max_rows as number) ?? 50;

      let table: Element | null = null;
      if (tableSelector) {
        table = document.querySelector(tableSelector);
      } else {
        table = document.querySelector('.ant-table-wrapper');
      }

      if (!table) {
        return { success: false, error: '未找到表格' };
      }

      const columns: string[] = [];
      const headerCells = table.querySelectorAll('.ant-table-thead > tr > th');
      headerCells.forEach((th) => {
        const text = th.textContent?.trim();
        if (text) columns.push(text);
      });

      const rows: string[][] = [];
      const rowElements = table.querySelectorAll('.ant-table-tbody > tr.ant-table-row');
      let count = 0;
      rowElements.forEach((row) => {
        if (count >= maxRows) return;
        const rowData: string[] = [];
        row.querySelectorAll('td, .ant-table-cell').forEach((cell) => {
          rowData.push(cell.textContent?.trim() || '');
        });
        rows.push(rowData);
        count++;
      });

      let pagination: { total: number; page: number; page_size: number } | undefined;

      const paginationEl = table.querySelector('.ant-pagination');
      if (paginationEl) {
        const totalEl = paginationEl.querySelector('.ant-pagination-total-text');
        let total = 0;
        if (totalEl) {
          const match = totalEl.textContent?.match(/\d+/g);
          if (match) total = parseInt(match[match.length - 1], 10);
        }

        const activeItem = paginationEl.querySelector('.ant-pagination-item-active');
        const page = activeItem ? parseInt(activeItem.textContent || '1', 10) : 1;

        const pageSizeEl = paginationEl.querySelector('.ant-select-selection-item');
        let pageSize = 10;
        if (pageSizeEl) {
          const ps = parseInt(pageSizeEl.textContent || '', 10);
          if (!isNaN(ps)) pageSize = ps;
        }

        pagination = { total, page, page_size: pageSize };
      }

      return { success: true, columns, rows, pagination };
    },
  );

  return null;
}
