/**
 * @packageDocumentation
 * useRemoteCallingRouter — core routing composable for RemoteCalling (D-137).
 *
 * Subscribes to `remote_calling` events from the eventEmitter and dispatches
 * each call to the appropriate handler via FunctionRegistry. If no handler is
 * found, the call is resolved with an error ("Function not found").
 *
 * Features:
 *   - Deduplication: ignores already-processed or currently-executing call IDs.
 *   - DeviceID filtering: skips calls targeted at a different device.
 *   - Execution tracking: exposes executingCalls for status indicators.
 *   - Cancellation: allows cancelling in-flight calls.
 *   - Retry with exponential backoff: failed agent_resume calls are retried
 *     indefinitely (1s/2s/4s/8s/16s cap) and persisted to IndexedDB for
 *     background retry on page reload.
 *   - Per-conversation serial queue (D-138): ensures RemoteCallings within
 *     the same conversation execute sequentially, while different
 *     conversations can run in parallel.
 */

import { ref, onMounted, onUnmounted, type Ref } from 'vue'
import { useXyncra } from './useXyncra'

/**
 * Tracks a currently-executing remote calling.
 */
export interface ExecutingCall {
  /** RemoteCalling ID. */
  id: string
  /** Function method name. */
  method: string
  /** Conversation ID. */
  conversationId: string
  /** Agent (user) ID that initiated the call. */
  agentId: string
  /** Timestamp when execution started (ms). */
  startedAt: number
  /** Whether the call has been cancelled by the user. */
  cancelled: boolean
}

/**
 * A queued task waiting to be executed in the serial queue.
 */
interface QueueItem {
  /** RemoteCalling ID. */
  id: string
  /** Conversation ID for queue isolation. */
  conversationId: string
  /** The async work to execute. */
  execute: () => Promise<void>
  /** Set to true if this item was cancelled before execution. */
  cancelled: boolean
}

export interface UseRemoteCallingRouterReturn {
  /** Map of currently-executing remote callings, keyed by ID. */
  executingCalls: Ref<Map<string, ExecutingCall>>
  /** Cancel an in-flight call by its ID. Also cancels queued (not yet started) calls. */
  cancelCall: (id: string) => void
}

export function useRemoteCallingRouter(): UseRemoteCallingRouterReturn {
  const { client, eventEmitter, registry, db } = useXyncra()
  const executingCalls = ref(new Map<string, ExecutingCall>()) as Ref<Map<string, ExecutingCall>>
  const processedIds = new Set<string>()

  // ---- Per-conversation serial queue (D-138) ----

  /** Queues keyed by conversationId. */
  const conversationQueues = new Map<string, QueueItem[]>()
  /** Set of conversationIds currently being processed. */
  const processingConversations = new Set<string>()

  /**
   * Enqueues a task for serial execution within its conversation.
   * If the conversation queue is idle, starts processing immediately.
   */
  function enqueueTask(item: QueueItem): void {
    const { conversationId } = item

    if (!conversationQueues.has(conversationId)) {
      conversationQueues.set(conversationId, [])
    }
    conversationQueues.get(conversationId)!.push(item)

    // Start processing if not already running for this conversation.
    if (!processingConversations.has(conversationId)) {
      processingConversations.add(conversationId)
      void processQueue(conversationId)
    }
  }

  /**
   * Processes the serial queue for a single conversation.
   * Drains items one by one; exceptions don't block subsequent items.
   * Auto-cleans the queue when empty.
   */
  async function processQueue(conversationId: string): Promise<void> {
    const queue = conversationQueues.get(conversationId)
    if (!queue) {
      processingConversations.delete(conversationId)
      return
    }

    while (queue.length > 0) {
      const item = queue[0]

      // Skip cancelled items.
      if (item.cancelled) {
        queue.shift()
        continue
      }

      try {
        await item.execute()
      } catch (error) {
        // Exception doesn't block subsequent tasks (D-138).
        console.error(`[RemoteCallingRouter] Queue task ${item.id} failed:`, error)
      }

      queue.shift()
    }

    // Cleanup: remove empty queue to prevent memory leaks.
    processingConversations.delete(conversationId)
    if (queue.length === 0) {
      conversationQueues.delete(conversationId)
    }
  }

  /**
   * Finds a QueueItem by RemoteCalling ID across all conversation queues.
   * Returns undefined if not found.
   */
  function findQueueItem(id: string): QueueItem | undefined {
    for (const queue of conversationQueues.values()) {
      const item = queue.find((item) => item.id === id)
      if (item) return item
    }
    return undefined
  }

  // ---- Route calling ----

  async function routeCalling(payload: {
    remoteCallingId: string
    method: string
    params: string
    conversationId: string
    userId: string
    deviceId: string
  }) {
    const { remoteCallingId, method, params, conversationId, userId, deviceId } = payload

    // Deduplication: skip if already processed or currently executing
    if (processedIds.has(remoteCallingId)) return
    if (executingCalls.value.has(remoteCallingId)) return
    processedIds.add(remoteCallingId)

    // DeviceID filtering: skip if targeted at a different device
    if (deviceId && client?.deviceID && deviceId !== client.deviceID) {
      processedIds.delete(remoteCallingId)
      return
    }

    // Parse parameters
    let parsedParams: Record<string, unknown> = {}
    try {
      parsedParams = JSON.parse(params)
    } catch {
      // keep empty
    }

    // Handle nested params: when the server sends client-function-call format,
    // the parsed object contains a `params` field that is a JSON string with the
    // actual handler arguments. Flatten it by merging inner params with outer metadata.
    let handlerParams: Record<string, unknown>
    if (typeof parsedParams.params === 'string') {
      let innerParams: Record<string, unknown> = {}
      try {
        innerParams = JSON.parse(parsedParams.params)
      } catch {
        // keep empty
      }
      // Merge outer metadata (device_id, method, timeout_ms, etc.) with inner params.
      // Inner params take precedence so handler sees its expected shape.
      const { params: _nested, ...outerMeta } = parsedParams
      handlerParams = { ...outerMeta, ...innerParams }
    } else {
      handlerParams = parsedParams
    }

    // Normalize newline characters in question field
    if (typeof handlerParams.question === 'string') {
      handlerParams.question = handlerParams.question
        .replace(/\\r\\n/g, '\r\n')
        .replace(/\\n/g, '\n')
        .replace(/\\r/g, '\r')
    }

    // Inject callingId for handler use
    const enrichedParams = { ...handlerParams, __callingId: remoteCallingId }

    // Look up handler in FunctionRegistry
    const handler = registry.getHandler(method)

    if (handler) {
      // Handler found: enqueue for serial execution per conversation (D-138).
      const queueItem: QueueItem = {
        id: remoteCallingId,
        conversationId,
        cancelled: false,
        execute: async () => {
          const execEntry: ExecutingCall = {
            id: remoteCallingId,
            method,
            conversationId,
            agentId: userId,
            startedAt: Date.now(),
            cancelled: false,
          }
          executingCalls.value.set(remoteCallingId, execEntry)

          try {
            const result = await handler(enrichedParams)
            // Check cancellation after handler completes — if cancelled while
            // executing, skip the resolve to avoid overwriting the cancellation.
            if (!execEntry.cancelled) {
              await resolveRemoteCalling(remoteCallingId, userId, true, JSON.stringify(result))
            }
          } catch (error) {
            if (!execEntry.cancelled) {
              await resolveRemoteCalling(
                remoteCallingId,
                userId,
                false,
                '',
                error instanceof Error ? error.message : String(error),
              )
            }
          } finally {
            executingCalls.value.delete(remoteCallingId)
            processedIds.delete(remoteCallingId)
          }
        },
      }

      enqueueTask(queueItem)
    } else {
      // No handler found: resolve with error (no queue needed)
      const errorMsg = `Function not found: ${method}`
      console.warn(`[RemoteCallingRouter] ${errorMsg}`)
      await resolveRemoteCalling(remoteCallingId, userId, false, '', errorMsg)
      processedIds.delete(remoteCallingId)
    }
  }

  /**
   * Resolve a remote calling by sending agent_resume to the server.
   * On failure, retries with exponential backoff (1s/2s/4s/8s/16s cap)
   * indefinitely. Failed attempts are persisted to IndexedDB for background
   * retry on page reload.
   *
   * Design note (D-137): Infinite retry is intentional — agent_resume must
   * eventually succeed to unblock the agent. The IndexedDB persistence ensures
   * retries survive page reloads. The loop will naturally terminate when the
   * client is shut down (the RPC call will fail with a connection error, and
   * the retry will continue in the background until the page is closed).
   */
  async function resolveRemoteCalling(
    id: string,
    agentId: string,
    success: boolean,
    result: string,
    errorMessage: string = '',
  ): Promise<void> {
    if (!client) return

    let retryCount = 0
    const maxBackoffMs = 16_000

    while (true) {
      try {
        await client.call('agent_resume', {
          id,
          success,
          result: result ?? '',
          error_message: errorMessage ?? '',
          agent_id: agentId,
        })

        // Success: clean up retry queue
        if (db) {
          await db.retryQueueStore.deleteByRemoteCallingId(id)
        }
        return
      } catch (error) {
        retryCount++

        // Persist to retry queue for background retry on page reload
        if (db) {
          try {
            const existing = await db.retryQueueStore.getByRemoteCallingId(id)
            if (existing.length === 0) {
              const backoffMs =
                Math.min(Math.pow(2, retryCount - 1), maxBackoffMs / 1000) * 1000
              await db.retryQueueStore.enqueue({
                remote_calling_id: id,
                success,
                result: result ?? '',
                error_message: errorMessage ?? '',
                agent_id: agentId,
                retry_count: retryCount,
                next_retry_at: new Date(Date.now() + backoffMs),
                created_at: new Date(),
              })
            } else {
              await db.retryQueueStore.incrementRetry(existing[0].id!)
            }
          } catch (dbError) {
            console.error('[RemoteCallingRouter] Failed to persist retry item:', dbError)
          }
        }

        // Exponential backoff: min(2^(retryCount-1), 16) seconds
        const backoffMs =
          Math.min(Math.pow(2, retryCount - 1), maxBackoffMs / 1000) * 1000
        console.warn(
          `[RemoteCallingRouter] agent_resume failed, retrying in ${backoffMs}ms (attempt ${retryCount})`,
          error,
        )
        await new Promise((resolve) => setTimeout(resolve, backoffMs))
      }
    }
  }

  /**
   * Cancel a remote calling by its ID.
   * If the call is queued (not yet started), marks it as cancelled so it
   * will be skipped when its turn comes. If already executing, marks the
   * executing entry as cancelled and resolves with a cancellation error.
   */
  function cancelCall(id: string) {
    // First, check if it's in the queue (not yet started).
    const queueItem = findQueueItem(id)
    if (queueItem) {
      queueItem.cancelled = true
      processedIds.delete(id)
      return
    }

    // Otherwise, it might be currently executing.
    const entry = executingCalls.value.get(id)
    if (!entry) return

    entry.cancelled = true
    executingCalls.value.delete(id)
    processedIds.delete(id)

    resolveRemoteCalling(id, entry.agentId, false, '', 'Cancelled by user').catch(
      (err) => console.error('[RemoteCallingRouter] Failed to resolve cancelled call:', err),
    )
  }

  // Subscribe to remote_calling events
  let unsub: (() => void) | null = null

  onMounted(() => {
    unsub = eventEmitter.on('remote_calling', (payload) => {
      routeCalling({
        remoteCallingId: payload.remoteCallingId,
        method: payload.method,
        params: payload.params,
        conversationId: payload.conversationId,
        userId: payload.userId,
        deviceId: payload.deviceId,
      })
    })
  })

  onUnmounted(() => {
    unsub?.()
  })

  return {
    executingCalls,
    cancelCall,
  }
}
