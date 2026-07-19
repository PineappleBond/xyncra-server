// 管理页 — 仅链接跳转，无独立交互元素
// 所有交互使用通用 DOM 函数（click_element 等）

import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';

const adminFunctions: FunctionEntry[] = [];

export function AdminFunctions() {
  useRegisterFunctions(adminFunctions);
  return null;
}
