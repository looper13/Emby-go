<script setup lang="ts">
import { computed, inject, nextTick, reactive, ref, shallowRef, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Database, ChevronLeft, ChevronRight, Search, RefreshCw } from '@lucide/vue';
import { ApiError } from '../api/client';
import { r18Api, type R18Page, type R18TagCategory } from '../api/r18';
import type { LibraryDto } from '../api/contracts';
import { usePageRequest } from '../composables/usePageRequest';
import { useAuthStore } from '../stores/auth';
import R18Cover from '../components/media/R18Cover.vue';
import CardSizeSelector from '../components/media/CardSizeSelector.vue';
import ReadState from '../components/common/ReadState.vue';
import EmptyState from '../components/common/EmptyState.vue';
import { viewHistoryKey, saveViewPosition } from '../router/view-history';
import { isR18Chart, readR18CatalogQuery, r18BasicFilters, r18Modes, r18Sorts, r18SpecialGroups, r18Types, serializeR18CatalogQuery, type R18BrowseMode } from '../composables/r18-catalog';

const route = useRoute(), router = useRouter(), auth = useAuthStore();
const history = inject(viewHistoryKey, null);
const scope = usePageRequest(), tagScope = usePageRequest();
const libraries = shallowRef<LibraryDto[]>([]), page = shallowRef<R18Page | null>(null);
const pageLibraryId = ref(''), pageQueryKey = ref(''), pageFrom = ref('');
const coverRevision = ref(0);
const loading = ref(false), error = ref('');
const draft = reactive(readR18CatalogQuery(route.query)), jumpPage = ref<number | string>(1);
const active = computed(() => readR18CatalogQuery(route.query));
const queryKey = computed(() => {
  try { return new URLSearchParams(serializeR18CatalogQuery(active.value)).toString(); }
  catch { return JSON.stringify(route.query); }
});
const libraryId = computed(() => active.value.library_id);
const currentLibrary = computed(() => libraries.value.find(l => String(l.Id) === libraryId.value));
const searching = computed(() => !!draft.search.trim()), chart = computed(() => isR18Chart(draft.mode));
const currentPage = computed(() => page.value?.page ?? active.value.page);
const pageMax = computed(() => active.value.mode === 'top250' ? Math.ceil(250 / Number(active.value.page_size)) : 9999);
const sorts = computed(() => r18Sorts.filter(choice => draft.mode !== 'categories' || choice.value !== 'update'));
const types = computed(() => r18Types(draft.mode));
const typeNames: Record<string, string> = { all: '全部', '0': '有码', '1': '无码', '2': '欧美', '3': 'FC2', '4': '动漫' };
const periodNames = [{ value: 'daily', label: '日榜' }, { value: 'weekly', label: '周榜' }, { value: 'monthly', label: '月榜' }];
const oldResults = computed(() => !!page.value && pageQueryKey.value !== queryKey.value);
const queryHint = computed(() => searching.value ? '关键词搜索按 JavDB 相关性排列，搜索时不使用分类标签、排序方向或年份筛选。'
  : draft.mode === 'top250' ? 'TOP250 使用 JavDB 官方榜单顺序；250 是榜单上限，不表示本次已获取全部影片。年份榜与影片类型互斥。'
    : chart.value ? '使用 JavDB 官方榜单顺序，不以评分排序或本服务播放次数代替上游榜单。'
      : draft.mode === 'categories' ? '选择题材、基础条件与发行信息后集中查询，最多选 5 个题材标签。'
        : '默认按发行日期从新到旧；发行与更新浏览使用快速入口，其余排序或年份走高级查询。');
let librariesLoaded = false;
const categories = shallowRef<R18TagCategory[]>([]), tagsLoading = ref(false), tagsError = ref(''), tagSearch = ref(''), selectionError = ref('');
const openedGroups = ref(new Set<string>()), groupLimits = reactive<Record<string, number>>({});
let tagsKey = '';
const groupNames: Record<string, string> = { subject: '题材', role: '角色', cloth: '服装', body: '身体', behavior: '行为', play_method: '玩法', category: '类别', other: '其他', tag: '标签', place: '场景' };
const groups = computed(() => categories.value.filter(group => !r18SpecialGroups.includes(group.id) && (draft.type === 'all' || group.type === draft.type)).map(group => ({ ...group,
  key: `${group.type}-${group.id}`, tags: group.tags.filter(tag => !tagSearch.value.trim() || tag.name.toLocaleLowerCase().includes(tagSearch.value.trim().toLocaleLowerCase())) })).filter(group => group.tags.length));
function specialTags(id: string) {
  const tags = categories.value.filter(group => group.id === id && (draft.type === 'all' || group.type === draft.type)).flatMap(group => group.tags);
  return tags.filter((tag, index) => tags.findIndex(other => other.id === tag.id) === index);
}
const basics = computed(() => specialTags('main').filter(tag => r18BasicFilters.includes(tag.id)));
const years = computed(() => specialTags('year')), months = computed(() => specialTags('month')), durations = computed(() => specialTags('duration'));
const selectedTags = computed(() => draft.tag_ids.map(id => {
  const tag = categories.value.flatMap(group => group.tags).find(tag => `${tag.type}:${tag.id}` === id);
  return { id, name: tag?.name || id };
}));
function groupOpen(key: string) { return !!tagSearch.value.trim() || openedGroups.value.has(key); }
function toggleGroup(key: string, event: Event) {
  const next = new Set(openedGroups.value);
  if ((event.target as HTMLDetailsElement).open) next.add(key); else next.delete(key);
  openedGroups.value = next;
}
function clearTags() {
  tagScope.cancel(); categories.value = []; tagsKey = ''; tagsLoading.value = false; tagsError.value = '';
}
function clearCategory() { draft.tag_ids = []; draft.main_filter = []; draft.year = ''; draft.month = ''; draft.duration = ''; selectionError.value = ''; }
function changeMode(mode: R18BrowseMode) {
  Object.assign(draft, readR18CatalogQuery({ library_id: libraryId.value, mode, page_size: draft.page_size }));
  tagSearch.value = ''; selectionError.value = ''; submit();
}
function changeType() {
  if (draft.mode === 'categories') { clearCategory(); return; }
  if (draft.mode === 'top250' && draft.type !== 'all') draft.year = '';
  submit();
}
function changeYear() {
  if (draft.mode === 'top250' && String(draft.year).trim()) draft.type = 'all';
  if (draft.mode === 'latest' && String(draft.year).trim() && draft.sort === 'update') draft.sort = 'release';
  if (draft.mode !== 'categories') submit();
}
function changeFilter() { if (draft.mode !== 'categories') submit(); }
function toggleTag(id: string, event: Event) {
  const input = event.target as HTMLInputElement;
  selectionError.value = '';
  if (input.checked && !draft.tag_ids.includes(id)) {
    if (draft.tag_ids.length >= 5) { input.checked = false; selectionError.value = '最多选 5 个题材标签'; return; }
    draft.tag_ids = [...draft.tag_ids, id];
  } else draft.tag_ids = draft.tag_ids.filter(tag => tag !== id);
}
function submit(id = libraryId.value) {
  try {
    const query = serializeR18CatalogQuery({ ...draft, library_id: id, year: String(draft.year) }, 1);
    jumpPage.value = 1; selectionError.value = ''; void router.push({ path: '/r18', query });
  } catch (cause) { error.value = cause instanceof Error ? cause.message : '查询条件不正确'; }
}
function changeLibrary(event: Event) { page.value = null; clearTags(); submit((event.target as HTMLSelectElement).value); }
function goPage(target: number) {
  if (loading.value || oldResults.value) return;
  try { void router.push({ path: '/r18', query: serializeR18CatalogQuery(active.value, target) }); }
  catch (cause) { error.value = cause instanceof Error ? cause.message : '页码不正确'; }
}
function turn(previous: boolean) {
  if (loading.value || oldResults.value) return;
  if (previous ? !page.value?.has_previous : !page.value?.has_more) return;
  goPage(currentPage.value + (previous ? -1 : 1));
}
function remember(id: string) {
  document.querySelector<HTMLElement>(`.r18-card[data-r18-id="${id}"]`)?.focus({ preventScroll: true });
  saveViewPosition({ scrollY: window.scrollY, wallCount: 0, focusID: id });
}
async function loadTags(force = false) {
  const library = currentLibrary.value;
  if (!library || draft.mode !== 'categories' || searching.value) return;
  const key = `${library.Id}:${library.Revision}`;
  if (!force && tagsKey === key) return;
  tagsKey = key; categories.value = []; tagsLoading.value = true; tagsError.value = '';
  const request = tagScope.begin(), token = auth.token;
  try {
    const result = await r18Api.tags(String(library.Id), 'all', request.signal, force);
    if (request.isCurrent() && auth.token === token) categories.value = result.categories;
  } catch (cause) { if (request.isCurrent() && auth.token === token) tagsError.value = cause instanceof Error ? cause.message : '分类加载失败'; }
  finally { if (request.isCurrent() && auth.token === token) tagsLoading.value = false; }
}
async function load(preservePosition = false, refresh = false) {
  const request = scope.begin(), token = auth.token;
  const valid = () => request.isCurrent() && auth.token === token;
  loading.value = true; error.value = '';
  const targetLibrary = libraryId.value;
  if (pageLibraryId.value !== targetLibrary) { page.value = null; pageLibraryId.value = ''; }
  const position = preservePosition ? { scrollY: window.scrollY, focusID: '' } : history?.target();
  if (!position) window.scrollTo(0, 0);
  Object.assign(draft, active.value); jumpPage.value = active.value.page;
  let permissionReady = false;
  try {
    const queryState = serializeR18CatalogQuery(active.value), key = queryKey.value, from = route.fullPath;
    const revision = currentLibrary.value?.Revision;
    // 同组件翻页复用资料库列表；影片 GET 每次仍由服务端重新鉴权并确认本地 STRM。
    if (!librariesLoaded || refresh) {
      const result = await r18Api.libraries(request.signal);
      if (!valid()) return;
      libraries.value = result.items ?? []; librariesLoaded = true;
    }
    if (!libraryId.value && libraries.value.length) {
      await router.replace({ path: '/r18', query: { ...queryState, library_id: String(libraries.value[0]!.Id) } }); return;
    }
    if (!libraries.value.length) { page.value = null; clearTags(); return; }
    if (!currentLibrary.value) { page.value = null; clearTags(); throw new Error('资料库不存在或你没有访问权限'); }
    permissionReady = true;
    if (refresh && revision !== currentLibrary.value.Revision) page.value = null;
    void loadTags(refresh);
    const query = new URLSearchParams(queryState); query.delete('library_id');
    if (refresh) query.set('refresh', '1');
    const next = await r18Api.videos(targetLibrary, query, request.signal);
    if (valid()) {
      if (preservePosition) coverRevision.value++;
      pageLibraryId.value = targetLibrary; pageQueryKey.value = key; pageFrom.value = from;
      page.value = next; jumpPage.value = next.page; await nextTick();
      if (valid() && position) {
        const card = Array.from(document.querySelectorAll<HTMLElement>('.r18-card')).find(card => card.dataset.r18Id === position.focusID);
        card?.focus({ preventScroll: true }); window.scrollTo(0, position.scrollY);
      }
    }
  } catch (cause) {
    if (valid()) {
      if ((refresh && !permissionReady) || (cause instanceof ApiError && [401, 403, 404].includes(cause.status))) {
        page.value = null; librariesLoaded = false; clearTags();
      }
      error.value = cause instanceof Error ? cause.message : 'JavDB 在线资料库加载失败';
    }
  } finally { if (valid()) loading.value = false; }
}
watch([() => route.name, queryKey], () => { if (route.name === 'r18') void load(); }, { immediate: true });
watch(libraryId, clearTags, { flush: 'sync' });
watch([() => route.name, () => active.value.mode], () => {
  if (route.name !== 'r18' || active.value.mode !== 'categories') {
    tagScope.cancel(); tagsLoading.value = false;
    if (!categories.value.length) tagsKey = '';
  }
});
watch(() => auth.token, () => {
  scope.cancel(); clearTags(); librariesLoaded = false; libraries.value = []; page.value = null; loading.value = false;
  if (auth.token && route.name === 'r18') void load();
}, { flush: 'sync' });
</script>

<template>
  <section class="panel r18-catalog">
    <header class="r18-header">
      <div><p class="r18-eyebrow"><Database :size="14" aria-hidden="true" /> AVDB-ONLY API / JAVDB</p><h2>{{ currentLibrary?.Name || 'JavDB 在线资料库' }}</h2><p class="hint">浏览 JavDB 在线影片，已有本地 STRM 优先直接播放；JavDB 在线与 AVdb 资源库磁力可另选 115 流程。</p></div>
      <button class="btn btn-sm" :disabled="loading" @click="load(true, true)"><RefreshCw :size="14" aria-hidden="true" /> 刷新</button>
    </header>
    <nav v-if="libraries.length" class="r18-modes filters" aria-label="JavDB 浏览模式">
      <button v-for="choice in r18Modes" :id="`r18-mode-${choice.value}`" :key="choice.value" type="button" class="pill" :class="{ 'is-active': draft.mode === choice.value }" :data-r18-mode="choice.value" :aria-pressed="draft.mode === choice.value" @click="changeMode(choice.value)">{{ choice.label }}</button>
    </nav>
    <form v-if="libraries.length" class="r18-filters" @submit.prevent="submit()">
      <div class="field"><label for="r18-library">资料库</label><select id="r18-library" :value="libraryId" @change="changeLibrary"><option v-for="l in libraries" :key="l.Id" :value="l.Id">{{ l.Name }}</option></select></div>
      <div class="field r18-search"><label for="r18-search">关键词搜索</label><input id="r18-search" v-model="draft.search" placeholder="搜索番号或影片标题" maxlength="120"></div>
      <div v-if="types.length > 1" class="field"><label for="r18-type">影片类型</label><select id="r18-type" v-model="draft.type" @change="changeType"><option v-for="choice in types" :key="choice" :value="choice">{{ typeNames[choice] }}</option></select></div>
      <div v-if="!chart" class="field"><label for="r18-filter">资源筛选</label><select id="r18-filter" v-model="draft.filter" @change="changeFilter"><option value="all">全部影片</option><option value="magnets">有磁力</option><option value="subtitle">有字幕</option><option value="can_play">JavDB 上游可播放</option></select></div>
      <div v-if="!chart" class="field"><label for="r18-sort">排序方式</label><select id="r18-sort" v-model="draft.sort" :disabled="searching" aria-describedby="r18-query-hint" @change="changeFilter"><option v-for="choice in sorts" :key="choice.value" :value="choice.value" :disabled="choice.value === 'update' && !!String(draft.year).trim()">{{ choice.label }}</option></select></div>
      <div v-if="!chart" class="field"><label for="r18-order">排序方向</label><select id="r18-order" v-model="draft.order" :disabled="searching" @change="changeFilter"><option value="desc">从高到低 / 最新在前</option><option value="asc">从低到高 / 最早在前</option></select></div>
      <div v-if="!chart || draft.mode === 'top250'" class="field"><label for="r18-year">{{ draft.mode === 'top250' ? '年份榜' : '发行年份' }}</label><select v-if="draft.mode === 'categories'" id="r18-year" v-model="draft.year" :disabled="searching"><option value="">不限年份</option><option v-if="draft.year && !years.some(tag => tag.id === draft.year)" :value="draft.year">{{ draft.year }}</option><option v-for="tag in years" :key="tag.id" :value="tag.id">{{ tag.name }}</option></select><input v-else id="r18-year" v-model="draft.year" type="number" min="1" max="9999" placeholder="不限年份" :disabled="searching" @change="changeYear"></div>
      <div v-if="chart && draft.mode !== 'top250'" class="field"><label for="r18-period">榜单周期</label><select id="r18-period" v-model="draft.period" :disabled="searching" @change="submit()"><option v-for="choice in periodNames" :key="choice.value" :value="choice.value">{{ choice.label }}</option></select></div>
      <label v-if="draft.mode === 'top250'" class="r18-ignore-watched"><input id="r18-ignore-watched" type="checkbox" :checked="draft.ignore_watched === 'true'" :disabled="searching" @change="draft.ignore_watched = ($event.target as HTMLInputElement).checked ? 'true' : 'false'; submit()">排除 AVdb 的 JavDB 账户已看</label>
      <button class="btn btn-accent" type="submit"><Search :size="15" aria-hidden="true" /> 查询</button>
      <section v-if="draft.mode === 'categories' && !searching" class="r18-category-panel" aria-label="JavDB 分类条件">
        <header class="r18-category-header"><div><h3>分类条件</h3><p class="hint">{{ draft.tag_ids.length }} / 5 个题材标签 · 可另选基础条件、年份、月份与时长。选择后点击查询统一应用。</p></div><button type="button" class="btn btn-sm" @click="clearCategory">清除分类</button></header>
        <ReadState :loading="tagsLoading" :empty="!categories.length" :error="tagsError" @retry="loadTags(true)" />
        <div v-if="categories.length" class="r18-category-basics">
          <fieldset v-if="basics.length" class="r18-basic-options"><legend>基础条件</legend><label v-for="tag in basics" :key="tag.id"><input :id="`r18-main-${tag.id}`" v-model="draft.main_filter" type="checkbox" :value="tag.id">{{ tag.name }}</label></fieldset>
          <div class="field"><label for="r18-month">发行月份</label><select id="r18-month" v-model="draft.month"><option value="">不限月份</option><option v-for="tag in months" :key="tag.id" :value="tag.id">{{ tag.name }}</option></select></div>
          <div class="field"><label for="r18-duration">影片时长</label><select id="r18-duration" v-model="draft.duration"><option value="">不限时长</option><option v-for="tag in durations" :key="tag.id" :value="tag.id">{{ tag.name }}</option></select></div>
          <div class="field r18-tag-search"><label for="r18-category-search">搜索题材标签</label><input id="r18-category-search" v-model="tagSearch" type="search" placeholder="输入标签名称，不发送影片查询" maxlength="120"></div>
        </div>
        <div v-if="selectedTags.length" class="r18-selected-tags" aria-label="已选题材标签"><button v-for="tag in selectedTags" :key="tag.id" type="button" class="filter-chip" @click="draft.tag_ids = draft.tag_ids.filter(id => id !== tag.id); selectionError = ''">{{ tag.name }} <span class="mono">{{ tag.id }}</span><span aria-hidden="true">×</span></button></div>
        <p v-if="selectionError" id="r18-category-warning" class="auth-error" role="alert">{{ selectionError }}</p>
        <div class="r18-tag-groups">
          <details v-for="group in groups" :key="group.key" class="r18-tag-group" :data-r18-group="group.key" :open="groupOpen(group.key)" @toggle="toggleGroup(group.key, $event)">
            <summary>{{ groupNames[group.id] || group.name }} <span>{{ typeNames[group.type] }} · {{ group.tags.length }} 项</span></summary>
            <div v-if="groupOpen(group.key)" class="r18-tag-options"><label v-for="tag in group.tags.slice(0, groupLimits[group.key] || 40)" :key="`${tag.type}:${tag.id}`"><input :id="`r18-tag-${tag.type}-${tag.id}`" type="checkbox" :checked="draft.tag_ids.includes(`${tag.type}:${tag.id}`)" @change="toggleTag(`${tag.type}:${tag.id}`, $event)">{{ tag.name }}</label><button v-if="group.tags.length > (groupLimits[group.key] || 40)" type="button" class="btn btn-sm" @click="groupLimits[group.key] = (groupLimits[group.key] || 40) + 40">显示更多标签</button></div>
          </details>
        </div>
        <p v-if="categories.length && !groups.length" class="hint">没有匹配的题材标签，请调整类型或标签搜索。</p>
      </section>
    </form>
    <p v-if="libraries.length" id="r18-query-hint" class="hint r18-query-hint">{{ queryHint }} 在线分页获取，不导入整库，也不伪造全库总数。上游可播放不代表本服务已有 115 播放源。<span v-if="draft.mode === 'top250'"> 排除已看针对 AVdb 的 JavDB 账户，与本 Emby 用户已看无关。</span></p>
    <ReadState :loading="loading" :empty="!page" :error="error" @retry="load(true, true)" />
    <EmptyState v-if="!loading && !error && !libraries.length" title="还没有可访问的 JavDB 资料库" hint="管理员可在媒体库页面添加 AVdb-Only API 来源，并在用户管理中授权。" />
    <RouterLink v-if="!libraries.length && auth.isAdministrator" class="btn btn-accent" to="/libraries">添加 JavDB 资料库</RouterLink>
    <template v-if="page">
      <p v-if="oldResults" class="r18-stale-results" :role="loading ? 'status' : 'alert'">{{ loading ? '正在加载新条件，当前是上次结果。' : '新条件尚未成功加载，当前是上次结果。' }}</p>
      <div class="r18-list-meta"><span>第 {{ page.page }} 页 · 本页 {{ page.items.length }} 条<span v-if="page.total !== undefined && page.total !== null"> · 榜单共 {{ page.total }} 条</span></span><div class="r18-list-options"><span v-if="loading && !oldResults" role="status">正在加载，保留当前页面…</span><label class="r18-page-size" for="r18-page-size">每页<select id="r18-page-size" v-model="draft.page_size" :disabled="loading" @change="submit()"><option value="24">24 条</option><option value="40">40 条</option><option value="50">50 条</option></select></label><CardSizeSelector /></div></div>
      <div class="wall r18-grid" :aria-busy="loading">
        <RouterLink v-for="v in page.items" :key="v.id" class="r18-card" :data-r18-id="v.id" :to="{ name: 'r18-detail', params: { libraryId, id: v.id }, query: { from: pageFrom } }" :aria-label="`查看 ${v.number || v.content_id} 详情`" @click="remember(v.id)">
          <R18Cover :key="`${v.id}:${coverRevision}`" class="r18-cover" :src="v.cover_url" :alt="v.number || v.content_id" loading="lazy"><span class="r18-service">JavDB</span><span v-if="v.rank" class="r18-rank" :data-rank="v.rank" :aria-label="`上游排名第 ${v.rank} 名`">#{{ v.rank }}</span></R18Cover>
          <div class="r18-card-copy"><strong class="mono">{{ v.number || v.content_id }}</strong><span v-if="v.has_local" class="r18-card-local">本地 STRM 可播</span><h3 :title="v.title">{{ v.title }}</h3><span>{{ v.release_date || '发行日期未知' }} · {{ v.duration ? `${v.duration} 分钟` : '时长未知' }}</span><span class="r18-card-magnets" title="JavDB 上游数量，实际在线版本以影片详情为准">上游磁力 {{ v.magnets_count }} · {{ v.score ? `评分 ${v.score.toFixed(1)}` : '评分未知' }}</span></div>
        </RouterLink>
      </div>
      <EmptyState v-if="!page.items.length && !loading" title="没有符合条件的影片" hint="请检查关键词，或调整影片分类。" />
      <nav class="r18-pagination" aria-label="JavDB 分页">
        <button class="btn" :disabled="loading || oldResults || !page.has_previous" @click="turn(true)"><ChevronLeft :size="16" aria-hidden="true" /> 上一页</button>
        <span class="r18-page-number">第 {{ currentPage }} 页</span>
        <button class="btn" :disabled="loading || oldResults || !page.has_more" @click="turn(false)">下一页 <ChevronRight :size="16" aria-hidden="true" /></button>
        <form class="r18-page-jump" @submit.prevent="goPage(Number(jumpPage))"><label for="r18-jump-page">跳到页码</label><input id="r18-jump-page" v-model="jumpPage" type="number" min="1" :max="pageMax" :disabled="loading || oldResults" required><button class="btn btn-sm" :disabled="loading || oldResults">跳转</button><span v-if="active.mode === 'top250'" class="hint">最多 {{ pageMax }} 页</span></form>
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
.r18-modes { margin-bottom:18px; }.r18-modes .pill { white-space:nowrap; }
.r18-filters .field, .r18-filters input, .r18-filters select { min-width:0; }
.r18-filters input, .r18-filters select { width:100%; }
.r18-ignore-watched { display:flex; gap:8px; align-items:flex-start; font-size:12px; line-height:1.6; }.r18-ignore-watched input { width:auto; flex:none; margin-top:4px; }
.r18-category-panel { grid-column:1/-1; min-width:0; padding:16px; border:1px solid var(--line); border-radius:var(--radius); background:var(--panel-2); }
.r18-category-header { display:flex; align-items:flex-start; justify-content:space-between; gap:12px; }.r18-category-header h3 { margin:0 0 6px; font-size:14px; }.r18-category-header .hint { margin:0 0 14px; }.r18-category-header .btn { flex:none; }
.r18-category-basics { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:12px; align-items:end; }.r18-basic-options { grid-column:1/-1; border:0; padding:0; margin:0 0 6px; min-width:0; display:flex; flex-wrap:wrap; gap:8px 16px; }.r18-basic-options legend { font-size:12px; color:var(--muted); padding:0 0 8px; }
.r18-basic-options label, .r18-tag-options label { display:flex; align-items:flex-start; gap:7px; min-width:0; font-size:12px; line-height:1.6; overflow-wrap:anywhere; }.r18-basic-options input, .r18-tag-options input { width:auto; flex:none; margin-top:4px; }
.r18-tag-search { grid-column:1/-1; }.r18-selected-tags { display:flex; flex-wrap:wrap; gap:6px; margin-top:14px; }.r18-selected-tags .filter-chip { min-width:0; max-width:100%; white-space:normal; overflow-wrap:anywhere; }.r18-selected-tags .mono { color:var(--muted); font-size:10px; }
.r18-tag-groups { display:grid; gap:8px; margin-top:14px; }.r18-tag-group { min-width:0; border:1px solid var(--line); border-radius:var(--radius-sm); background:var(--panel); }.r18-tag-group summary { padding:10px 12px; cursor:pointer; font-size:12px; font-weight:600; overflow-wrap:anywhere; }.r18-tag-group summary span { color:var(--muted); font-size:11px; font-weight:400; margin-left:8px; }
.r18-tag-options { display:grid; grid-template-columns:repeat(auto-fill,minmax(min(100%,150px),1fr)); gap:10px 16px; padding:4px 12px 14px; }.r18-tag-options .btn { justify-self:start; }
.r18-stale-results { color:var(--warn); font-size:12px; padding:10px 12px; border:1px solid var(--line); border-radius:var(--radius-sm); margin:16px 0 0; }
.r18-query-hint { margin:12px 0 0; }
.r18-list-meta { display:flex; justify-content:space-between; align-items:center; flex-wrap:wrap; gap:12px; font-size:12px; color:var(--muted); margin:18px 0; }
.r18-list-options { display:flex; align-items:center; flex-wrap:wrap; gap:12px; }
.r18-page-size { display:flex; gap:8px; align-items:center; }.r18-page-size select { width:auto; min-height:36px; }
.r18-readonly { color:var(--r18-accent); }
.r18-card { display:block; color:inherit; text-decoration:none; min-width:0; }
.r18-card:focus-visible { outline:2px solid var(--r18-accent); outline-offset:4px; }
.r18-card:hover .r18-cover { border-color:var(--r18-accent); }
.r18-service { position:absolute; left:9px; bottom:9px; padding:3px 7px; font-size:10px; background:rgba(0,0,0,.75); color:#fff; border-radius:4px; }
.r18-rank { position:absolute; right:9px; top:9px; padding:5px 8px; border-radius:4px; background:var(--accent); color:var(--accent-ink); font-size:13px; font-weight:700; font-family:var(--font-mono); }
.r18-card-copy { padding:10px 0 0; }
.r18-card-copy strong { font-size:13px; color:var(--r18-accent); overflow-wrap:anywhere; }
.r18-card-copy h3 { font-size:13px; line-height:1.65; margin:6px 0 9px; display:-webkit-box; -webkit-line-clamp:2; -webkit-box-orient:vertical; overflow:hidden; min-height:3.3em; }
.r18-card-copy > span { display:block; color:var(--muted); font-size:11px; line-height:1.7; }.r18-card-magnets { margin-top:4px; }
.r18-card-copy > .r18-card-local { display:inline-block; padding:1px 5px; margin-top:6px; border:1px solid var(--line); border-radius:4px; color:var(--accent); }
.r18-pagination { display:flex; justify-content:center; gap:14px; align-items:center; margin-top:28px; flex-wrap:wrap; }
.r18-page-number { color:var(--muted); font-size:12px; white-space:nowrap; }.r18-page-jump { display:flex; gap:8px; align-items:center; flex-wrap:wrap; font-size:12px; }.r18-page-jump input { width:80px; min-width:0; }
@media(max-width:1100px) { .r18-filters { grid-template-columns:repeat(2,minmax(0,1fr)); } }
@media(max-width:600px) { .r18-header { gap:10px; }.r18-header h2 { font-size:22px; }.r18-pagination { justify-content:flex-start; }.r18-page-jump { width:100%; }.r18-category-panel { padding:12px; }.r18-category-header { flex-wrap:wrap; }.r18-category-header > div { min-width:0; flex:1 1 180px; } }
</style>
