<script setup lang="ts">
import { ref } from "vue";
import { defineTestHelpers } from "../../../../packages/xyncra-client-vue/src/defineTestHelpers";

defineOptions({
  name: "ProfileAdvanced"
});

const activeTab = ref("detail");

const tabs = [
  { key: "detail", label: "详情" },
  { key: "rule", label: "规则" }
];

const onAction = (action: string) => {
  console.log("Action:", action);
};

const onTabChange = (tab: string) => {
  activeTab.value = tab;
};

defineTestHelpers("profile-advanced", {
  action1: {
    name: "action1",
    description: "执行主操作按钮",
    parameters: { type: "object", properties: {} },
    handler: () => onAction("action1")
  },
  action2: {
    name: "action2",
    description: "执行次要操作按钮",
    parameters: { type: "object", properties: {} },
    handler: () => onAction("action2")
  },
  switchTab: {
    name: "switchTab",
    description: "切换详情/规则 Tab",
    parameters: {
      type: "object",
      properties: {
        tab: { type: "string", description: "Tab 名称（detail/rule）" }
      },
      required: ["tab"]
    },
    handler: args => onTabChange((args as { tab: string }).tab)
  }
});
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div class="flex-bc">
        <span class="font-medium">高级详情</span>
        <div>
          <el-button type="primary" @click="onAction('action1')"
            >操作一</el-button
          >
          <el-button @click="onAction('action2')">操作二</el-button>
          <el-dropdown class="ml-2">
            <el-button>更多</el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item>选项一</el-dropdown-item>
                <el-dropdown-item>选项二</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </div>
    </template>
    <el-tabs v-model="activeTab" @tab-change="onTabChange">
      <el-tab-pane
        v-for="tab in tabs"
        :key="tab.key"
        :label="tab.label"
        :name="tab.key"
      />
    </el-tabs>
    <div v-if="activeTab === 'detail'">
      <el-descriptions :column="2" border>
        <el-descriptions-item label="项目名称">示例项目</el-descriptions-item>
        <el-descriptions-item label="项目 ID">12345</el-descriptions-item>
        <el-descriptions-item label="负责人">张三</el-descriptions-item>
        <el-descriptions-item label="生效时间">2024-01-01</el-descriptions-item>
      </el-descriptions>
    </div>
    <div v-else>
      <el-empty description="暂无规则" />
    </div>
  </el-card>
</template>
