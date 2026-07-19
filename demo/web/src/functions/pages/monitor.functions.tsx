// 监控仪表盘页面 — 纯展示页面，无独立交互元素
// 所有交互使用通用 DOM 函数（click_element 等）

import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';

const monitorFunctions: FunctionEntry[] = [];

export function MonitorFunctions() {
  useRegisterFunctions(monitorFunctions);
  return null;
}
