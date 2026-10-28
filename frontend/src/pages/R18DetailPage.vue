<script setup lang="ts">
import { computed, inject, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { ArrowLeft, Copy, ExternalLink, Magnet, Database } from '@lucide/vue';
import { r18Api, type R18Video, type R18Resource } from '../api/r18';
import { usePageRequest } from '../composables/usePageRequest';
import { useToastStore } from '../stores/toasts';
import MediaImage from '../components/media/MediaImage.vue';
import R18Cover from '../components/media/R18Cover.vue';
import ReadState from '../components/common/ReadState.vue';
import EmptyState from '../components/common/EmptyState.vue';
import { viewHistoryKey } from '../router/view-history';

const route = useRoute(), scope = usePageRequest(), resourceScope = usePageRequest(), toasts = useToastStore();
const router = useRouter(), history = inject(viewHistoryKey, null);
const video = ref<R18Video | null>(null), libraryName = ref('');
const loading = ref(false), error = ref(''), resourceLoading = ref(false), resourceError = ref('');
const resources = ref<R18Resource[]>([]), hasMore = ref(false), offset = ref(0);
const libraryId = computed(() => String(route.params.libraryId));
const id = computed(() => String(route.params.id));
const backTo = computed(() => {
  const from = typeof route.query.from === 'string' ? route.query.from : '';
  return (from === '/r18' || from.startsWith('/r18?')) && !/[\\\r\n]/u.test(from) && from.length < 4096 ? from : { path: '/r18', query: { library_id: libraryId.value } };
});
function back() { if (history) history.backTo(backTo.value); else void router.push(backTo.value); }
async function loadResources(more = false) {
  if (resourceLoading.value && more) return;
  const request = resourceScope.begin(); resourceLoading.value = true; resourceError.value = '';
  try {
    const next = await r18Api.resources(libraryId.value, id.value, more ? offset.value : 0, request.signal);
    if (!request.isCurrent()) return;
    const items = more ? [...resources.value, ...next.items] : next.items;
    resources.value = items.filter((item, i) => items.findIndex(v => v.id === item.id) === i);
    offset.value = next.next_offset; hasMore.value = next.has_more;
  } catch (cause) { if (request.isCurrent()) resourceError.value = cause instanceof Error ? cause.message : '磁力资源加载失败'; }
  finally { if (request.isCurrent()) resourceLoading.value = false; }
}
async function load() {
  const request = scope.begin(); resourceScope.cancel(); loading.value = true; error.value = '';
  video.value = null; resources.value = []; resourceError.value = ''; resourceLoading.value = false; hasMore.value = false; offset.value = 0;
  try {
    const result = await r18Api.detail(libraryId.value, id.value, request.signal);
    if (!request.isCurrent()) return;
    video.value = result.video; libraryName.value = result.library_name;
    void loadResources();
  } catch (cause) { if (request.isCurrent()) error.value = cause instanceof Error ? cause.message : '影片详情加载失败'; }
  finally { if (request.isCurrent()) loading.value = false; }
}
async function copy(magnet: string) {
  try {
    if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(magnet);
    else {
      const previous = document.activeElement as HTMLElement | null;
      const input = document.createElement('textarea'); input.value = magnet;
      input.style.position = 'fixed'; input.style.opacity = '0'; document.body.append(input);
      try { input.select(); if (!document.execCommand('copy')) throw new Error('Copy unavailable'); }
      finally { input.remove(); previous?.focus(); }
    }
    toasts.show('已复制磁力链接');
  }
  catch { toasts.show('复制失败，请选中磁力链接手动复制', 'error'); }
}
function size(r: R18Resource) {
  const bytes = r.size_bytes || r.size_mb * 1024 * 1024;
  return bytes > 0 ? `${(bytes / 1024 ** 3).toFixed(2)} GiB` : '大小未知';
}
watch(() => route.fullPath, () => { if (route.name === 'r18-detail') void load(); }, { immediate: true });
</script>

<template>
  <button class="btn btn-sm r18-back" type="button" @click="back"><ArrowLeft :size="15" aria-hidden="true" /> 返回资料库</button>
  <ReadState :loading="loading" :empty="!video" :error="error" @retry="load" />
  <template v-if="video">
    <section class="panel r18-detail-hero">
      <R18Cover class="r18-detail-cover" :src="video.cover_url" :alt="video.number || video.content_id" />
      <div class="r18-detail-info">
        <p class="r18-detail-source"><Database :size="14" aria-hidden="true" /> {{ libraryName }} / {{ video.service_code }}</p>
        <p class="r18-detail-number mono">{{ video.number || video.content_id }}</p>
        <h2>{{ video.title }}</h2>
        <p v-if="video.original_title && video.original_title !== video.title" class="r18-original">{{ video.original_title }}</p>
        <dl class="r18-facts"><div><dt>发行日期</dt><dd>{{ video.release_date || '未知' }}</dd></div><div><dt>内容 ID</dt><dd class="mono">{{ video.content_id }}</dd></div><div><dt>来源类型</dt><dd>{{ video.service_code }}</dd></div></dl>
        <p class="r18-play-todo">播放接入待实现（TODO）。磁力仅供查看和复制，本期不启动播放器、下载器或订阅任务。</p>
      </div>
    </section>
    <section v-if="video.images.length" class="panel">
      <div class="panel-head"><h2>剧照</h2><span class="hint">当前资料提供的首末剧照</span></div>
      <div class="r18-gallery"><a v-for="image in video.images" :key="image" :href="image" target="_blank" rel="noopener noreferrer" aria-label="查看剧照原图"><MediaImage :src="image" alt="影片剧照" loading="lazy" referrerpolicy="no-referrer" /></a></div>
    </section>
    <section class="panel r18-resources" :aria-busy="resourceLoading">
      <div class="panel-head"><h2><Magnet :size="18" aria-hidden="true" /> 磁力资源</h2><span class="hint">article · 已加载 {{ resources.length }} 条</span></div>
      <p class="hint">按 {{ video.number || video.content_id }} 匹配资源，已拒绝的记录不展示；资源可用性未经下载验证。</p>
      <ReadState :loading="resourceLoading" :empty="!resources.length" :error="resourceError" @retry="loadResources(false)" />
      <EmptyState v-if="!resourceLoading && !resourceError && !resources.length" title="暂无匹配的磁力资源" hint="影片资料已接入，但 article 中暂未找到对应番号的有效磁力链接。" />
      <article v-for="r in resources" :key="r.id" class="r18-resource">
        <div class="r18-resource-head"><h3>{{ r.title || r.number || '磁力资源' }}</h3><span>{{ size(r) }}</span></div>
        <div class="r18-resource-meta"><span>{{ r.website || '来源未知' }}</span><span>{{ r.date || '日期未知' }}</span><span v-for="tag in r.tags" :key="tag" class="r18-resource-tag">{{ tag }}</span></div>
        <code class="r18-magnet-text">{{ r.magnet }}</code>
        <div class="r18-resource-actions"><button class="btn btn-sm btn-accent" @click="copy(r.magnet)"><Copy :size="14" aria-hidden="true" /> 复制磁力</button><a v-if="r.detail_url" class="btn btn-sm" :href="r.detail_url" target="_blank" rel="noopener noreferrer"><ExternalLink :size="14" aria-hidden="true" /> 来源页面</a></div>
      </article>
      <button v-if="hasMore" class="btn r18-more" :disabled="resourceLoading" @click="loadResources(true)">{{ resourceLoading ? '加载中…' : '加载更多资源' }}</button>
    </section>
  </template>
</template>

<style scoped>
.r18-back { margin-bottom:18px; }
.r18-detail-hero { display:grid; grid-template-columns:156px minmax(0,1fr); align-items:start; gap:26px; }
.r18-detail-source { display:flex; align-items:center; gap:7px; font-size:12px; color:var(--muted); margin-top:0; }
.r18-detail-number { color:var(--accent); font-size:22px; margin:18px 0 12px; overflow-wrap:anywhere; }
.r18-detail-info h2 { font-size:24px; line-height:1.65; overflow-wrap:anywhere; }
.r18-original { color:var(--muted); line-height:1.7; overflow-wrap:anywhere; }
.r18-facts { display:flex; flex-wrap:wrap; gap:22px; margin:26px 0; }
.r18-facts dt { color:var(--muted); font-size:12px; margin-bottom:6px; }.r18-facts dd { margin:0; overflow-wrap:anywhere; }
.r18-play-todo { border-left:3px solid var(--accent); padding:12px 16px; background:var(--bg); color:var(--muted); font-size:13px; line-height:1.8; }
.r18-gallery { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:16px; }.r18-gallery a { position:relative; display:block; aspect-ratio:16/9; overflow:hidden; border-radius:8px; background:var(--bg); }.r18-gallery :deep(img), .r18-gallery :deep(.image-placeholder) { position:absolute; inset:0; width:100%; height:100%; }.r18-gallery :deep(img) { display:block; object-fit:contain; }
.r18-resources h2 { display:flex; gap:8px; align-items:center; }
.r18-resource { padding:20px 0; border-bottom:1px solid var(--line); }
.r18-resource-head { display:flex; justify-content:space-between; gap:16px; align-items:baseline; }.r18-resource-head h3 { margin:0; font-size:15px; line-height:1.7; overflow-wrap:anywhere; }.r18-resource-head > span { flex-shrink:0; color:var(--accent); font-size:12px; }
.r18-resource-meta { display:flex; gap:12px; flex-wrap:wrap; color:var(--muted); font-size:12px; margin:10px 0; }.r18-resource-tag { color:var(--accent); }
.r18-magnet-text { display:block; padding:12px; border:1px solid var(--line); border-radius:6px; background:var(--bg); overflow-wrap:anywhere; white-space:pre-wrap; font-size:12px; line-height:1.8; user-select:text; }
.r18-resource-actions { display:flex; flex-wrap:wrap; gap:10px; margin-top:14px; }.r18-more { display:block; margin:24px auto 0; }
@media(max-width:720px) { .r18-detail-hero { grid-template-columns:1fr; gap:20px; }.r18-detail-hero .r18-detail-cover { width:120px; }.r18-detail-info h2 { font-size:19px; }.r18-gallery { grid-template-columns:1fr; }.r18-resource-head { flex-direction:column; gap:4px; }.r18-facts { gap:18px; } }
</style>
