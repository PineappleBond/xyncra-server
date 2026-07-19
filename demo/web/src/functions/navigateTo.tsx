/**
 * navigateTo — Demo function that navigates the browser to a specified path.
 *
 * Registered via useRegisterFunction so the Xyncra server can invoke it
 * through the reverse-RPC mechanism.
 *
 * @module
 */

import { useRegisterFunction } from '@xyncra/client-web';
import { history } from '@umijs/max';
import type { FunctionInfo } from '@xyncra/protocol';

const navigateToInfo: FunctionInfo = {
  name: 'navigate_to',
  description: '导航到指定页面路径',
  parameters: {
    type: 'object',
    properties: {
      path: {
        type: 'string',
        description: '目标页面路径，例如 /dashboard 或 /user/login',
      },
    },
    required: ['path'],
  },
};

export function NavigateToFunction() {
  useRegisterFunction(navigateToInfo, async (params) => {
    const path = params.path as string;
    history.push(path);
    return { success: true, message: `已导航到 ${path}` };
  });

  return null;
}
