<script setup lang="ts">
import { Play } from '@lucide/vue'; import MediaImage from './MediaImage.vue';
defineProps<{ url: string; thumb: string; fallback: string }>(); const emit = defineEmits<{ play: [] }>();
</script>
<template><section v-if="url" class="detail-section"><h3>预告片</h3><button id="item-trailer" class="trailer-card" title="播放预告片" aria-label="播放预告片" @click="emit('play')"><span class="trailer-thumb"><MediaImage v-if="thumb" :src="thumb" :fallback="fallback" loading="lazy" alt="" /><span class="trailer-play"><Play :size="28" aria-hidden="true" /></span></span></button></section></template>
