/**
 * getCurrentPage — Demo function that returns information about the current page.
 *
 * Registered via useRegisterFunction so the Xyncra server can invoke it
 * through the reverse-RPC mechanism.
 *
 * @module
 */

import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';

const getCurrentPageInfo: FunctionInfo = {
  name: 'get_current_page',
  description: '获取当前页面的信息',
  parameters: {
    type: 'object',
    properties: {},
  },
};

export function GetCurrentPageFunction() {
  useRegisterFunction(getCurrentPageInfo, async () => {
    return {
      url: window.location.href,
      title: document.title,
      pathname: window.location.pathname,
    };
  });

  return null;
}
