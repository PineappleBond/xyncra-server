/**
 * showNotification — Demo function that displays an antd notification.
 *
 * Registered via useRegisterFunction so the Xyncra server can invoke it
 * through the reverse-RPC mechanism.
 *
 * @module
 */

import { useRegisterFunction } from '@xyncra/client-web';
import { notification } from 'antd';
import type { FunctionInfo } from '@xyncra/protocol';

const showNotificationInfo: FunctionInfo = {
  name: 'show_notification',
  description: '显示桌面通知消息',
  parameters: {
    type: 'object',
    properties: {
      type: {
        type: 'string',
        enum: ['success', 'error', 'warning', 'info'],
        description: '通知类型',
      },
      message: {
        type: 'string',
        description: '通知标题',
      },
      description: {
        type: 'string',
        description: '通知详细描述（可选）',
      },
    },
    required: ['type', 'message'],
  },
};

export function ShowNotificationFunction() {
  useRegisterFunction(
    showNotificationInfo,
    async (params) => {
      const type = params.type as 'success' | 'error' | 'warning' | 'info';
      const message = params.message as string;
      const description = params.description as string | undefined;
      notification[type]({
        message,
        description,
      });
      return { success: true };
    },
  );

  return null;
}
