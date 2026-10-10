<script setup lang="ts">
defineProps<{ error: string; loading: boolean; ready: boolean; done: boolean; total: number }>();
const emit = defineEmits<{ retry: [] }>();
</script>
<template><template v-if="error"><span role="alert">加载失败：{{ error }}</span><button class="btn" data-wall-retry :disabled="loading" @click="emit('retry')">重试</button></template><span v-else-if="loading || !ready">加载中…</span><span v-else-if="done && total">已全部加载（共 {{ total }} 条）</span></template>
