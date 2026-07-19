/**
 * Integration test: HITL flow — question received, answered, agent resumed.
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

  it('should handle full HITL lifecycle: question -> answer -> resume', async () => {
    const { result: hitlResult } = renderHook(() => useHITL(), {
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

    // Agent raises HITL question
    act(() => {
      emitter.emit('hitl:question', {
        userId: 'agent1',
        conversationId: 'conv-1',
        reason: 'Should I proceed with the deployment?',
      });
    });

    expect(hitlResult.current.pendingQuestion).toEqual({
      userId: 'agent1',
      conversationId: 'conv-1',
      question: 'Should I proceed with the deployment?',
    });

    // User answers the question
    await act(async () => {
      await hitlResult.current.answer('q-1', 'Yes, proceed');
    });

    expect(mockClient.call).toHaveBeenCalledWith('agent_resume', {
      question_id: 'q-1',
      answer: 'Yes, proceed',
    });

    // Question is cleared
    expect(hitlResult.current.pendingQuestion).toBeNull();

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

  it('should handle HITL dismiss', () => {
    const { result } = renderHook(() => useHITL(), {
      wrapper: createWrapper(),
    });

    act(() => {
      emitter.emit('hitl:question', {
        userId: 'agent1',
        conversationId: 'conv-1',
        reason: 'Need input',
      });
    });

    expect(result.current.pendingQuestion).not.toBeNull();

    act(() => {
      result.current.dismiss();
    });

    expect(result.current.pendingQuestion).toBeNull();
  });

  it('should handle multiple sequential HITL questions', async () => {
    const { result } = renderHook(() => useHITL(), {
      wrapper: createWrapper(),
    });

    // First question
    act(() => {
      emitter.emit('hitl:question', {
        userId: 'agent1',
        conversationId: 'conv-1',
        reason: 'Question 1?',
      });
    });

    expect(result.current.pendingQuestion?.question).toBe('Question 1?');

    await act(async () => {
      await result.current.answer('q-1', 'Answer 1');
    });

    expect(result.current.pendingQuestion).toBeNull();

    // Second question
    act(() => {
      emitter.emit('hitl:question', {
        userId: 'agent1',
        conversationId: 'conv-1',
        reason: 'Question 2?',
      });
    });

    expect(result.current.pendingQuestion?.question).toBe('Question 2?');
  });
});
