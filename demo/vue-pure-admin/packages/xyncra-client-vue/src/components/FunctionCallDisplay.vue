<template>
  <div v-if="displayCalls.length > 0" class="xyncra-function-calls">
    <div v-for="(call, index) in displayCalls" :key="index" class="function-call-item">
      <div class="function-name">{{ call.name }}</div>
      <div v-if="call.params" class="function-params">
        <pre>{{ JSON.stringify(call.params, null, 2) }}</pre>
      </div>
      <div v-if="call.result" class="function-result">
        <pre>{{ JSON.stringify(call.result, null, 2) }}</pre>
      </div>
      <div v-if="call.error" class="function-error">{{ call.error }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'

export interface FunctionCallInfo {
  name: string
  params?: Record<string, unknown>
  result?: unknown
  error?: string
}

const props = withDefaults(defineProps<{
  calls?: FunctionCallInfo[]
}>(), {
  calls: () => []
})

// Internal calls for imperative usage
const internalCalls = ref<FunctionCallInfo[]>([])

// Use external calls if provided, otherwise use internal
const displayCalls = computed(() =>
  props.calls.length > 0 ? props.calls : internalCalls.value
)

function addCall(call: FunctionCallInfo) {
  internalCalls.value.push(call)
}

function clearCalls() {
  internalCalls.value = []
}

defineExpose({ addCall, clearCalls })
</script>

<style scoped>
.xyncra-function-calls {
  padding: 8px 12px;
  background-color: var(--el-fill-color-light, #fafafa);
  border-radius: 6px;
  margin: 8px 0;
}
.function-call-item {
  margin-bottom: 8px;
}
.function-call-item:last-child {
  margin-bottom: 0;
}
.function-name {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-color-primary);
  margin-bottom: 4px;
}
.function-params pre,
.function-result pre {
  font-size: 11px;
  background-color: var(--el-fill-color, #f5f5f5);
  padding: 6px 8px;
  border-radius: 4px;
  overflow-x: auto;
  margin: 4px 0;
}
.function-error {
  font-size: 12px;
  color: var(--el-color-danger);
  padding: 4px 8px;
  background-color: var(--el-color-danger-light-9);
  border-radius: 4px;
}
</style>
