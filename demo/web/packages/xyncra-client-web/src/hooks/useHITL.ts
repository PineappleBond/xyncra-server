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
export function useHITL(): UseHITLReturn {
  const { client, eventEmitter } = useXyncra();
  const [pendingQuestion, setPendingQuestion] = useState<HITLQuestion | null>(
    null,
  );

  useEffect(() => {
    const unsub = eventEmitter.on(
      'hitl:question',
      ({ userId, conversationId, reason }) => {
        setPendingQuestion({
          userId,
          conversationId,
          question: reason,
        });
      },
    );

    return unsub;
  }, [eventEmitter]);

  const answer = useCallback(
    async (questionID: string, answerText: string): Promise<void> => {
      if (!client) throw new Error('client not initialized');
      await client.call('agent_resume', {
        question_id: questionID,
        answer: answerText,
      });
      setPendingQuestion(null);
    },
    [client],
  );

  const dismiss = useCallback(() => {
    setPendingQuestion(null);
  }, []);

  return { pendingQuestion, answer, dismiss };
}
