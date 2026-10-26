<script setup lang="ts">
import { computed } from 'vue';

const props = defineProps<{ error?: string }>();
const summary = computed(() => props.error?.split('\n', 1)[0] || '—');
const detail = computed(() => {
  const index = props.error?.indexOf('\n') ?? -1;
  return index < 0 ? '' : props.error!.slice(index + 1);
});
</script>

<template>
  <div class="task-error">
    <span>{{ summary }}</span>
    <details v-if="detail">
      <summary>查看失败明细</summary>
      <pre>{{ detail }}</pre>
    </details>
  </div>
</template>

<style scoped>
.task-error { min-width: 0; max-width: 560px; white-space: normal; overflow-wrap: anywhere; }
details { margin-top: 8px; }
summary { color: var(--accent); cursor: pointer; }
pre { max-height: min(360px, 50vh); margin: 10px 0 0; padding-right: 8px; overflow: auto; font: inherit; line-height: 1.65; white-space: pre-wrap; overflow-wrap: anywhere; }
</style>
