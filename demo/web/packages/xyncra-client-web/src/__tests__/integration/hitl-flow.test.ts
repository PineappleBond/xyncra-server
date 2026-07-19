/**
 * Integration test: HITL flow — questions received, answered, agent resumed.
 */

import { act, renderHook } from '@testing-library/react';
import React from 'react';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { useAgentStatus } from '../../hooks/useAgentStatus';
import { useHITL } from '../../hooks/useHITL';
import type { UpdateHandlerEventMap } from '../../internal/EventEmitter';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

describe('HITL Flow Integration', () => {
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

  it('should handle full HITL lifecycle: questions -> answerAll -> resume', async () => {
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
          question_text: 'Should I proceed with the deployment?',
          status: 'pending',
          created_at: new Date(),
        },
      ],
    });

    const { result: hitlResult } = renderHook(() => useHITL('conv-1'), {
      wrapper: createWrapper(),
    });

    const { result: statusResult } = renderHook(() => useAgentStatus(), {
      wrapper: createWrapper(),
    });

    // Agent is in timeout state
    act(() => {
      emitter.emit('agent:status', {
        userId: 'agent1',
        conversationId: 'conv-1',
        status: 'timeout',
      });
    });

    expect(statusResult.current.status?.status).toBe('timeout');

    // Agent raises HITL question - triggers fetch
    await act(async () => {
      emitter.emit('hitl:question', {
        userId: 'agent1',
        conversationId: 'conv-1',
        reason: 'Should I proceed with the deployment?',
      });
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(hitlResult.current.pendingQuestions).toHaveLength(1);
    expect(hitlResult.current.pendingQuestions[0]).toMatchObject({
      userId: 'agent1',
      conversationId: 'conv-1',
      question: 'Should I proceed with the deployment?',
    });

    // User answers all questions
    const answers = new Map<string, string>();
    answers.set('q-1', 'Yes, proceed');

    await act(async () => {
      await hitlResult.current.answerAll(answers);
    });

    expect(mockClient.call).toHaveBeenCalledWith('agent_resume', {
      conversation_id: 'conv-1',
      question_id: 'q-1',
      checkpoint_id: 'cp-1',
      interrupt_id: 'intr-1',
      agent_id: 'agent1',
      answer: 'Yes, proceed',
    });

    // Agent resumes
    act(() => {
      emitter.emit('agent:status', {
        userId: 'agent1',
        conversationId: 'conv-1',
        status: 'generating',
      });
    });

    expect(statusResult.current.status?.status).toBe('generating');
    expect(statusResult.current.isTyping).toBe(true);
  });

  it('should handle HITL dismiss', async () => {
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
          question_text: 'Need input',
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

    expect(result.current.pendingQuestions.length).toBeGreaterThan(0);

    act(() => {
      result.current.dismiss();
    });

    expect(result.current.pendingQuestions).toEqual([]);
  });

  it('should handle batch questions', async () => {
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
          question_text: 'Question 1?',
          status: 'pending',
          created_at: new Date(),
        },
        {
          id: 'q-2',
          conversation_id: 'conv-1',
          checkpoint_id: 'cp-2',
          interrupt_id: 'intr-2',
          question_text: 'Question 2?',
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

    expect(result.current.pendingQuestions).toHaveLength(2);
    expect(result.current.pendingQuestions[0].question).toBe('Question 1?');
    expect(result.current.pendingQuestions[1].question).toBe('Question 2?');

    // Answer all questions
    const answers = new Map<string, string>();
    answers.set('q-1', 'Answer 1');
    answers.set('q-2', 'Answer 2');

    await act(async () => {
      await result.current.answerAll(answers);
    });

    expect(mockClient.call).toHaveBeenCalledTimes(2);
  });
});
