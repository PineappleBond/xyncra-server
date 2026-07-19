import { act, renderHook } from '@testing-library/react';
import React from 'react';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { useMessages } from '../../hooks/useMessages';
import type { UpdateHandlerEventMap } from '../../internal/EventEmitter';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';
import { createMockDBMessage, resetTestCounters } from '../test-utils';

describe('useMessages', () => {
  let mockClient: any;
  let emitter: TypedEventEmitter<UpdateHandlerEventMap>;

  beforeEach(() => {
    resetTestCounters();
    emitter = new TypedEventEmitter<UpdateHandlerEventMap>();
    mockClient = {
      getMessages: jest.fn().mockResolvedValue({ messages: [] }),
      sendMessage: jest.fn().mockResolvedValue({}),
    };
  });

  function createWrapper(_convId: string | null = 'conv-1') {
    const contextValue: XyncraContextValue = {
      client: mockClient,
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

  it('should start with empty messages when no conversation', async () => {
    const { result } = renderHook(() => useMessages({ conversationId: null }), {
      wrapper: createWrapper(null),
    });

    expect(result.current.messages).toEqual([]);
    expect(result.current.loading).toBe(false);
  });

  it('should load messages for a conversation', async () => {
    const dbMsg = createMockDBMessage({ id: 'msg-1', content: 'Hello' });
    mockClient.getMessages.mockResolvedValue({ messages: [dbMsg] });

    const { result } = renderHook(
      () => useMessages({ conversationId: 'conv-1' }),
      { wrapper: createWrapper() },
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(result.current.loading).toBe(false);
    expect(result.current.messages).toHaveLength(1);
    expect(result.current.messages[0].id).toBe('msg-1');
  });

  it('should handle getMessages error', async () => {
    mockClient.getMessages.mockRejectedValue(new Error('Load failed'));

    const { result } = renderHook(
      () => useMessages({ conversationId: 'conv-1' }),
      { wrapper: createWrapper() },
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(result.current.loading).toBe(false);
    expect(result.current.error?.message).toBe('Load failed');
  });

  it('should add message on event for current conversation', async () => {
    const { result } = renderHook(
      () => useMessages({ conversationId: 'conv-1' }),
      { wrapper: createWrapper() },
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    act(() => {
      emitter.emit('message:added', {
        message: {
          id: 'msg-new',
          conversationId: 'conv-1',
          senderId: 'user1',
          content: 'New msg',
          clientMessageId: 'client-1',
          createdAt: new Date().toISOString(),
        },
      });
    });

    expect(result.current.messages).toHaveLength(1);
    expect(result.current.messages[0].id).toBe('msg-new');
  });

  it('should ignore messages for other conversations', async () => {
    const { result } = renderHook(
      () => useMessages({ conversationId: 'conv-1' }),
      { wrapper: createWrapper() },
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    act(() => {
      emitter.emit('message:added', {
        message: {
          id: 'msg-other',
          conversationId: 'conv-2',
          senderId: 'user1',
          content: 'Other conv',
          clientMessageId: 'client-2',
          createdAt: new Date().toISOString(),
        },
      });
    });

    expect(result.current.messages).toHaveLength(0);
  });

  it('should not add duplicate messages', async () => {
    const dbMsg = createMockDBMessage({ id: 'msg-1' });
    mockClient.getMessages.mockResolvedValue({ messages: [dbMsg] });

    const { result } = renderHook(
      () => useMessages({ conversationId: 'conv-1' }),
      { wrapper: createWrapper() },
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    act(() => {
      emitter.emit('message:added', {
        message: {
          id: 'msg-1',
          conversationId: 'conv-1',
          senderId: 'user1',
          content: 'Duplicate',
          clientMessageId: 'client-1',
          createdAt: new Date().toISOString(),
        },
      });
    });

    expect(result.current.messages).toHaveLength(1);
  });

  it('should update message on event', async () => {
    const dbMsg = createMockDBMessage({ id: 'msg-1', content: 'Original' });
    mockClient.getMessages.mockResolvedValue({ messages: [dbMsg] });

    const { result } = renderHook(
      () => useMessages({ conversationId: 'conv-1' }),
      { wrapper: createWrapper() },
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    act(() => {
      emitter.emit('message:updated', {
        message: {
          id: 'msg-1',
          conversationId: 'conv-1',
          senderId: 'user1',
          content: 'Updated',
          clientMessageId: 'client-1',
          createdAt: new Date().toISOString(),
        },
      });
    });

    expect(result.current.messages[0].content).toBe('Updated');
  });

  it('should remove message on event', async () => {
    const dbMsg = createMockDBMessage({ id: 'msg-1' });
    mockClient.getMessages.mockResolvedValue({ messages: [dbMsg] });

    const { result } = renderHook(
      () => useMessages({ conversationId: 'conv-1' }),
      { wrapper: createWrapper() },
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    act(() => {
      emitter.emit('message:removed', {
        messageId: 'msg-1',
        conversationId: 'conv-1',
      });
    });

    expect(result.current.messages).toHaveLength(0);
  });

  it('should send a message', async () => {
    const { result } = renderHook(
      () => useMessages({ conversationId: 'conv-1' }),
      { wrapper: createWrapper() },
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    await act(async () => {
      await result.current.send('Hello');
    });

    expect(mockClient.sendMessage).toHaveBeenCalledWith('conv-1', 'Hello');
  });

  it('should throw on send when no conversation', async () => {
    const { result } = renderHook(() => useMessages({ conversationId: null }), {
      wrapper: createWrapper(null),
    });

    await expect(result.current.send('Hello')).rejects.toThrow(
      'client not initialized or no conversation selected',
    );
  });

  it('should refresh messages', async () => {
    const dbMsg1 = createMockDBMessage({ id: 'msg-1' });
    mockClient.getMessages.mockResolvedValue({ messages: [dbMsg1] });

    const { result } = renderHook(
      () => useMessages({ conversationId: 'conv-1' }),
      { wrapper: createWrapper() },
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    const dbMsg2 = createMockDBMessage({ id: 'msg-2' });
    mockClient.getMessages.mockResolvedValue({ messages: [dbMsg2] });

    await act(async () => {
      await result.current.refresh();
    });

    expect(result.current.messages).toHaveLength(1);
    expect(result.current.messages[0].id).toBe('msg-2');
  });
});
