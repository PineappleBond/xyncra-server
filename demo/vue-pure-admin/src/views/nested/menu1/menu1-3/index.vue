<script setup lang="ts">
import { ref } from "vue";
import { defineTestHelpers } from '../../../../../packages/xyncra-client-vue/src/defineTestHelpers'
import { useI18n } from "vue-i18n";

defineOptions({
  name: "Menu1-3"
});

const input = ref("");
const { t } = useI18n();


defineTestHelpers('nested-menu1-3', {})
</script>

<template>
  <div class="dark:text-white">
    <p>{{ t("menus.pureMenu1") }}</p>
    <p style="text-indent: 2em">{{ t("menus.pureMenu1-3") }}</p>
    <el-input v-model="input" />
  </div>
</template>
