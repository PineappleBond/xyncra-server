/**
 * @packageDocumentation
 * useHITL — React hook for Human-in-the-Loop question handling.
 *
 * Subscribes to the `hitl:question` event emitted by ReactUpdateHandler
 * (triggered by agent timeout / HITL interrupts) and provides helpers
 * to answer or dismiss the pending question.
 *
 * @module
 */

import { useCallback, useEffect, useState } from 'react';
import { useXyncra } from './useXyncra';

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

/**
 * A pending HITL question displayed to the user.
 */
export interface HITLQuestion {
  /** The user/agent ID that raised the question. */
  userId: string;
  /** The conversation this question belongs to. */
  conversationId: string;
  /** The question text / reason from the agent. */
  question: string;
  /** Opaque question id used to resume the agent. */
  questionId?: string;
  /** Checkpoint id required by the agent_resume RPC. */
  checkpointId?: string;
  /** Interrupt id required by the agent_resume RPC. */
  interruptId?: string;
}

export interface UseHITLReturn {
  /** The currently pending HITL question, or null if none. */
  pendingQuestion: HITLQuestion | null;
  /**
   * Answer a HITL question by ID. Sends `agent_resume` RPC to the server.
   * Throws on failure; the UI should handle the error.
   */
  answer: (questionID: string, answer: string) => Promise<void>;
  /** Dismiss the current pending question without answering. */
  dismiss: () => void;
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

/**
 * Manages HITL (Human-in-the-Loop) questions.
 *
 * When the agent times out or raises an interrupt, the `hitl:question` event
 * fires and the question is stored as `pendingQuestion`. The UI can render
 * it for user input, then call `answer()` to resume the agent or `dismiss()`
 * to clear it.
 */
export function useHITL(conversationId?: string): UseHITLReturn {
  const { client, eventEmitter } = useXyncra();
  const [pendingQuestion, setPendingQuestion] = useState<HITLQuestion | null>(
    null,
  );

  // Check for pending questions on mount and conversation change
  useEffect(() => {
    console.log('[useHITL] useEffect triggered:', { client: !!client, conversationId });
    if (!client || !conversationId) {
      console.log('[useHITL] Skipping check - no client or conversationId');
      return;
    }

    const checkPendingQuestions = async () => {
      console.log('[useHITL] Checking for pending questions...');
      try {
        const result = await client.getConversation(conversationId);
        console.log('[useHITL] Got conversation:', {
          id: result.conversation.id,
          questionsCount: result.questions?.length,
          agent_status: result.conversation.agent_status,
        });
        if (result.questions && result.questions.length > 0) {
          const pendingQ = result.questions.find((q) => q.status === 'pending');
          console.log('[useHITL] Pending question:', pendingQ);
          if (pendingQ) {
            console.log('[useHITL] Found pending question on load:', {
              conversationId,
              questionId: pendingQ.id,
            });
            setPendingQuestion({
              userId: result.conversation.user_id2,
              conversationId: result.conversation.id,
              question: pendingQ.question_text,
              questionId: pendingQ.id,
              checkpointId: pendingQ.checkpoint_id,
              interruptId: pendingQ.interrupt_id,
            });
          }
        }
      } catch (error) {
        console.error('[useHITL] Failed to check pending questions:', error);
      }
    };

    checkPendingQuestions();
  }, [client, conversationId]);

  useEffect(() => {
    const unsub = eventEmitter.on(
      'hitl:question',
      ({ userId, conversationId, reason, questionId, checkpointId, interruptId }) => {
        setPendingQuestion({
          userId,
          conversationId,
          question: reason,
          questionId,
          checkpointId,
          interruptId,
        });
      },
    );

    return unsub;
  }, [eventEmitter]);

  const answer = useCallback(
    async (questionID: string, answerText: string): Promise<void> => {
      if (!client) throw new Error('client not initialized');
      const pending = pendingQuestion;
      if (!pending) throw new Error('no pending question to answer');

      // Recovery metadata must come from the local question store, not the
      // ephemeral hitl:question event, because the event alone lacks the
      // checkpoint/interrupt ids the server requires (D-125).
      let checkpointId = pending.checkpointId;
      let interruptId = pending.interruptId;
      if ((!checkpointId || !interruptId) && pending.conversationId) {
        try {
          const conv = await client.getConversation(pending.conversationId);
          const q =
            conv.questions.find((item) => item.id === pending.questionId) ??
            conv.questions[0];
          if (q) {
            checkpointId = q.checkpoint_id;
            interruptId = q.interrupt_id;
          }
        } catch {
          // Fall back to whatever metadata the event carried.
        }
      }

      await client.call('agent_resume', {
        conversation_id: pending.conversationId,
        question_id: questionID,
        checkpoint_id: checkpointId ?? '',
        interrupt_id: interruptId ?? '',
        agent_id: pending.userId,
        answer: answerText,
      });
      setPendingQuestion(null);
    },
    [client, pendingQuestion],
  );

  const dismiss = useCallback(() => {
    setPendingQuestion(null);
  }, []);

  return { pendingQuestion, answer, dismiss };
}
