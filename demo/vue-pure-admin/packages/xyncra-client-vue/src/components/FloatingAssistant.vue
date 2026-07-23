<template>
  <div class="xyncra-floating-assistant">
    <el-badge :is-dot="executingCalls.length > 0" class="floating-badge">
      <el-button
        class="floating-button"
        :type="buttonType"
        circle
        @click="openPanel"
      >
        AI
      </el-button>
    </el-badge>

    <!-- Custom sidebar panel (replaces el-drawer for better responsive control) -->
    <Teleport to="body">
      <div
        v-if="panelVisible"
        class="xyncra-sidebar-overlay"
        @click.self="closePanel"
      >
        <div
          ref="sidebarRef"
          class="xyncra-sidebar"
          :class="{ 'xyncra-sidebar--open': slideIn }"
          @transitionend="onTransitionEnd"
        >
          <!-- Header -->
          <div class="xyncra-sidebar__header">
            <div class="xyncra-sidebar__header-left">
              <ConnectionStatus
                :status="connectionStatus"
                @reconnect="handleReconnect"
              />
            </div>
            <div class="xyncra-sidebar__header-right">
              <el-tooltip content="选择 Agent" placement="bottom">
                <el-button
                  :icon="User"
                  text
                  circle
                  size="small"
                  @click="showAgentPanel = true"
                />
              </el-tooltip>
              <el-tooltip content="会话列表" placement="bottom">
                <el-button
                  :icon="ChatDotRound"
                  text
                  circle
                  size="small"
                  @click="showConvPanel = true"
                />
              </el-tooltip>
              <el-tooltip content="新建会话" placement="bottom">
                <el-button
                  :icon="Plus"
                  text
                  circle
                  size="small"
                  @click="handleCreateConversation"
                />
              </el-tooltip>
              <el-tooltip content="关闭" placement="bottom">
                <el-button
                  :icon="Close"
                  text
                  circle
                  size="small"
                  @click="closePanel"
                />
              </el-tooltip>
            </div>
          </div>

          <!-- Main content area -->
          <div class="xyncra-sidebar__body">
            <ChatPanel :conversation-id="currentConversationId" />
          </div>

          <!-- Agent selector drawer (nested) -->
          <Transition name="xyncra-slide-left">
            <div v-if="showAgentPanel" class="xyncra-sidebar__sub-panel">
              <div class="xyncra-sidebar__sub-header">
                <span>选择 Agent</span>
                <el-button
                  :icon="Close"
                  text
                  circle
                  size="small"
                  @click="showAgentPanel = false"
                />
              </div>
              <div class="xyncra-sidebar__sub-body">
                <AgentSelector
                  :selected-agent-id="selectedAgentID"
                  @select="handleAgentSelect"
                />
              </div>
            </div>
          </Transition>

          <!-- Conversation list drawer (nested) -->
          <Transition name="xyncra-slide-left">
            <div v-if="showConvPanel" class="xyncra-sidebar__sub-panel">
              <div class="xyncra-sidebar__sub-header">
                <span>会话列表</span>
                <el-button
                  :icon="Close"
                  text
                  circle
                  size="small"
                  @click="showConvPanel = false"
                />
              </div>
              <div class="xyncra-sidebar__sub-body">
                <ConversationList
                  :active-conversation-id="currentConversationId"
                  @select="handleConversationSelect"
                />
              </div>
            </div>
          </Transition>
        </div>
      </div>
    </Teleport>

    <AskUserDialog />
    <RemoteCallingStatusIndicator
      :executing-calls="executingCalls"
      @cancel="cancelCall"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue'
import { Close, Plus, User, ChatDotRound } from '@element-plus/icons-vue'
import { useXyncra } from '../composables/useXyncra'
import { useRemoteCallingRouter } from '../composables/useRemoteCallingRouter'
import { useConversations } from '../composables/useConversations'
import { getAgentName } from '../constants/agents'
import ConnectionStatus from './ConnectionStatus.vue'
import ChatPanel from './ChatPanel.vue'
import AskUserDialog from './AskUserDialog.vue'
import RemoteCallingStatusIndicator from './RemoteCallingStatusIndicator.vue'
import AgentSelector from './AgentSelector.vue'
import ConversationList from './ConversationList.vue'

const { connectionStatus, reconnect } = useXyncra()
const { conversations, currentConversationId, selectConversation, createConversation, createConversationWithAgent } = useConversations()
const { executingCalls, cancelCall } = useRemoteCallingRouter()

const panelVisible = ref(false)
const slideIn = ref(false)
const showAgentPanel = ref(false)
const showConvPanel = ref(false)
const sidebarRef = ref<HTMLElement | null>(null)
const selectedAgentID = ref<string | null>(null)

const buttonType = computed(() => {
  switch (connectionStatus.value) {
    case 'connected': return 'success'
    case 'syncing':
    case 'connecting': return 'warning'
    case 'disconnected': return 'danger'
    default: return 'info'
  }
})

// Slide-in animation (matching React: 250ms cubic-bezier)
watch(() => panelVisible.value, (visible) => {
  if (visible) {
    // Trigger slide-in after DOM update
    nextTick(() => {
      requestAnimationFrame(() => {
        slideIn.value = true
      })
    })
  } else {
    slideIn.value = false
  }
})

function onTransitionEnd() {
  if (!slideIn.value) {
    panelVisible.value = false
  }
}

function openPanel() {
  panelVisible.value = true
  if (!currentConversationId.value && conversations.value.length > 0) {
    selectConversation(conversations.value[0].id)
  }
}

function closePanel() {
  slideIn.value = false
}

async function handleAgentSelect(agentId: string) {
  selectedAgentID.value = agentId
  showAgentPanel.value = false
  await createConversationWithAgent(agentId)
}

function handleConversationSelect(id: string) {
  selectConversation(id)
  showConvPanel.value = false
}

function handleCreateConversation() {
  if (selectedAgentID.value) {
    // Has previous selection: create directly
    const agentName = getAgentName(selectedAgentID.value) ?? '新会话'
    createConversation(selectedAgentID.value, agentName)
  } else {
    // No previous selection: show agent selector
    showAgentPanel.value = true
  }
}

function handleReconnect() {
  reconnect()
}

// Close sidebar on Escape key
function onKeyDown(e: KeyboardEvent) {
  if (e.key === 'Escape' && panelVisible.value) {
    closePanel()
  }
}

onMounted(() => {
  document.addEventListener('keydown', onKeyDown)
})

onUnmounted(() => {
  document.removeEventListener('keydown', onKeyDown)
})
</script>

<script lang="ts">
export default { name: 'FloatingAssistant' }
</script>

<style scoped>
.xyncra-floating-assistant {
  position: fixed;
  bottom: 24px;
  right: 24px;
  z-index: 999;
}

.floating-button {
  width: 56px;
  height: 56px;
  font-size: 16px;
  font-weight: 700;
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.15);
  transition: transform 0.2s, box-shadow 0.2s;
}

.floating-button:hover {
  transform: scale(1.08);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.2);
}

.floating-badge :deep(.el-badge__dot) {
  width: 10px;
  height: 10px;
}

/* Sidebar overlay */
.xyncra-sidebar-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 1000;
  background-color: rgba(0, 0, 0, 0.3);
  display: flex;
  justify-content: flex-end;
}

/* Sidebar panel */
.xyncra-sidebar {
  width: 420px;
  height: 100vh;
  background-color: var(--el-bg-color, #fff);
  box-shadow: -4px 0 24px rgba(0, 0, 0, 0.1);
  display: flex;
  flex-direction: column;
  transform: translateX(100%);
  transition: transform 250ms cubic-bezier(0.4, 0, 0.2, 1);
  position: relative;
  overflow: hidden;
}

.xyncra-sidebar--open {
  transform: translateX(0);
}

/* Mobile responsive: full width */
@media (max-width: 767px) {
  .xyncra-sidebar {
    width: 100vw;
  }
}

/* Tablet responsive */
@media (min-width: 768px) and (max-width: 1023px) {
  .xyncra-sidebar {
    width: 90vw;
    max-width: 420px;
  }
}

/* Header */
.xyncra-sidebar__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  border-bottom: 1px solid var(--el-border-color-light, #f0f0f0);
  flex-shrink: 0;
  min-height: 48px;
}

.xyncra-sidebar__header-left {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex: 1;
}

.xyncra-sidebar__header-right {
  display: flex;
  align-items: center;
  gap: 2px;
  flex-shrink: 0;
}

/* Body (message area) */
.xyncra-sidebar__body {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}

/* Sub-panel (agent selector, conversation list) */
.xyncra-sidebar__sub-panel {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: var(--el-bg-color, #fff);
  z-index: 10;
  display: flex;
  flex-direction: column;
}

.xyncra-sidebar__sub-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--el-border-color-light, #f0f0f0);
  font-size: 15px;
  font-weight: 600;
  flex-shrink: 0;
}

.xyncra-sidebar__sub-body {
  flex: 1;
  overflow-y: auto;
  min-height: 0;
}

/* Sub-panel slide animation */
.xyncra-slide-left-enter-active,
.xyncra-slide-left-leave-active {
  transition: transform 250ms cubic-bezier(0.4, 0, 0.2, 1), opacity 200ms ease;
}

.xyncra-slide-left-enter-from {
  transform: translateX(-100%);
  opacity: 0;
}

.xyncra-slide-left-leave-to {
  transform: translateX(-100%);
  opacity: 0;
}
</style>
