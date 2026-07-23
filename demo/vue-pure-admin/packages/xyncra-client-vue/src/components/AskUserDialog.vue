<template>
  <el-dialog
    :model-value="askUserState.visible"
    title="Agent 需要您的输入"
    width="500px"
    :close-on-click-modal="false"
    :close-on-press-escape="false"
    :show-close="false"
    append-to-body
    @close="handleCancel"
  >
    <div class="ask-user-content">
      <div class="ask-user-question">{{ askUserState.question }}</div>
      <el-input
        v-model="answer"
        type="textarea"
        :rows="4"
        placeholder="请输入您的回答..."
        autofocus
        @keyup.enter.ctrl="handleSubmit"
      />
      <div class="ask-user-hint">Ctrl+Enter 提交</div>
    </div>
    <template #footer>
      <el-button @click="handleCancel">取消</el-button>
      <el-button type="primary" :disabled="!answer.trim()" @click="handleSubmit">
        提交
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { askUserState, submitAskUserAnswer, cancelAskUser } from '../composables/useAskUserState'

const answer = ref('')

// Reset answer when dialog opens
watch(() => askUserState.value.visible, (visible) => {
  if (visible) answer.value = ''
})

function handleSubmit() {
  if (!answer.value.trim()) return
  submitAskUserAnswer(answer.value.trim())
}

function handleCancel() {
  cancelAskUser()
}
</script>

<style scoped>
.ask-user-content {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.ask-user-question {
  font-size: 15px;
  line-height: 1.6;
  color: var(--el-text-color-primary);
  white-space: pre-wrap;
}

.ask-user-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  text-align: right;
}
</style>
