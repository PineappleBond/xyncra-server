/**
 * @packageDocumentation
 * useAgentStatus — React hook for tracking the agent's current status.
 *
 * Subscribes to agent:status and agent:thinking events emitted by
 * ReactUpdateHandler and exposes a combined AgentStatus object plus
 * a convenience isTyping flag.
 *
 * @module
 */

import { useEffect, useState } from 'react';
import { useXyncra } from './useXyncra';

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

/**
 * Represents the current status of an agent.
 */
export interface AgentStatus {
  /** The user/agent ID this status pertains to. */
  userId: string;
  /** The conversation ID this status pertains to. */
  conversationId: string;
  /**
   * Agent status string.
   * Known values from db/models.ts AgentStatus: 'idle' | 'thinking' | 'tool_calling' | 'generating' | 'asking_user' | 'timeout'
   */
  status: string;
}

export interface UseAgentStatusReturn {
  /** The latest agent status, or null if no status event has been received. */
  status: AgentStatus | null;
  /** Convenience flag: true when the agent is thinking, generating, or using tools. */
  isTyping: boolean;
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

/**
 * Tracks the agent's current status based on real-time events.
 *
 * `isTyping` is true when the agent is in an active processing state
 * (thinking, generating, or tool_calling), suitable for showing a
 * typing indicator in the UI.
 */
export function useAgentStatus(): UseAgentStatusReturn {
  const { eventEmitter } = useXyncra();
  const [status, setStatus] = useState<AgentStatus | null>(null);

  useEffect(() => {
    // -- agent:status — full status update --
    const unsubStatus = eventEmitter.on(
      'agent:status',
      ({ userId, conversationId, status: newStatus }) => {
        setStatus({ userId, conversationId, status: newStatus });
      },
    );

    // -- agent:thinking — typing indicator (maps to thinking/idle) --
    const unsubThinking = eventEmitter.on(
      'agent:thinking',
      ({ userId, conversationId, isTyping }) => {
        setStatus({
          userId,
          conversationId,
          status: isTyping ? 'thinking' : 'idle',
        });
      },
    );

    return () => {
      unsubStatus();
      unsubThinking();
    };
  }, [eventEmitter]);

  const isTyping =
    status?.status === 'thinking' ||
    status?.status === 'generating' ||
    status?.status === 'tool_calling';

  return { status, isTyping };
}
