/**
 * @packageDocumentation
 * useMessages — React hook for loading and sending messages in a conversation.
 *
 * Reads messages from the local DB via XyncraClient.getMessages() and
 * subscribes to message events. Converts DBMessage (snake_case) to
 * MessageEvent (camelCase) at the boundary.
 *
 * @module
 */

import type { Message as DBMessage } from '@xyncra/client-core/db/models';
import { useCallback, useEffect, useState } from 'react';
import { safeISODate } from '../internal/dateUtils';
import type { MessageEvent } from '../internal/EventEmitter';
import { useXyncra } from './useXyncra';

// ---------------------------------------------------------------------------
// DB → Event type conversion
// ---------------------------------------------------------------------------

/**
 * Convert a DBMessage (snake_case, Date fields) to a MessageEvent
 * (camelCase, string fields) for consistent consumer-facing types.
 */
function dbMessageToEvent(msg: DBMessage): MessageEvent {
  return {
    id: msg.id,
    conversationId: msg.conversation_id,
    senderId: msg.sender_id,
    content: msg.content,
    clientMessageId: msg.client_message_id,
    replyToId: msg.reply_to ? String(msg.reply_to) : undefined,
    createdAt: safeISODate(msg.created_at) ?? '',
    updatedAt: undefined,
    deletedAt: safeISODate(msg.deleted_at),
  };
}

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

export interface UseMessagesParams {
  /** The conversation ID to load messages for. Null means no conversation selected. */
  conversationId: string | null;
}

export interface UseMessagesReturn {
  /** Current list of messages for the conversation (camelCase). */
  messages: MessageEvent[];
  /** True while messages are being loaded. */
  loading: boolean;
  /** The last error encountered (null if none). */
  error: Error | null;
  /** Send a text message in the current conversation. Throws on failure. */
  send: (content: string) => Promise<void>;
  /** Re-fetch messages from the local DB. */
  refresh: () => Promise<void>;
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

/**
 * Manages messages for a single conversation: initial load, real-time event
 * subscriptions, and sending new messages.
 */
export function useMessages({
  conversationId,
}: UseMessagesParams): UseMessagesReturn {
  const { client, eventEmitter } = useXyncra();
  const [messages, setMessages] = useState<MessageEvent[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  // -- Load messages + subscribe to events for the current conversation --
  useEffect(() => {
    if (!client || !conversationId) {
      setMessages([]);
      setLoading(false);
      return;
    }

    setLoading(true);
    setError(null);

    client
      .getMessages(conversationId)
      .then((result) => {
        setMessages(result.messages.map(dbMessageToEvent));
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err : new Error(String(err)));
      })
      .finally(() => {
        setLoading(false);
      });

    // Subscribe to message events filtered by current conversation.
    const unsubAdded = eventEmitter.on('message:added', ({ message }) => {
      if (message.conversationId === conversationId) {
        setMessages((prev) => {
          // Avoid duplicates (by id).
          if (prev.some((m) => m.id === message.id)) return prev;
          return [...prev, message];
        });
      }
    });

    const unsubUpdated = eventEmitter.on('message:updated', ({ message }) => {
      if (message.conversationId === conversationId) {
        setMessages((prev) =>
          prev.map((m) => (m.id === message.id ? message : m)),
        );
      }
    });

    const unsubRemoved = eventEmitter.on(
      'message:removed',
      ({ messageId, conversationId: cid }) => {
        if (cid === conversationId) {
          setMessages((prev) => prev.filter((m) => m.id !== messageId));
        }
      },
    );

    return () => {
      unsubAdded();
      unsubUpdated();
      unsubRemoved();
    };
  }, [client, conversationId, eventEmitter]);

  // -- Mutations --

  const send = useCallback(
    async (content: string): Promise<void> => {
      if (!client || !conversationId) {
        throw new Error('client not initialized or no conversation selected');
      }
      // sendMessage returns SendMessageResult; the message:added event will
      // update the list automatically.
      await client.sendMessage(conversationId, content);
    },
    [client, conversationId],
  );

  const refresh = useCallback(async (): Promise<void> => {
    if (!client || !conversationId) return;
    setLoading(true);
    try {
      const result = await client.getMessages(conversationId);
      setMessages(result.messages.map(dbMessageToEvent));
      setError(null);
    } catch (err: unknown) {
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  }, [client, conversationId]);

  return { messages, loading, error, send, refresh };
}
