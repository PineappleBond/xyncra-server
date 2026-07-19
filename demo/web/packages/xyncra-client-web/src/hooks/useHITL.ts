/**
 * @packageDocumentation
 * useHITL — React hook for Human-in-the-Loop question handling.
 *
 * Manages a batch of HITL questions. All questions are shown at once
 * and must be answered together. After submission, fetches again to
 * check for new questions.
 *
 * @module
 */

import { useCallback, useEffect, useRef, useState } from 'react';
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
  /** Array of pending HITL questions, or empty array if none. */
  pendingQuestions: HITLQuestion[];
  /**
   * Answer all HITL questions. Sends `agent_resume` RPC for each question.
   * After all answers are sent, fetches again to check for new questions.
   * Throws on failure; the UI should handle the error.
   */
  answerAll: (answers: Map<string, string>) => Promise<void>;
  /** Dismiss all pending questions without answering. */
  dismiss: () => void;
  /** Whether answers are currently being submitted. */
  isSubmitting: boolean;
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

/**
 * Manages HITL (Human-in-the-Loop) questions in batch mode.
 *
 * All pending questions are fetched and displayed together. The user must
 * answer all questions before submitting. After submission, fetches again
 * to check for new questions.
 */
export function useHITL(conversationId?: string): UseHITLReturn {
  const { client, eventEmitter } = useXyncra();
  const [pendingQuestions, setPendingQuestions] = useState<HITLQuestion[]>([]);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const isFetching = useRef(false);

  // Fetch all pending questions
  const fetchQuestions = useCallback(async () => {
    if (!client || !conversationId || isFetching.current) {
      return;
    }

    console.log('[useHITL] Fetching questions from server...');
    isFetching.current = true;

    try {
      const result = await client.getConversation(conversationId);
      console.log('[useHITL] Got conversation:', {
        id: result.conversation.id,
        questionsCount: result.questions?.length,
        agent_status: result.conversation.agent_status,
      });

      if (result.questions && result.questions.length > 0) {
        const questions: HITLQuestion[] = result.questions
          .filter((q) => q.status === 'pending')
          .map((q) => ({
            userId: result.conversation.user_id2,
            conversationId: result.conversation.id,
            question: q.question_text,
            questionId: q.id,
            checkpointId: q.checkpoint_id,
            interruptId: q.interrupt_id,
          }));

        console.log('[useHITL] Found', questions.length, 'pending questions');
        setPendingQuestions(questions);
      } else {
        console.log('[useHITL] No more questions');
        setPendingQuestions([]);
      }
    } catch (error) {
      console.error('[useHITL] Failed to fetch questions:', error);
    } finally {
      isFetching.current = false;
    }
  }, [client, conversationId]);

  // Initial load and conversation change
  useEffect(() => {
    console.log('[useHITL] useEffect triggered:', { client: !!client, conversationId });
    if (!client || !conversationId) {
      console.log('[useHITL] Skipping check - no client or conversationId');
      return;
    }

    fetchQuestions();
  }, [client, conversationId, fetchQuestions]);

  // Listen for new HITL events
  useEffect(() => {
    const unsub = eventEmitter.on('hitl:question', () => {
      console.log('[useHITL] Received hitl:question event, re-fetching...');
      fetchQuestions();
    });

    return unsub;
  }, [eventEmitter, fetchQuestions]);

  const answerAll = useCallback(
    async (answers: Map<string, string>): Promise<void> => {
      if (!client) throw new Error('client not initialized');
      if (pendingQuestions.length === 0) throw new Error('no pending questions to answer');

      console.log('[useHITL] Answering', answers.size, 'questions');
      setIsSubmitting(true);

      try {
        // Send answers sequentially
        for (const q of pendingQuestions) {
          const answerText = answers.get(q.questionId ?? '');
          if (!answerText) {
            throw new Error(`No answer provided for question ${q.questionId}`);
          }

          console.log('[useHITL] Submitting answer for question:', q.questionId);

          // Get checkpoint/interrupt IDs from local store
          let checkpointId = q.checkpointId;
          let interruptId = q.interruptId;
          if ((!checkpointId || !interruptId) && q.conversationId) {
            try {
              const conv = await client.getConversation(q.conversationId);
              const storedQ = conv.questions.find((item) => item.id === q.questionId);
              if (storedQ) {
                checkpointId = storedQ.checkpoint_id;
                interruptId = storedQ.interrupt_id;
              }
            } catch {
              // Fall back to event data
            }
          }

          await client.call('agent_resume', {
            conversation_id: q.conversationId,
            question_id: q.questionId,
            checkpoint_id: checkpointId ?? '',
            interrupt_id: interruptId ?? '',
            agent_id: q.userId,
            answer: answerText,
          });
        }

        console.log('[useHITL] All answers submitted, clearing questions');

        // Clear questions - don't fetch again to avoid delay issues
        // New questions will be handled by hitl:question events
        setPendingQuestions([]);
      } catch (error) {
        console.error('[useHITL] Failed to submit answers:', error);
        throw error;
      } finally {
        setIsSubmitting(false);
      }
    },
    [client, pendingQuestions],
  );

  const dismiss = useCallback(() => {
    console.log('[useHITL] Dismissing all questions');
    setPendingQuestions([]);
  }, []);

  return { pendingQuestions, answerAll, dismiss, isSubmitting };
}
