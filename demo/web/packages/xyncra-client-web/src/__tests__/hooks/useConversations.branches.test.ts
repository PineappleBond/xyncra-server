import { act, renderHook } from '@testing-library/react';
import React from 'react';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { useConversations } from '../../hooks/useConversations';
import type { UpdateHandlerEventMap } from '../../internal/EventEmitter';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';
import { createMockDBConversation, resetTestCounters } from '../test-utils';

describe('useConversations branch coverage', () => {
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

  it('should handle conversations with optional fields missing', async () => {
    const dbConv = createMockDBConversation({
      id: 'conv-1',
      title: null,
      last_message_at: null,
      updated_at: null,
      deleted_at: null,
    });
    mockClient.listConversations.mockResolvedValue({ conversations: [dbConv] });

    const { result } = renderHook(() => useConversations(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(result.current.conversations[0].title).toBeUndefined();
    expect(result.current.conversations[0].lastMessageAt).toBeUndefined();
  });

  it('should handle conversations with all optional fields present', async () => {
    const now = new Date();
    const dbConv = createMockDBConversation({
      id: 'conv-1',
      title: 'Has Title',
      last_message_at: now,
      updated_at: now,
      deleted_at: now,
    });
    mockClient.listConversations.mockResolvedValue({ conversations: [dbConv] });

    const { result } = renderHook(() => useConversations(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(result.current.conversations[0].title).toBe('Has Title');
    expect(result.current.conversations[0].lastMessageAt).toBeTruthy();
    expect(result.current.conversations[0].updatedAt).toBeTruthy();
    expect(result.current.conversations[0].deletedAt).toBeTruthy();
  });

  it('should deduplicate conversations on add event', async () => {
    const dbConv = createMockDBConversation({ id: 'conv-1' });
    mockClient.listConversations.mockResolvedValue({ conversations: [dbConv] });

    const { result } = renderHook(() => useConversations(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    // Add same conversation again
    act(() => {
      emitter.emit('conversation:added', {
        conversation: {
          id: 'conv-1',
          userId1: 'user1',
          userId2: 'user2',
          title: 'Updated Title',
          createdAt: new Date().toISOString(),
        },
      });
    });

    expect(result.current.conversations).toHaveLength(1);
    expect(result.current.conversations[0].title).toBe('Updated Title');
  });

  it('should handle refresh error', async () => {
    mockClient.listConversations.mockResolvedValue({ conversations: [] });

    const { result } = renderHook(() => useConversations(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    mockClient.listConversations.mockRejectedValue(new Error('Refresh failed'));

    await act(async () => {
      await result.current.refresh();
    });

    expect(result.current.error?.message).toBe('Refresh failed');
  });

  it('should handle createConversation deduplication', async () => {
    const dbConv = createMockDBConversation({ id: 'conv-1' });
    mockClient.listConversations.mockResolvedValue({ conversations: [dbConv] });

    const { result } = renderHook(() => useConversations(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    // Create conversation with same id
    mockClient.createConversation.mockResolvedValue({
      conversation: createMockDBConversation({ id: 'conv-1' }),
    });

    await act(async () => {
      await result.current.createConversation('user2');
    });

    // Should not duplicate
    expect(result.current.conversations).toHaveLength(1);
  });
});
