<script setup lang="ts">
import { Play, Film } from '@lucide/vue'; import MediaImage from './MediaImage.vue';
import { useMediaVisibility } from '../../composables/useMediaVisibility';
defineProps<{ url: string; thumb: string; fallback: string }>(); const emit = defineEmits<{ play: [] }>();
const { hideImages } = useMediaVisibility();
</script>
<template><section v-if="url" class="detail-section"><h3>预告片</h3><button id="item-trailer" class="trailer-card" title="播放预告片" aria-label="播放预告片" @click="emit('play')"><span class="trailer-thumb"><MediaImage v-if="!hideImages && thumb" :src="thumb" :fallback="fallback" loading="lazy" alt="" /><span v-else class="image-placeholder"><Film :size="28" aria-hidden="true" /></span><span class="trailer-play"><Play :size="28" aria-hidden="true" /></span></span></button></section></template>
