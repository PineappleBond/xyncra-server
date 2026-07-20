/**
 * @packageDocumentation
 * useConversations — React hook for managing the conversation list.
 *
 * Loads conversations from the local DB via XyncraClient.listConversations()
 * and subscribes to conversation events emitted by ReactUpdateHandler.
 *
 * @module
 */

import type { Conversation as DBConversation } from '@xyncra/client-core/db/models';
import { useCallback, useEffect, useState } from 'react';
import { getAgentName } from '../constants/agents';
import { safeISODate } from '../internal/dateUtils';
import type { ConversationEvent } from '../internal/EventEmitter';
import { useXyncra } from './useXyncra';

// ---------------------------------------------------------------------------
// DB → Event type conversion
// ---------------------------------------------------------------------------

/**
 * Convert a DBConversation (snake_case, Date fields) to a ConversationEvent
 * (camelCase, string fields) for consistent consumer-facing types.
 */
function dbConversationToEvent(conv: DBConversation): ConversationEvent {
  return {
    id: conv.id,
    userId1: conv.user_id1,
    userId2: conv.user_id2,
    title: conv.title || undefined,
    lastMessageAt: safeISODate(conv.last_message_at),
    lastReadMessageId1: conv.last_read_message_id1
      ? String(conv.last_read_message_id1)
      : undefined,
    lastReadMessageId2: conv.last_read_message_id2
      ? String(conv.last_read_message_id2)
      : undefined,
    createdAt: safeISODate(conv.created_at) ?? '',
    updatedAt: safeISODate(conv.updated_at),
    deletedAt: safeISODate(conv.deleted_at),
  };
}

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

export interface UseConversationsReturn {
  /** Current list of conversations (camelCase). */
  conversations: ConversationEvent[];
  /** True while the initial load is in progress. */
  loading: boolean;
  /** The last error encountered (null if none). */
  error: Error | null;
  /** Create a new 1-on-1 conversation with the given user. */
  createConversation: (
    userId: string,
    title?: string,
  ) => Promise<ConversationEvent>;
  /** Create a conversation with an agent (auto-generates title, returns conversation). */
  createConversationWithAgent: (
    agentId: string,
  ) => Promise<ConversationEvent>;
  /** Soft-delete a conversation by ID. */
  deleteConversation: (id: string) => Promise<void>;
  /** Re-fetch the conversation list from the local DB. */
  refresh: () => Promise<void>;
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

/**
 * Manages the conversation list: initial load, real-time event subscriptions,
 * and mutation helpers (create, delete, refresh).
 */
export function useConversations(): UseConversationsReturn {
  const { client, eventEmitter } = useXyncra();
  const [conversations, setConversations] = useState<ConversationEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  // -- Initial load + event subscriptions --
  useEffect(() => {
    if (!client) {
      console.log('[useConversations] No client yet');
      return;
    }

    console.log('[useConversations] Starting initial load...');
    // Initial load from local DB.
    client
      .listConversations()
      .then((result) => {
        console.log('[useConversations] Loaded conversations:', {
          count: result.conversations.length,
          hasMore: result.has_more,
        });
        setConversations(result.conversations.map(dbConversationToEvent));
      })
      .catch((err: unknown) => {
        console.error('[useConversations] Load failed:', err);
        setError(err instanceof Error ? err : new Error(String(err)));
      })
      .finally(() => {
        setLoading(false);
      });

    // Subscribe to conversation events.
    const unsubAdded = eventEmitter.on(
      'conversation:added',
      ({ conversation }) => {
        setConversations((prev) => {
          // Avoid duplicates.
          if (prev.some((c) => c.id === conversation.id)) {
            return prev.map((c) =>
              c.id === conversation.id ? conversation : c,
            );
          }
          return [...prev, conversation];
        });
      },
    );

    const unsubUpdated = eventEmitter.on(
      'conversation:updated',
      ({ conversation }) => {
        setConversations((prev) =>
          prev.map((c) => (c.id === conversation.id ? conversation : c)),
        );
      },
    );

    const unsubRemoved = eventEmitter.on(
      'conversation:removed',
      ({ conversationId }) => {
        setConversations((prev) => prev.filter((c) => c.id !== conversationId));
      },
    );

    const unsubRead = eventEmitter.on('read:updated', ({ conversationId, lastReadMessageId }) => {
      const readId = String(lastReadMessageId);
      setConversations((prev) =>
        prev.map((c) => {
          if (c.id !== conversationId) return c;
          // The read cursor is advanced by a peer; without the acting user id
          // in the event payload we update both columns (mirrors core's
          // userID-less markRead path in sync-manager).
          return { ...c, lastReadMessageId1: readId, lastReadMessageId2: readId };
        }),
      );
    });

    return () => {
      unsubAdded();
      unsubUpdated();
      unsubRemoved();
      unsubRead();
    };
  }, [client, eventEmitter]);

  // -- Mutations --

  const createConversation = useCallback(
    async (userId: string, title?: string): Promise<ConversationEvent> => {
      if (!client) throw new Error('client not initialized');
      const result = await client.createConversation(userId, title);
      const event = dbConversationToEvent(result.conversation);
      setConversations((prev) => {
        if (prev.some((c) => c.id === event.id)) return prev;
        return [...prev, event];
      });
      return event;
    },
    [client],
  );

  const createConversationWithAgent = useCallback(
    async (agentId: string): Promise<ConversationEvent> => {
      const agentName = getAgentName(agentId);
      const title = agentName ? `与 ${agentName} 的对话` : '新会话';
      return createConversation(agentId, title);
    },
    [createConversation],
  );

  const deleteConversation = useCallback(
    async (id: string): Promise<void> => {
      if (!client) throw new Error('client not initialized');
      await client.deleteConversation(id);
      setConversations((prev) => prev.filter((c) => c.id !== id));
    },
    [client],
  );

  const refresh = useCallback(async (): Promise<void> => {
    if (!client) return;
    setLoading(true);
    try {
      const result = await client.listConversations();
      setConversations(result.conversations.map(dbConversationToEvent));
      setError(null);
    } catch (err: unknown) {
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  }, [client]);

  return {
    conversations,
    loading,
    error,
    createConversation,
    createConversationWithAgent,
    deleteConversation,
    refresh,
  };
}
