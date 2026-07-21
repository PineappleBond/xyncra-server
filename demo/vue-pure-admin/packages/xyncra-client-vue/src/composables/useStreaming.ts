import { ref, onMounted, onUnmounted, type Ref } from 'vue'
import { useXyncra } from './useXyncra'

export interface UseStreamingReturn {
  streamingText: Ref<string>
  isStreaming: Ref<boolean>
  currentStreamID: Ref<string | null>
  agentThinking: Ref<boolean>
  agentStatus: Ref<string | null>
}

const STREAM_CLEANUP_DELAY = 500

export function useStreaming(): UseStreamingReturn {
  const { eventEmitter } = useXyncra()

  const streamingText = ref('')
  const isStreaming = ref(false)
  const currentStreamID = ref<string | null>(null)
  const agentThinking = ref(false)
  const agentStatus = ref<string | null>(null)

  // Mutable buffers — not tied to the Vue reactivity cycle.
  const accumulated = new Map<string, string>()
  let activeStreamID: string | null = null
  let cleanupTimer: ReturnType<typeof setTimeout> | null = null
  // Pending text to flush on the next animation frame.
  let pendingText = ''
  // rAF handle for cancellation.
  let rafHandle: number | null = null

  /**
   * Flush pending text to Vue reactive state on the next animation frame.
   * Coalesces rapid stream:text events into a single DOM update.
   */
  function scheduleUpdate() {
    if (rafHandle !== null) return // Already scheduled.
    rafHandle = requestAnimationFrame(() => {
      rafHandle = null
      streamingText.value = pendingText
    })
  }

  function scheduleCleanup(streamId: string) {
    if (cleanupTimer) clearTimeout(cleanupTimer)
    cleanupTimer = setTimeout(() => {
      accumulated.delete(streamId)
      if (activeStreamID === streamId) {
        activeStreamID = null
        currentStreamID.value = null
        isStreaming.value = false
        pendingText = ''
        streamingText.value = ''
      }
    }, STREAM_CLEANUP_DELAY)
  }

  onMounted(() => {
    const unsubText = eventEmitter.on('stream:text', ({ streamId, text }) => {
      if (cleanupTimer) {
        clearTimeout(cleanupTimer)
        cleanupTimer = null
      }

      //const updated = (accumulated.get(streamId) ?? '') + text
      const updated = text
      accumulated.set(streamId, updated)

      activeStreamID = streamId
      currentStreamID.value = streamId
      isStreaming.value = true

      pendingText = updated
      scheduleUpdate()
    })

    const unsubDone = eventEmitter.on('stream:done', ({ streamId, text }) => {
      isStreaming.value = false
      accumulated.set(streamId, text)
      scheduleCleanup(streamId)
    })

    const unsubThinking = eventEmitter.on('agent:thinking', ({ isTyping }) => {
      agentThinking.value = isTyping
    })

    const unsubStatus = eventEmitter.on('agent:status', ({ status }) => {
      agentStatus.value = status || null
    })

    onUnmounted(() => {
      unsubText()
      unsubDone()
      unsubThinking()
      unsubStatus()
      // Cancel pending rAF.
      if (rafHandle !== null) {
        cancelAnimationFrame(rafHandle)
        rafHandle = null
      }
      if (cleanupTimer) clearTimeout(cleanupTimer)
    })
  })

  return { streamingText, isStreaming, currentStreamID, agentThinking, agentStatus }
}
