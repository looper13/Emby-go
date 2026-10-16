<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { ArrowLeft, RefreshCw } from '@lucide/vue';
import { televisionApi, type TelevisionItem } from '../../api/television';
import { usePageRequest } from '../../composables/usePageRequest';
import MediaImage from './MediaImage.vue';
import { imageURL } from '../../lib/image-url';
const props = defineProps<{ library: number; search: string; revision: number; series?: string; season?: string }>();
const emit = defineEmits<{ open: [id: number]; browse: [series: string, season: string] }>();
const scope = usePageRequest();
const items = ref<TelevisionItem[]>([]); const seasons = ref<TelevisionItem[]>([]);
const selected = ref<TelevisionItem>(); const season = ref('');
const loading = ref(false); const error = ref(''); const total = ref(0);
const retryMore = ref(false);
const title = computed(() => selected.value?.Name || '电视节目');
async function load(more = false) {
  const current = scope.begin(); loading.value = true; error.value = '';
  retryMore.value = more;
  const offset = more ? items.value.length : 0;
  if (!more) items.value = [];
  try {
    const result = selected.value
      ? await televisionApi.episodes(selected.value.Id, season.value, offset, current.signal)
      : await televisionApi.list(props.library, props.search, offset, current.signal);
    if (current.isCurrent()) { items.value = more ? [...items.value, ...result.items] : result.items; total.value = result.total; }
  } catch (cause) { if (current.isCurrent()) error.value = cause instanceof Error ? cause.message : '加载失败'; }
  finally { if (current.isCurrent()) loading.value = false; }
}
async function select(item: TelevisionItem) {
  if (item.Type === 'Episode') { emit('open', Number(item.Id)); return; }
  emit('browse', item.Id, '');
}
async function loadSeasons() {
  if (!props.series) return;
  const current = scope.begin(); loading.value = true; error.value = '';
  try {
    if (!props.series.startsWith(`tv-series:${props.library}:`)) throw new Error('剧集不属于当前媒体库');
    const item = await televisionApi.series(props.series, current.signal);
    if (!current.isCurrent()) return;
    const result = await televisionApi.seasons(item.Id, current.signal);
    if (!current.isCurrent()) return;
    selected.value = item;
    seasons.value = result.items;
    season.value = result.items.find(s => s.Id === props.season)?.Id
      ?? result.items.find(s => s.IndexNumber > 0)?.Id ?? result.items[0]?.Id ?? '';
    if (props.season && season.value !== props.season) emit('browse', item.Id, season.value);
    if (season.value) await load(); else { items.value = []; total.value = 0; loading.value = false; }
  } catch (cause) { if (current.isCurrent()) { error.value = cause instanceof Error ? cause.message : '加载失败'; loading.value = false; } }
}
function back() { emit('browse', '', ''); }
function retry() { if (props.series && !seasons.value.length) void loadSeasons(); else void load(retryMore.value); }
function enter() {
  selected.value = undefined; seasons.value = []; season.value = ''; total.value = 0; items.value = [];
  retryMore.value = false;
  if (props.series) void loadSeasons(); else void load();
}
watch(() => [props.library, props.search, props.revision, props.series, props.season], enter, { immediate:true });
</script>
<template>
  <div class="television-browser">
    <div class="television-head"><button v-if="selected || series" class="icon-btn" title="返回电视节目" @click="back"><ArrowLeft :size="18" /></button><h3>{{ title }}</h3><span class="hint">{{ total }} {{ selected ? '集' : '部' }}</span></div>
    <div v-if="selected && seasons.length" class="television-seasons" role="tablist" aria-label="季"><button v-for="s in seasons" :key="s.Id" role="tab" :aria-selected="season === s.Id" class="btn btn-sm" :class="{'season-active':season === s.Id}" @click="emit('browse',selected.Id,s.Id)">{{ s.Name }}</button></div>
    <p v-if="error" role="alert">{{ error }} <button class="btn btn-sm" @click="retry"><RefreshCw :size="14" /><span>重试</span></button></p>
    <div class="television-grid" :class="{'episode-grid':selected}">
      <button v-for="item in items" :key="item.Id" class="television-card" :data-tv-id="item.Id" @click="select(item)">
        <div class="television-cover"><MediaImage v-if="item.ImageTags.Primary" :src="imageURL(item.Id,'Primary',320,item.ImageTags.Primary)" alt="" loading="lazy" /><span v-else class="image-placeholder">{{ item.Name }}</span></div>
        <strong>{{ selected ? `${item.IndexNumber}. ${item.Name}` : item.Name }}</strong><small>{{ selected ? item.SeriesName : [item.ProductionYear || '',`${item.ChildCount} 季`,`${item.RecursiveItemCount} 集`].filter(Boolean).join(' · ') }}</small>
      </button>
    </div>
    <p v-if="loading" role="status">加载中…</p>
    <p v-else-if="!error && !items.length" class="hint">{{ selected ? '暂无单集' : '暂无电视节目' }}</p>
    <button v-if="items.length < total && !error" class="btn btn-sm" :disabled="loading" @click="load(true)">加载更多</button>
  </div>
</template>
<style scoped>
.television-browser { margin: 18px 0 24px; min-width: 0; }
.television-head { display: flex; align-items: center; gap: 12px; margin-bottom: 14px; min-width: 0; }
.television-head h3 { font-size: 18px; margin: 0; overflow-wrap: anywhere; }
.television-head .hint { margin: 0; white-space: nowrap; }
.television-seasons { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 16px; }
.season-active { border-color: var(--accent); color: var(--accent); }
.television-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(130px, 1fr)); gap: 20px 16px; margin-bottom: 16px; }
.episode-grid { grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); }
.television-card { appearance: none; padding: 0; border: 0; background: transparent; color: inherit; cursor: pointer; text-align: left; min-width: 0; display: grid; align-content: start; gap: 6px; }
.television-card:focus-visible { outline: 2px solid var(--accent); outline-offset: 4px; }
.television-cover { aspect-ratio: 2/3; overflow: hidden; border-radius: 6px; background: var(--panel-3); display: flex; align-items: center; justify-content: center; }
.episode-grid .television-cover { aspect-ratio: 16/9; }
.television-cover img { width: 100%; height: 100%; object-fit: cover; }
.television-cover span { padding: 12px; font-size: 14px; overflow-wrap: anywhere; max-width: 100%; }
.television-card strong { font-size: 14px; line-height: 1.5; overflow-wrap: anywhere; }
.television-card small { font-size: 12px; color: var(--muted); overflow-wrap: anywhere; }
@media (max-width: 480px) { .television-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px 12px; } }
</style>
