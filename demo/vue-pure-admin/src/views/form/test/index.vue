<script setup lang="ts">
import { ref, reactive } from "vue";
import { defineTestHelpers } from "../../../../packages/xyncra-client-vue/src/defineTestHelpers";

// Form data
const formData = reactive({
  name: "",
  email: "",
  city: "",
  remark: ""
});

// Table data
const tableData = ref([
  { id: 1, name: "张三", email: "zhangsan@example.com", city: "北京" },
  { id: 2, name: "李四", email: "lisi@example.com", city: "上海" },
  { id: 3, name: "王五", email: "wangwu@example.com", city: "广州" }
]);

// Highlighted field
const highlightedField = ref<string | null>(null);

// Form ref for validation
const formRef = ref();

// Register page-level test helpers
defineTestHelpers("form-test", {
  getFormData: {
    name: "getFormData",
    description: "获取表单数据",
    parameters: { type: "object", properties: {} },
    handler: () => {
      return { ...formData };
    }
  },
  setFieldValue: {
    name: "setFieldValue",
    description: "设置表单字段值",
    parameters: {
      type: "object",
      properties: {
        field: { type: "string", description: "字段名" },
        value: { type: "string", description: "字段值" }
      },
      required: ["field", "value"]
    },
    handler: (args: Record<string, unknown>) => {
      const { field, value } = args as { field: string; value: string };
      if (field in formData) {
        (formData as any)[field] = value;
        return { success: true };
      }
      return { success: false, error: `字段不存在: ${field}` };
    }
  },
  submitForm: {
    name: "submitForm",
    description: "提交表单",
    parameters: { type: "object", properties: {} },
    handler: async () => {
      // Simulate form submission
      console.log("Form submitted:", { ...formData });
      return { success: true, data: { ...formData } };
    }
  },
  getTableData: {
    name: "getTableData",
    description: "获取表格数据",
    parameters: { type: "object", properties: {} },
    handler: () => {
      return tableData.value;
    }
  },
  highlightField: {
    name: "highlightField",
    description: "高亮指定表单项",
    parameters: {
      type: "object",
      properties: {
        field: { type: "string", description: "要高亮的字段名" }
      },
      required: ["field"]
    },
    handler: (args: Record<string, unknown>) => {
      const { field } = args as { field: string };
      if (field in formData) {
        highlightedField.value = field;
        // Auto-remove highlight after 3 seconds
        setTimeout(() => {
          if (highlightedField.value === field) {
            highlightedField.value = null;
          }
        }, 3000);
        return { success: true };
      }
      return { success: false, error: `字段不存在: ${field}` };
    }
  },
  scrollToSection: {
    name: "scrollToSection",
    description: "滚动到指定区域",
    parameters: {
      type: "object",
      properties: {
        section: {
          type: "string",
          description: "区域标识（form 或 table）"
        }
      },
      required: ["section"]
    },
    handler: (args: Record<string, unknown>) => {
      const { section } = args as { section: string };
      const element = document.getElementById(`section-${section}`);
      if (element) {
        element.scrollIntoView({ behavior: "smooth", block: "start" });
        return { success: true };
      }
      return { success: false, error: `区域不存在: ${section}` };
    }
  }
});
</script>

<template>
  <div class="form-test-container">
    <h1>Xyncra 测试表单</h1>
    <p class="description">
      此页面用于验证页面级函数注册机制（defineTestHelpers）。
      Agent 可以通过 pg_form_test_* 函数操作表单和表格数据。
    </p>

    <!-- Form Section -->
    <div id="section-form" class="section">
      <h2>表单演示区域</h2>
      <el-form ref="formRef" :model="formData" label-width="100px">
        <el-form-item
          label="姓名"
          :class="{ 'is-highlighted': highlightedField === 'name' }"
        >
          <el-input v-model="formData.name" placeholder="请输入姓名" />
        </el-form-item>
        <el-form-item
          label="邮箱"
          :class="{ 'is-highlighted': highlightedField === 'email' }"
        >
          <el-input v-model="formData.email" placeholder="请输入邮箱" />
        </el-form-item>
        <el-form-item
          label="城市"
          :class="{ 'is-highlighted': highlightedField === 'city' }"
        >
          <el-select v-model="formData.city" placeholder="请选择城市">
            <el-option label="北京" value="北京" />
            <el-option label="上海" value="上海" />
            <el-option label="广州" value="广州" />
            <el-option label="深圳" value="深圳" />
          </el-select>
        </el-form-item>
        <el-form-item
          label="备注"
          :class="{ 'is-highlighted': highlightedField === 'remark' }"
        >
          <el-input
            v-model="formData.remark"
            type="textarea"
            placeholder="请输入备注"
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="console.log('Submitted:', formData)">
            提交
          </el-button>
        </el-form-item>
      </el-form>
    </div>

    <!-- Table Section -->
    <div id="section-table" class="section">
      <h2>表格演示区域</h2>
      <el-table :data="tableData" border style="width: 100%">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="姓名" />
        <el-table-column prop="email" label="邮箱" />
        <el-table-column prop="city" label="城市" />
      </el-table>
    </div>
  </div>
</template>

<style scoped>
.form-test-container {
  padding: 20px;
}

.description {
  color: #666;
  margin-bottom: 20px;
}

.section {
  margin-bottom: 30px;
  padding: 20px;
  background: #fff;
  border-radius: 4px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

h1 {
  margin-bottom: 20px;
  color: #333;
}

h2 {
  margin-bottom: 15px;
  color: #444;
}

.is-highlighted {
  animation: highlight-pulse 1s ease-in-out 3;
}

@keyframes highlight-pulse {
  0%,
  100% {
    background-color: transparent;
  }
  50% {
    background-color: #e6f7ff;
  }
}
</style>
