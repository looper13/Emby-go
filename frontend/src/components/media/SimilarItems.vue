<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { ChevronLeft, ChevronRight, Film } from '@lucide/vue';
import type { SimilarDto } from '../../api/contracts';
import { imageURL } from '../../lib/image-url';
import { fmtDuration } from '../../lib/media';
import MediaImage from './MediaImage.vue';
import { useMediaVisibility } from '../../composables/useMediaVisibility';

const props = defineProps<{ items: SimilarDto[] }>();
const emit = defineEmits<{ open: [id: string] }>();
const { hideImages } = useMediaVisibility();
const row = ref<HTMLDivElement>();
const overflowing = ref(false);
const atStart = ref(true);
const atEnd = ref(true);
let observer: ResizeObserver | undefined;

function updateScroll() {
  const element = row.value;
  const remaining = element ? element.scrollWidth - element.clientWidth : 0;
  overflowing.value = remaining > 1;
  atStart.value = !element || element.scrollLeft <= 1;
  atEnd.value = !element || element.scrollLeft >= remaining - 1;
}
function move(direction: number) {
  const element = row.value;
  if (!element) return;
  element.scrollBy({
    left: direction * element.clientWidth,
    behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth',
  });
}
onMounted(() => {
  if (typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(updateScroll);
    if (row.value) observer.observe(row.value);
  }
  updateScroll();
});
watch(row, element => {
  observer?.disconnect();
  if (element) observer?.observe(element);
  updateScroll();
}, { flush: 'post' });
watch(() => props.items, updateScroll, { flush: 'post' });
onBeforeUnmount(() => observer?.disconnect());
</script>
<template>
  <section id="item-similar" class="detail-section" :hidden="!items.length">
    <div v-if="items.length" class="similar-heading">
      <h3>相似影片 <small>{{ items.length }}</small></h3>
      <div v-if="overflowing" class="similar-nav">
        <button type="button" class="btn" title="上一组相似影片" aria-label="上一组相似影片" :disabled="atStart" @click="move(-1)"><ChevronLeft :size="18" aria-hidden="true" /></button>
        <button type="button" class="btn" title="下一组相似影片" aria-label="下一组相似影片" :disabled="atEnd" @click="move(1)"><ChevronRight :size="18" aria-hidden="true" /></button>
      </div>
    </div>
    <div v-if="items.length" ref="row" class="similar-row" @scroll.passive="updateScroll">
      <button v-for="item in items" :key="item.Id" class="similar-card" :data-similar="item.Id" :title="item.Name" @click="emit('open', item.Id)">
        <span class="similar-poster"><MediaImage v-if="!hideImages && item.ImageTags?.Primary" :src="imageURL(item.Id, 'Primary', 320, item.ImageTags.Primary)" loading="lazy" alt="" /><span v-else class="image-placeholder"><Film :size="24" aria-hidden="true" /></span></span>
        <strong>{{ item.Name || item.Id }}</strong>
        <small>{{ [item.ProductionYear, item.RunTimeTicks ? fmtDuration(item.RunTimeTicks / 10000000) : '', item.CommunityRating ? `★ ${item.CommunityRating}` : ''].filter(Boolean).join(' · ') }}</small>
      </button>
    </div>
  </section>
</template>
<style scoped>
.similar-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 10px; min-height: 30px; }
.similar-heading h3 { margin: 0; }
.similar-nav { display: flex; gap: 6px; flex-shrink: 0; }
.similar-nav .btn { width: 30px; height: 30px; padding: 0; display: grid; place-items: center; }
.similar-row { min-width: 0; scrollbar-width: none; }
.similar-row::-webkit-scrollbar { display: none; }
.similar-card { min-width: 0; }
.similar-card:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
</style>
