<template>
  <el-dialog
    :model-value="visible"
    :title="`需要您的确认（${callings.length} 个请求）`"
    width="600px"
    :close-on-click-modal="false"
    @update:model-value="$emit('update:visible', $event)"
    @close="handleClose"
  >
    <div v-if="callings.length > 0" class="rc-content">
      <el-tabs v-model="activeTab" type="card" class="rc-tabs">
        <el-tab-pane
          v-for="(rc, index) in callings"
          :key="rc.id ?? index"
          :label="getTabLabel(rc, index)"
          :name="String(index)"
        >
          <div class="rc-calling-block">
            <div class="rc-method-label">方法</div>
            <div class="rc-method-name">{{ rc.method }}</div>
            <div v-if="isAskUser(rc)" class="rc-question-section">
              <div class="rc-question-label">问题</div>
              <div class="rc-question-text">{{ getQuestionText(rc) }}</div>
              <div class="rc-answer-section">
                <div class="rc-answer-label">您的回答</div>
                <el-input
                  v-model="answers[index]"
                  type="textarea"
                  :rows="4"
                  placeholder="请输入..."
                  :disabled="isSubmitting"
                />
              </div>
            </div>
            <div v-else class="rc-params-section">
              <div class="rc-params-label">参数</div>
              <div class="rc-params-text">{{ rc.params }}</div>
              <div class="rc-result-section">
                <div class="rc-result-label">结果</div>
                <el-input
                  v-model="results[index]"
                  type="textarea"
                  :rows="4"
                  placeholder="请输入结果..."
                  :disabled="isSubmitting"
                />
              </div>
            </div>
          </div>
        </el-tab-pane>
      </el-tabs>
      <div class="rc-queue-status">
        <span>{{ resolvedCount }} / {{ callings.length }} 已填写</span>
      </div>
    </div>
    <div v-else class="rc-empty">
      <p>没有待处理的请求</p>
    </div>
    <template #footer>
      <el-button @click="handleDismiss" :disabled="isSubmitting">
        取消
      </el-button>
      <el-button
        type="primary"
        :loading="isSubmitting"
        :disabled="!allResolved || isSubmitting"
        @click="handleSubmit"
      >
        {{ isSubmitting ? '提交中...' : '提交全部' }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { RemoteCallingItem } from '../composables/useRemoteCalling'

const props = defineProps<{
  visible: boolean
  callings: RemoteCallingItem[]
  isSubmitting?: boolean
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
  submit: [resolutions: Map<string, { success: boolean; result?: string; errorMessage?: string }>]
  dismiss: []
}>()

const activeTab = ref('0')
const answers = ref<string[]>([])
const results = ref<string[]>([])

// Reset when callings change
watch(() => props.callings.length, (newLen) => {
  answers.value = Array.from({ length: newLen }, () => '')
  results.value = Array.from({ length: newLen }, () => '')
  activeTab.value = '0'
}, { immediate: true })

function isAskUser(rc: RemoteCallingItem): boolean {
  return rc.method === 'ask_user'
}

function getQuestionText(rc: RemoteCallingItem): string {
  try {
    const params = JSON.parse(rc.params)
    return params.question ?? rc.params
  } catch {
    return rc.params
  }
}

function getTabLabel(rc: RemoteCallingItem, index: number): string {
  if (isAskUser(rc)) {
    return `问题 ${index + 1}`
  }
  return rc.method
}

const resolvedCount = computed(() => {
  let count = 0
  for (let i = 0; i < props.callings.length; i++) {
    const rc = props.callings[i]
    if (isAskUser(rc)) {
      if (answers.value[i] && answers.value[i].trim() !== '') count++
    } else {
      if (results.value[i] && results.value[i].trim() !== '') count++
    }
  }
  return count
})

const allResolved = computed(() => {
  if (props.callings.length === 0) return false
  for (let i = 0; i < props.callings.length; i++) {
    const rc = props.callings[i]
    if (isAskUser(rc)) {
      if (!answers.value[i] || answers.value[i].trim() === '') return false
    } else {
      if (!results.value[i] || results.value[i].trim() === '') return false
    }
  }
  return true
})

function handleSubmit() {
  if (!allResolved.value) {
    ElMessage.warning('请填写所有请求的结果')
    return
  }

  const resolutions = new Map<string, { success: boolean; result?: string; errorMessage?: string }>()
  props.callings.forEach((rc, index) => {
    if (isAskUser(rc)) {
      resolutions.set(rc.id, {
        success: true,
        result: answers.value[index],
      })
    } else {
      resolutions.set(rc.id, {
        success: true,
        result: results.value[index],
      })
    }
  })

  emit('submit', resolutions)
}

function handleDismiss() {
  emit('dismiss')
  emit('update:visible', false)
}

function handleClose() {
  emit('update:visible', false)
}
</script>

<style scoped>
.rc-content {
  padding: 4px 0;
}
.rc-tabs {
  margin-bottom: 12px;
}
.rc-calling-block {
  padding: 8px 0;
}
.rc-method-label,
.rc-question-label,
.rc-answer-label,
.rc-params-label,
.rc-result-label {
  font-size: 13px;
  font-weight: 500;
  color: var(--el-text-color-regular);
  margin-bottom: 8px;
}
.rc-method-name {
  padding: 8px 12px;
  background-color: var(--el-fill-color-light, #fafafa);
  border-radius: 6px;
  font-size: 14px;
  color: var(--el-text-color-primary);
  margin-bottom: 16px;
  font-family: monospace;
}
.rc-question-text {
  padding: 12px;
  background-color: var(--el-fill-color-light, #fafafa);
  border-radius: 6px;
  font-size: 14px;
  line-height: 1.6;
  color: var(--el-text-color-primary);
  margin-bottom: 16px;
}
.rc-params-text {
  padding: 12px;
  background-color: var(--el-fill-color-light, #fafafa);
  border-radius: 6px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-primary);
  margin-bottom: 16px;
  font-family: monospace;
  word-break: break-all;
}
.rc-answer-section,
.rc-result-section {
  margin-top: 8px;
}
.rc-queue-status {
  text-align: center;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  padding-top: 4px;
}
.rc-empty {
  text-align: center;
  padding: 24px 0;
  color: var(--el-text-color-secondary);
}
</style>
