<script setup lang="ts">
import { ref, watch } from 'vue';
import MediaImage from './MediaImage.vue';

const props = defineProps<{ src: string; alt: string; loading?: 'lazy' | 'eager' }>();
const isJacket = ref(false);
watch(() => props.src, () => { isJacket.value = false; });

function fitCover(event: Event) {
  const image = event.target as HTMLImageElement;
  // Wide DVD jackets have the front cover on the right; portraits stay centered.
  isJacket.value = image.naturalWidth > image.naturalHeight * 1.2;
}
</script>

<template>
  <div class="r18-poster" :class="{ 'is-jacket': isJacket }">
    <MediaImage :src="src" :alt="alt" :loading="loading" referrerpolicy="no-referrer" @load="fitCover" />
    <slot />
  </div>
</template>

<style scoped>
.r18-poster { position:relative; width:100%; aspect-ratio:2/3; overflow:hidden; border-radius:10px; background:var(--panel-2); border:1px solid var(--line); }
.r18-poster :deep(img), .r18-poster :deep(.image-placeholder) { position:absolute; inset:0; display:block; width:100%; height:100%; }
.r18-poster :deep(img) { object-fit:cover; object-position:center; }
.r18-poster.is-jacket :deep(img) { object-position:right center; }
.r18-poster :deep(.image-placeholder) { display:grid; place-items:center; }
</style>
