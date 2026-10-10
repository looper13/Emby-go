<script setup lang="ts">
import { computed, inject, nextTick, onScopeDispose, ref, shallowRef, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { catalogApi } from '../api/catalog';
import type { DetailDto, SimilarDto } from '../api/contracts';
import { usePageRequest } from '../composables/usePageRequest';
import { isValidItemId } from '../router/legacy-url';
import { viewHistoryKey } from '../router/view-history';
import ItemHero from '../components/media/ItemHero.vue';
import { useTasksStore } from '../stores/tasks';
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
const player = usePlayerStore(); const actions = useMediaActions(() => enter(true), () => route.fullPath);
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
function escape(event: KeyboardEvent) {
  if (event.key !== 'Escape' || event.defaultPrevented || (event.target instanceof Element
    && event.target.closest('input, textarea, select, [contenteditable="true"], [role="dialog"]'))) return;
  event.preventDefault(); back();
}
window.addEventListener('keydown', escape);
onScopeDispose(() => window.removeEventListener('keydown', escape));
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
    <ItemHero :movie="data.movie" :images="data.images" :playable="data.playable" :busy="actions.busy.value" @back="back" @scrape="scrape" @play="player.media(data.movie)" @probe="actions.run('probe', data.movie.id)" @reread="actions.run('reread', data.movie.id)" @entity="entity" />
    <div class="item-sections" style="grid-template-columns:minmax(0,1fr)">
      <PlotSection :plot="data.movie.Plot" :expanded="plotExpanded" @toggle="plotExpanded = !plotExpanded" />
      <TrailerSection :url="data.movie.trailer_url?.trim() || ''" :thumb="trailerThumb" :fallback="data.movie.cover_url" @play="player.trailer(data.movie, data.movie.trailer_url || '')" />
      <ArtworkGallery :id="data.movie.id" :images="backdrops" />
      <ActorList :actors="data.actors ?? []" @entity="entity" />
      <SimilarItems :items="similar" @open="openSimilar" />
      <FileDetails :files="data.files ?? []" />
      <MetadataList :movie="data.movie" :library-name="data.library_name" @entity="entity" />
      <FileTimeline :movie="data.movie" :modified-at="data.modified_at" :expanded="filesExpanded" @expanded="filesExpanded = $event" />
    </div>
  </article>
  <section v-else class="panel"><p v-if="error" role="alert">{{ error }} <button class="btn" @click="enter()">重试</button></p><p v-else>加载中…</p><button class="btn" @click="back">← 返回媒体墙</button></section>
</template>
