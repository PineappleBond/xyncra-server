<script setup lang="ts">
import { ref } from "vue";
import { defineTestHelpers } from "../../../../packages/xyncra-client-vue/src/defineTestHelpers";

defineOptions({
  name: "AccountCenter"
});

const activeTab = ref("articles");
const tags = ref(["前端", "Vue", "Element Plus", "TypeScript"]);
const newTag = ref("");

const tabs = [
  { key: "articles", label: "文章" },
  { key: "applications", label: "应用" },
  { key: "projects", label: "项目" }
];

const onTabChange = (tab: string) => {
  activeTab.value = tab;
};

const onAddTag = () => {
  if (newTag.value.trim() && !tags.value.includes(newTag.value.trim())) {
    tags.value.push(newTag.value.trim());
    newTag.value = "";
  }
};

defineTestHelpers("account-center", {
  switchTab: {
    name: "switchTab",
    description:
      "切换个人中心 Tab。tab 参数必须传英文 key：articles=文章, applications=应用, projects=项目",
    parameters: {
      type: "object",
      properties: {
        tab: {
          type: "string",
          description:
            "Tab key（articles=文章, applications=应用, projects=项目）"
        }
      },
      required: ["tab"]
    },
    handler: args => onTabChange((args as { tab: string }).tab)
  },
  addTag: {
    name: "addTag",
    description: "添加个人标签",
    parameters: {
      type: "object",
      properties: {
        tag: { type: "string", description: "标签名称" }
      },
      required: ["tag"]
    },
    handler: args => {
      newTag.value = (args as { tag: string }).tag;
      onAddTag();
    }
  },
  setTagInput: {
    name: "setTagInput",
    description: "设置标签输入框的值",
    parameters: {
      type: "object",
      properties: {
        value: { type: "string", description: "标签文本" }
      },
      required: ["value"]
    },
    handler: args => {
      newTag.value = (args as { value: string }).value;
    }
  }
});
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div class="card-header">
        <span class="font-medium">个人中心</span>
      </div>
    </template>
    <div class="mb-6">
      <el-avatar
        :size="80"
        src="https://avatars.githubusercontent.com/u/1?v=4"
      />
      <h3 class="mt-2">用户名</h3>
      <p class="text-gray-500">这是一段个人简介</p>
    </div>
    <div class="mb-4" data-testid="user-tags">
      <span class="mr-2">标签：</span>
      <el-tag v-for="tag in tags" :key="tag" class="mr-2 mb-2">{{
        tag
      }}</el-tag>
      <el-input
        v-model="newTag"
        size="small"
        style="width: 100px"
        placeholder="新标签"
        @keyup.enter="onAddTag"
      />
      <el-button size="small" @click="onAddTag">添加</el-button>
    </div>
    <el-tabs v-model="activeTab" @tab-change="onTabChange">
      <el-tab-pane
        v-for="tab in tabs"
        :key="tab.key"
        :label="tab.label"
        :name="tab.key"
      />
    </el-tabs>
    <el-empty description="暂无内容" />
  </el-card>
</template>
