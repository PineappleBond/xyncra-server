import { act, renderHook } from '@testing-library/react';
import React from 'react';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { useHITL } from '../../hooks/useHITL';
import type { UpdateHandlerEventMap } from '../../internal/EventEmitter';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

describe('useHITL', () => {
  let emitter: TypedEventEmitter<UpdateHandlerEventMap>;
  let mockClient: any;

  beforeEach(() => {
    emitter = new TypedEventEmitter<UpdateHandlerEventMap>();
    mockClient = {
      call: jest.fn().mockResolvedValue(undefined),
      getConversation: jest.fn().mockResolvedValue({ questions: [] }),
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

  it('should start with no pending question', () => {
    const { result } = renderHook(() => useHITL(), {
      wrapper: createWrapper(),
    });

    expect(result.current.pendingQuestion).toBeNull();
  });

  it('should set pending question on hitl:question event', () => {
    const { result } = renderHook(() => useHITL(), {
      wrapper: createWrapper(),
    });

    act(() => {
      emitter.emit('hitl:question', {
        userId: 'agent1',
        conversationId: 'conv-1',
        reason: 'Need confirmation',
      });
    });

    expect(result.current.pendingQuestion).toEqual({
      userId: 'agent1',
      conversationId: 'conv-1',
      question: 'Need confirmation',
    });
  });

  it('should answer question with full agent_resume contract', async () => {
    const { result } = renderHook(() => useHITL(), {
      wrapper: createWrapper(),
    });

    mockClient.getConversation = jest.fn().mockResolvedValue({
      questions: [
        {
          id: 'q-1',
          conversation_id: 'conv-1',
          checkpoint_id: 'cp-1',
          interrupt_id: 'intr-1',
          question_text: 'Confirm?',
          status: 'pending',
          created_at: new Date(),
        },
      ],
    });

    act(() => {
      emitter.emit('hitl:question', {
        userId: 'agent1',
        conversationId: 'conv-1',
        reason: 'Confirm?',
      });
    });

    await act(async () => {
      await result.current.answer('q-1', 'Yes');
    });

    expect(mockClient.call).toHaveBeenCalledWith('agent_resume', {
      conversation_id: 'conv-1',
      question_id: 'q-1',
      checkpoint_id: 'cp-1',
      interrupt_id: 'intr-1',
      agent_id: 'agent1',
      answer: 'Yes',
    });
    expect(result.current.pendingQuestion).toBeNull();
  });

  it('should dismiss pending question', () => {
    const { result } = renderHook(() => useHITL(), {
      wrapper: createWrapper(),
    });

    act(() => {
      emitter.emit('hitl:question', {
        userId: 'agent1',
        conversationId: 'conv-1',
        reason: 'Confirm?',
      });
    });

    act(() => {
      result.current.dismiss();
    });

    expect(result.current.pendingQuestion).toBeNull();
  });

  it('should throw on answer when client not initialized', async () => {
    const contextValue: XyncraContextValue = {
      client: null,
      connectionStatus: 'disconnected',
      deviceID: 'test-device',
      agentID: 'test-agent',
      functionRegistry: new FunctionRegistry(),
      eventEmitter: emitter,
      registerFunction: jest.fn(),
      unregisterFunction: jest.fn(),
    };

    const wrapper = ({ children }: { children: React.ReactNode }) =>
      React.createElement(
        XyncraContext.Provider,
        { value: contextValue },
        children,
      );

    const { result } = renderHook(() => useHITL(), { wrapper });

    await expect(result.current.answer('q-1', 'Yes')).rejects.toThrow(
      'client not initialized',
    );
  });
});
