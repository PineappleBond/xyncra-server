import { useRegisterFunction } from '@xyncra/client-web';
import type { FunctionInfo } from '@xyncra/protocol';
import { waitForLoadingComplete } from './dom-engine';

const scrollToInfo: FunctionInfo = {
  name: 'scroll_to',
  description: '滚动页面到指定元素位置',
  parameters: {
    type: 'object',
    properties: {
      selector: { type: 'string', description: 'CSS 选择器' },
      behavior: { type: 'string', enum: ['smooth', 'auto'], description: '滚动行为，默认 smooth' },
    },
    required: ['selector'],
  },
  tags: ['dom', 'generic'],
};

export function ScrollToFunction() {
  useRegisterFunction(
    scrollToInfo,
    async (params) => {
      const selector = params.selector as string;
      const behavior = (params.behavior as string) || 'smooth';

      const el = document.querySelector(selector);
      if (!el) {
        return { success: false, error: '未找到元素' };
      }

      el.scrollIntoView({ behavior: behavior as ScrollBehavior, block: 'center' });

      let parent = el.parentElement;
      while (parent) {
        const style = getComputedStyle(parent);
        if (style.overflow === 'auto' || style.overflow === 'scroll') {
          const elRect = el.getBoundingClientRect()
          const parentRect = parent.getBoundingClientRect()
          parent.scrollTop = elRect.top - parentRect.top + parent.scrollTop
          break;
        }
        parent = parent.parentElement;
      }

      await waitForLoadingComplete()

      return { success: true };
    },
  );

  return null;
}
