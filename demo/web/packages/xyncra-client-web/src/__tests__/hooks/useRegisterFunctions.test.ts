import { renderHook } from '@testing-library/react';
import React from 'react';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import type { FunctionEntry } from '../../hooks/useRegisterFunctions';
import { useRegisterFunctions } from '../../hooks/useRegisterFunctions';
import type { UpdateHandlerEventMap } from '../../internal/EventEmitter';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

describe('useRegisterFunctions', () => {
  let registerFunction: jest.Mock;
  let unregisterFunction: jest.Mock;

  const handler1 = jest.fn();
  const handler2 = jest.fn();
  const functions: FunctionEntry[] = [
    {
      info: { name: 'fn1', description: 'First', parameters: {} },
      handler: handler1,
    },
    {
      info: { name: 'fn2', description: 'Second', parameters: {} },
      handler: handler2,
    },
  ];

  beforeEach(() => {
    registerFunction = jest.fn();
    unregisterFunction = jest.fn();
  });

  function createWrapper() {
    const contextValue: XyncraContextValue = {
      client: {} as any,
      connectionStatus: 'connected',
      deviceID: 'test-device',
      agentID: 'test-agent',
      functionRegistry: new FunctionRegistry(),
      eventEmitter: new TypedEventEmitter<UpdateHandlerEventMap>(),
      registerFunction,
      unregisterFunction,
    };
    return ({ children }: { children: React.ReactNode }) =>
      React.createElement(
        XyncraContext.Provider,
        { value: contextValue },
        children,
      );
  }

  it('should register all functions on mount', () => {
    renderHook(() => useRegisterFunctions(functions), {
      wrapper: createWrapper(),
    });

    expect(registerFunction).toHaveBeenCalledTimes(2);
    expect(registerFunction).toHaveBeenCalledWith(functions[0].info, handler1);
    expect(registerFunction).toHaveBeenCalledWith(functions[1].info, handler2);
  });

  it('should unregister all functions on unmount', () => {
    const { unmount } = renderHook(() => useRegisterFunctions(functions), {
      wrapper: createWrapper(),
    });

    unmount();

    expect(unregisterFunction).toHaveBeenCalledTimes(2);
    expect(unregisterFunction).toHaveBeenCalledWith('fn1');
    expect(unregisterFunction).toHaveBeenCalledWith('fn2');
  });
});
