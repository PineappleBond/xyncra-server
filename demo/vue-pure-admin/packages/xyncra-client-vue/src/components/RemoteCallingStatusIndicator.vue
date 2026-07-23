<template>
  <div v-if="executingCalls.size > 0" class="rc-status-indicator">
    <el-popover placement="top-end" :width="300" trigger="hover">
      <template #reference>
        <div class="rc-status-badge">
          <el-icon class="rc-status-spin"><Loading /></el-icon>
          <span>{{ executingCalls.size }} 个函数执行中</span>
        </div>
      </template>
      <div class="rc-status-list">
        <div v-for="[id, call] in executingCalls" :key="id" class="rc-status-item">
          <div class="rc-status-info">
            <span class="rc-status-method">{{ call.method }}</span>
            <span class="rc-status-duration">{{ getDuration(call.startedAt) }}</span>
          </div>
          <el-button
            type="danger"
            size="small"
            text
            @click="$emit('cancel', id)"
          >
            取消
          </el-button>
        </div>
      </div>
    </el-popover>
  </div>
</template>

<script setup lang="ts">
import { Loading } from '@element-plus/icons-vue'
import type { ExecutingCall } from '../composables/useRemoteCallingRouter'

defineProps<{
  executingCalls: Map<string, ExecutingCall>
}>()

defineEmits<{
  cancel: [id: string]
}>()

function getDuration(startedAt: number): string {
  const seconds = Math.floor((Date.now() - startedAt) / 1000)
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m${seconds % 60}s`
}
</script>

<style scoped>
.rc-status-indicator {
  position: fixed;
  bottom: 90px;
  right: 24px;
  z-index: 998;
}

.rc-status-badge {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  background: var(--el-color-primary-light-9);
  border: 1px solid var(--el-color-primary-light-5);
  border-radius: 20px;
  font-size: 13px;
  color: var(--el-color-primary);
  cursor: pointer;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

.rc-status-spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.rc-status-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.rc-status-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 0;
}

.rc-status-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.rc-status-method {
  font-size: 13px;
  font-weight: 500;
  font-family: monospace;
}

.rc-status-duration {
  font-size: 11px;
  color: var(--el-text-color-secondary);
}
</style>
