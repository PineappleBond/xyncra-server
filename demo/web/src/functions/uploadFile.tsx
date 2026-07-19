import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';
import { waitForSelector } from './dom-engine';

const uploadFileInfo: FunctionInfo = {
  name: 'upload_file',
  description: '通过 antd Upload 组件上传文件',
  parameters: {
    type: 'object',
    properties: {
      selector: { type: 'string', description: 'Upload 按钮/区域选择器' },
      file_name: { type: 'string', description: '文件名（含后缀）' },
      file_content: { type: 'string', description: 'Base64 编码的文件内容' },
      mime_type: { type: 'string', description: 'MIME 类型（可选，默认 application/octet-stream）' },
    },
    required: ['selector', 'file_name', 'file_content'],
  },
  tags: ['dom', 'generic'],
  timeout_ms: 120000,
};

export function UploadFileFunction() {
  useRegisterFunction(
    uploadFileInfo,
    async (params) => {
      const selector = params.selector as string;
      const fileName = params.file_name as string;
      const fileContent = params.file_content as string;
      const mimeType = (params.mime_type as string) || 'application/octet-stream';

      const container = await waitForSelector(selector, 5000);
      if (!container) {
        return { success: false, error: `Upload 区域未找到: ${selector}` };
      }

      const fileInput = container.querySelector<HTMLInputElement>('input[type="file"]');
      if (!fileInput) {
        return { success: false, error: '未找到文件上传 input' };
      }

      const byteString = atob(fileContent);
      const ab = new ArrayBuffer(byteString.length);
      const ia = new Uint8Array(ab);
      for (let i = 0; i < byteString.length; i++) {
        ia[i] = byteString.charCodeAt(i);
      }
      const file = new File([ab], fileName, { type: mimeType });
      const dt = new DataTransfer();
      dt.items.add(file);

      fileInput.files = dt.files;
      fileInput.dispatchEvent(new Event('change', { bubbles: true }));

      return { success: true, file_name: fileName };
    },
  );

  return null;
}
