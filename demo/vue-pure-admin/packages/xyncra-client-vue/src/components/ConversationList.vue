<template>
  <div class="conversation-list">
    <div v-if="conversations.length === 0" class="empty-state">
      <el-empty description="暂无会话" :image-size="60" />
    </div>
    <div v-else class="conversation-items">
      <div
        v-for="conv in conversations"
        :key="conv.id"
        class="conversation-item"
        :class="{ 'is-active': activeConversationId === conv.id }"
        @click="$emit('select', conv.id)"
      >
        <div class="conversation-content">
          <div class="conversation-title">{{ conv.title || '新会话' }}</div>
          <div class="conversation-time">{{ formatTime(conv.lastMessageAt || conv.createdAt) }}</div>
        </div>
        <el-dropdown
          trigger="click"
          @command="(cmd: string) => handleCommand(cmd, conv.id)"
          @click.stop
        >
          <el-button
            class="more-button"
            :icon="MoreFilled"
            text
            circle
            size="small"
            @click.stop
          />
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="delete" class="delete-item">
                <el-icon><Delete /></el-icon>
                删除
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Delete, MoreFilled } from '@element-plus/icons-vue'
import { useConversations } from '../composables/useConversations'

defineProps<{
  activeConversationId: string | null
}>()

defineEmits<{
  select: [id: string]
}>()

const { conversations, deleteConversation } = useConversations()

function handleCommand(command: string, conversationId: string) {
  if (command === 'delete') {
    deleteConversation(conversationId)
  }
}

function formatTime(dateStr: string | undefined): string {
  if (!dateStr) return ''
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  const diffHours = Math.floor(diffMs / 3600000)
  const diffDays = Math.floor(diffMs / 86400000)

  if (diffMins < 1) return '刚刚'
  if (diffMins < 60) return `${diffMins}分钟前`
  if (diffHours < 24) return `${diffHours}小时前`
  if (diffDays < 7) return `${diffDays}天前`

  return date.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
}
</script>

<style scoped>
.conversation-list {
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.empty-state {
  padding: 32px 0;
}
.conversation-items {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.conversation-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 16px;
  border-radius: 8px;
  cursor: pointer;
  transition: background-color 0.2s;
}
.conversation-item:hover {
  background-color: var(--el-fill-color-light);
}
.conversation-item.is-active {
  background-color: var(--el-color-primary-light-9);
}
.conversation-content {
  flex: 1;
  min-width: 0;
}
.conversation-title {
  font-size: 14px;
  color: var(--el-text-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.conversation-time {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  margin-top: 2px;
}
.more-button {
  opacity: 0;
  transition: opacity 0.2s;
}
.conversation-item:hover .more-button {
  opacity: 1;
}
.delete-item {
  color: var(--el-color-danger);
}
.delete-item:hover {
  background-color: var(--el-color-danger-light-9);
}
</style>
