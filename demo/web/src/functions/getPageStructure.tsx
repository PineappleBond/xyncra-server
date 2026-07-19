import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';
import { extractInteractiveElements } from './dom-engine';

const getPageStructureInfo: FunctionInfo = {
  name: 'get_page_structure',
  description: '获取当前页面所有可交互元素的结构化摘要',
  parameters: {
    type: 'object',
    properties: {
      region_type: {
        type: 'string',
        enum: ['filter-bar', 'table', 'form', 'card-list', 'drawer', 'modal'],
        description: '可选过滤：只返回指定类型的区域',
      },
    },
  },
  tags: ['dom', 'generic'],
  timeout_ms: 10000,
};

export function GetPageStructureFunction() {
  useRegisterFunction(
    getPageStructureInfo,
    async (params) => {
      const regionType = params.region_type as string | undefined;
      return { success: true, data: extractInteractiveElements(regionType) };
    },
  );

  return null;
}
