<script setup lang="ts">
import { ref, watch } from 'vue';
const props = defineProps<{ src: string; fallback?: string; alt?: string }>();
const source = ref(props.src);
let triedFallback = false;
watch(() => [props.src, props.fallback], () => { source.value = props.src; triedFallback = false; });
function failed() {
  if (!triedFallback && props.fallback && props.fallback !== source.value) {
    triedFallback = true; source.value = props.fallback;
  } else source.value = '';
}
</script>
<template>
  <img v-if="source" :src="source" :alt="alt ?? ''" decoding="async" @error="failed">
  <span v-else class="image-placeholder" role="img" :aria-label="alt || '图片不可用'">暂无图片</span>
</template>
