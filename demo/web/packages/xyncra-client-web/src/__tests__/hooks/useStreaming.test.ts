import { act, renderHook } from '@testing-library/react';
import React from 'react';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { useStreaming } from '../../hooks/useStreaming';
import type { UpdateHandlerEventMap } from '../../internal/EventEmitter';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

describe('useStreaming', () => {
  let emitter: TypedEventEmitter<UpdateHandlerEventMap>;

  beforeEach(() => {
    emitter = new TypedEventEmitter<UpdateHandlerEventMap>();
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
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

  it('should start with empty streaming state', () => {
    const { result } = renderHook(() => useStreaming(), {
      wrapper: createWrapper(),
    });

    expect(result.current.streamingText).toBe('');
    expect(result.current.isStreaming).toBe(false);
    expect(result.current.currentStreamID).toBeNull();
  });

  it('should accumulate streaming text', async () => {
    const { result } = renderHook(() => useStreaming(), {
      wrapper: createWrapper(),
    });

    act(() => {
      emitter.emit('stream:text', {
        userId: 'agent1',
        conversationId: 'conv-1',
        streamId: 'stream-1',
        text: 'Hello',
      });
    });

    // Flush rAF
    await act(async () => {
      jest.runAllTimers();
    });

    expect(result.current.isStreaming).toBe(true);
    expect(result.current.currentStreamID).toBe('stream-1');
    expect(result.current.streamingText).toBe('Hello');
  });

  it('should accumulate multiple chunks', async () => {
    const { result } = renderHook(() => useStreaming(), {
      wrapper: createWrapper(),
    });

    act(() => {
      emitter.emit('stream:text', {
        userId: 'agent1',
        conversationId: 'conv-1',
        streamId: 'stream-1',
        text: 'Hello',
      });
    });

    act(() => {
      emitter.emit('stream:text', {
        userId: 'agent1',
        conversationId: 'conv-1',
        streamId: 'stream-1',
        text: ' World',
      });
    });

    await act(async () => {
      jest.runAllTimers();
    });

    expect(result.current.streamingText).toBe('Hello World');
  });

  it('should set isStreaming to false on stream:done', async () => {
    const { result } = renderHook(() => useStreaming(), {
      wrapper: createWrapper(),
    });

    act(() => {
      emitter.emit('stream:text', {
        userId: 'agent1',
        conversationId: 'conv-1',
        streamId: 'stream-1',
        text: 'Hello',
      });
    });

    act(() => {
      emitter.emit('stream:done', {
        userId: 'agent1',
        conversationId: 'conv-1',
        streamId: 'stream-1',
      });
    });

    expect(result.current.isStreaming).toBe(false);
  });

  it('should cleanup streaming state after delay', async () => {
    const { result } = renderHook(() => useStreaming(), {
      wrapper: createWrapper(),
    });

    act(() => {
      emitter.emit('stream:text', {
        userId: 'agent1',
        conversationId: 'conv-1',
        streamId: 'stream-1',
        text: 'Hello',
      });
    });

    act(() => {
      emitter.emit('stream:done', {
        userId: 'agent1',
        conversationId: 'conv-1',
        streamId: 'stream-1',
      });
    });

    // Advance past the cleanup delay (500ms)
    await act(async () => {
      jest.advanceTimersByTime(600);
    });

    expect(result.current.streamingText).toBe('');
    expect(result.current.currentStreamID).toBeNull();
    expect(result.current.isStreaming).toBe(false);
  });
});
