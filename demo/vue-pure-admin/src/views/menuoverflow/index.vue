<script setup lang="ts">
import { defineTestHelpers } from "../../../packages/xyncra-client-vue/src/defineTestHelpers";

defineOptions({
  name: "MenuOverflow"
});

defineTestHelpers("menuoverflow", {});
</script>

<template>
  <div>目录、菜单文字超出显示 Tooltip 文字提示</div>
</template>
