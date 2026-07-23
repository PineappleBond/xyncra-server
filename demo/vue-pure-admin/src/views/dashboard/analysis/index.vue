<script setup lang="ts">
import { ref } from "vue";
import { defineTestHelpers } from "../../../../packages/xyncra-client-vue/src/defineTestHelpers";

defineOptions({
  name: "DashboardAnalysis"
});

const activeMetric = ref("sales");
const activeDateRange = ref("today");

const metrics = [
  { key: "sales", label: "销售额" },
  { key: "visits", label: "访问量" }
];

const dateRanges = [
  { key: "today", label: "今日" },
  { key: "week", label: "本周" },
  { key: "month", label: "本月" },
  { key: "year", label: "本年" }
];

const onMetricChange = (metric: string) => {
  activeMetric.value = metric;
};

const onDateRangeChange = (range: string) => {
  activeDateRange.value = range;
};

defineTestHelpers("dashboard-analysis", {
  switchMetric: {
    name: "switchMetric",
    description: "切换指标（销售额/访问量）",
    parameters: {
      type: "object",
      properties: {
        metric: { type: "string", description: "指标名称（sales/visits）" }
      },
      required: ["metric"]
    },
    handler: args => onMetricChange((args as { metric: string }).metric)
  },
  switchDateRange: {
    name: "switchDateRange",
    description: "切换时间维度",
    parameters: {
      type: "object",
      properties: {
        range: {
          type: "string",
          description: "时间范围（today/week/month/year）"
        }
      },
      required: ["range"]
    },
    handler: args => onDateRangeChange((args as { range: string }).range)
  }
});
</script>

<template>
  <div>
    <el-row :gutter="16" class="mb-4">
      <el-col :span="12">
        <el-card shadow="never">
          <template #header>
            <div class="flex-bc">
              <span class="font-medium">数据概览</span>
              <el-segmented
                v-model="activeMetric"
                :options="metrics.map(m => ({ value: m.key, label: m.label }))"
                @change="onMetricChange"
              />
            </div>
          </template>
          <div class="text-3xl font-bold">
            {{ activeMetric === "sales" ? "¥ 126,560" : "8,846" }}
          </div>
          <div class="text-green-500 mt-2">周同比 12% 日同比 11%</div>
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card shadow="never">
          <template #header>
            <span class="font-medium">时间筛选</span>
          </template>
          <el-radio-group v-model="activeDateRange" @change="onDateRangeChange">
            <el-radio-button
              v-for="range in dateRanges"
              :key="range.key"
              :value="range.key"
            >
              {{ range.label }}
            </el-radio-button>
          </el-radio-group>
        </el-card>
      </el-col>
    </el-row>
    <el-card shadow="never">
      <el-empty description="图表区域" />
    </el-card>
  </div>
</template>
