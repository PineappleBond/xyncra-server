<script setup lang="ts">
import { ref } from "vue";
import { defineTestHelpers } from "../../../../packages/xyncra-client-vue/src/defineTestHelpers";

defineOptions({
  name: "ListSearch"
});

const activeTab = ref("articles");
const searchKeyword = ref("");

const tabs = [
  { key: "articles", label: "文章" },
  { key: "projects", label: "项目" },
  { key: "applications", label: "应用" }
];

const onSearch = () => {
  console.log("Searching:", searchKeyword.value, "in tab:", activeTab.value);
};

const onTabChange = (tab: string) => {
  activeTab.value = tab;
};

defineTestHelpers("list-search", {
  switchTab: {
    name: "switchTab",
    description: "切换搜索分类 Tab",
    parameters: {
      type: "object",
      properties: {
        tab: {
          type: "string",
          description: "Tab 名称（articles/projects/applications）"
        }
      },
      required: ["tab"]
    },
    handler: args => onTabChange((args as { tab: string }).tab)
  },
  search: {
    name: "search",
    description: "执行搜索",
    parameters: {
      type: "object",
      properties: {
        keyword: { type: "string", description: "搜索关键词" }
      }
    },
    handler: args => {
      const { keyword } = args as { keyword?: string };
      if (keyword) searchKeyword.value = keyword;
      onSearch();
    }
  },
  setSearchKeyword: {
    name: "setSearchKeyword",
    description: "设置搜索关键词",
    parameters: {
      type: "object",
      properties: {
        keyword: { type: "string", description: "搜索关键词" }
      },
      required: ["keyword"]
    },
    handler: args => {
      searchKeyword.value = (args as { keyword: string }).keyword;
    }
  }
});
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div class="card-header">
        <span class="font-medium">搜索列表</span>
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
    <div class="mb-4">
      <el-input
        v-model="searchKeyword"
        placeholder="请输入搜索关键词"
        clearable
        style="width: 300px; margin-right: 16px"
      >
        <template #append>
          <el-button @click="onSearch">搜索</el-button>
        </template>
      </el-input>
    </div>
    <el-empty description="请输入关键词搜索" />
  </el-card>
</template>
