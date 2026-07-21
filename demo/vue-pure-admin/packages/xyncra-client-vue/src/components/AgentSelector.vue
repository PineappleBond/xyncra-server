<template>
  <div class="agent-selector">
    <div
      v-for="agent in agents"
      :key="agent.id"
      class="agent-item"
      :class="{ 'is-selected': selectedAgentId === agent.id }"
      @click="$emit('select', agent.id)"
    >
      <el-avatar :size="40" class="agent-avatar">
        <el-icon><Monitor /></el-icon>
      </el-avatar>
      <div class="agent-info">
        <div class="agent-name">{{ agent.name }}</div>
        <div class="agent-description">{{ agent.description }}</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Monitor } from '@element-plus/icons-vue'
import { DEFAULT_AGENTS } from '../constants/agents'

defineProps<{
  selectedAgentId: string | null
}>()

defineEmits<{
  select: [agentId: string]
}>()

const agents = DEFAULT_AGENTS
</script>

<style scoped>
.agent-selector {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.agent-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  border-radius: 8px;
  cursor: pointer;
  transition: background-color 0.2s;
}
.agent-item:hover {
  background-color: var(--el-fill-color-light);
}
.agent-item.is-selected {
  background-color: var(--el-color-primary-light-9);
}
.agent-avatar {
  flex-shrink: 0;
  background-color: var(--el-color-primary-light-5);
  color: var(--el-color-primary);
}
.agent-info {
  flex: 1;
  min-width: 0;
}
.agent-name {
  font-size: 14px;
  font-weight: 500;
  color: var(--el-text-color-primary);
}
.agent-description {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 2px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
