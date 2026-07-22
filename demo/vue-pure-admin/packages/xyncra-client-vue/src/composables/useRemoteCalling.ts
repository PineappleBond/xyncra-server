import { ref, onMounted, onUnmounted, watch, type Ref } from 'vue'
import { useXyncra } from './useXyncra'
import type { RemoteCalling as DBRemoteCalling } from '@xyncra/client-core'

/**
 * A pending RemoteCalling item displayed to the user (D-137).
 */
export interface RemoteCallingItem {
  /** The RemoteCalling ID. */
  id: string
  /** The conversation this remote calling belongs to. */
  conversationId: string
  /** The agent ID that initiated the call. */
  agentId: string
  /** The method name (e.g. ask_user, pg_chatai_sendMessage). */
  method: string
  /** JSON parameters. */
  params: string
  /** Device ID (empty = any device). */
  deviceId: string
  /** Checkpoint ID for agent_resume RPC. */
  checkpointId: string
  /** Status: pending, resolved, cancelled, expired. */
  status: string
}

export interface UseRemoteCallingReturn {
  /** Array of pending remote callings, or empty array if none. */
  pendingCallings: Ref<RemoteCallingItem[]>
  /** Index of the currently active calling in the queue. */
  activeIndex: Ref<number>
  /** The currently active calling. */
  currentCalling: Ref<RemoteCallingItem | null>
  /**
   * Resolve a remote calling. Sends `agent_resume` RPC.
   * On failure, retries with exponential backoff (1s/2s/4s/8s/16s cap).
   * Failed attempts are persisted to IndexedDB for background retry.
   */
  resolveCalling: (id: string, success: boolean, result?: string, errorMessage?: string) => Promise<void>
  /** Navigate to the next calling in the queue. */
  nextCalling: () => void
  /** Navigate to the previous calling in the queue. */
  prevCalling: () => void
  /** Dismiss all pending callings without resolving. */
  dismiss: () => void
  /** Whether resolutions are currently being submitted. */
  isSubmitting: Ref<boolean>
}

/**
 * Manages RemoteCalling items in batch mode (D-137).
 *
 * All pending remote callings are fetched and displayed together. The user can
 * resolve them individually. After resolution, removes from queue; new callings arrive via events.
 *
 * Retry mechanism: on agent_resume failure, retries with exponential backoff
 * (1s/2s/4s/8s/16s cap) indefinitely. Failed attempts are persisted to IndexedDB
 * via RetryQueueStore for background retry on page reload.
 */
export function useRemoteCalling(conversationId?: string | Ref<string | null>): UseRemoteCallingReturn {
  const { client, eventEmitter, db } = useXyncra()
  const pendingCallings = ref<RemoteCallingItem[]>([])
  const activeIndex = ref(0)
  const isSubmitting = ref(false)
  const isFetching = ref(false)
  const currentDeviceID = client.deviceID

  const currentCalling = ref<RemoteCallingItem | null>(null)

  // Keep currentCalling in sync with activeIndex
  function updateCurrentCalling() {
    const idx = activeIndex.value
    currentCalling.value = pendingCallings.value[idx] ?? null
  }

  // Resolve conversationId if it's a Ref
  function getConversationId(): string | undefined {
    if (!conversationId) return undefined
    if (typeof conversationId === 'string') return conversationId
    return conversationId.value ?? undefined
  }

  // Fetch all pending remote callings from local IndexedDB (D-137).
  // getConversation() reads from local DB and does not include remote_callings,
  // so we fetch directly from the remoteCallingsStore.
  async function fetchRemoteCallings() {
    const convId = getConversationId()
    if (!client || !convId || isFetching.value || !db) return

    isFetching.value = true
    try {
      const storedRCs = await db.remoteCallingsStore.getByConversation(convId)

      if (storedRCs.length > 0) {
        const callings: RemoteCallingItem[] = storedRCs
          .filter((rc: DBRemoteCalling) => {
            // Filter by status
            if (rc.status !== 'pending') return false
            // Filter by device_id: empty = any device, non-empty = must match current device
            if (rc.device_id !== '' && rc.device_id !== currentDeviceID) return false
            return true
          })
          .map((rc: DBRemoteCalling) => ({
            id: rc.id,
            conversationId: rc.conversation_id,
            agentId: rc.agent_id,
            method: rc.method,
            params: rc.params,
            deviceId: rc.device_id,
            checkpointId: rc.checkpoint_id,
            status: rc.status,
          }))

        pendingCallings.value = callings
        // Reset index if out of bounds
        if (activeIndex.value >= callings.length) {
          activeIndex.value = Math.max(0, callings.length - 1)
        }
        updateCurrentCalling()
      } else {
        pendingCallings.value = []
        activeIndex.value = 0
        updateCurrentCalling()
      }
    } catch (error) {
      console.error('[useRemoteCalling] Failed to fetch remote callings:', error)
    } finally {
      isFetching.value = false
    }
  }

  // Process retry queue: attempt persisted failed retries
  async function processRetryQueue() {
    if (!db) return
    try {
      const readyItems = await db.retryQueueStore.getReady()
      for (const item of readyItems) {
        try {
          await client.call('agent_resume', {
            id: item.remote_calling_id,
            success: item.success,
            result: item.result,
            error_message: item.error_message,
            agent_id: item.agent_id,
          })
          // Success — remove from retry queue
          await db.retryQueueStore.remove(item.id!)
          // Also remove from pending callings if present
          pendingCallings.value = pendingCallings.value.filter((c) => c.id !== item.remote_calling_id)
          updateCurrentCalling()
        } catch {
          // Still failing — increment retry count and backoff
          await db.retryQueueStore.incrementRetry(item.id!)
        }
      }
    } catch (error) {
      console.error('[useRemoteCalling] Failed to process retry queue:', error)
    }
  }

  // Listen for new remote_calling events and re-fetch
  function onRemoteCalling() {
    fetchRemoteCallings()
  }

  onMounted(() => {
    const unsub = eventEmitter.on('remote_calling', onRemoteCalling)
    onUnmounted(() => unsub())

    // Process retry queue on mount and periodically
    processRetryQueue()
    const retryInterval = setInterval(processRetryQueue, 10_000)
    onUnmounted(() => clearInterval(retryInterval))
  })

  // Fetch on conversationId change
  const convIdRef = typeof conversationId === 'string' ? ref(conversationId) : conversationId
  if (convIdRef) {
    watch(convIdRef, () => {
      fetchRemoteCallings()
    }, { immediate: true })
  }

  // Navigate callings
  function nextCalling() {
    if (activeIndex.value < pendingCallings.value.length - 1) {
      activeIndex.value++
      updateCurrentCalling()
    }
  }

  function prevCalling() {
    if (activeIndex.value > 0) {
      activeIndex.value--
      updateCurrentCalling()
    }
  }

  /**
   * Resolve a remote calling with infinite retry logic (D-137).
   * On failure, retries with exponential backoff (1s/2s/4s/8s/16s cap).
   * Failed attempts are persisted to IndexedDB for background retry.
   * The executed function result is never lost.
   */
  async function resolveCalling(
    id: string,
    success: boolean,
    result?: string,
    errorMessage?: string,
  ): Promise<void> {
    if (!client) throw new Error('client not initialized')

    const calling = pendingCallings.value.find((c) => c.id === id)
    if (!calling) throw new Error(`No remote calling found with id ${id}`)

    isSubmitting.value = true

    try {
      let retryCount = 0
      const maxBackoffMs = 16_000 // 16s cap

      while (true) {
        try {
          await client.call('agent_resume', {
            id: calling.id,
            success,
            result: result ?? '',
            error_message: errorMessage ?? '',
            agent_id: calling.agentId,
          })

          // Success — remove from queue and retry queue
          pendingCallings.value = pendingCallings.value.filter((c) => c.id !== id)
          if (activeIndex.value >= pendingCallings.value.length) {
            activeIndex.value = Math.max(0, pendingCallings.value.length - 1)
          }
          updateCurrentCalling()

          // Clean up any persisted retry item
          if (db) {
            await db.retryQueueStore.deleteByRemoteCallingId(id)
          }
          return
        } catch (error) {
          retryCount++

          // Persist to retry queue for background retry on page reload
          if (db) {
            try {
              // Check if already enqueued
              const existing = await db.retryQueueStore.getByRemoteCallingId(id)
              if (existing.length === 0) {
                const nextRetryAt = new Date(Date.now() + Math.min(Math.pow(2, retryCount - 1), maxBackoffMs / 1000) * 1000)
                await db.retryQueueStore.enqueue({
                  remote_calling_id: id,
                  success,
                  result: result ?? '',
                  error_message: errorMessage ?? '',
                  agent_id: calling.agentId,
                  retry_count: retryCount,
                  next_retry_at: nextRetryAt,
                  created_at: new Date(),
                })
              } else {
                await db.retryQueueStore.incrementRetry(existing[0].id!)
              }
            } catch (dbError) {
              console.error('[useRemoteCalling] Failed to persist retry item:', dbError)
            }
          }

          // Exponential backoff: min(2^(retryCount-1), 16) seconds
          // 1st retry: 1s, 2nd: 2s, 3rd: 4s, 4th: 8s, 5th+: 16s
          const backoffMs = Math.min(Math.pow(2, retryCount - 1), maxBackoffMs / 1000) * 1000
          console.warn(
            `[useRemoteCalling] agent_resume failed, retrying in ${backoffMs}ms (attempt ${retryCount})`,
            error,
          )
          await new Promise((resolve) => setTimeout(resolve, backoffMs))
        }
      }
    } catch (error) {
      console.error('[useRemoteCalling] Failed to resolve calling:', error)
      throw error
    } finally {
      isSubmitting.value = false
    }
  }

  function dismiss() {
    pendingCallings.value = []
    activeIndex.value = 0
    updateCurrentCalling()
  }

  return {
    pendingCallings,
    activeIndex,
    currentCalling,
    resolveCalling,
    nextCalling,
    prevCalling,
    dismiss,
    isSubmitting,
  }
}
