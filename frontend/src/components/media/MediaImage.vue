<script setup lang="ts">
import { onScopeDispose, ref, watch } from 'vue';
import { Film } from '@lucide/vue';
import { useMediaVisibility } from '../../composables/useMediaVisibility';

const props = defineProps<{ src: string; fallback?: string; alt?: string }>();
const { hideImages } = useMediaVisibility();
const source = ref(props.src);
const isLoaded = ref(false);
const hasFailed = ref(false), retrySource = ref(''), imageRevision = ref(0);
let triedFallback = false, triedRetry = false;
let retryTimer: ReturnType<typeof setTimeout> | undefined;

function cancelRetry() { clearTimeout(retryTimer); retryTimer = undefined; }
function scheduleRetry() {
  cancelRetry();
  if (hideImages.value || !retrySource.value) return;
  // 只重挂载原 URL 一次，不附带令牌或绕过缓存的查询参数。
  retryTimer = setTimeout(() => {
    retryTimer = undefined;
    const next = retrySource.value; retrySource.value = '';
    if (!hideImages.value && next) { imageRevision.value++; source.value = next; }
  }, 500);
}

watch(() => [props.src, props.fallback], () => {
  cancelRetry(); retrySource.value = ''; hasFailed.value = false;
  source.value = props.src;
  triedFallback = false; triedRetry = false; imageRevision.value++;
  isLoaded.value = false;
}, { flush: 'sync' });
watch(hideImages, hidden => { if (hidden) cancelRetry(); else scheduleRetry(); }, { flush: 'sync' });
onScopeDispose(cancelRetry);

function onLoad() {
  cancelRetry(); retrySource.value = ''; hasFailed.value = false;
  isLoaded.value = true;
}

function failed() {
  if (hideImages.value || !source.value) return;
  isLoaded.value = false;
  if (!triedFallback && props.fallback && props.fallback !== source.value) {
    triedFallback = true;
    source.value = props.fallback;
  } else if (!triedFallback && !triedRetry) {
    triedRetry = true; retrySource.value = source.value; source.value = '';
    scheduleRetry();
  } else {
    hasFailed.value = true;
    source.value = '';
  }
}
</script>

<template>
  <span v-if="hideImages" class="image-placeholder image-hidden" role="img" :aria-label="alt || '已隐藏图片'"><Film :size="24" aria-hidden="true" /></span>
  <img
    v-else-if="source"
    :key="imageRevision"
    :src="source"
    :alt="alt ?? ''"
    decoding="async"
    :class="{ 'is-loaded': isLoaded }"
    @load="onLoad"
    @error="failed"
  >
  <span v-else class="image-placeholder" role="img" :aria-label="alt || (hasFailed ? '图片加载失败' : '图片不可用')">{{ retrySource ? '正在重试…' : hasFailed ? '图片加载失败' : '暂无图片' }}</span>
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
