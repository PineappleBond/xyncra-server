import { ref, onMounted, onUnmounted, watch, type Ref } from 'vue'
import { useXyncra } from './useXyncra'

/**
 * A pending HITL question displayed to the user.
 */
export interface HITLQuestion {
  /** The user/agent ID that raised the question. */
  userId: string
  /** The conversation this question belongs to. */
  conversationId: string
  /** The question text / reason from the agent. */
  question: string
  /** Opaque question id used to resume the agent. */
  questionId?: string
  /** Checkpoint id required by the agent_resume RPC. */
  checkpointId?: string
  /** Interrupt id required by the agent_resume RPC. */
  interruptId?: string
}

export interface UseHITLReturn {
  /** Array of pending HITL questions, or empty array if none. */
  pendingQuestions: Ref<HITLQuestion[]>
  /** Index of the currently active question in the queue. */
  activeIndex: Ref<number>
  /** The currently active question. */
  currentQuestion: Ref<HITLQuestion | null>
  /**
   * Answer all HITL questions. Sends `agent_resume` RPC for each question.
   * After all answers are sent, clears the queue.
   */
  answerAll: (answers: Map<string, string>) => Promise<void>
  /** Answer a single question by index. */
  answerSingle: (index: number, answer: string) => Promise<void>
  /** Navigate to the next question in the queue. */
  nextQuestion: () => void
  /** Navigate to the previous question in the queue. */
  prevQuestion: () => void
  /** Dismiss all pending questions without answering. */
  dismiss: () => void
  /** Whether answers are currently being submitted. */
  isSubmitting: Ref<boolean>
}

/**
 * Manages HITL (Human-in-the-Loop) questions in batch mode.
 *
 * All pending questions are fetched and displayed together. The user can
 * answer all questions at once or navigate through them individually.
 * After submission, clears the queue; new questions arrive via events.
 */
export function useHITL(conversationId?: string | Ref<string | null>): UseHITLReturn {
  const { client, eventEmitter } = useXyncra()
  const pendingQuestions = ref<HITLQuestion[]>([])
  const activeIndex = ref(0)
  const isSubmitting = ref(false)
  const isFetching = ref(false)

  const currentQuestion = ref<HITLQuestion | null>(null)

  // Keep currentQuestion in sync with activeIndex
  function updateCurrentQuestion() {
    const idx = activeIndex.value
    currentQuestion.value = pendingQuestions.value[idx] ?? null
  }

  // Resolve conversationId if it's a Ref
  function getConversationId(): string | undefined {
    if (!conversationId) return undefined
    if (typeof conversationId === 'string') return conversationId
    return conversationId.value ?? undefined
  }

  // Fetch all pending questions from the server
  async function fetchQuestions() {
    const convId = getConversationId()
    if (!client || !convId || isFetching.value) return

    isFetching.value = true
    try {
      const result = await client.getConversation(convId)
      const conv = result.conversation as any

      if (result.questions && result.questions.length > 0) {
        const questions: HITLQuestion[] = result.questions
          .filter((q: any) => q.status === 'pending')
          .map((q: any) => ({
            userId: conv.user_id2 ?? conv.userId2 ?? '',
            conversationId: conv.id,
            question: q.question_text,
            questionId: q.id,
            checkpointId: q.checkpoint_id,
            interruptId: q.interrupt_id,
          }))

        pendingQuestions.value = questions
        // Reset index if out of bounds
        if (activeIndex.value >= questions.length) {
          activeIndex.value = Math.max(0, questions.length - 1)
        }
        updateCurrentQuestion()
      } else {
        pendingQuestions.value = []
        activeIndex.value = 0
        updateCurrentQuestion()
      }
    } catch (error) {
      console.error('[useHITL] Failed to fetch questions:', error)
    } finally {
      isFetching.value = false
    }
  }

  // Listen for new HITL events and re-fetch
  function onHitlQuestion() {
    fetchQuestions()
  }

  onMounted(() => {
    const unsub = eventEmitter.on('hitl:question', onHitlQuestion)
    onUnmounted(() => unsub())
  })

  // Fetch on conversationId change
  const convIdRef = typeof conversationId === 'string' ? ref(conversationId) : conversationId
  if (convIdRef) {
    watch(convIdRef, () => {
      fetchQuestions()
    }, { immediate: true })
  }

  // Navigate questions
  function nextQuestion() {
    if (activeIndex.value < pendingQuestions.value.length - 1) {
      activeIndex.value++
      updateCurrentQuestion()
    }
  }

  function prevQuestion() {
    if (activeIndex.value > 0) {
      activeIndex.value--
      updateCurrentQuestion()
    }
  }

  // Answer all questions in batch
  async function answerAll(answers: Map<string, string>): Promise<void> {
    if (!client) throw new Error('client not initialized')
    if (pendingQuestions.value.length === 0) throw new Error('no pending questions to answer')

    isSubmitting.value = true

    try {
      for (const q of pendingQuestions.value) {
        const answerText = answers.get(q.questionId ?? '')
        if (!answerText) {
          throw new Error(`No answer provided for question ${q.questionId}`)
        }

        // Get checkpoint/interrupt IDs from server if not available locally
        let checkpointId = q.checkpointId
        let interruptId = q.interruptId
        if ((!checkpointId || !interruptId) && q.conversationId) {
          try {
            const conv = await client.getConversation(q.conversationId)
            const storedQ = conv.questions?.find((item: any) => item.id === q.questionId)
            if (storedQ) {
              checkpointId = storedQ.checkpoint_id
              interruptId = storedQ.interrupt_id
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
        })
      }

      // Clear questions after successful submission
      pendingQuestions.value = []
      activeIndex.value = 0
      updateCurrentQuestion()
    } catch (error) {
      console.error('[useHITL] Failed to submit answers:', error)
      throw error
    } finally {
      isSubmitting.value = false
    }
  }

  // Answer a single question
  async function answerSingle(index: number, answer: string): Promise<void> {
    if (!client) throw new Error('client not initialized')
    const q = pendingQuestions.value[index]
    if (!q) throw new Error(`No question at index ${index}`)

    isSubmitting.value = true

    try {
      let checkpointId = q.checkpointId
      let interruptId = q.interruptId
      if ((!checkpointId || !interruptId) && q.conversationId) {
        try {
          const conv = await client.getConversation(q.conversationId)
          const storedQ = conv.questions?.find((item: any) => item.id === q.questionId)
          if (storedQ) {
            checkpointId = storedQ.checkpoint_id
            interruptId = storedQ.interrupt_id
          }
        } catch {
          // Fall back
        }
      }

      await client.call('agent_resume', {
        conversation_id: q.conversationId,
        question_id: q.questionId,
        checkpoint_id: checkpointId ?? '',
        interrupt_id: interruptId ?? '',
        agent_id: q.userId,
        answer,
      })

      // Remove answered question from queue
      pendingQuestions.value = pendingQuestions.value.filter((_, i) => i !== index)
      if (activeIndex.value >= pendingQuestions.value.length) {
        activeIndex.value = Math.max(0, pendingQuestions.value.length - 1)
      }
      updateCurrentQuestion()
    } catch (error) {
      console.error('[useHITL] Failed to submit answer:', error)
      throw error
    } finally {
      isSubmitting.value = false
    }
  }

  function dismiss() {
    pendingQuestions.value = []
    activeIndex.value = 0
    updateCurrentQuestion()
  }

  return {
    pendingQuestions,
    activeIndex,
    currentQuestion,
    answerAll,
    answerSingle,
    nextQuestion,
    prevQuestion,
    dismiss,
    isSubmitting,
  }
}
