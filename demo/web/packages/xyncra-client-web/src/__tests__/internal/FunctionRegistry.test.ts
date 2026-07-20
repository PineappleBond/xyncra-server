import type { FunctionInfo } from '@xyncra/protocol';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

describe('FunctionRegistry', () => {
  let registry: FunctionRegistry;
  const testInfo: FunctionInfo = {
    name: 'test_fn',
    description: 'A test function',
    parameters: {
      type: 'object',
      properties: { x: { type: 'string' } },
    },
  };

  beforeEach(() => {
    registry = new FunctionRegistry();
  });

  describe('register', () => {
    it('should register a function and retrieve its handler', () => {
      const handler = jest.fn();
      registry.register(testInfo, handler);
      expect(registry.getHandler('test_fn')).toBe(handler);
    });

    it('should store function info', () => {
      registry.register(testInfo, jest.fn());
      expect(registry.getFunctionInfos()).toContainEqual(testInfo);
    });

    it('should increment version on register', () => {
      expect(registry.getVersion()).toBe(0);
      registry.register(testInfo, jest.fn());
      expect(registry.getVersion()).toBe(1);
    });

    it('should throw if function name is empty', () => {
      expect(() =>
        registry.register(
          { name: '', description: '', parameters: {} },
          jest.fn(),
        ),
      ).toThrow('Function name must be non-empty');
    });

    it('should replace an existing function with the same name', () => {
      const handler1 = jest.fn();
      const handler2 = jest.fn();
      registry.register(testInfo, handler1);
      registry.register({ ...testInfo, description: 'Updated' }, handler2);
      expect(registry.getHandler('test_fn')).toBe(handler2);
      expect(registry.size).toBe(1);
    });

    it('should notify onChange listeners', () => {
      const changeCallback = jest.fn();
      registry.onChange(changeCallback);
      registry.register(testInfo, jest.fn());
      expect(changeCallback).toHaveBeenCalledTimes(1);
    });
  });

  describe('unregister', () => {
    it('should remove a registered function', () => {
      registry.register(testInfo, jest.fn());
      registry.unregister('test_fn');
      expect(registry.getHandler('test_fn')).toBeUndefined();
      expect(registry.size).toBe(0);
    });

    it('should increment version on unregister', () => {
      registry.register(testInfo, jest.fn());
      expect(registry.getVersion()).toBe(1);
      registry.unregister('test_fn');
      expect(registry.getVersion()).toBe(2);
    });

    it('should be a no-op for non-existent functions', () => {
      const version = registry.getVersion();
      registry.unregister('nonexistent');
      expect(registry.getVersion()).toBe(version);
    });

    it('should notify onChange listeners only when a function is actually removed', () => {
      const changeCallback = jest.fn();
      registry.onChange(changeCallback);
      registry.unregister('nonexistent');
      expect(changeCallback).not.toHaveBeenCalled();
      registry.register(testInfo, jest.fn());
      expect(changeCallback).toHaveBeenCalledTimes(1);
    });
  });

  describe('getFunctionInfos', () => {
    it('should return empty array when no functions registered', () => {
      expect(registry.getFunctionInfos()).toEqual([]);
    });

    it('should return all registered function infos', () => {
      const info1: FunctionInfo = {
        name: 'fn1',
        description: 'First',
        parameters: {},
      };
      const info2: FunctionInfo = {
        name: 'fn2',
        description: 'Second',
        parameters: {},
      };
      registry.register(info1, jest.fn());
      registry.register(info2, jest.fn());
      const infos = registry.getFunctionInfos();
      expect(infos).toHaveLength(2);
      expect(infos).toContainEqual(info1);
      expect(infos).toContainEqual(info2);
    });

    it('should return a new array each time', () => {
      registry.register(testInfo, jest.fn());
      const infos1 = registry.getFunctionInfos();
      const infos2 = registry.getFunctionInfos();
      expect(infos1).not.toBe(infos2);
    });
  });

  describe('onChange', () => {
    it('should return an unsubscribe function', () => {
      const callback = jest.fn();
      const unsub = registry.onChange(callback);
      registry.register(testInfo, jest.fn());
      expect(callback).toHaveBeenCalledTimes(1);
      unsub();
      registry.unregister('test_fn');
      expect(callback).toHaveBeenCalledTimes(1);
    });

    it('should swallow listener errors', () => {
      const badCallback = jest.fn(() => {
        throw new Error('bad');
      });
      const goodCallback = jest.fn();
      registry.onChange(badCallback);
      registry.onChange(goodCallback);
      registry.register(testInfo, jest.fn());
      expect(goodCallback).toHaveBeenCalled();
    });
  });

  describe('createRequestHandler', () => {
    it('should return undefined for non-existent functions', () => {
      expect(registry.createRequestHandler('nonexistent')).toBeUndefined();
    });

    it('should return a handler for registered functions', async () => {
      const handler = jest.fn().mockResolvedValue('result');
      registry.register(testInfo, handler);
      const reqHandler = registry.createRequestHandler('test_fn');
      expect(reqHandler).toBeDefined();
      const result = await reqHandler?.({ params: { x: 'hello' } } as any);
      expect(handler).toHaveBeenCalledWith({ x: 'hello' });
      expect(result).toBe('result');
    });

    it('should handle missing params gracefully', async () => {
      const handler = jest.fn().mockResolvedValue('ok');
      registry.register(testInfo, handler);
      const reqHandler = registry.createRequestHandler('test_fn');
      await reqHandler?.({} as any);
      expect(handler).toHaveBeenCalledWith({});
    });
  });

  describe('batchUnregister', () => {
    it('should remove multiple functions at once', () => {
      const info1: FunctionInfo = { name: 'fn1', description: 'First', parameters: {} };
      const info2: FunctionInfo = { name: 'fn2', description: 'Second', parameters: {} };
      const info3: FunctionInfo = { name: 'fn3', description: 'Third', parameters: {} };
      registry.register(info1, jest.fn());
      registry.register(info2, jest.fn());
      registry.register(info3, jest.fn());
      expect(registry.size).toBe(3);

      registry.batchUnregister(['fn1', 'fn3']);
      expect(registry.size).toBe(1);
      expect(registry.getHandler('fn1')).toBeUndefined();
      expect(registry.getHandler('fn2')).toBeDefined();
      expect(registry.getHandler('fn3')).toBeUndefined();
    });

    it('should only increment version once', () => {
      const info1: FunctionInfo = { name: 'fn1', description: 'First', parameters: {} };
      const info2: FunctionInfo = { name: 'fn2', description: 'Second', parameters: {} };
      registry.register(info1, jest.fn());
      registry.register(info2, jest.fn());
      const v = registry.getVersion();
      registry.batchUnregister(['fn1', 'fn2']);
      expect(registry.getVersion()).toBe(v + 1);
    });

    it('should only trigger onChange once', () => {
      const info1: FunctionInfo = { name: 'fn1', description: 'First', parameters: {} };
      const info2: FunctionInfo = { name: 'fn2', description: 'Second', parameters: {} };
      registry.register(info1, jest.fn());
      registry.register(info2, jest.fn());
      const changeCallback = jest.fn();
      registry.onChange(changeCallback);
      registry.batchUnregister(['fn1', 'fn2']);
      expect(changeCallback).toHaveBeenCalledTimes(1);
    });

    it('should be a no-op when no functions match', () => {
      registry.register(testInfo, jest.fn());
      const v = registry.getVersion();
      const changeCallback = jest.fn();
      registry.onChange(changeCallback);
      registry.batchUnregister(['nonexistent']);
      expect(registry.getVersion()).toBe(v);
      expect(changeCallback).not.toHaveBeenCalled();
    });

    it('should handle empty array', () => {
      registry.register(testInfo, jest.fn());
      const v = registry.getVersion();
      registry.batchUnregister([]);
      expect(registry.getVersion()).toBe(v);
    });
  });

  describe('clear', () => {
    it('should remove all functions', () => {
      registry.register(testInfo, jest.fn());
      registry.register(
        { name: 'fn2', description: 'Second', parameters: {} },
        jest.fn(),
      );
      registry.clear();
      expect(registry.size).toBe(0);
      expect(registry.getFunctionInfos()).toEqual([]);
    });

    it('should increment version', () => {
      registry.register(testInfo, jest.fn());
      const v = registry.getVersion();
      registry.clear();
      expect(registry.getVersion()).toBe(v + 1);
    });

    it('should not increment version when already empty', () => {
      const v = registry.getVersion();
      registry.clear();
      expect(registry.getVersion()).toBe(v);
    });
  });

  describe('size', () => {
    it('should return 0 for empty registry', () => {
      expect(registry.size).toBe(0);
    });

    it('should return the correct count', () => {
      registry.register(testInfo, jest.fn());
      expect(registry.size).toBe(1);
    });
  });
});
