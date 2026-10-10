<script setup lang="ts">
defineProps<{ error: string; loading: boolean; empty: boolean }>();
const emit = defineEmits<{ retry: [] }>();
</script>
<template>
  <p v-if="error" class="auth-error" role="alert">{{ error }} <button class="btn btn-sm" :disabled="loading" @click="emit('retry')">重试</button></p>
  <p v-else-if="loading && empty" role="status">加载中…</p>
</template>
