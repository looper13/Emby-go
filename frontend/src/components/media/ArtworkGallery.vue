<script setup lang="ts">
import type { ImageDto } from '../../api/contracts'; import { imageURL } from '../../lib/image-url'; import MediaImage from './MediaImage.vue';
defineProps<{ id: number; images: ImageDto[] }>();
</script>
<template><section v-if="images.length" class="detail-section"><h3>剧照 <small>{{ images.length }}</small></h3><div class="detail-artwork-grid"><a v-for="(image, index) in images" :key="image.ImageIndex ?? index" :href="imageURL(id, 'Backdrop', undefined, image.ImageTag, image.ImageIndex)" target="_blank" rel="noopener"><MediaImage :src="imageURL(id, 'Backdrop', 480, image.ImageTag, image.ImageIndex)" loading="lazy" :alt="'剧照 ' + (index + 1)" /></a></div></section></template>
