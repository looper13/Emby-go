<script setup lang="ts">
import { Archive } from '@lucide/vue';
defineProps<{ title: string; hint: string }>();
</script>
<template><div class="empty"><Archive :size="40" :stroke-width="1.8" aria-hidden="true" /><p>{{ title }}</p><small>{{ hint }}</small></div></template>
