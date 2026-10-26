<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { ArrowLeft, RefreshCw, Tv, Layers } from '@lucide/vue';
import { televisionApi, type TelevisionItem } from '../../api/television';
import { usePageRequest } from '../../composables/usePageRequest';
import MediaImage from './MediaImage.vue';
import FavoriteButton from './FavoriteButton.vue';
import SkeletonCard from '../common/SkeletonCard.vue';
import { imageURL } from '../../lib/image-url';
import { useMediaVisibility } from '../../composables/useMediaVisibility';

const props = defineProps<{ library: number; search: string; revision: number; series?: string; season?: string }>();
const emit = defineEmits<{ open: [id: number]; browse: [series: string, season: string] }>();
const { hideImages } = useMediaVisibility();
const scope = usePageRequest();
const items = ref<TelevisionItem[]>([]);
const seasons = ref<TelevisionItem[]>([]);
const selected = ref<TelevisionItem>();
const season = ref('');
const loading = ref(false);
const error = ref('');
const total = ref(0);
const retryMore = ref(false);
const title = computed(() => selected.value?.Name || '电视节目');

async function load(more = false) {
  const current = scope.begin();
  loading.value = true;
  error.value = '';
  retryMore.value = more;
  const offset = more ? items.value.length : 0;
  if (!more) items.value = [];
  try {
    const result = selected.value
      ? await televisionApi.episodes(selected.value.Id, season.value, offset, current.signal)
      : await televisionApi.list(props.library, props.search, offset, current.signal);
    if (current.isCurrent()) {
      items.value = more ? [...items.value, ...result.items] : result.items;
      total.value = result.total;
    }
  } catch (cause) {
    if (current.isCurrent()) error.value = cause instanceof Error ? cause.message : '加载失败';
  } finally {
    if (current.isCurrent()) loading.value = false;
  }
}

async function select(item: TelevisionItem) {
  if (item.Type === 'Episode') {
    emit('open', Number(item.Id));
    return;
  }
  emit('browse', item.Id, '');
}

async function loadSeasons() {
  if (!props.series) return;
  const current = scope.begin();
  loading.value = true;
  error.value = '';
  try {
    if (!props.series.startsWith(`tv-series:${props.library}:`)) throw new Error('剧集不属于当前媒体库');
    const item = await televisionApi.series(props.series, current.signal);
    if (!current.isCurrent()) return;
    const result = await televisionApi.seasons(item.Id, current.signal);
    if (!current.isCurrent()) return;
    selected.value = item;
    seasons.value = result.items;
    season.value =
      result.items.find(s => s.Id === props.season)?.Id ??
      result.items.find(s => s.IndexNumber > 0)?.Id ??
      result.items[0]?.Id ??
      '';
    if (props.season && season.value !== props.season) emit('browse', item.Id, season.value);
    if (season.value) await load();
    else {
      items.value = [];
      total.value = 0;
      loading.value = false;
    }
  } catch (cause) {
    if (current.isCurrent()) {
      error.value = cause instanceof Error ? cause.message : '加载失败';
      loading.value = false;
    }
  }
}

function back() {
  emit('browse', '', '');
}

function retry() {
  if (props.series && !seasons.value.length) void loadSeasons();
  else void load(retryMore.value);
}

function enter() {
  selected.value = undefined;
  seasons.value = [];
  season.value = '';
  total.value = 0;
  items.value = [];
  retryMore.value = false;
  if (props.series) void loadSeasons();
  else void load();
}

watch(() => [props.library, props.search, props.revision, props.series, props.season], enter, { immediate: true });
</script>

<template>
  <div class="television-browser">
    <div class="television-head">
      <button v-if="selected || series" class="icon-btn btn-tv-back" title="返回电视节目" @click="back">
        <ArrowLeft :size="16" aria-hidden="true" />
      </button>
      <h3>{{ title }}</h3>
      <FavoriteButton v-if="selected" :item-id="selected.Id" :favorite="selected.Favorite" :label="selected.Name" />
      <span class="hint">{{ total }} {{ selected ? '集' : '部' }}</span>
    </div>

    <!-- 季选择标签组（类似 Nowen Video SeriesEpisodeBrowser 季切换标签） -->
    <div v-if="selected && seasons.length" class="television-seasons" role="tablist" aria-label="季">
      <button
        v-for="s in seasons"
        :key="s.Id"
        role="tab"
        :aria-selected="season === s.Id"
        class="btn btn-sm season-tab"
        :class="{ 'season-active': season === s.Id }"
        @click="emit('browse', selected.Id, s.Id)"
      >
        <span>{{ s.Name }}</span>
      </button>
    </div>

    <div v-if="selected && season" class="season-favorite">
      <span class="season-title">{{ seasons.find(s => s.Id === season)?.Name }}</span>
      <FavoriteButton :item-id="season" :favorite="seasons.find(s => s.Id === season)?.Favorite" :label="seasons.find(s => s.Id === season)?.Name" />
    </div>

    <p v-if="error" role="alert">
      {{ error }}
      <button class="btn btn-sm" @click="retry">
        <RefreshCw :size="13" />
        <span>重试</span>
      </button>
    </p>

    <div class="television-grid" :class="{ 'episode-grid': selected }">
      <SkeletonCard v-if="loading && !items.length" :count="12" />
      <article v-for="item in items" :key="item.Id" class="television-entry">
        <button class="television-card" :data-tv-id="item.Id" @click="select(item)">
          <div class="television-cover">
            <MediaImage
              v-if="!hideImages && (item.ImageTags.Primary || (item.Type === 'Episode' && item.ImageTags.Thumb))"
              :src="imageURL(item.Id, item.ImageTags.Primary ? 'Primary' : 'Thumb', 320, item.ImageTags.Primary || item.ImageTags.Thumb)"
              alt=""
              loading="lazy"
            />
            <span v-else class="image-placeholder">{{ item.Name }}</span>
          </div>
          <strong>{{ selected ? `${item.IndexNumber}. ${item.Name}` : item.Name }}</strong>
          <small>{{ selected ? item.SeriesName : [item.ProductionYear || '', `${item.ChildCount} 季`, `${item.RecursiveItemCount} 集`].filter(Boolean).join(' · ') }}</small>
        </button>
        <div class="television-favorite">
          <FavoriteButton :item-id="item.Id" :favorite="item.Favorite" :label="item.Name" @changed="item.Favorite = $event" />
        </div>
      </article>
    </div>

    <p v-if="loading && items.length" role="status">加载中…</p>
    <p v-else-if="!error && !items.length && !loading" class="hint">{{ selected ? '暂无单集' : '暂无电视节目' }}</p>
    <button v-if="items.length < total && !error" class="btn btn-sm" :disabled="loading" @click="load(true)">
      加载更多
    </button>
  </div>
</template>

<style scoped>
.television-browser {
  margin: 18px 0 24px;
  min-width: 0;
}
.television-entry {
  position: relative;
  min-width: 0;
  display: flex;
  flex-direction: column;
  transition: transform 0.2s cubic-bezier(0.2, 0, 0, 1);
}
.television-entry:hover {
  transform: translateY(-4px);
}
.television-entry .television-card {
  width: 100%;
}
.television-favorite {
  position: absolute;
  top: 6px;
  right: 6px;
  z-index: 2;
  display: flex;
  align-items: center;
  justify-content: center;
  margin: 0;
}
.television-favorite :deep(.favorite-button) {
  width: 30px;
  height: 30px;
  flex: 0 0 30px;
  border-radius: 50%;
  background: rgba(15, 23, 42, 0.68);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  border: 1px solid rgba(255, 255, 255, 0.16);
  color: rgba(255, 255, 255, 0.75);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
  transition: all 0.18s cubic-bezier(0.2, 0, 0, 1);
  opacity: 0;
  transform: scale(0.9);
}
.television-favorite :deep(.favorite-button svg) {
  width: 15px;
  height: 15px;
}
.television-entry:hover .television-favorite :deep(.favorite-button),
.television-entry:focus-within .television-favorite :deep(.favorite-button),
.television-favorite :deep(.favorite-button.selected) {
  opacity: 1;
  transform: scale(1);
}
.television-favorite :deep(.favorite-button:hover) {
  background: rgba(15, 23, 42, 0.88);
  color: #ff5c7c;
  border-color: rgba(255, 92, 124, 0.45);
  transform: scale(1.08);
}
.television-favorite :deep(.favorite-button.selected) {
  background: rgba(15, 23, 42, 0.82);
  color: #ff5c7c;
  border-color: rgba(255, 92, 124, 0.4);
  box-shadow: 0 2px 10px rgba(255, 92, 124, 0.35);
}
@media (hover: none) {
  .television-favorite :deep(.favorite-button) {
    opacity: 0.85;
    transform: scale(1);
  }
}
.season-favorite {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0 0 16px;
}
.season-title {
  font-weight: 600;
  font-size: 14px;
  color: var(--text);
}
.television-head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 14px;
  min-width: 0;
}
.television-head h3 {
  font-size: 18px;
  margin: 0;
  overflow-wrap: anywhere;
}
.television-head .hint {
  margin: 0;
  white-space: nowrap;
}
.btn-tv-back {
  width: 32px;
  height: 32px;
  border-radius: var(--radius-sm);
}
.television-seasons {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
}
.season-tab {
  border-radius: 999px;
  padding: 0 14px;
  height: 30px;
  font-size: 12px;
  transition: all 0.15s ease;
}
.season-active {
  background: var(--accent) !important;
  border-color: var(--accent) !important;
  color: #ffffff !important;
  font-weight: 600;
  box-shadow: 0 2px 8px rgba(26, 113, 255, 0.35);
}
.television-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(134px, 1fr));
  gap: 20px 16px;
  margin-bottom: 16px;
}
.episode-grid {
  grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
}
.television-card {
  appearance: none;
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
  cursor: pointer;
  text-align: left;
  min-width: 0;
  display: grid;
  align-content: start;
  gap: 6px;
}
.television-card:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 4px;
}
.television-cover {
  position: relative;
  aspect-ratio: 2 / 3;
  overflow: hidden;
  border-radius: 10px;
  border: 1px solid var(--line);
  background: var(--panel-3);
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: var(--shadow);
  transition: border-color 0.2s ease, box-shadow 0.2s ease;
}
.episode-grid .television-cover {
  aspect-ratio: 16 / 9;
}
.television-entry:hover .television-cover,
.television-card:focus-visible .television-cover {
  border-color: var(--accent);
  box-shadow: 0 10px 24px rgba(26, 113, 255, 0.25);
}
.television-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  transition: transform 0.25s ease, filter 0.25s ease;
}
.television-entry:hover .television-cover img {
  transform: scale(1.04);
  filter: brightness(0.9);
}
.television-cover span {
  padding: 12px;
  font-size: 13px;
  overflow-wrap: anywhere;
  max-width: 100%;
  text-align: center;
}
.television-card strong {
  font-size: 13.5px;
  font-weight: 600;
  line-height: 1.4;
  overflow-wrap: anywhere;
  transition: color 0.15s ease;
}
.television-entry:hover strong {
  color: var(--accent);
}
.television-card small {
  font-size: 11.5px;
  color: var(--dim);
  overflow-wrap: anywhere;
}
@media (max-width: 480px) {
  .television-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 16px 12px;
  }
}
</style>
