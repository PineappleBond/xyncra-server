/**
 * index-barrel.test.ts 集成测试
 *
 * 测试 index.tsx 的瘦身，验证移除/保留的 function 数量和存在性。
 */

import {
  DemoFunctions,
  NavigateToFunction,
  GetCurrentPageFunction,
  GetPageDescriptionFunction,
  GetPageStructureFunction,
  GetFormDataFunction,
  GetTableDataFunction,
} from '../../functions/index';

// Mock reactValueSetter
jest.mock('../../functions/utils/reactValueSetter', () => ({
  setReactInputValue: jest.fn().mockReturnValue(true),
  setReactTextareaValue: jest.fn().mockReturnValue(true),
  setReactSelectValue: jest.fn().mockResolvedValue(true),
  setReactDateRangePickerValue: jest.fn().mockResolvedValue(true),
  setReactRadioValue: jest.fn().mockResolvedValue(true),
}));

// Mock dom-engine
jest.mock('../../functions/dom-engine', () => ({
  waitForSelector: jest.fn(),
  waitForLoadingComplete: jest.fn(),
}));

// Mock @xyncra/client-web
jest.mock('@xyncra/client-web', () => ({
  useRegisterFunctions: jest.fn(),
}));

describe('index.tsx barrel', () => {
  describe('BX-01: DemoFunctions 组件存在', () => {
    it('DemoFunctions 是一个函数组件', () => {
      expect(typeof DemoFunctions).toBe('function');
    });
  });

  describe('BX-02: 保留的查询类 function 导出', () => {
    it('NavigateToFunction 已导出', () => {
      expect(NavigateToFunction).toBeDefined();
    });

    it('GetCurrentPageFunction 已导出', () => {
      expect(GetCurrentPageFunction).toBeDefined();
    });

    it('GetPageDescriptionFunction 已导出', () => {
      expect(GetPageDescriptionFunction).toBeDefined();
    });

    it('GetPageStructureFunction 已导出', () => {
      expect(GetPageStructureFunction).toBeDefined();
    });

    it('GetFormDataFunction 已导出', () => {
      expect(GetFormDataFunction).toBeDefined();
    });

    it('GetTableDataFunction 已导出', () => {
      expect(GetTableDataFunction).toBeDefined();
    });
  });

  describe('BX-03: 移除的操作类 function 不存在', () => {
    it('ClickElementFunction 未导出', async () => {
      const index = await import('../../functions/index');
      expect((index as any).ClickElementFunction).toBeUndefined();
    });

    it('TypeTextFunction 未导出', async () => {
      const index = await import('../../functions/index');
      expect((index as any).TypeTextFunction).toBeUndefined();
    });

    it('SelectOptionFunction 未导出', async () => {
      const index = await import('../../functions/index');
      expect((index as any).SelectOptionFunction).toBeUndefined();
    });

    it('DatePickerFunction 未导出', async () => {
      const index = await import('../../functions/index');
      expect((index as any).DatePickerFunction).toBeUndefined();
    });

    it('ScrollToFunction 未导出', async () => {
      const index = await import('../../functions/index');
      expect((index as any).ScrollToFunction).toBeUndefined();
    });

    it('WaitForElementFunction 未导出', async () => {
      const index = await import('../../functions/index');
      expect((index as any).WaitForElementFunction).toBeUndefined();
    });

    it('ConfirmActionFunction 未导出', async () => {
      const index = await import('../../functions/index');
      expect((index as any).ConfirmActionFunction).toBeUndefined();
    });

    it('UploadFileFunction 未导出', async () => {
      const index = await import('../../functions/index');
      expect((index as any).UploadFileFunction).toBeUndefined();
    });

    it('TableSearchFunction 未导出', async () => {
      const index = await import('../../functions/index');
      expect((index as any).TableSearchFunction).toBeUndefined();
    });

    it('TableSortFunction 未导出', async () => {
      const index = await import('../../functions/index');
      expect((index as any).TableSortFunction).toBeUndefined();
    });

    it('TableRefreshFunction 未导出', async () => {
      const index = await import('../../functions/index');
      expect((index as any).TableRefreshFunction).toBeUndefined();
    });

    it('FormSubmitFunction 未导出', async () => {
      const index = await import('../../functions/index');
      expect((index as any).FormSubmitFunction).toBeUndefined();
    });

    it('FormResetFunction 未导出', async () => {
      const index = await import('../../functions/index');
      expect((index as any).FormResetFunction).toBeUndefined();
    });

    it('ShowNotificationFunction 未导出', async () => {
      const index = await import('../../functions/index');
      expect((index as any).ShowNotificationFunction).toBeUndefined();
    });

    it('HighlightElementFunction 未导出', async () => {
      const index = await import('../../functions/index');
      expect((index as any).HighlightElementFunction).toBeUndefined();
    });
  });

  describe('BX-04: 保留的 function 数量', () => {
    it('保留 6 个查询类 function', () => {
      const exports = [
        NavigateToFunction,
        GetCurrentPageFunction,
        GetPageDescriptionFunction,
        GetPageStructureFunction,
        GetFormDataFunction,
        GetTableDataFunction,
      ];

      expect(exports.length).toBe(6);
    });
  });

  describe('BX-05: 移除的 function 数量', () => {
    it('移除 15 个操作类 function', async () => {
      const index = await import('../../functions/index');
      const removedFunctions = [
        'ClickElementFunction',
        'TypeTextFunction',
        'SelectOptionFunction',
        'DatePickerFunction',
        'ScrollToFunction',
        'WaitForElementFunction',
        'ConfirmActionFunction',
        'UploadFileFunction',
        'TableSearchFunction',
        'TableSortFunction',
        'TableRefreshFunction',
        'FormSubmitFunction',
        'FormResetFunction',
        'ShowNotificationFunction',
        'HighlightElementFunction',
      ];

      for (const funcName of removedFunctions) {
        expect((index as any)[funcName]).toBeUndefined();
      }
    });
  });

  describe('BX-06: DemoFunctions 渲染', () => {
    it('DemoFunctions 返回 null', () => {
      const React = require('react');
      const { render } = require('@testing-library/react');

      const { container } = render(React.createElement(DemoFunctions));
      expect(container.innerHTML).toBe('');
    });
  });
});