<script setup lang="ts">
import { computed, inject, nextTick, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Database, ChevronLeft, ChevronRight, Search, RefreshCw } from '@lucide/vue';
import { r18Api, type R18Page } from '../api/r18';
import type { LibraryDto } from '../api/contracts';
import { usePageRequest } from '../composables/usePageRequest';
import { useAuthStore } from '../stores/auth';
import MediaImage from '../components/media/MediaImage.vue';
import ReadState from '../components/common/ReadState.vue';
import EmptyState from '../components/common/EmptyState.vue';
import { viewHistoryKey, saveViewPosition } from '../router/view-history';

const route = useRoute(), router = useRouter(), auth = useAuthStore();
const history = inject(viewHistoryKey, null);
const scope = usePageRequest();
const libraries = ref<LibraryDto[]>([]), page = ref<R18Page | null>(null);
const loading = ref(false), error = ref('');
const search = ref(''), searchMode = ref('number'), service = ref(''), order = ref('asc');
const value = (key: string) => typeof route.query[key] === 'string' ? route.query[key] as string : '';
const libraryId = computed(() => value('library_id'));
const currentLibrary = computed(() => libraries.value.find(l => String(l.Id) === libraryId.value));
const estimate = computed(() => (page.value?.estimated_total ?? 0).toLocaleString('zh-CN'));

function filters(id = libraryId.value) {
  return { library_id: id, ...(search.value.trim() ? { search: search.value.trim() } : {}),
    search_mode: searchMode.value, ...(service.value ? { service: service.value } : {}), order: order.value };
}
function submit() { void router.push({ path: '/r18', query: filters() }); }
function changeLibrary(event: Event) { page.value = null; void router.push({ path: '/r18', query: { library_id: (event.target as HTMLSelectElement).value } }); }
function turn(previous: boolean) {
  const cursor = previous ? page.value?.previous_cursor : page.value?.next_cursor;
  if (!cursor || loading.value) return;
  void router.push({ path: '/r18', query: { ...route.query, cursor, direction: previous ? 'prev' : 'next' } });
}
function remember(id: string) {
  document.querySelector<HTMLElement>(`.r18-card[data-r18-id="${id}"]`)?.focus({ preventScroll: true });
  saveViewPosition({ scrollY: window.scrollY, wallCount: 0, focusID: id });
}
async function load(preservePosition = false) {
  const request = scope.begin(); loading.value = true; error.value = '';
  const position = preservePosition ? { scrollY: window.scrollY, focusID: '' } : history?.target();
  if (!position) window.scrollTo(0, 0);
  search.value = value('search'); searchMode.value = value('search_mode') || 'number'; service.value = value('service'); order.value = value('order') || 'asc';
  try {
    const result = await r18Api.libraries(request.signal);
    if (!request.isCurrent()) return;
    libraries.value = result.items ?? [];
    if (!libraryId.value && libraries.value.length) {
      await router.replace({ path: '/r18', query: { ...route.query, library_id: String(libraries.value[0]!.Id) } }); return;
    }
    if (!libraries.value.length) { page.value = null; return; }
    if (!currentLibrary.value) { page.value = null; throw new Error('资料库不存在或你没有访问权限'); }
    const query = new URLSearchParams();
    for (const key of ['search', 'search_mode', 'service', 'order', 'cursor', 'direction']) if (value(key)) query.set(key, value(key));
    const next = await r18Api.videos(libraryId.value, query, request.signal);
    if (request.isCurrent()) {
      page.value = next; await nextTick();
      if (request.isCurrent() && position) {
        const card = Array.from(document.querySelectorAll<HTMLElement>('.r18-card')).find(card => card.dataset.r18Id === position.focusID);
        card?.focus({ preventScroll: true }); window.scrollTo(0, position.scrollY);
      }
    }
  } catch (cause) { if (request.isCurrent()) error.value = cause instanceof Error ? cause.message : 'R18 资料库加载失败'; }
  finally { if (request.isCurrent()) loading.value = false; }
}
watch(() => route.fullPath, () => { if (route.name === 'r18') void load(); }, { immediate: true });
</script>

<template>
  <section class="panel r18-catalog">
    <header class="r18-header">
      <div><p class="r18-eyebrow"><Database :size="14" aria-hidden="true" /> POSTGRESQL / R18</p><h2>{{ currentLibrary?.Name || 'R18 资料库' }}</h2><p class="hint">浏览影片资料，打开详情查看 article 磁力资源。本期不提供播放或下载。</p></div>
      <button class="btn btn-sm" :disabled="loading" @click="load(true)"><RefreshCw :size="14" aria-hidden="true" /> 刷新</button>
    </header>
    <form v-if="libraries.length" class="r18-filters" @submit.prevent="submit">
      <div class="field"><label for="r18-library">资料库</label><select id="r18-library" :value="libraryId" :disabled="loading" @change="changeLibrary"><option v-for="l in libraries" :key="l.Id" :value="l.Id">{{ l.Name }}</option></select></div>
      <div class="field"><label for="r18-search-mode">搜索方式</label><select id="r18-search-mode" v-model="searchMode"><option value="number">番号 / 内容 ID 精确搜索</option><option value="title">标题关键词</option></select></div>
      <div class="field r18-search"><label for="r18-search">搜索影片</label><input id="r18-search" v-model="search" :placeholder="searchMode === 'number' ? '输入完整番号，如 SSIS-001' : '输入日文或英文标题关键词'" maxlength="120"></div>
      <div class="field"><label for="r18-service">来源类型</label><select id="r18-service" v-model="service"><option value="">全部</option><option value="digital">digital</option><option value="mono">mono</option></select></div>
      <div class="field"><label for="r18-order">内容 ID 排序</label><select id="r18-order" v-model="order"><option value="asc">顺序</option><option value="desc">倒序</option></select></div>
      <button class="btn btn-accent" type="submit" :disabled="loading"><Search :size="15" aria-hidden="true" /> 查询</button>
    </form>
    <p v-if="libraries.length" class="hint r18-query-hint">使用内容 ID 游标分页，避免百万级目录的深翻页。标题搜索可能较慢；全库数量为 PostgreSQL 统计估计，不是筛选后的精确计数。</p>
    <ReadState :loading="loading" :empty="!page" :error="error" @retry="load(true)" />
    <EmptyState v-if="!loading && !error && !libraries.length" title="还没有可访问的 R18 资料库" hint="管理员可在媒体库页面添加 PostgreSQL 来源，并在用户管理中授权。" />
    <RouterLink v-if="!libraries.length && auth.isAdministrator" class="btn btn-accent" to="/libraries">添加 R18 资料库</RouterLink>
    <template v-if="page">
      <div class="r18-list-meta"><span>本页 {{ page.items.length }} 条 <span v-if="page.estimated_total">· 全库约 {{ estimate }} 条</span></span><span v-if="loading" role="status">正在加载，保留当前页面…</span><span v-else class="r18-readonly">只读资料库</span></div>
      <div class="r18-grid" :aria-busy="loading">
        <RouterLink v-for="v in page.items" :key="v.id" class="r18-card" :data-r18-id="v.id" :to="{ name: 'r18-detail', params: { libraryId, id: v.id }, query: { from: route.fullPath } }" :aria-label="`查看 ${v.number || v.content_id} 详情`" @click="remember(v.id)">
          <div class="r18-cover"><MediaImage :src="v.cover_url" :alt="v.number || v.content_id" loading="lazy" referrerpolicy="no-referrer" /><span class="r18-service">{{ v.service_code }}</span></div>
          <div class="r18-card-copy"><strong class="mono">{{ v.number || v.content_id }}</strong><h3 :title="v.title">{{ v.title }}</h3><span>{{ v.release_date || '发行日期未知' }}</span></div>
        </RouterLink>
      </div>
      <EmptyState v-if="!page.items.length && !loading" title="没有符合条件的影片" hint="请检查番号，或改用标题关键词搜索。" />
      <nav class="r18-pagination" aria-label="R18 分页">
        <button class="btn" :disabled="loading || !page.has_previous" @click="turn(true)"><ChevronLeft :size="16" aria-hidden="true" /> 上一页</button>
        <button class="btn btn-sm" :disabled="loading || !value('cursor')" @click="submit">回到第一页</button>
        <button class="btn" :disabled="loading || !page.has_more" @click="turn(false)">下一页 <ChevronRight :size="16" aria-hidden="true" /></button>
      </nav>
    </template>
  </section>
</template>

<style scoped>
.r18-catalog { --r18-accent: var(--accent, #c8a76a); }
.r18-header { display:flex; justify-content:space-between; align-items:flex-start; gap:20px; margin-bottom:22px; }
.r18-header h2 { margin:4px 0 8px; font-size:26px; }
.r18-eyebrow { display:flex; align-items:center; gap:8px; color:var(--r18-accent); font-size:11px; letter-spacing:2px; margin:0; }
.r18-filters { display:grid; grid-template-columns:1fr 1.3fr 2fr 1fr 1fr auto; gap:12px; align-items:end; }
.r18-filters .field, .r18-filters input, .r18-filters select { min-width:0; }
.r18-query-hint { margin:12px 0 22px; }
.r18-list-meta { display:flex; justify-content:space-between; gap:12px; font-size:12px; color:var(--muted); margin:18px 0; }
.r18-readonly { color:var(--r18-accent); }
.r18-grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(170px,1fr)); gap:20px 16px; }
.r18-card { display:block; color:inherit; text-decoration:none; min-width:0; border:1px solid var(--line); border-radius:12px; overflow:hidden; background:var(--panel-2); }
.r18-card:focus-visible { outline:2px solid var(--r18-accent); outline-offset:4px; }
.r18-card:hover { border-color:var(--r18-accent); }
.r18-cover { position:relative; aspect-ratio:3/4; overflow:hidden; background:var(--bg); }
.r18-cover :deep(img) { object-fit:contain; }
.r18-service { position:absolute; left:9px; bottom:9px; padding:3px 7px; font-size:10px; background:rgba(0,0,0,.75); color:#fff; border-radius:4px; }
.r18-card-copy { padding:12px; }
.r18-card-copy strong { font-size:13px; color:var(--r18-accent); overflow-wrap:anywhere; }
.r18-card-copy h3 { font-size:13px; line-height:1.65; margin:6px 0 9px; display:-webkit-box; -webkit-line-clamp:2; -webkit-box-orient:vertical; overflow:hidden; min-height:3.3em; }
.r18-card-copy > span { color:var(--muted); font-size:11px; }
.r18-pagination { display:flex; justify-content:center; gap:14px; align-items:center; margin-top:28px; flex-wrap:wrap; }
@media(max-width:1100px) { .r18-filters { grid-template-columns:1fr 1fr 1fr; }.r18-search { grid-column:span 2; } }
@media(max-width:600px) { .r18-header { gap:10px; }.r18-header h2 { font-size:22px; }.r18-filters { grid-template-columns:repeat(2,minmax(0,1fr)); }.r18-search { grid-column:span 2; }.r18-grid { grid-template-columns:repeat(2,minmax(0,1fr)); gap:12px; }.r18-card-copy { padding:9px; }.r18-list-meta { flex-wrap:wrap; } }
</style>
