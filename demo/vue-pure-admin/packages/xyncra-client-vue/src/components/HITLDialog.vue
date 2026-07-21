<template>
  <el-dialog
    :model-value="visible"
    :title="`需要您的确认（${questions.length} 个问题）`"
    width="600px"
    :close-on-click-modal="false"
    @update:model-value="$emit('update:visible', $event)"
    @close="handleClose"
  >
    <div v-if="questions.length > 0" class="hitl-content">
      <el-tabs v-model="activeTab" type="card" class="hitl-tabs">
        <el-tab-pane
          v-for="(q, index) in questions"
          :key="q.questionId ?? index"
          :label="`问题 ${index + 1}`"
          :name="String(index)"
        >
          <div class="hitl-question-block">
            <div class="hitl-question-label">问题</div>
            <div class="hitl-question-text">{{ q.question }}</div>
            <div class="hitl-answer-section">
              <div class="hitl-answer-label">您的回答</div>
              <el-input
                v-model="answers[index]"
                type="textarea"
                :rows="4"
                placeholder="请输入..."
                :disabled="isSubmitting"
              />
            </div>
          </div>
        </el-tab-pane>
      </el-tabs>
      <div class="hitl-queue-status">
        <span>{{ answeredCount }} / {{ questions.length }} 已填写</span>
      </div>
    </div>
    <div v-else class="hitl-empty">
      <p>没有待处理的审批请求</p>
    </div>
    <template #footer>
      <el-button @click="handleDismiss" :disabled="isSubmitting">
        取消
      </el-button>
      <el-button
        type="primary"
        :loading="isSubmitting"
        :disabled="!allAnswered || isSubmitting"
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
import type { HITLQuestion } from '../composables/useHITL'

const props = defineProps<{
  visible: boolean
  questions: HITLQuestion[]
  isSubmitting?: boolean
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
  submit: [answers: Map<string, string>]
  dismiss: []
}>()

const activeTab = ref('0')
const answers = ref<string[]>([])

// Reset answers when questions change
watch(() => props.questions.length, (newLen) => {
  answers.value = Array.from({ length: newLen }, () => '')
  activeTab.value = '0'
}, { immediate: true })

const answeredCount = computed(() => {
  return answers.value.filter(a => a && a.trim() !== '').length
})

const allAnswered = computed(() => {
  return props.questions.length > 0 &&
    answers.value.every(a => a && a.trim() !== '')
})

function handleSubmit() {
  if (!allAnswered.value) {
    ElMessage.warning('请填写所有问题的回答')
    return
  }

  const answersMap = new Map<string, string>()
  props.questions.forEach((q, index) => {
    const answerText = answers.value[index]
    if (answerText && q.questionId) {
      answersMap.set(q.questionId, answerText)
    }
  })

  emit('submit', answersMap)
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
.hitl-content {
  padding: 4px 0;
}
.hitl-tabs {
  margin-bottom: 12px;
}
.hitl-question-block {
  padding: 8px 0;
}
.hitl-question-label,
.hitl-answer-label {
  font-size: 13px;
  font-weight: 500;
  color: var(--el-text-color-regular);
  margin-bottom: 8px;
}
.hitl-question-text {
  padding: 12px;
  background-color: var(--el-fill-color-light, #fafafa);
  border-radius: 6px;
  font-size: 14px;
  line-height: 1.6;
  color: var(--el-text-color-primary);
  margin-bottom: 16px;
}
.hitl-answer-section {
  margin-top: 8px;
}
.hitl-queue-status {
  text-align: center;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  padding-top: 4px;
}
.hitl-empty {
  text-align: center;
  padding: 24px 0;
  color: var(--el-text-color-secondary);
}
</style>
