<script setup lang="ts">
import { plotClamped } from '../../lib/media';
defineProps<{ plot: string; expanded: boolean }>(); const emit = defineEmits<{ toggle: [] }>();
</script>
<template><section v-if="plot" class="detail-section"><h3>简介</h3><p id="item-plot" class="detail-plot" :class="{ 'is-clamped': plotClamped(plot) && !expanded }">{{ plot }}</p><button v-if="plotClamped(plot)" id="plot-toggle" class="btn plot-toggle" :aria-expanded="expanded" aria-controls="item-plot" @click="emit('toggle')">{{ expanded ? '收起简介' : '展开全部' }}</button></section></template>
