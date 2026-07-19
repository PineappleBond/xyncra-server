import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';
import { waitForSelector } from './dom-engine';

const confirmActionInfo: FunctionInfo = {
  name: 'confirm_action',
  description: '操作 antd Modal.confirm 确认弹窗（确认或取消）',
  parameters: {
    type: 'object',
    properties: {
      action: {
        type: 'string',
        enum: ['confirm', 'cancel'],
        description: 'confirm=确认按钮，cancel=取消按钮',
      },
    },
    required: ['action'],
  },
  tags: ['dom', 'generic'],
  timeout_ms: 10000,
};

export function ConfirmActionFunction() {
  useRegisterFunction(
    confirmActionInfo,
    async (params) => {
      const action = params.action as string;

      const modal = document.querySelector('.ant-modal-confirm');
      if (!modal) {
        return { success: false, error: '未找到确认弹窗' };
      }

      if (action === 'confirm') {
        const confirmBtn = modal.querySelector<HTMLElement>('.ant-modal-confirm-btns .ant-btn-primary');
        if (!confirmBtn) return { success: false, error: '未找到确认按钮' };
        confirmBtn.click();
      } else if (action === 'cancel') {
        const cancelBtn = modal.querySelector<HTMLElement>('.ant-modal-confirm-btns .ant-btn:not(.ant-btn-primary)');
        if (!cancelBtn) return { success: false, error: '未找到取消按钮' };
        cancelBtn.click();
      }

      const disappearDeadline = Date.now() + 5000
      await new Promise<void>((resolve) => {
        const poll = () => {
          if (!document.querySelector('.ant-modal-confirm')) { resolve(); return }
          if (Date.now() >= disappearDeadline) { resolve(); return }
          requestAnimationFrame(poll)
        }
        requestAnimationFrame(poll)
      })

      return { success: true };
    },
  );

  return null;
}
