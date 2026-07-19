import { act, renderHook } from '@testing-library/react';
import React from 'react';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { useConversations } from '../../hooks/useConversations';
import type { UpdateHandlerEventMap } from '../../internal/EventEmitter';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';
import { createMockDBConversation, resetTestCounters } from '../test-utils';

describe('useConversations', () => {
  let mockClient: any;
  let emitter: TypedEventEmitter<UpdateHandlerEventMap>;

  beforeEach(() => {
    resetTestCounters();
    emitter = new TypedEventEmitter<UpdateHandlerEventMap>();
    mockClient = {
      listConversations: jest.fn().mockResolvedValue({ conversations: [] }),
      createConversation: jest.fn().mockResolvedValue({
        conversation: createMockDBConversation(),
      }),
      deleteConversation: jest.fn().mockResolvedValue(undefined),
    };
  });

  function createWrapper() {
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

  it('should start with loading true and empty conversations', () => {
    const { result } = renderHook(() => useConversations(), {
      wrapper: createWrapper(),
    });

    expect(result.current.loading).toBe(true);
    expect(result.current.conversations).toEqual([]);
    expect(result.current.error).toBeNull();
  });

  it('should load conversations from client', async () => {
    const dbConv = createMockDBConversation({ id: 'conv-1', title: 'Test' });
    mockClient.listConversations.mockResolvedValue({
      conversations: [dbConv],
    });

    const { result } = renderHook(() => useConversations(), {
      wrapper: createWrapper(),
    });

    // Wait for async loading
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(result.current.loading).toBe(false);
    expect(result.current.conversations).toHaveLength(1);
    expect(result.current.conversations[0].id).toBe('conv-1');
  });

  it('should handle listConversations error', async () => {
    mockClient.listConversations.mockRejectedValue(new Error('DB error'));

    const { result } = renderHook(() => useConversations(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(result.current.loading).toBe(false);
    expect(result.current.error).toBeInstanceOf(Error);
    expect(result.current.error?.message).toBe('DB error');
  });

  it('should add conversation on event', async () => {
    const { result } = renderHook(() => useConversations(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    act(() => {
      emitter.emit('conversation:added', {
        conversation: {
          id: 'conv-new',
          userId1: 'user1',
          userId2: 'user2',
          title: 'New Conv',
          createdAt: new Date().toISOString(),
        },
      });
    });

    expect(result.current.conversations).toHaveLength(1);
    expect(result.current.conversations[0].id).toBe('conv-new');
  });

  it('should update conversation on event', async () => {
    const dbConv = createMockDBConversation({ id: 'conv-1', title: 'Old' });
    mockClient.listConversations.mockResolvedValue({
      conversations: [dbConv],
    });

    const { result } = renderHook(() => useConversations(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    act(() => {
      emitter.emit('conversation:updated', {
        conversation: {
          id: 'conv-1',
          userId1: 'user1',
          userId2: 'user2',
          title: 'Updated',
          createdAt: new Date().toISOString(),
        },
      });
    });

    expect(result.current.conversations[0].title).toBe('Updated');
  });

  it('should remove conversation on event', async () => {
    const dbConv = createMockDBConversation({ id: 'conv-1' });
    mockClient.listConversations.mockResolvedValue({
      conversations: [dbConv],
    });

    const { result } = renderHook(() => useConversations(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    act(() => {
      emitter.emit('conversation:removed', { conversationId: 'conv-1' });
    });

    expect(result.current.conversations).toHaveLength(0);
  });

  it('should create a conversation', async () => {
    const { result } = renderHook(() => useConversations(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    await act(async () => {
      await result.current.createConversation('user2', 'New Chat');
    });

    expect(mockClient.createConversation).toHaveBeenCalledWith(
      'user2',
      'New Chat',
    );
    expect(result.current.conversations).toHaveLength(1);
  });

  it('should delete a conversation', async () => {
    const dbConv = createMockDBConversation({ id: 'conv-1' });
    mockClient.listConversations.mockResolvedValue({
      conversations: [dbConv],
    });

    const { result } = renderHook(() => useConversations(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    await act(async () => {
      await result.current.deleteConversation('conv-1');
    });

    expect(mockClient.deleteConversation).toHaveBeenCalledWith('conv-1');
    expect(result.current.conversations).toHaveLength(0);
  });

  it('should refresh conversations', async () => {
    const dbConv1 = createMockDBConversation({ id: 'conv-1' });
    mockClient.listConversations.mockResolvedValue({
      conversations: [dbConv1],
    });

    const { result } = renderHook(() => useConversations(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    // Change the mock return value
    const dbConv2 = createMockDBConversation({ id: 'conv-2' });
    mockClient.listConversations.mockResolvedValue({
      conversations: [dbConv2],
    });

    await act(async () => {
      await result.current.refresh();
    });

    expect(result.current.conversations).toHaveLength(1);
    expect(result.current.conversations[0].id).toBe('conv-2');
  });
});
