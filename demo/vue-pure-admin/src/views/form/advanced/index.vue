<script setup lang="ts">
import { ref } from "vue";
import { defineTestHelpers } from '../../../../packages/xyncra-client-vue/src/defineTestHelpers'

defineOptions({
  name: "AdvancedForm"
});

const formRef = ref();
const formData = ref({
  name: "",
  url: "",
  owner: "",
  approver: "",
  dateRange: "",
  type: "",
  taskName: "",
  taskDesc: "",
  taskOwner: "",
  taskApprover: ""
});

const onSubmit = () => {
  console.log("Advanced form submitted:", formData.value);
};

defineTestHelpers('advanced-form', {
  submit: {
    name: 'submit',
    description: '提交高级表单',
    parameters: { type: 'object', properties: {} },
    handler: () => onSubmit()
  },
  setFieldValue: {
    name: 'setFieldValue',
    description: '设置表单字段值',
    parameters: {
      type: 'object',
      properties: {
        field: { type: 'string', description: '字段名' },
        value: { description: '字段值' }
      },
      required: ['field', 'value']
    },
    handler: (args) => {
      const { field, value } = args as { field: string; value: unknown };
      if (field in formData.value) {
        (formData.value as Record<string, unknown>)[field] = value;
      }
    }
  }
});
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div class="card-header">
        <span class="font-medium">高级表单</span>
      </div>
    </template>
    <el-form ref="formRef" :model="formData" label-width="120px">
      <el-divider content-position="left">仓库管理</el-divider>
      <el-form-item label="仓库名">
        <el-input v-model="formData.name" placeholder="请输入仓库名" />
      </el-form-item>
      <el-form-item label="域名">
        <el-input v-model="formData.url" placeholder="请输入域名" />
      </el-form-item>
      <el-form-item label="管理员">
        <el-select v-model="formData.owner" placeholder="请选择管理员">
          <el-option label="付晓晓" value="付晓晓" />
          <el-option label="周星星" value="周星星" />
        </el-select>
      </el-form-item>
      <el-form-item label="审批人">
        <el-select v-model="formData.approver" placeholder="请选择审批人">
          <el-option label="王晓丽" value="王晓丽" />
          <el-option label="李宁" value="李宁" />
        </el-select>
      </el-form-item>
      <el-form-item label="生效日期">
        <el-date-picker v-model="formData.dateRange" type="daterange" range-separator="至" start-placeholder="开始日期" end-placeholder="结束日期" />
      </el-form-item>
      <el-form-item label="仓库类型">
        <el-select v-model="formData.type" placeholder="请选择仓库类型">
          <el-option label="私密" value="private" />
          <el-option label="公开" value="public" />
        </el-select>
      </el-form-item>

      <el-divider content-position="left">任务管理</el-divider>
      <el-form-item label="任务名">
        <el-input v-model="formData.taskName" placeholder="请输入任务名" />
      </el-form-item>
      <el-form-item label="任务描述">
        <el-input v-model="formData.taskDesc" type="textarea" placeholder="请输入任务描述" />
      </el-form-item>
      <el-form-item label="执行人">
        <el-select v-model="formData.taskOwner" placeholder="请选择执行人">
          <el-option label="付晓晓" value="付晓晓" />
          <el-option label="周星星" value="周星星" />
        </el-select>
      </el-form-item>
      <el-form-item label="责任人">
        <el-select v-model="formData.taskApprover" placeholder="请选择责任人">
          <el-option label="王晓丽" value="王晓丽" />
          <el-option label="李宁" value="李宁" />
        </el-select>
      </el-form-item>

      <el-form-item>
        <el-button type="primary" @click="onSubmit">提交</el-button>
      </el-form-item>
    </el-form>
  </el-card>
</template>
