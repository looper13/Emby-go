<script setup lang="ts">
import { computed, inject, nextTick, onScopeDispose, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { catalogApi } from '../api/catalog';
import type { LibraryDto } from '../api/contracts';
import { usePageRequest } from '../composables/usePageRequest';
import { useMediaWallStore, WALL_PAGE_SIZE } from '../stores/media-wall';
import { viewHistoryKey, saveViewPosition, type ViewPosition } from '../router/view-history';
import { useTasksStore } from '../stores/tasks';
import BackgroundTaskProgress from '../components/tasks/BackgroundTaskProgress.vue';
import MediaGrid from '../components/media/MediaGrid.vue';
import WallFilters from '../components/media/WallFilters.vue';
import WallLoadState from '../components/media/WallLoadState.vue';
import EmptyState from '../components/common/EmptyState.vue';
import { entityParams, statusText } from '../lib/media';
import { useMediaActions } from '../composables/useMediaActions';
import { usePlayerStore } from '../stores/player';

const route = useRoute(); const router = useRouter();
const history = inject(viewHistoryKey)!;
const wall = useMediaWallStore(); const scope = usePageRequest();
const tasks = useTasksStore();
const player = usePlayerStore();
const libraries = ref<LibraryDto[]>([]); const pageError = ref('');
const search = ref(''); const ready = ref(false); const more = ref<HTMLElement | null>(null);
const actions = useMediaActions(refresh, () => route.fullPath);
const entity = computed(() => entityParams.map(([key, label]) => [key, label, value(key)]).find(row => row[2]));
const library = computed(() => libraries.value.find(lib => String(lib.Id) === value('library_id')));
const scopeLabel = computed(() => [library.value?.Name, entity.value ? `${entity.value[1]}：${entity.value[2]}` : '', statusText[value('status')] || value('status')].filter(Boolean).join(' · '));
const emptyTitle = computed(() => { const what = entity.value ? `「${entity.value[2]}」的影片` : library.value ? `「${library.value.Name}」的影片` : '影片'; return value('status') ? `没有 ${statusText[value('status')] || value('status')} 的${what}` : `没有符合条件的${what}`; });
let request: ReturnType<typeof scope.begin> | undefined;
let timer: ReturnType<typeof setTimeout> | undefined;
function value(key: string) { const v = route.query[key]; return typeof v === 'string' ? v : ''; }
function query() {
  const output = new URLSearchParams();
  for (const [key, v] of Object.entries(route.query)) if (typeof v === 'string' && key !== 'offset' && key !== 'limit') output.set(key, v);
  return output;
}
function filter(key: string, v: string, toggle = false) {
  clearTimeout(timer);
  const q = { ...route.query }; if (v && !(toggle && value(key) === v)) q[key] = v; else delete q[key];
  if (key === 'status') delete q.search;
  if (key === 'sort') delete q.order;
  void router.push({ path: '/items', query: q });
}
function searchChanged() {
  clearTimeout(timer);
  timer = setTimeout(() => {
    const q = { ...route.query }; if (search.value.trim()) q.search = search.value.trim(); else delete q.search;
    void router.replace({ path: '/items', query: q });
  }, 400);
}
function clearEntity() { clearTimeout(timer); const q = { ...route.query }; for (const [key] of entityParams) delete q[key]; void router.push({ path: '/items', query: q }); }
function refresh() {
  const card = document.activeElement?.closest<HTMLElement>('.wall-card');
  return enter({ scrollY: window.scrollY, wallCount: wall.items.length, focusID: card?.dataset.play ?? '' });
}
function quickplay(id: number) { const movie = wall.items.find(m => m.id === id); if (movie) player.media(movie); }
async function load(retry = false) {
  if (!request?.isCurrent()) return false;
  const loaded = await wall.loadMore(request.signal, retry);
  await nextTick();
  return loaded;
}
function maybeLoad() {
  if (ready.value && !wall.error && !wall.loading && !wall.done
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
    libraries.value = result.items ?? [];
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
    <div class="panel-head"><h2>媒体墙</h2><div class="panel-actions"><span id="wall-count" class="hint" style="margin:0">共 {{ wall.total }} 条{{ scopeLabel ? ' · ' + scopeLabel : '' }} · 已加载 {{ wall.items.length }}</span><button id="probe-media" class="btn" :disabled="tasks.mutationBusy" @click="tasks.probe.run({only_missing:true})">{{ tasks.probe.busy ? '探测中…' : '探测媒体信息' }}</button></div></div>
    <p v-if="pageError" role="alert">{{ pageError }} <button class="btn" @click="enter()">重试</button></p>
    <BackgroundTaskProgress kind="probe" :progress="tasks.probe.progress" :visible="tasks.probe.visible" :busy="tasks.probe.busy" :unavailable="tasks.probe.unavailable" :cancelling="tasks.probe.cancelling" @cancel="tasks.probe.cancel()" />
    <WallFilters :query="route.query" :libraries="libraries" :search="search" @filter="filter" @search="search = $event; searchChanged()" @clear-entity="clearEntity" />
    <MediaGrid :items="wall.items" :image-tags="wall.imageTags" :userdata="wall.userdata" :busy="actions.busy.value" @open="open" @quickplay="quickplay" @probe="actions.run('probe', $event)" @reread="actions.run('reread', $event)" @remove="actions.run('remove', $event)" />
    <div id="wall-empty"><EmptyState v-if="ready && !wall.error && wall.total === 0" :title="emptyTitle" :hint="value('search') ? '试试其它关键词。' : '开始扫描或手动补录后再来看看。'" /></div>
    <div id="wall-more" ref="more" class="wall-more">
      <WallLoadState :error="wall.error" :loading="wall.loading" :ready="ready" :done="wall.done" :total="wall.total" @retry="load(true).then(maybeLoad)" />
    </div>
    <p class="hint">点击卡片进入详情页（可播放、刮削、探测媒体信息、重读源）；海报中央的播放按钮为快捷播放。播放时检查当前 .strm 地址；替换为 http(s) 后即可重试。</p>
  </section>
</template>
