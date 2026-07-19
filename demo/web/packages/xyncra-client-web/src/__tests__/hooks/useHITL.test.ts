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

  it('should start with no pending questions', () => {
    const { result } = renderHook(() => useHITL(), {
      wrapper: createWrapper(),
    });

    expect(result.current.pendingQuestions).toEqual([]);
  });

  it('should fetch questions on hitl:question event', async () => {
    mockClient.getConversation = jest.fn().mockResolvedValue({
      conversation: {
        id: 'conv-1',
        user_id2: 'agent1',
        agent_status: 'asking_user',
      },
      questions: [
        {
          id: 'q-1',
          conversation_id: 'conv-1',
          checkpoint_id: 'cp-1',
          interrupt_id: 'intr-1',
          question_text: 'Need confirmation',
          status: 'pending',
          created_at: new Date(),
        },
      ],
    });

    const { result } = renderHook(() => useHITL('conv-1'), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      emitter.emit('hitl:question', {
        userId: 'agent1',
        conversationId: 'conv-1',
        reason: 'Need confirmation',
      });
    });

    expect(result.current.pendingQuestions).toHaveLength(1);
    expect(result.current.pendingQuestions[0]).toMatchObject({
      userId: 'agent1',
      conversationId: 'conv-1',
      question: 'Need confirmation',
    });
  });

  it('should answer all questions with full agent_resume contract', async () => {
    mockClient.getConversation = jest.fn().mockResolvedValue({
      conversation: {
        id: 'conv-1',
        user_id2: 'agent1',
        agent_status: 'asking_user',
      },
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

    const { result } = renderHook(() => useHITL('conv-1'), {
      wrapper: createWrapper(),
    });

    // Wait for initial fetch
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    const answers = new Map<string, string>();
    answers.set('q-1', 'Yes');

    await act(async () => {
      await result.current.answerAll(answers);
    });

    expect(mockClient.call).toHaveBeenCalledWith('agent_resume', {
      conversation_id: 'conv-1',
      question_id: 'q-1',
      checkpoint_id: 'cp-1',
      interrupt_id: 'intr-1',
      agent_id: 'agent1',
      answer: 'Yes',
    });
  });

  it('should dismiss pending questions', async () => {
    mockClient.getConversation = jest.fn().mockResolvedValue({
      conversation: {
        id: 'conv-1',
        user_id2: 'agent1',
        agent_status: 'asking_user',
      },
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

    const { result } = renderHook(() => useHITL('conv-1'), {
      wrapper: createWrapper(),
    });

    // Wait for initial fetch
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    act(() => {
      result.current.dismiss();
    });

    expect(result.current.pendingQuestions).toEqual([]);
  });

  it('should throw on answerAll when client not initialized', async () => {
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

    const answers = new Map<string, string>();
    await expect(result.current.answerAll(answers)).rejects.toThrow(
      'client not initialized',
    );
  });
});
