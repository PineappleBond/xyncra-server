/**
 * Integration test: Message flow — send messages, receive events, streaming.
 */

import { act, renderHook } from '@testing-library/react';
import React from 'react';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { useMessages } from '../../hooks/useMessages';
import { useStreaming } from '../../hooks/useStreaming';
import type { UpdateHandlerEventMap } from '../../internal/EventEmitter';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';
import { resetTestCounters } from '../test-utils';

describe('Message Flow Integration', () => {
  let emitter: TypedEventEmitter<UpdateHandlerEventMap>;
  let mockClient: any;

  beforeEach(() => {
    resetTestCounters();
    emitter = new TypedEventEmitter<UpdateHandlerEventMap>();
    mockClient = {
      getMessages: jest.fn().mockResolvedValue({ messages: [] }),
      sendMessage: jest.fn().mockResolvedValue({}),
    };
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  function createWrapper(_convId: string = 'conv-1') {
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

  it('should send a message and receive it back via event', async () => {
    const { result } = renderHook(
      () => useMessages({ conversationId: 'conv-1' }),
      { wrapper: createWrapper() },
    );

    // Flush the microtask queue so getMessages resolves
    await act(async () => {
      jest.runAllTimers();
    });

    // Send a message
    await act(async () => {
      await result.current.send('Hello, AI!');
    });

    expect(mockClient.sendMessage).toHaveBeenCalledWith('conv-1', 'Hello, AI!');

    // Simulate the message arriving back via event
    act(() => {
      emitter.emit('message:added', {
        message: {
          id: 'msg-response',
          conversationId: 'conv-1',
          senderId: 'user1',
          content: 'Hello, AI!',
          clientMessageId: 'client-1',
          createdAt: new Date().toISOString(),
        },
      });
    });

    expect(result.current.messages).toHaveLength(1);
    expect(result.current.messages[0].content).toBe('Hello, AI!');
  });

  it('should handle streaming text alongside messages', async () => {
    const { result: msgResult } = renderHook(
      () => useMessages({ conversationId: 'conv-1' }),
      { wrapper: createWrapper() },
    );

    const { result: streamResult } = renderHook(() => useStreaming(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      jest.runAllTimers();
    });

    // Simulate streaming text
    act(() => {
      emitter.emit('stream:text', {
        userId: 'agent1',
        conversationId: 'conv-1',
        streamId: 'stream-1',
        text: 'Thinking...',
      });
    });

    await act(async () => {
      jest.runAllTimers();
    });

    expect(streamResult.current.isStreaming).toBe(true);
    expect(streamResult.current.streamingText).toBe('Thinking...');

    // Simulate stream done
    act(() => {
      emitter.emit('stream:done', {
        userId: 'agent1',
        conversationId: 'conv-1',
        streamId: 'stream-1',
      });
    });

    expect(streamResult.current.isStreaming).toBe(false);

    // Simulate the final message arriving
    act(() => {
      emitter.emit('message:added', {
        message: {
          id: 'msg-final',
          conversationId: 'conv-1',
          senderId: 'agent1',
          content: 'Thinking... Here is the answer.',
          clientMessageId: 'client-2',
          createdAt: new Date().toISOString(),
        },
      });
    });

    expect(msgResult.current.messages).toHaveLength(1);
  });

  it('should handle multiple messages in sequence', async () => {
    const { result } = renderHook(
      () => useMessages({ conversationId: 'conv-1' }),
      { wrapper: createWrapper() },
    );

    await act(async () => {
      jest.runAllTimers();
    });

    // Send multiple messages
    for (let i = 0; i < 3; i++) {
      act(() => {
        emitter.emit('message:added', {
          message: {
            id: `msg-${i}`,
            conversationId: 'conv-1',
            senderId: i === 0 ? 'user1' : 'agent1',
            content: `Message ${i}`,
            clientMessageId: `client-${i}`,
            createdAt: new Date().toISOString(),
          },
        });
      });
    }

    expect(result.current.messages).toHaveLength(3);
  });
});
