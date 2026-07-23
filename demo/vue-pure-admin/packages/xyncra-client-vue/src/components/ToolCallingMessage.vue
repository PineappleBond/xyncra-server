<template>
  <div class="xyncra-tool-calling" :class="statusClass">
    <div class="tool-header">
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
      <span class="tool-name">{{ toolName }}</span>
      <span v-if="durationMs > 0" class="tool-duration">{{ formatDuration(durationMs) }}</span>
    </div>

    <div v-if="args" class="tool-section">
      <div class="section-label" @click="argsExpanded = !argsExpanded">
        <span>Arguments</span>
        <el-icon class="expand-icon" :class="{ expanded: argsExpanded }">
          <ArrowDown />
        </el-icon>
      </div>
      <pre v-if="argsExpanded" class="tool-content">{{ args }}</pre>
      <pre v-else class="tool-content truncated">{{ truncateText(args, 200) }}</pre>
    </div>

    <div v-if="result && status === 'completed'" class="tool-section">
      <div class="section-label" @click="resultExpanded = !resultExpanded">
        <span>Result</span>
        <el-icon class="expand-icon" :class="{ expanded: resultExpanded }">
          <ArrowDown />
        </el-icon>
      </div>
      <pre v-if="resultExpanded" class="tool-content">{{ result }}</pre>
      <pre v-else class="tool-content truncated">{{ truncateText(result, 200) }}</pre>
    </div>

    <div v-if="error && status === 'failed'" class="tool-error">
      {{ error }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { Loading, CircleCheck, CircleClose, Tools, ArrowDown } from '@element-plus/icons-vue'

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

const argsExpanded = ref(false)
const resultExpanded = ref(false)

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
const rawContent = computed(() => props.content || '')

// Status-based styling
const statusClass = computed(() => {
  switch (status.value) {
    case 'executing': return 'status-executing'
    case 'completed': return 'status-completed'
    case 'failed': return 'status-failed'
    default: return ''
  }
})

// Reset expand state when content changes
watch(() => props.content, () => {
  argsExpanded.value = false
  resultExpanded.value = false
})

// Truncate text with ellipsis
function truncateText(text: string, maxLen: number): string {
  if (!text) return ''
  if (text.length <= maxLen) return text
  return text.substring(0, maxLen) + '...'
}

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
  gap: 8px;
  margin-bottom: 8px;
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
  margin-left: auto;
}

.tool-section {
  margin-top: 8px;
}

.section-label {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  font-weight: 500;
  color: var(--el-text-color-secondary);
  cursor: pointer;
  user-select: none;
}

.section-label:hover {
  color: var(--el-text-color-primary);
}

.expand-icon {
  font-size: 12px;
  transition: transform 0.2s;
}

.expand-icon.expanded {
  transform: rotate(180deg);
}

.tool-content {
  font-size: 12px;
  background-color: var(--el-fill-color, #f5f5f5);
  padding: 8px;
  border-radius: 4px;
  overflow-x: auto;
  margin: 4px 0 0 0;
  white-space: pre-wrap;
  word-break: break-all;
}

.tool-content.truncated {
  max-height: 60px;
  overflow: hidden;
}

.tool-error {
  font-size: 12px;
  color: var(--el-color-danger);
  padding: 8px;
  background-color: var(--el-color-danger-light-9);
  border-radius: 4px;
  margin-top: 8px;
}
</style>
