<template>
  <div v-if="status !== 'idle'" class="agent-status-indicator">
    <el-badge :type="badgeType" is-dot class="status-badge">
      <span class="status-text">{{ statusLabel }}</span>
    </el-badge>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useXyncra } from '../composables/useXyncra'

const props = defineProps<{
  conversationId?: string | null
}>()

const { eventEmitter } = useXyncra()

const status = ref<string>('idle')
let statusTimer: ReturnType<typeof setTimeout> | null = null

const STATUS_TIMEOUT = 30000 // Reset to idle after 30s of no updates

const statusLabel = computed(() => {
  switch (status.value) {
    case 'thinking': return '思考中...'
    case 'generating': return '生成中...'
    case 'tool_calling': return '调用工具中...'
    case 'asking_user': return '等待确认'
    case 'timeout': return '超时'
    default: return ''
  }
})

const badgeType = computed(() => {
  switch (status.value) {
    case 'thinking': return 'warning'
    case 'generating': return 'primary'
    case 'tool_calling': return 'info'
    case 'asking_user': return 'danger'
    case 'timeout': return 'danger'
    default: return 'info'
  }
})

function resetStatus() {
  if (statusTimer) clearTimeout(statusTimer)
  statusTimer = setTimeout(() => {
    status.value = 'idle'
  }, STATUS_TIMEOUT)
}

onMounted(() => {
  const unsubStatus = eventEmitter.on('agent:status', ({ conversationId, status: newStatus }) => {
    if (props.conversationId && conversationId !== props.conversationId) return
    status.value = newStatus
    resetStatus()
  })

  const unsubThinking = eventEmitter.on('agent:thinking', ({ conversationId, isTyping, isAgent }) => {
    if (props.conversationId && conversationId !== props.conversationId) return
    if (isAgent) {
      status.value = isTyping ? 'thinking' : 'idle'
      if (isTyping) resetStatus()
    }
  })

  onUnmounted(() => {
    unsubStatus()
    unsubThinking()
    if (statusTimer) clearTimeout(statusTimer)
  })
})

// Reset status when conversation changes
watch(() => props.conversationId, () => {
  status.value = 'idle'
  if (statusTimer) clearTimeout(statusTimer)
})
</script>

<style scoped>
.agent-status-indicator {
  display: inline-flex;
  align-items: center;
  padding: 2px 0;
}
.status-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.status-text {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>
