<template>
  <div class="xyncra-tool-calling" :class="statusClass">
    <!-- Header: tool name left, duration + icon right -->
    <div class="tool-header">
      <span class="tool-name">{{ toolName }}</span>
      <div class="tool-header-right">
        <span v-if="durationMs > 0" class="tool-duration">{{ formatDuration(durationMs) }}</span>
        <el-icon v-if="status === 'executing'" class="tool-icon spinning">
          <Loading />
        </el-icon>
        <el-icon v-else-if="status === 'completed'" class="tool-icon success">
          <CircleCheck />
        </el-icon>
        <el-icon v-else-if="status === 'failed'" class="tool-icon error">
          <CircleClose />
        </el-icon>
        <el-icon v-else class="tool-icon">
          <Tools />
        </el-icon>
      </div>
    </div>

    <hr class="tool-divider" />

    <!-- IN: input parameters -->
    <div class="tool-section">
      <div class="section-label">IN</div>
      <pre class="tool-content">{{ args || '(empty)' }}</pre>
    </div>

    <hr class="tool-divider" />

    <!-- OUT: result -->
    <div class="tool-section">
      <div class="section-label">OUT</div>
      <pre v-if="error && status === 'failed'" class="tool-content error-content">{{ error }}</pre>
      <pre v-else class="tool-content">{{ result || '(pending)' }}</pre>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Loading, CircleCheck, CircleClose, Tools } from '@element-plus/icons-vue'

interface ToolCallingPayload {
  name: string
  args?: string
  status: 'executing' | 'completed' | 'failed'
  result?: string
  error?: string
  duration_ms?: number
}

const props = defineProps<{
  content: string
}>()

// Parse payload with graceful degradation
const payload = computed<ToolCallingPayload | null>(() => {
  if (!props.content) return null
  try {
    return JSON.parse(props.content) as ToolCallingPayload
  } catch {
    return null
  }
})

// Extract fields with fallbacks
const toolName = computed(() => payload.value?.name || 'Unknown Tool')
const status = computed(() => payload.value?.status || 'executing')
const args = computed(() => payload.value?.args || '')
const result = computed(() => payload.value?.result || '')
const error = computed(() => payload.value?.error || '')
const durationMs = computed(() => payload.value?.duration_ms || 0)

// Status-based styling
const statusClass = computed(() => {
  switch (status.value) {
    case 'executing': return 'status-executing'
    case 'completed': return 'status-completed'
    case 'failed': return 'status-failed'
    default: return ''
  }
})

// Format duration for display
function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}
</script>

<style scoped>
.xyncra-tool-calling {
  padding: 12px;
  background-color: var(--el-fill-color-light, #fafafa);
  border-radius: 8px;
  margin: 8px 0;
  border-left: 3px solid var(--el-border-color);
}

.status-executing {
  border-left-color: var(--el-color-primary);
}

.status-completed {
  border-left-color: var(--el-color-success);
}

.status-failed {
  border-left-color: var(--el-color-danger);
}

.tool-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.tool-header-right {
  display: flex;
  align-items: center;
  gap: 6px;
}

.tool-icon {
  font-size: 16px;
}

.tool-icon.spinning {
  color: var(--el-color-primary);
  animation: spin 1s linear infinite;
}

.tool-icon.success {
  color: var(--el-color-success);
}

.tool-icon.error {
  color: var(--el-color-danger);
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.tool-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.tool-duration {
  font-size: 11px;
  color: var(--el-text-color-secondary);
}

.tool-divider {
  border: none;
  border-top: 1px solid var(--el-border-color-lighter, #e4e7ed);
  margin: 8px 0;
}

.tool-section {
  /* no extra margin needed, divider handles spacing */
}

.section-label {
  font-size: 11px;
  font-weight: 500;
  color: var(--el-text-color-secondary);
  margin-bottom: 4px;
}

.tool-content {
  font-size: 12px;
  background-color: var(--el-fill-color, #f5f5f5);
  padding: 8px;
  border-radius: 4px;
  overflow-x: auto;
  overflow-y: auto;
  max-height: 150px;
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
}

.tool-content.error-content {
  color: var(--el-color-danger);
  background-color: var(--el-color-danger-light-9);
}
</style>
