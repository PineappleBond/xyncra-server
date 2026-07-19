import { renderHook } from '@testing-library/react';
import React from 'react';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { useXyncra } from '../../hooks/useXyncra';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

describe('useXyncra', () => {
  function createMockValue(): XyncraContextValue {
    return {
      client: {} as any,
      connectionStatus: 'connected',
      deviceID: 'test-device',
      agentID: 'test-agent',
      functionRegistry: new FunctionRegistry(),
      eventEmitter: new TypedEventEmitter(),
      registerFunction: jest.fn(),
      unregisterFunction: jest.fn(),
    };
  }

  it('should return context value when inside provider', () => {
    const mockValue = createMockValue();
    const wrapper = ({ children }: { children: React.ReactNode }) =>
      React.createElement(
        XyncraContext.Provider,
        { value: mockValue },
        children,
      );

    const { result } = renderHook(() => useXyncra(), { wrapper });

    expect(result.current.connectionStatus).toBe('connected');
    expect(result.current.deviceID).toBe('test-device');
    expect(result.current.agentID).toBe('test-agent');
  });

  it('should throw when used outside provider', () => {
    // Suppress console.error from the thrown error
    jest.spyOn(console, 'error').mockImplementation();

    expect(() => {
      renderHook(() => useXyncra());
    }).toThrow('useXyncra must be used within a <XyncraProvider>');

    jest.restoreAllMocks();
  });

  it('should expose function registry methods', () => {
    const mockValue = createMockValue();
    const wrapper = ({ children }: { children: React.ReactNode }) =>
      React.createElement(
        XyncraContext.Provider,
        { value: mockValue },
        children,
      );

    const { result } = renderHook(() => useXyncra(), { wrapper });
    expect(result.current.registerFunction).toBeDefined();
    expect(result.current.unregisterFunction).toBeDefined();
  });
});
