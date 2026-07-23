<script setup lang="ts">
import { Amap } from "@/components/ReMap";
import { defineTestHelpers } from "../../../packages/xyncra-client-vue/src/defineTestHelpers";

defineOptions({
  name: "MapPage"
});

defineTestHelpers("able-map", {});
</script>

<template>
  <Amap />
</template>

<style scoped>
.main-content {
  margin: 2px 0 0 !important;
}
</style>
