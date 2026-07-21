<template>
  <div class="xyncra-connection-status" :class="statusClass">
    <span class="status-dot" />
    <span class="status-label">{{ statusLabel }}</span>
    <span v-if="reconnectInfo" class="reconnect-info">
      ({{ reconnection.attempt.value }}/{{ reconnection.maxRetries.value }})
      <span v-if="reconnection.nextRetryIn.value > 0" class="reconnect-countdown">
        {{ reconnection.nextRetryIn.value }}s
      </span>
    </span>
    <el-button
      v-if="showReconnectButton"
      class="reconnect-btn"
      type="primary"
      text
      size="small"
      @click="$emit('reconnect')"
    >
      重新连接
    </el-button>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useXyncra } from '../composables/useXyncra'
import type { ConnectionStatus } from '../plugin'

const props = defineProps<{
  status: ConnectionStatus
}>()

defineEmits<{
  reconnect: []
}>()

const { reconnection } = useXyncra()

const reconnectInfo = computed(() =>
  reconnection.isReconnecting.value
)

const showReconnectButton = computed(() =>
  props.status === 'disconnected' && !reconnection.isReconnecting.value
)

const statusClass = computed(() => ({
  'status-connected': props.status === 'connected',
  'status-syncing': props.status === 'syncing' || props.status === 'connecting',
  'status-disconnected': props.status === 'disconnected',
  'status-reconnecting': reconnection.isReconnecting.value,
}))

const statusLabel = computed(() => {
  if (reconnection.isReconnecting.value) {
    return '正在重新连接...'
  }
  switch (props.status) {
    case 'connected': return '已连接'
    case 'syncing': return '同步中'
    case 'connecting': return '连接中'
    case 'disconnected': return '未连接'
    default: return '未知'
  }
})
</script>

<style scoped>
.xyncra-connection-status {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 0;
  flex-shrink: 0;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background-color: var(--el-color-info);
  flex-shrink: 0;
}

.status-connected .status-dot {
  background-color: var(--el-color-success);
}

.status-syncing .status-dot,
.status-reconnecting .status-dot {
  background-color: var(--el-color-warning);
  animation: pulse 1.5s ease-in-out infinite;
}

.status-disconnected .status-dot {
  background-color: var(--el-color-danger);
}

.status-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.reconnect-info {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}

.reconnect-countdown {
  font-variant-numeric: tabular-nums;
}

.reconnect-btn {
  margin-left: auto;
  font-size: 12px;
  padding: 2px 8px;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}
</style>
