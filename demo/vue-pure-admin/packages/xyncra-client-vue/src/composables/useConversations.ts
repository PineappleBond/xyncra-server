/**
 * @packageDocumentation
 * useConversations — Vue composable for managing the conversation list.
 *
 * Loads conversations from the local DB via XyncraClient.listConversations()
 * and subscribes to conversation events emitted by VueUpdateHandler.
 *
 * Message management is handled separately by useMessages.
 *
 * @module
 */

import { ref, onMounted, onUnmounted, type Ref } from 'vue'
import { useXyncra } from './useXyncra'
import { getAgentName } from '../constants/agents'
import type { ConversationEvent } from '../internal/EventEmitter'

// ---------------------------------------------------------------------------
// Raw → Event type conversion
// ---------------------------------------------------------------------------

function toConversationEvent(raw: any): ConversationEvent {
  return {
    id: raw.id,
    userId1: raw.user_id1 ?? raw.userId1,
    userId2: raw.user_id2 ?? raw.userId2,
    title: raw.title,
    lastMessageId: raw.last_message_id ?? raw.lastMessageId,
    lastMessageAt: raw.last_message_at?.toISOString?.() ?? raw.last_message_at ?? raw.lastMessageAt,
    lastReadMessageId1: typeof raw.last_read_message_id1 === 'number' ? String(raw.last_read_message_id1) : raw.last_read_message_id1 ?? raw.lastReadMessageId1,
    lastReadMessageId2: typeof raw.last_read_message_id2 === 'number' ? String(raw.last_read_message_id2) : raw.last_read_message_id2 ?? raw.lastReadMessageId2,
    createdAt: raw.created_at?.toISOString?.() ?? raw.created_at ?? raw.createdAt,
    updatedAt: raw.updated_at?.toISOString?.() ?? raw.updated_at ?? raw.updatedAt,
    deletedAt: raw.deleted_at?.toISOString?.() ?? raw.deleted_at ?? raw.deletedAt,
  }
}

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

export interface UseConversationsReturn {
  /** Current list of conversations (camelCase). */
  conversations: Ref<ConversationEvent[]>
  /** The currently selected conversation ID (null if none). */
  currentConversationId: Ref<string | null>
  /** True while conversations are being loaded. */
  loading: Ref<boolean>
  /** The last error encountered (null if none). */
  error: Ref<Error | null>
  /** Select a conversation by ID (sets currentConversationId). */
  selectConversation: (id: string) => void
  /** Create a new 1-on-1 conversation with the given user. */
  createConversation: (userId2: string, title?: string) => Promise<ConversationEvent>
  /** Create a conversation with an agent (auto-generates title). */
  createConversationWithAgent: (agentId: string) => Promise<ConversationEvent>
  /** Soft-delete a conversation by ID. */
  deleteConversation: (id: string) => Promise<void>
  /** Re-fetch the conversation list from the local DB. */
  refresh: () => Promise<void>
}

// ---------------------------------------------------------------------------
// Composable
// ---------------------------------------------------------------------------

/**
 * Manages the conversation list: initial load, real-time event subscriptions,
 * and mutation helpers (create, delete, refresh).
 */
export function useConversations(): UseConversationsReturn {
  const { client, eventEmitter } = useXyncra()
  const conversations = ref<ConversationEvent[]>([])
  const currentConversationId = ref<string | null>(null)
  const loading = ref(false)
  const error = ref<Error | null>(null)

  // -- Load conversations --

  async function loadConversations() {
    loading.value = true
    try {
      const result = await client.listConversations()
      conversations.value = (result.conversations as any[]).map(toConversationEvent)
      error.value = null
    } catch (err) {
      console.error('[useConversations] Failed to load conversations', err)
      error.value = err instanceof Error ? err : new Error(String(err))
    } finally {
      loading.value = false
    }
  }

  // -- Select conversation (sets ID only; message loading is handled by useMessages) --

  function selectConversation(id: string) {
    currentConversationId.value = id
  }

  // -- Mutations --

  async function createConversation(userId2: string, title?: string): Promise<ConversationEvent> {
    const result = await client.createConversation(userId2, title)
    const event = toConversationEvent(result.conversation)
    // Dedup: if the conversation already exists (e.g. from event), replace it
    const exists = conversations.value.some(c => c.id === event.id)
    if (exists) {
      conversations.value = conversations.value.map(c => c.id === event.id ? event : c)
    } else {
      conversations.value = [event, ...conversations.value]
    }
    return event
  }

  async function createConversationWithAgent(agentId: string): Promise<ConversationEvent> {
    const agentName = getAgentName(agentId)
    const title = agentName ? `与 ${agentName} 的对话` : '新会话'
    const conv = await createConversation(agentId, title)
    // Auto-select the newly created conversation
    selectConversation(conv.id)
    return conv
  }

  async function deleteConversation(id: string) {
    try {
      await client.deleteConversation(id)
      conversations.value = conversations.value.filter(c => c.id !== id)
      if (currentConversationId.value === id) {
        currentConversationId.value = null
      }
    } catch (err) {
      console.error('[useConversations] Failed to delete conversation', err)
    }
  }

  async function refresh() {
    await loadConversations()
  }

  // -- Lifecycle: initial load + event subscriptions --

  onMounted(() => {
    loadConversations()

    const unsubConvAdded = eventEmitter.on('conversation:added', ({ conversation }) => {
      // Dedup: if already exists, replace; otherwise prepend
      const exists = conversations.value.some(c => c.id === conversation.id)
      if (exists) {
        conversations.value = conversations.value.map(c => c.id === conversation.id ? conversation : c)
      } else {
        conversations.value = [conversation, ...conversations.value]
      }
    })

    const unsubConvUpdated = eventEmitter.on('conversation:updated', ({ conversation }) => {
      const idx = conversations.value.findIndex(c => c.id === conversation.id)
      if (idx !== -1) {
        conversations.value[idx] = conversation
        conversations.value = [...conversations.value]
      }
    })

    const unsubConvRemoved = eventEmitter.on('conversation:removed', ({ conversationId }) => {
      conversations.value = conversations.value.filter(c => c.id !== conversationId)
    })

    onUnmounted(() => {
      unsubConvAdded()
      unsubConvUpdated()
      unsubConvRemoved()
    })
  })

  return {
    conversations,
    currentConversationId,
    loading,
    error,
    selectConversation,
    createConversation,
    createConversationWithAgent,
    deleteConversation,
    refresh,
  }
}
