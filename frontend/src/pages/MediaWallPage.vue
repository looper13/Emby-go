<script setup lang="ts">
import { computed, inject, nextTick, onScopeDispose, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { catalogApi } from '../api/catalog';
import type { LibraryDto } from '../api/contracts';
import { usePageRequest } from '../composables/usePageRequest';
import { useMediaWallStore, WALL_PAGE_SIZE } from '../stores/media-wall';
import { viewHistoryKey, saveViewPosition, type ViewPosition } from '../router/view-history';
import { useTasksStore } from '../stores/tasks';
import { useAuthStore } from '../stores/auth';
import BackgroundTaskProgress from '../components/tasks/BackgroundTaskProgress.vue';
import MediaGrid from '../components/media/MediaGrid.vue';
import WallFilters from '../components/media/WallFilters.vue';
import WallLoadState from '../components/media/WallLoadState.vue';
import EmptyState from '../components/common/EmptyState.vue';
import { entityParams, statusText } from '../lib/media';
import { useMediaActions } from '../composables/useMediaActions';
import { usePlayerStore } from '../stores/player';
import TelevisionBrowser from '../components/media/TelevisionBrowser.vue';
import FavoriteButton from '../components/media/FavoriteButton.vue';
import { favoriteEntityId, favoritesApi, type FavoriteItem } from '../api/favorites';

const route = useRoute(); const router = useRouter();
const history = inject(viewHistoryKey)!;
const wall = useMediaWallStore(); const scope = usePageRequest();
const tasks = useTasksStore();
const auth = useAuthStore();
const player = usePlayerStore();
const libraries = ref<LibraryDto[]>([]); const pageError = ref('');
const search = ref(''); const ready = ref(false); const more = ref<HTMLElement | null>(null);
const actions = useMediaActions(refresh, () => route.fullPath);
const entity = computed(() => entityParams.map(([key, label]) => [key, label, value(key)]).find(row => row[2]));
const entityItem = ref<FavoriteItem | null>(null), entityScope = usePageRequest();
watch(entity, async currentEntity => {
  const current = entityScope.begin(); entityItem.value = null;
  if (!currentEntity) return;
  try {
    const item = await favoritesApi.item(favoriteEntityId(currentEntity[0]!, currentEntity[2]!), current.signal);
    if (current.isCurrent()) entityItem.value = item;
  } catch { /* Unknown or no longer visible entities cannot be favorited. */ }
}, { immediate: true });
const library = computed(() => libraries.value.find(lib => String(lib.Id) === value('library_id')));
const televisionLibrary = computed(() => library.value?.Type === 'tvshows' || library.value?.Type === 'mixed');
const browseTelevision = computed(() => televisionLibrary.value && value('view') !== 'index' && !value('status') && !entity.value && !value('scrape') && !value('favorite'));
const showWall = computed(() => !browseTelevision.value || library.value?.Type !== 'tvshows');
const scopeLabel = computed(() => [library.value?.Name, entity.value ? `${entity.value[1]}：${entity.value[2]}` : '', statusText[value('status')] || value('status')].filter(Boolean).join(' · '));
const emptyTitle = computed(() => { const what = entity.value ? `「${entity.value[2]}」的影片` : library.value ? `「${library.value.Name}」的影片` : '影片'; return value('status') ? `没有 ${statusText[value('status')] || value('status')} 的${what}` : `没有符合条件的${what}`; });
let request: ReturnType<typeof scope.begin> | undefined;
let timer: ReturnType<typeof setTimeout> | undefined;
function value(key: string) { const v = route.query[key]; return typeof v === 'string' ? v : ''; }
function query() {
  const output = new URLSearchParams();
  for (const [key, v] of Object.entries(route.query)) if (typeof v === 'string' && key !== 'offset' && key !== 'limit') output.set(key, v);
  if (browseTelevision.value && library.value?.Type === 'mixed') output.set('media_type', 'Movie');
  return output;
}
function filter(key: string, v: string, toggle = false) {
  clearTimeout(timer);
  const q = { ...route.query }; if (v && !(toggle && value(key) === v)) q[key] = v; else delete q[key];
  if (key === 'library_id' || key === 'view') { delete q.tv_series; delete q.tv_season; }
  if (key === 'status') delete q.search;
  if (key === 'sort') delete q.order;
  void router.push({ path: '/items', query: q });
}
function searchChanged() {
  clearTimeout(timer);
  timer = setTimeout(() => {
    const q = { ...route.query }; delete q.tv_series; delete q.tv_season;
    if (search.value.trim()) q.search = search.value.trim(); else delete q.search;
    void router.replace({ path: '/items', query: q });
  }, 400);
}
function browseSeries(series: string, season: string) {
  const q = { ...route.query };
  if (series) q.tv_series = series; else delete q.tv_series;
  if (season) q.tv_season = season; else delete q.tv_season;
  void router.replace({ path: '/items', query: q });
}
function clearEntity() { clearTimeout(timer); const q = { ...route.query }; for (const [key] of entityParams) delete q[key]; void router.push({ path: '/items', query: q }); }
function refresh() {
  const card = document.activeElement?.closest<HTMLElement>('.wall-card');
  return enter({ scrollY: window.scrollY, wallCount: wall.items.length, focusID: card?.dataset.play ?? '' });
}
function reshuffle() {
  wall.clear();
  void enter({ scrollY: 0, wallCount: WALL_PAGE_SIZE, focusID: '' });
}
function quickplay(id: number) { const movie = wall.items.find(m => m.id === id); if (movie) player.media(movie); }
async function load(retry = false) {
  if (!request?.isCurrent()) return false;
  const loaded = await wall.loadMore(request.signal, retry);
  await nextTick();
  return loaded;
}
function maybeLoad() {
  if (showWall.value && ready.value && !wall.error && !wall.loading && !wall.done
    && more.value && more.value.getBoundingClientRect().top <= innerHeight + 600) void load().then(maybeLoad);
}
async function enter(position?: ViewPosition) {
  clearTimeout(timer); wall.cancel();
  const current = scope.begin(); request = current;
  const restore = position ?? history.target(); ready.value = false; pageError.value = '';
  search.value = value('search');
  if (!restore) window.scrollTo(0, 0);
  try {
    const result = await catalogApi.libraries(current.signal);
    if (!current.isCurrent()) return;
    libraries.value = (result.items ?? []).filter(lib => lib.SourceType !== 'anime_catalog' && lib.Enabled !== false);
    if (value('library_id') && !libraries.value.some(lib => String(lib.Id) === value('library_id'))) {
      const q = { ...route.query }; delete q.library_id;
      await router.replace({ path: '/items', query: q }); return;
    }
    const cached = wall.prepare(query(), Boolean(restore));
    if (!cached) {
      const count = Math.max(WALL_PAGE_SIZE, restore?.wallCount ?? WALL_PAGE_SIZE);
      do { if (!await load()) break; } while (current.isCurrent() && wall.items.length < count && !wall.done);
    }
    if (!current.isCurrent()) return;
    ready.value = true; await nextTick();
    if (restore) {
      const card = Array.from(document.querySelectorAll<HTMLElement>('.wall-card')).find(card => card.dataset.play === restore.focusID);
      card?.focus({ preventScroll: true }); window.scrollTo(0, restore.scrollY);
    }
    maybeLoad();
  } catch (cause) {
    if (current.isCurrent()) pageError.value = cause instanceof Error ? cause.message : '加载失败';
  }
}
function open(id: number) {
  // Mobile clicks do not necessarily focus an article: save the explicit target.
  saveViewPosition({ scrollY: window.scrollY, wallCount: wall.items.length, focusID: String(id) });
  document.querySelector<HTMLElement>(`.wall-card[data-play="${id}"]`)?.focus({ preventScroll: true });
  void router.push({ path: `/item/${id}`, query: route.query });
}
window.addEventListener('scroll', maybeLoad, { passive: true });
onScopeDispose(() => { clearTimeout(timer); window.removeEventListener('scroll', maybeLoad); wall.cancel(); });
watch(() => route.fullPath, () => { if (route.name === 'items') void enter(); }, { immediate: true });
watch(() => tasks.revision, () => {
  void refresh();
});
</script>
<template>
  <section class="panel">
    <div class="panel-head wall-head"><h2>媒体墙</h2><div class="panel-actions"><span id="wall-count" class="hint" style="margin:0">共 {{ wall.total }} 条{{ scopeLabel ? ' · ' + scopeLabel : '' }} · 已加载 {{ wall.items.length }}</span><button v-if="auth.isAdministrator" id="probe-media" class="btn" :disabled="tasks.mutationBusy" @click="tasks.probe.run({only_missing:true})">{{ tasks.probe.busy ? '探测中…' : '探测媒体信息' }}</button></div></div>
    <p v-if="pageError" role="alert">{{ pageError }} <button class="btn" @click="enter()">重试</button></p>
    <BackgroundTaskProgress v-if="auth.isAdministrator" kind="probe" :progress="tasks.probe.progress" :visible="tasks.probe.visible" :busy="tasks.probe.busy" :unavailable="tasks.probe.unavailable" :cancelling="tasks.probe.cancelling" @cancel="tasks.probe.cancel()" />
    <WallFilters :can-manage="auth.isAdministrator" :query="route.query" :libraries="libraries" :search="search" @filter="filter" @search="search = $event; searchChanged()" @clear-entity="clearEntity" @shuffle="reshuffle"><template #entity-action><FavoriteButton v-if="entityItem" :item-id="entityItem.Id" :favorite="entityItem.UserData.IsFavorite" :label="entityItem.Name" /></template></WallFilters>
    <div v-if="televisionLibrary && !value('status') && !entity" class="television-views" role="tablist" aria-label="媒体视图"><button class="btn btn-sm" role="tab" :aria-selected="browseTelevision" @click="filter('view','')">{{ library?.Type === 'mixed' ? '影片与电视节目' : '电视节目' }}</button><button class="btn btn-sm" role="tab" :aria-selected="!browseTelevision" @click="filter('view','index')">全部媒体索引</button></div>
    <TelevisionBrowser v-if="browseTelevision && library" :key="library.Id" :library="library.Id" :search="value('search')" :revision="tasks.revision" :series="value('tv_series')" :season="value('tv_season')" @browse="browseSeries" @open="open" />
    <h3 v-if="browseTelevision && library?.Type === 'mixed'">影片</h3>
    <MediaGrid :can-manage="auth.isAdministrator" v-if="showWall" :items="wall.items" :image-tags="wall.imageTags" :userdata="wall.userdata" :busy="actions.busy.value" @open="open" @quickplay="quickplay" @probe="actions.run('probe', $event)" @reread="actions.run('reread', $event)" @remove="actions.run('remove', $event)" />
    <div id="wall-empty"><EmptyState v-if="showWall && ready && !wall.error && wall.total === 0" :title="emptyTitle" :hint="value('search') ? '试试其它关键词。' : auth.isAdministrator ? '开始扫描或手动补录后再来看看。' : ''" /></div>
    <div v-show="showWall" id="wall-more" ref="more" class="wall-more">
      <WallLoadState :error="wall.error" :loading="wall.loading" :ready="ready" :done="wall.done" :total="wall.total" @retry="load(true).then(maybeLoad)" />
    </div>
  </section>
</template>
<style scoped>
.television-views { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 16px; }
.television-views [aria-selected="true"] { border-color: var(--accent); color: var(--accent); }
.wall-head { flex-wrap: wrap; }
.wall-head h2 { flex-shrink: 0; white-space: nowrap; }
.wall-head .panel-actions { flex-wrap: wrap; min-width: 0; }
.wall-head #wall-count { overflow-wrap: anywhere; }
</style>
