import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';
import { matchPageDescription } from './pageDescriptions';

const getPageDescriptionInfo: FunctionInfo = {
  name: 'get_page_description',
  description:
    '获取当前页面的业务语义描述：页面是什么、有哪些业务区块、每个区块关联哪些 pg_ 函数。调用具体 pg_ 函数前先调此函数建立页面认知',
  parameters: {
    type: 'object',
    properties: {},
  },
  tags: ['dom', 'generic', 'page-context'],
  timeout_ms: 10000,
};

export function GetPageDescriptionFunction() {
  useRegisterFunction(getPageDescriptionInfo, async () => {
    const desc = matchPageDescription(window.location.pathname);
    if (!desc) {
      return {
        success: true,
        data: {
          page_id: 'unknown',
          route: window.location.pathname,
          title: document.title,
          summary: '未知页面或纯展示页面，请使用通用 DOM 函数操作',
          business_goal: '',
          regions: [],
        },
      };
    }
    return { success: true, data: desc };
  });

  return null;
}
