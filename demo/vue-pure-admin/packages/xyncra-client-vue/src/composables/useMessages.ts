/**
 * @packageDocumentation
 * useMessages — Vue composable for loading and sending messages in a conversation.
 *
 * Reads messages from the local DB via XyncraClient.getMessages() and
 * subscribes to message events. Converts raw snake_case messages to
 * MessageEvent (camelCase) at the boundary.
 *
 * Design: accepts a reactive conversationId (Ref). When it changes, messages
 * are automatically re-loaded. Event subscriptions are cleaned up on unmount.
 *
 * @module
 */

import { ref, watch, onMounted, onUnmounted, type Ref } from 'vue'
import { useXyncra } from './useXyncra'
import type { MessageEvent } from '../internal/EventEmitter'

// ---------------------------------------------------------------------------
// Raw → Event type conversion
// ---------------------------------------------------------------------------

/**
 * Convert a raw message object (snake_case or camelCase) to a MessageEvent
 * (camelCase, string fields) for consistent consumer-facing types.
 */
function toMessageEvent(raw: any): MessageEvent {
  return {
    id: raw.id,
    conversationId: raw.conversation_id ?? raw.conversationId,
    senderId: raw.sender_id ?? raw.senderId,
    content: raw.content,
    type: raw.type ?? 'text',
    clientMessageId: raw.client_message_id ?? raw.clientMessageId,
    replyToId: raw.reply_to_id ?? raw.replyToId,
    createdAt: raw.created_at?.toISOString?.() ?? raw.created_at ?? raw.createdAt,
    updatedAt: raw.updated_at?.toISOString?.() ?? raw.updated_at ?? raw.updatedAt,
    deletedAt: raw.deleted_at?.toISOString?.() ?? raw.deleted_at ?? raw.deletedAt,
  }
}

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

export interface UseMessagesParams {
  /** The conversation ID to load messages for. Null means no conversation selected. */
  conversationId: Ref<string | null>
}

export interface UseMessagesReturn {
  /** Current list of messages for the active conversation (camelCase). */
  messages: Ref<MessageEvent[]>
  /** True while messages are being loaded. */
  loading: Ref<boolean>
  /** The last error encountered (null if none). */
  error: Ref<Error | null>
  /** Send a text message in the current conversation. */
  send: (content: string) => Promise<void>
  /** Fetch older messages and prepend them to the list. */
  loadMore: () => Promise<void>
  /** Whether there are more older messages to load. */
  hasMore: Ref<boolean>
  /** True while fetching more messages. */
  loadingMore: Ref<boolean>
  /** Re-fetch messages for the current conversation. */
  refresh: () => Promise<void>
}

// ---------------------------------------------------------------------------
// Composable
// ---------------------------------------------------------------------------

/**
 * Manages messages for a single conversation: initial load when conversationId
 * changes, real-time event subscriptions, and sending new messages.
 */
export function useMessages({ conversationId }: UseMessagesParams): UseMessagesReturn {
  const { client, eventEmitter } = useXyncra()
  const messages = ref<MessageEvent[]>([])
  const loading = ref(false)
  const error = ref<Error | null>(null)
  const hasMore = ref(true)
  const loadingMore = ref(false)

  // -- Unsubscribe handles, cleaned up on unmount --
  let unsubMsgAdded: (() => void) | null = null
  let unsubMsgUpdated: (() => void) | null = null
  let unsubMsgRemoved: (() => void) | null = null

  /**
   * Subscribe to message events filtered by the given conversation ID.
   * Cleans up any previous subscriptions first.
   */
  function subscribeToEvents(convId: string) {
    // Clean up previous subscriptions
    unsubscribeFromEvents()

    unsubMsgAdded = eventEmitter.on('message:added', ({ message }) => {
      if (message.conversationId === convId) {
        const list = messages.value
        const existingIndex = list.findIndex(m => m.id === message.id)
        const existingIds = list.map(m => m.id)
        if (existingIndex >= 0) {
          // Update existing message (e.g., tool_calling status change)
          console.log('[useMessages] message:added UPDATE existing', {
            id: message.id,
            type: message.type,
            content: message.content?.substring(0, 80),
            existingIndex,
            listLength: list.length,
            existingIds: existingIds.slice(-5), // last 5 ids
          })
          const updated = [...list]
          updated[existingIndex] = message
          messages.value = updated
        } else {
          // Add new message
          console.log('[useMessages] message:added NEW', {
            id: message.id,
            type: message.type,
            content: message.content?.substring(0, 80),
            listLength: list.length,
            existingIds: existingIds.slice(-5), // last 5 ids
          })
          messages.value = [...list, message]
        }
      }
    })

    unsubMsgUpdated = eventEmitter.on('message:updated', ({ message }) => {
      if (message.conversationId === convId) {
        messages.value = messages.value.map(m => m.id === message.id ? message : m)
      }
    })

    unsubMsgRemoved = eventEmitter.on('message:removed', ({ messageId, conversationId: cid }) => {
      if (cid === convId) {
        messages.value = messages.value.filter(m => m.id !== messageId)
      }
    })
  }

  function unsubscribeFromEvents() {
    unsubMsgAdded?.()
    unsubMsgUpdated?.()
    unsubMsgRemoved?.()
    unsubMsgAdded = null
    unsubMsgUpdated = null
    unsubMsgRemoved = null
  }

  /**
   * Load messages for the given conversation ID.
   */
  async function loadMessages(convId: string) {
    loading.value = true
    try {
      const result = await client.getMessages(convId)
      messages.value = (result.messages as any[]).map(toMessageEvent)
      hasMore.value = true
      error.value = null
    } catch (err) {
      console.error('[useMessages] Failed to load messages', err)
      error.value = err instanceof Error ? err : new Error(String(err))
    } finally {
      loading.value = false
    }
  }

  // -- Watch conversationId: load messages + resubscribe on change --
  watch(
    conversationId,
    (newId, oldId) => {
      if (newId === oldId) return

      if (!newId) {
        // No conversation selected: clear state
        messages.value = []
        hasMore.value = true
        error.value = null
        unsubscribeFromEvents()
        return
      }

      // Load messages and subscribe to events for the new conversation
      loadMessages(newId)
      subscribeToEvents(newId)
    },
    { immediate: true },
  )

  // -- Cleanup on unmount --
  onMounted(() => {
    // If conversationId is already set at mount time, subscribe immediately.
    // (The watch with immediate: true handles the initial load.)
    if (conversationId.value) {
      subscribeToEvents(conversationId.value)
    }
  })

  onUnmounted(() => {
    unsubscribeFromEvents()
  })

  // -- Mutations --

  async function send(content: string) {
    const convId = conversationId.value
    if (!convId) {
      console.warn('[useMessages] No conversation selected, cannot send message')
      return
    }
    try {
      await client.sendMessage(convId, content)
      // The message:added event will update the list automatically
    } catch (err) {
      console.error('[useMessages] Failed to send message', err)
    }
  }

  async function loadMore() {
    const convId = conversationId.value
    if (!convId || loadingMore.value) return

    loadingMore.value = true
    try {
      const existing = messages.value
      // Find the oldest message id to use as cursor for fetching older messages
      const oldestId = existing.length > 0 ? Number(existing[0].id) || undefined : undefined
      const result = await client.fetchMoreMessages(convId, oldestId)
      const older = (result.messages as any[]).map(toMessageEvent)

      if (older.length === 0) {
        hasMore.value = false
      } else {
        // Prepend older messages, deduplicating by id
        const existingIds = new Set(existing.map(m => m.id))
        const deduped = older.filter(m => !existingIds.has(m.id))
        messages.value = [...deduped, ...existing]
        if (!result.has_more) {
          hasMore.value = false
        }
      }
    } catch (err) {
      console.error('[useMessages] Failed to load more messages', err)
    } finally {
      loadingMore.value = false
    }
  }

  async function refresh() {
    const convId = conversationId.value
    if (!convId) return
    await loadMessages(convId)
  }

  return {
    messages,
    loading,
    error,
    send,
    loadMore,
    hasMore,
    loadingMore,
    refresh,
  }
}
