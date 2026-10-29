<script setup lang="ts">
import { computed, inject, nextTick, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Database, ChevronLeft, ChevronRight, Search, RefreshCw } from '@lucide/vue';
import { r18Api, type R18Page } from '../api/r18';
import type { LibraryDto } from '../api/contracts';
import { usePageRequest } from '../composables/usePageRequest';
import { useAuthStore } from '../stores/auth';
import R18Cover from '../components/media/R18Cover.vue';
import CardSizeSelector from '../components/media/CardSizeSelector.vue';
import ReadState from '../components/common/ReadState.vue';
import EmptyState from '../components/common/EmptyState.vue';
import { viewHistoryKey, saveViewPosition } from '../router/view-history';

const route = useRoute(), router = useRouter(), auth = useAuthStore();
const history = inject(viewHistoryKey, null);
const scope = usePageRequest();
const libraries = ref<LibraryDto[]>([]), page = ref<R18Page | null>(null);
const pageLibraryId = ref('');
const coverRevision = ref(0);
const loading = ref(false), error = ref('');
const search = ref(''), type = ref('all'), resourceFilter = ref('magnets'), sort = ref('release'), order = ref('desc');
const year = ref<number | string>(''), pageSize = ref('24'), jumpPage = ref<number | string>(1);
const value = (key: string) => typeof route.query[key] === 'string' ? route.query[key] as string : '';
const libraryId = computed(() => value('library_id'));
const currentLibrary = computed(() => libraries.value.find(l => String(l.Id) === libraryId.value));
const searching = computed(() => !!search.value.trim());
const currentPage = computed(() => page.value?.page ?? routePage());
const sorts = [
  { value: 'release', label: '发行日期' }, { value: 'score', label: '评分' }, { value: 'hit', label: '热度' },
  { value: 'want_watch_count', label: '想看人数' }, { value: 'watched_count', label: '看过人数' },
];

function routePage() { const n = Number(value('page')); return Number.isSafeInteger(n) && n >= 1 && n <= 9999 ? n : 1; }
function routeSize() { return ['24', '40', '50'].includes(value('page_size')) ? value('page_size') : '24'; }
function routeFilters(target = routePage()) {
  const query: Record<string, string> = { library_id: libraryId.value, type: value('type') || 'all',
    filter: value('filter') || 'magnets', page: String(target), page_size: routeSize() };
  if (value('search').trim()) query.search = value('search').trim();
  else {
    query.sort = value('sort') || 'release'; query.order = value('order') || 'desc';
    if (value('year')) query.year = value('year');
  }
  return query;
}
function submit(id = libraryId.value) {
  const query: Record<string, string> = { library_id: id,
    type: type.value, filter: resourceFilter.value, page: '1', page_size: pageSize.value };
  if (searching.value) query.search = search.value.trim();
  else {
    query.sort = sort.value; query.order = order.value;
    if (String(year.value).trim()) {
      const n = Number(year.value);
      if (!Number.isSafeInteger(n) || n < 1 || n > 9999) { error.value = '请输入有效的发行年份'; return; }
      query.year = String(n);
    }
  }
  jumpPage.value = 1;
  void router.push({ path: '/r18', query });
}
function changeLibrary(event: Event) { page.value = null; submit((event.target as HTMLSelectElement).value); }
function goPage(target: number) {
  if (loading.value) return;
  if (!Number.isSafeInteger(target) || target < 1 || target > 9999) { error.value = '请输入 1 到 9999 的整数页码'; return; }
  void router.push({ path: '/r18', query: routeFilters(target) });
}
function turn(previous: boolean) {
  if (previous ? !page.value?.has_previous : !page.value?.has_more) return;
  goPage(currentPage.value + (previous ? -1 : 1));
}
function remember(id: string) {
  document.querySelector<HTMLElement>(`.r18-card[data-r18-id="${id}"]`)?.focus({ preventScroll: true });
  saveViewPosition({ scrollY: window.scrollY, wallCount: 0, focusID: id });
}
async function load(preservePosition = false) {
  const request = scope.begin(); loading.value = true; error.value = '';
  const targetLibrary = libraryId.value;
  if (pageLibraryId.value !== targetLibrary) { page.value = null; pageLibraryId.value = ''; }
  const position = preservePosition ? { scrollY: window.scrollY, focusID: '' } : history?.target();
  if (!position) window.scrollTo(0, 0);
  const active = routeFilters();
  search.value = active.search || ''; type.value = active.type!; resourceFilter.value = active.filter!;
  sort.value = active.sort || 'release'; order.value = active.order || 'desc'; year.value = active.year || '';
  pageSize.value = active.page_size!; jumpPage.value = routePage();
  try {
    const result = await r18Api.libraries(request.signal);
    if (!request.isCurrent()) return;
    libraries.value = result.items ?? [];
    if (!libraryId.value && libraries.value.length) {
      await router.replace({ path: '/r18', query: { ...active, library_id: String(libraries.value[0]!.Id) } }); return;
    }
    if (!libraries.value.length) { page.value = null; return; }
    if (!currentLibrary.value) { page.value = null; throw new Error('资料库不存在或你没有访问权限'); }
    const query = new URLSearchParams(active); query.delete('library_id');
    const next = await r18Api.videos(targetLibrary, query, request.signal);
    if (request.isCurrent()) {
      if (preservePosition) coverRevision.value++;
      pageLibraryId.value = targetLibrary; page.value = next; jumpPage.value = next.page; await nextTick();
      if (request.isCurrent() && position) {
        const card = Array.from(document.querySelectorAll<HTMLElement>('.r18-card')).find(card => card.dataset.r18Id === position.focusID);
        card?.focus({ preventScroll: true }); window.scrollTo(0, position.scrollY);
      }
    }
  } catch (cause) { if (request.isCurrent()) error.value = cause instanceof Error ? cause.message : 'JavDB 在线资料库加载失败'; }
  finally { if (request.isCurrent()) loading.value = false; }
}
watch(() => route.fullPath, () => { if (route.name === 'r18') void load(); }, { immediate: true });
</script>

<template>
  <section class="panel r18-catalog">
    <header class="r18-header">
      <div><p class="r18-eyebrow"><Database :size="14" aria-hidden="true" /> AVDB-ONLY API / JAVDB</p><h2>{{ currentLibrary?.Name || 'JavDB 在线资料库' }}</h2><p class="hint">浏览 JavDB 在线影片，已有本地 STRM 优先直接播放；JavDB 在线与 AVdb 资源库磁力可另选 115 流程。</p></div>
      <button class="btn btn-sm" :disabled="loading" @click="load(true)"><RefreshCw :size="14" aria-hidden="true" /> 刷新</button>
    </header>
    <form v-if="libraries.length" class="r18-filters" @submit.prevent="submit()">
      <div class="field"><label for="r18-library">资料库</label><select id="r18-library" :value="libraryId" :disabled="loading" @change="changeLibrary"><option v-for="l in libraries" :key="l.Id" :value="l.Id">{{ l.Name }}</option></select></div>
      <div class="field r18-search"><label for="r18-search">关键词搜索</label><input id="r18-search" v-model="search" placeholder="搜索番号或影片标题" maxlength="120"></div>
      <div class="field"><label for="r18-type">影片分类</label><select id="r18-type" v-model="type" :disabled="loading" @change="submit()"><option value="all">全部</option><option value="0">有码</option><option value="1">无码</option><option value="2">欧美</option><option value="3">FC2</option></select></div>
      <div class="field"><label for="r18-filter">资源筛选</label><select id="r18-filter" v-model="resourceFilter" :disabled="loading" @change="submit()"><option value="all">全部影片</option><option value="magnets">有磁力</option><option value="subtitle">有字幕</option><option value="can_play">JavDB 上游可播放</option></select></div>
      <div class="field"><label for="r18-sort">排序方式</label><select id="r18-sort" v-model="sort" :disabled="searching || loading" aria-describedby="r18-query-hint" @change="submit()"><option v-for="choice in sorts" :key="choice.value" :value="choice.value">{{ choice.label }}</option></select></div>
      <div class="field"><label for="r18-order">排序方向</label><select id="r18-order" v-model="order" :disabled="searching || loading" @change="submit()"><option value="desc">从高到低 / 最新在前</option><option value="asc">从低到高 / 最早在前</option></select></div>
      <div class="field"><label for="r18-year">发行年份</label><input id="r18-year" v-model="year" type="number" min="1" max="9999" placeholder="不限年份" :disabled="searching || loading" @change="submit()"></div>
      <button class="btn btn-accent" type="submit" :disabled="loading"><Search :size="15" aria-hidden="true" /> 查询</button>
    </form>
    <p v-if="libraries.length" id="r18-query-hint" class="hint r18-query-hint">{{ searching ? '关键词搜索按 JavDB 相关性排列，搜索时不使用排序方向或年份筛选。' : '列表使用 JavDB 上游排序，默认按发行日期从新到旧；可按年份缩小范围。' }} 在线分页获取，不导入整库，也不伪造全库总数。上游可播放不代表本服务已有 115 播放源。</p>
    <ReadState :loading="loading" :empty="!page" :error="error" @retry="load(true)" />
    <EmptyState v-if="!loading && !error && !libraries.length" title="还没有可访问的 JavDB 资料库" hint="管理员可在媒体库页面添加 AVdb-Only API 来源，并在用户管理中授权。" />
    <RouterLink v-if="!libraries.length && auth.isAdministrator" class="btn btn-accent" to="/libraries">添加 JavDB 资料库</RouterLink>
    <template v-if="page">
      <div class="r18-list-meta"><span>第 {{ page.page }} 页 · 本页 {{ page.items.length }} 条</span><div class="r18-list-options"><span v-if="loading" role="status">正在加载，保留当前页面…</span><label class="r18-page-size" for="r18-page-size">每页<select id="r18-page-size" v-model="pageSize" :disabled="loading" @change="submit()"><option value="24">24 条</option><option value="40">40 条</option><option value="50">50 条</option></select></label><CardSizeSelector /></div></div>
      <div class="wall r18-grid" :aria-busy="loading">
        <RouterLink v-for="v in page.items" :key="v.id" class="r18-card" :data-r18-id="v.id" :to="{ name: 'r18-detail', params: { libraryId, id: v.id }, query: { from: route.fullPath } }" :aria-label="`查看 ${v.number || v.content_id} 详情`" @click="remember(v.id)">
          <R18Cover :key="`${v.id}:${coverRevision}`" class="r18-cover" :src="v.cover_url" :alt="v.number || v.content_id" loading="lazy"><span class="r18-service">JavDB</span></R18Cover>
          <div class="r18-card-copy"><strong class="mono">{{ v.number || v.content_id }}</strong><span v-if="v.has_local" class="r18-card-local">本地 STRM 可播</span><h3 :title="v.title">{{ v.title }}</h3><span>{{ v.release_date || '发行日期未知' }} · {{ v.duration ? `${v.duration} 分钟` : '时长未知' }}</span><span class="r18-card-magnets" title="JavDB 上游数量，实际在线版本以影片详情为准">上游磁力 {{ v.magnets_count }} · {{ v.score ? `评分 ${v.score.toFixed(1)}` : '评分未知' }}</span></div>
        </RouterLink>
      </div>
      <EmptyState v-if="!page.items.length && !loading" title="没有符合条件的影片" hint="请检查关键词，或调整影片分类。" />
      <nav class="r18-pagination" aria-label="JavDB 分页">
        <button class="btn" :disabled="loading || !page.has_previous" @click="turn(true)"><ChevronLeft :size="16" aria-hidden="true" /> 上一页</button>
        <span class="r18-page-number">第 {{ currentPage }} 页</span>
        <button class="btn" :disabled="loading || !page.has_more" @click="turn(false)">下一页 <ChevronRight :size="16" aria-hidden="true" /></button>
        <form class="r18-page-jump" @submit.prevent="goPage(Number(jumpPage))"><label for="r18-jump-page">跳到页码</label><input id="r18-jump-page" v-model="jumpPage" type="number" min="1" max="9999" :disabled="loading" required><button class="btn btn-sm" :disabled="loading">跳转</button></form>
      </nav>
    </template>
  </section>
</template>

<style scoped>
.r18-catalog { --r18-accent: var(--accent, #c8a76a); }
.r18-header { display:flex; justify-content:space-between; align-items:flex-start; gap:20px; margin-bottom:22px; }
.r18-header > div { min-width:0; }.r18-header h2 { margin:4px 0 8px; font-size:26px; overflow-wrap:anywhere; }
.r18-eyebrow { display:flex; align-items:center; gap:8px; color:var(--r18-accent); font-size:11px; letter-spacing:2px; margin:0; }
.r18-filters { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); gap:12px; align-items:end; }
.r18-search { grid-column:span 2; }
.r18-filters .field, .r18-filters input, .r18-filters select { min-width:0; }
.r18-filters input, .r18-filters select { width:100%; }
.r18-query-hint { margin:12px 0 0; }
.r18-list-meta { display:flex; justify-content:space-between; align-items:center; flex-wrap:wrap; gap:12px; font-size:12px; color:var(--muted); margin:18px 0; }
.r18-list-options { display:flex; align-items:center; flex-wrap:wrap; gap:12px; }
.r18-page-size { display:flex; gap:8px; align-items:center; }.r18-page-size select { width:auto; min-height:36px; }
.r18-readonly { color:var(--r18-accent); }
.r18-card { display:block; color:inherit; text-decoration:none; min-width:0; }
.r18-card:focus-visible { outline:2px solid var(--r18-accent); outline-offset:4px; }
.r18-card:hover .r18-cover { border-color:var(--r18-accent); }
.r18-service { position:absolute; left:9px; bottom:9px; padding:3px 7px; font-size:10px; background:rgba(0,0,0,.75); color:#fff; border-radius:4px; }
.r18-card-copy { padding:10px 0 0; }
.r18-card-copy strong { font-size:13px; color:var(--r18-accent); overflow-wrap:anywhere; }
.r18-card-copy h3 { font-size:13px; line-height:1.65; margin:6px 0 9px; display:-webkit-box; -webkit-line-clamp:2; -webkit-box-orient:vertical; overflow:hidden; min-height:3.3em; }
.r18-card-copy > span { display:block; color:var(--muted); font-size:11px; line-height:1.7; }.r18-card-magnets { margin-top:4px; }
.r18-card-copy > .r18-card-local { display:inline-block; padding:1px 5px; margin-top:6px; border:1px solid var(--line); border-radius:4px; color:var(--accent); }
.r18-pagination { display:flex; justify-content:center; gap:14px; align-items:center; margin-top:28px; flex-wrap:wrap; }
.r18-page-number { color:var(--muted); font-size:12px; white-space:nowrap; }.r18-page-jump { display:flex; gap:8px; align-items:center; flex-wrap:wrap; font-size:12px; }.r18-page-jump input { width:80px; min-width:0; }
@media(max-width:1100px) { .r18-filters { grid-template-columns:repeat(2,minmax(0,1fr)); } }
@media(max-width:600px) { .r18-header { gap:10px; }.r18-header h2 { font-size:22px; }.r18-pagination { justify-content:flex-start; }.r18-page-jump { width:100%; } }
</style>
