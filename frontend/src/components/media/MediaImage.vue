<script setup lang="ts">
import { ref, watch } from 'vue';
import { Film } from '@lucide/vue';
import { useMediaVisibility } from '../../composables/useMediaVisibility';

const props = defineProps<{ src: string; fallback?: string; alt?: string }>();
const { hideImages } = useMediaVisibility();
const source = ref(props.src);
const isLoaded = ref(false);
let triedFallback = false;

watch(() => [props.src, props.fallback], () => {
  source.value = props.src;
  triedFallback = false;
  isLoaded.value = false;
});

function onLoad() {
  isLoaded.value = true;
}

function failed() {
  isLoaded.value = false;
  if (!triedFallback && props.fallback && props.fallback !== source.value) {
    triedFallback = true;
    source.value = props.fallback;
  } else {
    source.value = '';
  }
}
</script>

<template>
  <span v-if="hideImages" class="image-placeholder image-hidden" role="img" :aria-label="alt || '已隐藏图片'"><Film :size="24" aria-hidden="true" /></span>
  <img
    v-else-if="source"
    :src="source"
    :alt="alt ?? ''"
    decoding="async"
    :class="{ 'is-loaded': isLoaded }"
    @load="onLoad"
    @error="failed"
  >
  <span v-else class="image-placeholder" role="img" :aria-label="alt || '图片不可用'">暂无图片</span>
</template>

<style scoped>
img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  transition: opacity 0.25s ease;
}
@keyframes mediaFadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}
img.is-loaded {
  animation: mediaFadeIn 0.25s ease forwards;
}
</style>
