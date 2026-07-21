<template>
  <div class="xyncra-chat-panel">
    <div
      class="messages-container"
      ref="messagesContainer"
      @scroll="onScroll"
    >
      <!-- Load more trigger -->
      <div v-if="hasMoreMessages && displayedMessages.length > 0" class="load-more-trigger">
        <el-button
          text
          size="small"
          :loading="loadingMore"
          @click="loadMore"
        >
          加载更多消息
        </el-button>
      </div>

      <!-- Empty state -->
      <el-empty
        v-if="displayedMessages.length === 0 && !isStreaming && !agentThinking"
        description="还没有消息，发送一条开始对话吧"
        :image-size="80"
        class="empty-state"
      />

      <!-- Messages -->
      <template v-for="msg in displayedMessages" :key="msg.id">
        <div
          class="message-item"
          :class="msg.senderId === 'user' ? 'message-user' : 'message-ai'"
        >
          <div class="message-bubble" :class="msg.senderId === 'user' ? 'bubble-user' : 'bubble-ai'">
            <div
              v-if="msg.senderId !== 'user'"
              class="message-content markdown-body"
              v-html="renderMarkdown(msg.content)"
            />
            <div v-else class="message-content">{{ msg.content }}</div>
          </div>
          <div class="message-time" :class="msg.senderId === 'user' ? 'time-right' : 'time-left'">
            {{ formatRelativeTime(msg.createdAt) }}
          </div>
        </div>
        <!-- Function calls rendered after AI messages -->
        <FunctionCallDisplay
          v-if="msg.senderId !== 'user' && getFunctionCalls(msg.id).length > 0"
          :key="'fc-' + msg.id"
          :calls="getFunctionCalls(msg.id)"
        />
      </template>

      <!-- Streaming message (appended to last AI message or as new bubble) -->
      <div v-if="isStreaming && streamingText" class="message-item message-ai">
        <div class="message-bubble bubble-ai streaming">
          <div class="message-content markdown-body" v-html="renderMarkdown(streamingText)" />
          <span class="typing-cursor" />
        </div>
      </div>

      <!-- Agent thinking indicator -->
      <div v-if="agentThinking && !isStreaming" class="message-item message-ai">
        <div class="message-bubble bubble-ai thinking-bubble">
          <div class="thinking-indicator">
            <span class="thinking-dot" />
            <span class="thinking-dot" />
            <span class="thinking-dot" />
          </div>
          <span class="thinking-text">{{ agentStatus || 'AI 正在思考...' }}</span>
        </div>
      </div>

      <!-- Scroll anchor for auto-scroll -->
      <div ref="scrollAnchor" class="scroll-anchor" />
    </div>

    <div class="input-area">
      <el-input
        v-model="inputText"
        placeholder="输入消息..."
        :disabled="!conversationId"
        @keyup.enter="handleSend"
      />
      <el-button
        type="primary"
        @click="handleSend"
        :disabled="!inputText.trim() || !conversationId"
      >
        发送
      </el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, nextTick, toRef, onMounted, onUnmounted } from 'vue'
import { Marked } from 'marked'
import { markedHighlight } from 'marked-highlight'
import hljs from 'highlight.js'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import 'dayjs/locale/zh-cn'
import 'highlight.js/styles/github.css'
import { useMessages } from '../composables/useMessages'
import { useStreaming } from '../composables/useStreaming'
import { useXyncra } from '../composables/useXyncra'
import { sanitizeHtml } from '../utils/sanitize'
import FunctionCallDisplay from './FunctionCallDisplay.vue'
import type { FunctionCallInfo } from './FunctionCallDisplay.vue'

dayjs.extend(relativeTime)
dayjs.locale('zh-cn')

const props = defineProps<{
  conversationId: string | null
}>()

const inputText = ref('')
const messagesContainer = ref<HTMLElement | null>(null)
const scrollAnchor = ref<HTMLElement | null>(null)
const isNearBottom = ref(true)

// useMessages accepts a Ref<string | null> and automatically loads messages
// when conversationId changes, and subscribes to real-time message events.
const conversationIdRef = toRef(props, 'conversationId')
const { messages, send, loadMore, hasMore: hasMoreMessages, loadingMore } = useMessages({ conversationId: conversationIdRef })
const { streamingText, isStreaming, agentThinking, agentStatus } = useStreaming()
const { eventEmitter } = useXyncra()

// Function calls map (message id -> function calls)
const functionCallsMap = ref<Map<string, FunctionCallInfo[]>>(new Map())

// Track the last AI message ID for associating function calls
let lastAIMessageId: string | null = null

// Update lastAIMessageId when messages change
watch(messages, (newMsgs) => {
  for (let i = newMsgs.length - 1; i >= 0; i--) {
    if (newMsgs[i].senderId !== 'user') {
      lastAIMessageId = newMsgs[i].id
      break
    }
  }
}, { immediate: true })

// Listen for function call events and populate functionCallsMap
let unsubFunctionCall: (() => void) | null = null

onMounted(() => {
  unsubFunctionCall = eventEmitter.on('function:called', (payload) => {
    // Associate function calls with the current conversation
    if (payload.conversationId !== props.conversationId) return

    // Find the most recent AI message to attach function calls to
    const targetId = lastAIMessageId
    if (!targetId) return

    const existing = functionCallsMap.value.get(targetId) ?? []
    const callInfo: FunctionCallInfo = {
      name: payload.name,
      params: payload.args ? tryParseJSON(payload.args) : undefined,
      result: payload.result ? tryParseJSON(payload.result) : undefined,
      error: payload.error || undefined,
    }

    if (payload.isDone) {
      // Update the last pending call with result
      const updated = [...existing]
      const pendingIdx = updated.findIndex(c => c.name === payload.name && !c.result && !c.error)
      if (pendingIdx >= 0) {
        updated[pendingIdx] = callInfo
      } else {
        updated.push(callInfo)
      }
      functionCallsMap.value = new Map(functionCallsMap.value).set(targetId, updated)
    } else {
      // Add a pending call
      functionCallsMap.value = new Map(functionCallsMap.value).set(targetId, [...existing, callInfo])
    }
  })
})

onUnmounted(() => {
  unsubFunctionCall?.()
})

function tryParseJSON(str: string): unknown {
  try {
    return JSON.parse(str)
  } catch {
    return str
  }
}

function getFunctionCalls(messageId: string): FunctionCallInfo[] {
  return functionCallsMap.value.get(messageId) ?? []
}

// Configure marked with highlight.js
const marked = new Marked(
  markedHighlight({
    langPrefix: 'hljs language-',
    highlight(code: string, lang: string) {
      if (lang && hljs.getLanguage(lang)) {
        try {
          return hljs.highlight(code, { language: lang }).value
        } catch {
          // fall through
        }
      }
      return hljs.highlightAuto(code).value
    },
  }),
  {
    gfm: true,
    breaks: true,
  },
)

function renderMarkdown(text: string): string {
  if (!text) return ''
  try {
    const html = marked.parse(text) as string
    // Sanitize HTML to prevent XSS attacks
    return sanitizeHtml(html)
  } catch {
    return text
  }
}

function formatRelativeTime(dateStr: string | undefined): string {
  if (!dateStr) return ''
  const date = dayjs(dateStr)
  const now = dayjs()
  const diffMinutes = now.diff(date, 'minute')

  if (diffMinutes < 1) return '刚刚'
  if (diffMinutes < 60) return `${diffMinutes}分钟前`

  const diffHours = now.diff(date, 'hour')
  if (diffHours < 24) return `${diffHours}小时前`

  const diffDays = now.diff(date, 'day')
  if (diffDays < 7) return `${diffDays}天前`

  return date.format('MM-DD HH:mm')
}

// Messages are now a flat list from useMessages (no Map lookup needed)
const displayedMessages = messages

// Auto-scroll only when user is near bottom
watch(displayedMessages, () => {
  if (isNearBottom.value) scrollToBottom()
}, { flush: 'post' })

watch(isStreaming, () => {
  if (isNearBottom.value) scrollToBottom()
}, { flush: 'post' })

watch(streamingText, () => {
  if (isNearBottom.value) scrollToBottom()
}, { flush: 'post' })

function scrollToBottom() {
  nextTick(() => {
    if (scrollAnchor.value) {
      scrollAnchor.value.scrollIntoView({ behavior: 'smooth' })
    } else if (messagesContainer.value) {
      messagesContainer.value.scrollTop = messagesContainer.value.scrollHeight
    }
  })
}

function onScroll() {
  if (!messagesContainer.value) return
  const { scrollTop, scrollHeight, clientHeight } = messagesContainer.value
  isNearBottom.value = scrollHeight - scrollTop - clientHeight < 150
}

// loadMore is now provided by useMessages directly

async function handleSend() {
  const text = inputText.value.trim()
  if (!text || !props.conversationId) return
  inputText.value = ''
  isNearBottom.value = true
  await send(text)
}

// Reset local state when conversation changes
// (hasMore and messages are now managed by useMessages)
watch(() => props.conversationId, () => {
  functionCallsMap.value = new Map()
  isNearBottom.value = true
  nextTick(scrollToBottom)
})
</script>

<style scoped>
.xyncra-chat-panel {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
}

.messages-container {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  background-color: var(--el-bg-color-page, #f5f5f5);
}

.empty-state {
  margin: auto;
}

.load-more-trigger {
  display: flex;
  justify-content: center;
  padding: 4px 0;
}

.scroll-anchor {
  height: 1px;
  flex-shrink: 0;
}

/* Message layout */
.message-item {
  display: flex;
  flex-direction: column;
  max-width: 85%;
}

.message-user {
  align-self: flex-end;
  align-items: flex-end;
}

.message-ai {
  align-self: flex-start;
  align-items: flex-start;
}

/* Bubble styles */
.message-bubble {
  padding: 10px 14px;
  border-radius: 12px;
  font-size: 14px;
  line-height: 1.6;
  word-break: break-word;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
}

.bubble-user {
  background-color: var(--el-color-primary);
  color: #fff;
  border-bottom-right-radius: 4px;
}

.bubble-ai {
  background-color: var(--el-bg-color, #fff);
  color: var(--el-text-color-primary);
  border-bottom-left-radius: 4px;
  border: 1px solid var(--el-border-color-lighter, #e4e7ed);
}

/* Timestamp */
.message-time {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
  margin-top: 4px;
  padding: 0 4px;
}

.time-right {
  text-align: right;
}

.time-left {
  text-align: left;
}

/* Streaming animation */
.streaming {
  position: relative;
}

.typing-cursor {
  display: inline-block;
  width: 2px;
  height: 14px;
  background-color: var(--el-text-color-primary);
  margin-left: 2px;
  vertical-align: text-bottom;
  animation: cursor-blink 1s infinite;
}

@keyframes cursor-blink {
  0%, 50% { opacity: 1; }
  51%, 100% { opacity: 0; }
}

/* Thinking indicator */
.thinking-bubble {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 16px;
}

.thinking-indicator {
  display: flex;
  gap: 4px;
}

.thinking-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background-color: var(--el-text-color-secondary);
  animation: thinking-bounce 1.4s ease-in-out infinite;
}

.thinking-dot:nth-child(2) {
  animation-delay: 0.2s;
}

.thinking-dot:nth-child(3) {
  animation-delay: 0.4s;
}

@keyframes thinking-bounce {
  0%, 80%, 100% {
    transform: scale(0.6);
    opacity: 0.4;
  }
  40% {
    transform: scale(1);
    opacity: 1;
  }
}

.thinking-text {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

/* Markdown content styles */
.markdown-body :deep(p) {
  margin: 0 0 8px;
}

.markdown-body :deep(p:last-child) {
  margin-bottom: 0;
}

.markdown-body :deep(code) {
  background-color: rgba(0, 0, 0, 0.06);
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 13px;
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
}

.bubble-user .markdown-body :deep(code) {
  background-color: rgba(255, 255, 255, 0.2);
}

.markdown-body :deep(pre) {
  background-color: var(--el-fill-color-light, #f5f7fa);
  border-radius: 6px;
  padding: 12px;
  overflow-x: auto;
  margin: 8px 0;
}

.markdown-body :deep(pre code) {
  background: none;
  padding: 0;
  font-size: 13px;
  line-height: 1.5;
}

.markdown-body :deep(ul),
.markdown-body :deep(ol) {
  padding-left: 20px;
  margin: 4px 0;
}

.markdown-body :deep(blockquote) {
  border-left: 3px solid var(--el-border-color);
  padding-left: 12px;
  margin: 8px 0;
  color: var(--el-text-color-secondary);
}

.markdown-body :deep(table) {
  border-collapse: collapse;
  margin: 8px 0;
}

.markdown-body :deep(th),
.markdown-body :deep(td) {
  border: 1px solid var(--el-border-color);
  padding: 6px 10px;
  font-size: 13px;
}

.markdown-body :deep(th) {
  background-color: var(--el-fill-color-light);
}

.markdown-body :deep(h1),
.markdown-body :deep(h2),
.markdown-body :deep(h3),
.markdown-body :deep(h4) {
  margin: 12px 0 6px;
  font-weight: 600;
}

.markdown-body :deep(a) {
  color: var(--el-color-primary);
  text-decoration: none;
}

.markdown-body :deep(a:hover) {
  text-decoration: underline;
}

/* Input area */
.input-area {
  display: flex;
  gap: 8px;
  padding: 12px 16px;
  border-top: 1px solid var(--el-border-color-light, #f0f0f0);
  flex-shrink: 0;
  background-color: var(--el-bg-color);
}

.input-area .el-input {
  flex: 1;
}
</style>
