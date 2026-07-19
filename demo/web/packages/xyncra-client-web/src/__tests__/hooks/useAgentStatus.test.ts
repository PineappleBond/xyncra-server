import { act, renderHook } from '@testing-library/react';
import React from 'react';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { useAgentStatus } from '../../hooks/useAgentStatus';
import type { UpdateHandlerEventMap } from '../../internal/EventEmitter';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

describe('useAgentStatus', () => {
  let emitter: TypedEventEmitter<UpdateHandlerEventMap>;

  beforeEach(() => {
    emitter = new TypedEventEmitter<UpdateHandlerEventMap>();
  });

  function createWrapper() {
    const contextValue: XyncraContextValue = {
      client: {} as any,
      connectionStatus: 'connected',
      deviceID: 'test-device',
      agentID: 'test-agent',
      functionRegistry: new FunctionRegistry(),
      eventEmitter: emitter,
      registerFunction: jest.fn(),
      unregisterFunction: jest.fn(),
    };
    return ({ children }: { children: React.ReactNode }) =>
      React.createElement(
        XyncraContext.Provider,
        { value: contextValue },
        children,
      );
  }

  it('should start with null status and isTyping false', () => {
    const { result } = renderHook(() => useAgentStatus(), {
      wrapper: createWrapper(),
    });

    expect(result.current.status).toBeNull();
    expect(result.current.isTyping).toBe(false);
  });

  it('should update on agent:status event', () => {
    const { result } = renderHook(() => useAgentStatus(), {
      wrapper: createWrapper(),
    });

    act(() => {
      emitter.emit('agent:status', {
        userId: 'agent1',
        conversationId: 'conv-1',
        status: 'thinking',
      });
    });

    expect(result.current.status).toEqual({
      userId: 'agent1',
      conversationId: 'conv-1',
      status: 'thinking',
    });
    expect(result.current.isTyping).toBe(true);
  });

  it('should set isTyping true for thinking/generating/tool_calling', () => {
    const { result } = renderHook(() => useAgentStatus(), {
      wrapper: createWrapper(),
    });

    for (const status of ['thinking', 'generating', 'tool_calling']) {
      act(() => {
        emitter.emit('agent:status', {
          userId: 'agent1',
          conversationId: 'conv-1',
          status,
        });
      });
      expect(result.current.isTyping).toBe(true);
    }
  });

  it('should set isTyping false for idle', () => {
    const { result } = renderHook(() => useAgentStatus(), {
      wrapper: createWrapper(),
    });

    act(() => {
      emitter.emit('agent:status', {
        userId: 'agent1',
        conversationId: 'conv-1',
        status: 'idle',
      });
    });

    expect(result.current.isTyping).toBe(false);
  });

  it('should update on agent:thinking event', () => {
    const { result } = renderHook(() => useAgentStatus(), {
      wrapper: createWrapper(),
    });

    act(() => {
      emitter.emit('agent:thinking', {
        userId: 'agent1',
        conversationId: 'conv-1',
        isTyping: true,
        isAgent: true,
      });
    });

    expect(result.current.status?.status).toBe('thinking');
    expect(result.current.isTyping).toBe(true);
  });

  it('should set idle from agent:thinking with isTyping false', () => {
    const { result } = renderHook(() => useAgentStatus(), {
      wrapper: createWrapper(),
    });

    act(() => {
      emitter.emit('agent:thinking', {
        userId: 'agent1',
        conversationId: 'conv-1',
        isTyping: false,
        isAgent: true,
      });
    });

    expect(result.current.status?.status).toBe('idle');
    expect(result.current.isTyping).toBe(false);
  });
});
