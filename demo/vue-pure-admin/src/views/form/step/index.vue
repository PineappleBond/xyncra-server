<script setup lang="ts">
import { ref, computed } from "vue";
import { defineTestHelpers } from '../../../../packages/xyncra-client-vue/src/defineTestHelpers'

defineOptions({
  name: "StepForm"
});

const activeStep = ref(0);
const formData = ref({
  payAccount: "",
  receiveAccount: "",
  receiverName: "",
  amount: ""
});

const stepTitle = computed(() => {
  const titles = ["填写转账信息", "确认转账信息"];
  return titles[activeStep.value] || "";
});

const onNext = () => {
  if (activeStep.value < 1) {
    activeStep.value++;
  }
};

const onPrev = () => {
  if (activeStep.value > 0) {
    activeStep.value--;
  }
};

const onSubmit = () => {
  console.log("Step form submitted:", formData.value);
};

const onTransferAgain = () => {
  activeStep.value = 0;
  formData.value = { payAccount: "", receiveAccount: "", receiverName: "", amount: "" };
};

defineTestHelpers('step-form', {
  next: {
    name: 'next',
    description: '进入下一步',
    parameters: { type: 'object', properties: {} },
    handler: () => onNext()
  },
  prev: {
    name: 'prev',
    description: '返回上一步',
    parameters: { type: 'object', properties: {} },
    handler: () => onPrev()
  },
  submit: {
    name: 'submit',
    description: '确认提交转账',
    parameters: { type: 'object', properties: {} },
    handler: () => onSubmit()
  },
  transferAgain: {
    name: 'transferAgain',
    description: '再次转账',
    parameters: { type: 'object', properties: {} },
    handler: () => onTransferAgain()
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
        <span class="font-medium">分步表单</span>
      </div>
    </template>
    <el-steps :active="activeStep" finish-status="success" class="mb-8">
      <el-step title="填写转账信息" />
      <el-step title="确认转账信息" />
    </el-steps>

    <div v-if="activeStep === 0">
      <el-form :model="formData" label-width="120px">
        <el-form-item label="付款账户">
          <el-input v-model="formData.payAccount" placeholder="请输入付款账户" />
        </el-form-item>
        <el-form-item label="收款账户">
          <el-input v-model="formData.receiveAccount" placeholder="请输入收款账户" />
        </el-form-item>
        <el-form-item label="收款人姓名">
          <el-input v-model="formData.receiverName" placeholder="请输入收款人姓名" />
        </el-form-item>
        <el-form-item label="转账金额">
          <el-input v-model="formData.amount" placeholder="请输入转账金额">
            <template #prefix>¥</template>
          </el-input>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="onNext">下一步</el-button>
        </el-form-item>
      </el-form>
    </div>

    <div v-else>
      <el-result icon="info" title="确认转账信息" sub-title="请确认以下转账信息无误后提交">
        <template #extra>
          <el-descriptions :column="1" border>
            <el-descriptions-item label="付款账户">{{ formData.payAccount }}</el-descriptions-item>
            <el-descriptions-item label="收款账户">{{ formData.receiveAccount }}</el-descriptions-item>
            <el-descriptions-item label="收款人姓名">{{ formData.receiverName }}</el-descriptions-item>
            <el-descriptions-item label="转账金额">¥ {{ formData.amount }}</el-descriptions-item>
          </el-descriptions>
          <div class="mt-4">
            <el-button @click="onPrev">上一步</el-button>
            <el-button type="primary" @click="onSubmit">提交</el-button>
          </div>
        </template>
      </el-result>
    </div>
  </el-card>
</template>
