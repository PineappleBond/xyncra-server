// 基础详情页 — 纯展示页面，无独立交互元素
// 所有交互使用通用 DOM 函数（click_element 等）

import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';

const profileBasicFunctions: FunctionEntry[] = [];

export function ProfileBasicFunctions() {
  useRegisterFunctions(profileBasicFunctions);
  return null;
}
