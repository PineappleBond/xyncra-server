import { renderHook } from '@testing-library/react';
import type { FunctionInfo } from '@xyncra/protocol';
import React from 'react';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { useRegisterFunction } from '../../hooks/useRegisterFunction';
import type { UpdateHandlerEventMap } from '../../internal/EventEmitter';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

describe('useRegisterFunction', () => {
  let registerFunction: jest.Mock;
  let unregisterFunction: jest.Mock;

  const testInfo: FunctionInfo = {
    name: 'get_weather',
    description: 'Get weather',
    parameters: {},
  };
  const testHandler = jest.fn();

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

  it('should register function on mount', () => {
    renderHook(() => useRegisterFunction(testInfo, testHandler), {
      wrapper: createWrapper(),
    });

    expect(registerFunction).toHaveBeenCalledWith(testInfo, testHandler);
  });

  it('should unregister function on unmount', () => {
    const { unmount } = renderHook(
      () => useRegisterFunction(testInfo, testHandler),
      { wrapper: createWrapper() },
    );

    unmount();

    expect(unregisterFunction).toHaveBeenCalledWith('get_weather');
  });
});
