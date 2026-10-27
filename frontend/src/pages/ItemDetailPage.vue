<script setup lang="ts">
import { computed, inject, nextTick, onScopeDispose, ref, shallowRef, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { catalogApi } from '../api/catalog';
import { televisionApi, type TelevisionItem } from '../api/television';
import type { DetailDto, SimilarDto } from '../api/contracts';
import { usePageRequest } from '../composables/usePageRequest';
import { isValidItemId } from '../router/legacy-url';
import { viewHistoryKey, saveViewPosition } from '../router/view-history';
import { useMediaWallStore } from '../stores/media-wall';
import ItemHero from '../components/media/ItemHero.vue';
import DetailPlaybackControls from '../components/media/DetailPlaybackControls.vue';
import type { TrackSelection } from '../lib/player-tracks';
import { useTasksStore } from '../stores/tasks';
import { useAuthStore } from '../stores/auth';
import { useToastStore } from '../stores/toasts';
import { usePlayerStore } from '../stores/player';
import { useMediaActions } from '../composables/useMediaActions';
import { mediaApi } from '../api/media';
import { imageURL } from '../lib/image-url';
import PlotSection from '../components/media/PlotSection.vue';
import TrailerSection from '../components/media/TrailerSection.vue';
import ArtworkGallery from '../components/media/ArtworkGallery.vue';
import ActorList from '../components/media/ActorList.vue';
import SimilarItems from '../components/media/SimilarItems.vue';
import MetadataList from '../components/media/MetadataList.vue';
import FileDetails from '../components/media/FileDetails.vue';
import FileTimeline from '../components/media/FileTimeline.vue';
import { useScrapePreviewStore } from '../stores/scrape-preview';
const emit = defineEmits<{ title: [value: string] }>();
const route = useRoute(); const router = useRouter(); const history = inject(viewHistoryKey)!;
const scope = usePageRequest(); const data = shallowRef<DetailDto | null>(null); const error = ref('');
const tasks = useTasksStore(); const toasts = useToastStore();
const auth = useAuthStore();
const player = usePlayerStore(); const actions = useMediaActions(() => enter(true), () => route.fullPath);
const playbackSelection = shallowRef<TrackSelection>();
const selectedVersionId = ref<number>();
const selectedVersion = computed(() => data.value?.versions?.find(version => version.id === selectedVersionId.value));
function play() { if (data.value) player.media(data.value.movie, playbackSelection.value, selectedVersion.value); }
const scrapePreview = useScrapePreviewStore(); let mounted = true; onScopeDispose(() => { mounted = false; });
function scrape() { if (!data.value || tasks.mutationBusy) return; const path = route.fullPath, id = data.value.movie.id; void scrapePreview.open(id, () => { if (route.fullPath === path && data.value?.movie.id === id && mounted) return enter(true); }); }
const plotExpanded = ref(false); const filesExpanded = ref(false); const similar = shallowRef<SimilarDto[]>([]); let similarLoaded = false;
const backdrops = computed(() => data.value?.images?.filter(image => image.ImageType === 'Backdrop') ?? []);
const trailerThumb = computed(() => {
  if (!data.value) return ''; const m = data.value.movie; const first = backdrops.value[0]; const tag = (type: string) => data.value?.images?.find(i => i.ImageType === type)?.ImageTag;
  const poster = m.PosterPath ? imageURL(m.id, 'Primary', 320, tag('Primary')) : m.cover_url.trim();
  return first ? imageURL(m.id, 'Backdrop', 840, first.ImageTag, first.ImageIndex) : m.BackdropPath ? imageURL(m.id, 'Backdrop', 1280, tag('Backdrop'), 0) : m.LandscapePath ? imageURL(m.id, 'Thumb', 1280, tag('Thumb')) : poster;
});
function entity(key: string, value: string) { void router.push({ path: '/items', query: { [key]: value, ...(typeof route.query.library_id === 'string' ? { library_id: route.query.library_id } : {}) } }); }
function openSimilar(id: string) { void router.push({ path: `/item/${id}`, query: route.query }); }
function back() { history.backFromItem(route.query); }

const wall = useMediaWallStore();
const currentId = computed(() => Number(route.params.id));
const tvEpisodes = shallowRef<TelevisionItem[]>([]);

const tvIndex = computed(() => {
  if (!tvEpisodes.value.length) return -1;
  return tvEpisodes.value.findIndex(item => String(item.Id) === String(route.params.id));
});

const wallIndex = computed(() => {
  return wall.items.findIndex(item => item.id === currentId.value);
});

const hasPrev = computed(() => {
  if (tvIndex.value !== -1) return tvIndex.value > 0;
  if (wallIndex.value !== -1) return wallIndex.value > 0;
  return false;
});

const hasNext = computed(() => {
  if (tvIndex.value !== -1) return tvIndex.value < tvEpisodes.value.length - 1;
  if (wallIndex.value !== -1) return wallIndex.value < wall.items.length - 1 || !wall.done;
  return false;
});

const prevTitle = computed(() => {
  if (tvIndex.value > 0) return tvEpisodes.value[tvIndex.value - 1]?.Name || '';
  if (wallIndex.value > 0) return wall.items[wallIndex.value - 1]?.Title || '';
  return '';
});

const nextTitle = computed(() => {
  if (tvIndex.value >= 0 && tvIndex.value < tvEpisodes.value.length - 1) {
    return tvEpisodes.value[tvIndex.value + 1]?.Name || '';
  }
  if (wallIndex.value >= 0 && wallIndex.value < wall.items.length - 1) {
    return wall.items[wallIndex.value + 1]?.Title || '';
  }
  return '';
});

function openItem(id: number) {
  saveViewPosition({ scrollY: window.scrollY, wallCount: wall.items.length, focusID: String(id) });
  void router.push({ path: `/item/${id}`, query: route.query });
}

function prev() {
  if (tvIndex.value > 0) {
    openItem(Number(tvEpisodes.value[tvIndex.value - 1]!.Id));
  } else if (wallIndex.value > 0) {
    openItem(wall.items[wallIndex.value - 1]!.id);
  }
}

async function next() {
  if (tvIndex.value >= 0 && tvIndex.value < tvEpisodes.value.length - 1) {
    openItem(Number(tvEpisodes.value[tvIndex.value + 1]!.Id));
  } else if (wallIndex.value >= 0 && wallIndex.value < wall.items.length - 1) {
    openItem(wall.items[wallIndex.value + 1]!.id);
  } else if (wallIndex.value >= 0 && !wall.done) {
    const current = scope.begin();
    const loaded = await wall.loadMore(current.signal);
    if (loaded && wallIndex.value < wall.items.length - 1) {
      openItem(wall.items[wallIndex.value + 1]!.id);
    }
  }
}

function escape(event: KeyboardEvent) {
  if (event.key !== 'Escape' || event.defaultPrevented || (event.target instanceof Element
    && event.target.closest('input, textarea, select, [contenteditable="true"], [role="dialog"]'))) return;
  event.preventDefault(); back();
}
function keyNav(event: KeyboardEvent) {
  if (event.defaultPrevented || (event.target instanceof Element
    && event.target.closest('input, textarea, select, [contenteditable="true"], [role="dialog"]'))
    || player.current || document.querySelector('#artwork-overlay')) return;
  if ((event.key === 'ArrowLeft' || event.key === '[') && hasPrev.value) {
    event.preventDefault();
    prev();
  } else if ((event.key === 'ArrowRight' || event.key === ']') && hasNext.value) {
    event.preventDefault();
    void next();
  }
}
window.addEventListener('keydown', escape);
window.addEventListener('keydown', keyNav);
onScopeDispose(() => {
  window.removeEventListener('keydown', escape);
  window.removeEventListener('keydown', keyNav);
});
async function enter(preserve = false) {
  const current = scope.begin(); const id = String(route.params.id); const restore = history.target();
  const scrollY = window.scrollY;
  if (!preserve) { data.value = null; similar.value = []; similarLoaded = false; plotExpanded.value = false; filesExpanded.value = false; window.scrollTo(0, 0); }
  error.value = '';
  if (!isValidItemId(id)) { await router.replace({ path: '/items', query: route.query }); return; }
  if (!similarLoaded) void mediaApi.similar(id, current.signal).then(items => { if (current.isCurrent()) { similar.value = items; similarLoaded = true; } }).catch(() => {});
  try {
    const detail = await catalogApi.detail(id, current.signal);
    if (!current.isCurrent()) return;
    data.value = detail;
    if (!preserve || !detail.versions?.some(version => version.id === selectedVersionId.value && version.playable)) {
      selectedVersionId.value = detail.versions?.find(version => version.id === detail.movie.id && version.playable)?.id
        ?? detail.versions?.find(version => version.playable)?.id ?? detail.movie.id;
    }

    if (typeof route.query.tv_series === 'string' && route.query.tv_series) {
      void televisionApi.episodes(route.query.tv_series, typeof route.query.tv_season === 'string' ? route.query.tv_season : '', 0, current.signal).then(res => {
        if (current.isCurrent()) tvEpisodes.value = res.items;
      }).catch(() => {});
    } else if (wall.items.length === 0) {
      const q = new URLSearchParams();
      for (const [k, v] of Object.entries(route.query)) {
        if (typeof v === 'string' && k !== 'offset' && k !== 'limit') q.set(k, v);
      }
      if (!q.has('library_id') && detail.movie.library_id) {
        q.set('library_id', String(detail.movie.library_id));
      }
      wall.prepare(q, false);
      void wall.loadMore(current.signal);
    }

    await nextTick();
    if (!current.isCurrent()) return;
    emit('title', detail.movie.Title || id);
    if (preserve) window.scrollTo(0, scrollY); else if (restore) window.scrollTo(0, restore.scrollY);
  } catch (cause) {
    if (current.isCurrent()) { error.value = cause instanceof Error ? cause.message : '加载失败'; if (preserve) toasts.show(error.value, 'error'); }
  }
}
watch(() => route.fullPath, () => { if (route.name === 'item') void enter(); }, { immediate: true });
watch(() => tasks.revision, () => { void enter(true); });
</script>
<template>
  <article v-if="data" class="item-page" style="grid-template-columns:minmax(0,1fr)">
    <ItemHero
      :can-manage="auth.isAdministrator"
      :movie="data.movie"
      :favorite="data.favorite"
      :images="data.images"
      :playable="data.playable"
      :busy="actions.busy.value"
      :has-prev="hasPrev"
      :has-next="hasNext"
      :prev-title="prevTitle"
      :next-title="nextTitle"
      @back="back"
      @scrape="scrape"
      @play="play"
      @probe="actions.run('probe', data.movie.id)"
      @reread="actions.run('reread', data.movie.id)"
      @entity="entity"
      @prev="prev"
      @next="next"
    >
      <template #playback>
        <label v-if="(data.versions?.length ?? 0) > 1" class="version-choice"><span>播放版本</span><select v-model.number="selectedVersionId" aria-label="播放版本">
          <option v-for="version in data.versions" :key="version.id" :value="version.id" :disabled="!version.playable">{{ version.name }}{{ version.playable ? '' : ' · 不可播放' }}</option>
        </select></label>
        <DetailPlaybackControls v-if="data.playable" :key="selectedVersionId" :item-id="selectedVersionId ?? data.movie.id" :refresh-key="data" @selection="playbackSelection = $event" />
      </template>
    </ItemHero>
    <div class="item-sections" style="grid-template-columns:minmax(0,1fr)">
      <PlotSection :plot="data.movie.Plot" :expanded="plotExpanded" @toggle="plotExpanded = !plotExpanded" />
      <TrailerSection :url="data.movie.trailer_url?.trim() || ''" :thumb="trailerThumb" :fallback="data.movie.cover_url" @play="player.trailer(data.movie, data.movie.trailer_url || '')" />
      <ArtworkGallery :id="data.movie.id" :images="backdrops" />
      <ActorList :actors="data.actors ?? []" @entity="entity" />
      <SimilarItems :items="similar" @open="openSimilar" />
      <FileDetails v-if="auth.isAdministrator" :files="data.files ?? []" />
      <MetadataList :movie="data.movie" :library-name="data.library_name" @entity="entity" />
      <FileTimeline v-if="auth.isAdministrator" :movie="data.movie" :modified-at="data.modified_at" :expanded="filesExpanded" @expanded="filesExpanded = $event" />
    </div>
  </article>
  <section v-else class="panel"><p v-if="error" role="alert">{{ error }} <button class="btn" @click="enter()">重试</button></p><p v-else>加载中…</p><button class="btn" @click="back">← 返回媒体墙</button></section>
</template>
<style scoped>
.item-sections { min-width: 0; max-width: none; }
.version-choice { display: flex; flex-direction: column; gap: 6px; max-width: min(100%, 560px); margin-top: 16px; color: var(--muted); font-size: 12px; }
.version-choice select { width: 100%; min-height: 38px; padding: 0 10px; border: 1px solid var(--line); border-radius: 8px; background: var(--panel-2); color: var(--text); font: inherit; font-size: 13px; }
</style>
