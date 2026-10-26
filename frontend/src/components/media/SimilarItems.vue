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
      <h3>
        <span>相似影片</span>
        <small>{{ items.length }}</small>
      </h3>
      <div v-if="overflowing" class="similar-nav">
        <button type="button" class="btn" title="上一组相似影片" aria-label="上一组相似影片" :disabled="atStart" @click="move(-1)">
          <ChevronLeft :size="16" aria-hidden="true" />
        </button>
        <button type="button" class="btn" title="下一组相似影片" aria-label="下一组相似影片" :disabled="atEnd" @click="move(1)">
          <ChevronRight :size="16" aria-hidden="true" />
        </button>
      </div>
    </div>
    <div v-if="items.length" ref="row" class="similar-row" @scroll.passive="updateScroll">
      <button
        v-for="item in items"
        :key="item.Id"
        class="similar-card"
        :data-similar="item.Id"
        :title="item.Name"
        @click="emit('open', item.Id)"
      >
        <span class="similar-poster">
          <MediaImage
            v-if="!hideImages && item.ImageTags?.Primary"
            :src="imageURL(item.Id, 'Primary', 320, item.ImageTags.Primary)"
            loading="lazy"
            alt=""
          />
          <span v-else class="image-placeholder"><Film :size="24" aria-hidden="true" /></span>
        </span>
        <strong>{{ item.Name || item.Id }}</strong>
        <small>{{ [item.ProductionYear, item.RunTimeTicks ? fmtDuration(item.RunTimeTicks / 10000000) : '', item.CommunityRating ? `★ ${item.CommunityRating}` : ''].filter(Boolean).join(' · ') }}</small>
      </button>
    </div>
  </section>
</template>

<style scoped>
.similar-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
  min-height: 30px;
}
.similar-heading h3 {
  margin: 0;
  display: flex;
  align-items: center;
  gap: 8px;
}
.similar-nav {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}
.similar-nav .btn {
  width: 30px;
  height: 30px;
  padding: 0;
  display: grid;
  place-items: center;
  border-radius: var(--radius-sm);
}
.similar-row {
  min-width: 0;
  scrollbar-width: none;
  display: flex;
  gap: 14px;
  overflow-x: auto;
  padding-bottom: 4px;
}
.similar-row::-webkit-scrollbar {
  display: none;
}
.similar-card {
  min-width: 0;
  flex: 0 0 136px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  background: transparent;
  border: 0;
  padding: 0;
  cursor: pointer;
  text-align: left;
  color: inherit;
  transition: transform 0.2s cubic-bezier(0.2, 0, 0, 1);
}
.similar-card:hover {
  transform: translateY(-4px);
}
.similar-card:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}
.similar-poster {
  position: relative;
  width: 100%;
  aspect-ratio: 2 / 3;
  border-radius: 10px;
  overflow: hidden;
  border: 1px solid var(--line);
  box-shadow: var(--shadow);
  transition: border-color 0.2s ease, box-shadow 0.2s ease;
}
.similar-card:hover .similar-poster {
  border-color: var(--accent);
  box-shadow: 0 8px 22px rgba(26, 113, 255, 0.3);
}
.similar-poster img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  transition: transform 0.25s ease, filter 0.25s ease;
}
.similar-card:hover .similar-poster img {
  transform: scale(1.04);
  filter: brightness(0.9);
}
.similar-card strong {
  font-size: 13px;
  font-weight: 600;
  line-height: 1.35;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition: color 0.15s ease;
}
.similar-card:hover strong {
  color: var(--accent);
}
.similar-card small {
  font-size: 11.5px;
  color: var(--dim);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
