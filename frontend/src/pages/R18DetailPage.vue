<script setup lang="ts">
import { computed, inject, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { ArrowLeft, Copy, ExternalLink, Magnet, Database, Play } from '@lucide/vue';
import { r18Api, type R18Video, type R18Resource } from '../api/r18';
import { usePageRequest } from '../composables/usePageRequest';
import { useToastStore } from '../stores/toasts';
import MediaImage from '../components/media/MediaImage.vue';
import R18Cover from '../components/media/R18Cover.vue';
import R18OfflinePanel from '../components/media/R18OfflinePanel.vue';
import { useAuthStore } from '../stores/auth';
import ReadState from '../components/common/ReadState.vue';
import EmptyState from '../components/common/EmptyState.vue';
import { viewHistoryKey } from '../router/view-history';
import { useR18Playback } from '../composables/useR18Playback';
import { magnetHash, offlineLabels, type R18PlaybackVersion } from '../lib/r18-playback';

const route = useRoute(), scope = usePageRequest(), resourceScope = usePageRequest(), toasts = useToastStore();
const router = useRouter(), history = inject(viewHistoryKey, null);
const video = ref<R18Video | null>(null), libraryName = ref('');
const auth = useAuthStore();
const loading = ref(false), error = ref(''), resourceLoading = ref(false), resourceError = ref('');
const resources = ref<R18Resource[]>([]), hasMore = ref(false), offset = ref(0);
const libraryId = computed(() => String(route.params.libraryId));
const id = computed(() => String(route.params.id));
const playback = useR18Playback(() => ({ library: libraryId.value, video: id.value, title: video.value?.title || '播放' }), resources);
const { versions, loaded: offlineLoaded, busy: playbackBusy, state: offlineState, waiting, message: playbackMessage } = playback;
const selectedVersionId = ref('');
const selectedVersion = computed(() => versions.value.find(v => v.id === selectedVersionId.value));
const canPlay = (version?: R18PlaybackVersion) => !!version && (auth.isAdministrator || !!version.source);
function versionStatus(version?: R18PlaybackVersion) { return version?.source ? '已就绪' : version?.task ? offlineLabels[version.task.status] : '未下载'; }
function choose(version: R18PlaybackVersion) { selectedVersionId.value = version.id; void playback.play(version); }
function resourceVersion(resource: R18Resource) { return versions.value.find(v => v.id === magnetHash(resource.magnet)); }
function showMagnets() { document.getElementById('r18-magnets')?.scrollIntoView({ behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth', block: 'start' }); }
const magnetVersions = computed(() => {
  const seen = new Set<string>();
  return resources.value.filter(r => { const key = magnetHash(r.magnet) || r.id; if (seen.has(key)) return false; seen.add(key); return true; });
});
const extraVersions = computed(() => versions.value.filter(v => v.magnet && !magnetVersions.value.some(r => magnetHash(r.magnet) === v.id)));
watch(versions, next => { if (!next.some(v => v.id === selectedVersionId.value)) selectedVersionId.value = next.find(v => v.source?.Default)?.id || next.find(v => v.source)?.id || next[0]?.id || ''; });
watch(selectedVersionId, (next, old) => { if (old && next !== old && waiting.value !== next) playback.stopWaiting(); });
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
        <div class="r18-playback">
          <label v-if="versions.length" class="version-choice" for="r18-play-version"><span>播放版本 · {{ versions.length }} 个磁力版本</span><select id="r18-play-version" v-model="selectedVersionId" :disabled="playbackBusy"><option v-for="(version, index) in versions" :key="version.id" :value="version.id">版本 {{ index + 1 }} · {{ version.name }} · {{ versionStatus(version) }}</option></select></label>
          <div class="r18-play-actions"><button id="detail-play" class="btn btn-accent btn-play-large" :disabled="!offlineLoaded || playbackBusy || !offlineState.configured || !canPlay(selectedVersion) || waiting === selectedVersionId" @click="selectedVersion && playback.play(selectedVersion)"><Play :size="17" fill="currentColor" aria-hidden="true" /> {{ waiting === selectedVersionId && waiting ? '准备播放中…' : '播放' }}</button><button class="btn" @click="showMagnets"><Magnet :size="16" aria-hidden="true" /> 查看磁力列表</button></div>
          <p v-if="waiting" class="hint" role="status">{{ playbackMessage }}</p>
          <p v-else class="hint">{{ auth.isAdministrator ? '点击播放，自动添加磁力 → 等待下载 → 获取直链播放。多个视频默认推荐最大文件，可在下方切换正片。' : '已就绪版本可直接播放；未下载版本由管理员准备。' }}</p>
        </div>
      </div>
    </section>
    <section id="r18-magnets" class="panel r18-resources" :aria-busy="resourceLoading">
      <div class="panel-head"><h2><Magnet :size="18" aria-hidden="true" /> 当前磁力列表</h2><span class="hint">已加载 {{ magnetVersions.length + extraVersions.length }} 个磁力版本{{ hasMore ? ' · 还有更多' : '' }}</span></div>
      <p class="hint">按 {{ video.number || video.content_id }} 匹配资源；每个不同磁力对应一个版本，相同 BTIH 合并展示。资源可用性未经下载验证。</p>
      <ReadState :loading="resourceLoading" :empty="!resources.length" :error="resourceError" @retry="loadResources(false)" />
      <EmptyState v-if="!resourceLoading && !resourceError && !resources.length && !extraVersions.length" title="暂无匹配的磁力资源" hint="影片资料已接入，但 article 中暂未找到对应番号的有效磁力链接。" />
      <article v-for="(r, index) in magnetVersions" :key="r.id" class="r18-resource" :class="{ 'is-selected': resourceVersion(r)?.id === selectedVersionId }">
        <div class="r18-resource-head"><h3>版本 {{ index + 1 }} · {{ r.title || r.number || '磁力资源' }}</h3><span>{{ size(r) }} · {{ versionStatus(resourceVersion(r)) }}</span></div>
        <div class="r18-resource-meta"><span>{{ r.website || '来源未知' }}</span><span>{{ r.date || '日期未知' }}</span><span v-for="tag in r.tags" :key="tag" class="r18-resource-tag">{{ tag }}</span></div>
        <code class="r18-magnet-text">{{ r.magnet }}</code>
        <div class="r18-resource-actions"><button class="btn btn-sm btn-accent" :disabled="playbackBusy || !offlineLoaded || !offlineState.configured || !canPlay(resourceVersion(r)) || waiting === resourceVersion(r)?.id" @click="resourceVersion(r) && choose(resourceVersion(r)!)"><Play :size="15" fill="currentColor" aria-hidden="true" /> {{ waiting === resourceVersion(r)?.id ? '准备播放中…' : '播放此版本' }}</button><button class="btn btn-sm" @click="copy(r.magnet)"><Copy :size="14" aria-hidden="true" /> 复制磁力</button><a v-if="r.detail_url" class="btn btn-sm" :href="r.detail_url" target="_blank" rel="noopener noreferrer"><ExternalLink :size="14" aria-hidden="true" /> 来源页面</a></div>
      </article>
      <article v-for="version in extraVersions" :key="version.id" class="r18-resource"><div class="r18-resource-head"><h3>{{ version.name }}</h3><span>{{ versionStatus(version) }}</span></div><code class="r18-magnet-text">{{ version.magnet }}</code><div class="r18-resource-actions"><button class="btn btn-sm btn-accent" :disabled="playbackBusy || !offlineState.configured || !canPlay(version) || waiting === version.id" @click="choose(version)"><Play :size="15" aria-hidden="true" /> 播放此版本</button><button class="btn btn-sm" @click="copy(version.magnet)"><Copy :size="14" aria-hidden="true" /> 复制磁力</button></div></article>
      <button v-if="hasMore" class="btn r18-more" :disabled="resourceLoading" @click="loadResources(true)">{{ resourceLoading ? '加载中…' : '加载更多资源' }}</button>
    </section>
    <R18OfflinePanel :library="libraryId" :video="id" :title="video.title" :playback="playback" />
    <section v-if="video.images.length" class="panel">
      <div class="panel-head"><h2>剧照</h2><span class="hint">当前资料提供的首末剧照</span></div>
      <div class="r18-gallery"><a v-for="image in video.images" :key="image" :href="image" target="_blank" rel="noopener noreferrer" aria-label="查看剧照原图"><MediaImage :src="image" alt="影片剧照" loading="lazy" referrerpolicy="no-referrer" /></a></div>
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
.r18-playback { max-width:560px; }.version-choice { display:grid; gap:8px; color:var(--muted); font-size:12px; }.version-choice select { width:100%; min-width:0; min-height:44px; }.r18-play-actions { display:flex; flex-wrap:wrap; gap:10px; margin:16px 0; }.btn-play-large { padding:0 20px; font-size:14px; font-weight:600; box-shadow:0 4px 14px rgba(26,113,255,.25); }.r18-playback .hint { line-height:1.8; }
.r18-gallery { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:16px; }.r18-gallery a { position:relative; display:block; aspect-ratio:16/9; overflow:hidden; border-radius:8px; background:var(--bg); }.r18-gallery :deep(img), .r18-gallery :deep(.image-placeholder) { position:absolute; inset:0; width:100%; height:100%; }.r18-gallery :deep(img) { display:block; object-fit:contain; }
.r18-resources h2 { display:flex; gap:8px; align-items:center; }
.r18-resource { padding:20px 0; border-bottom:1px solid var(--line); }.r18-resource.is-selected h3 { color:var(--accent); }.r18-resources { margin:24px 0; scroll-margin-top:24px; }.r18-resource .btn,.r18-play-actions .btn { min-height:44px; }
.r18-resource-head { display:flex; justify-content:space-between; gap:16px; align-items:baseline; }.r18-resource-head h3 { margin:0; font-size:15px; line-height:1.7; overflow-wrap:anywhere; }.r18-resource-head > span { flex-shrink:0; color:var(--accent); font-size:12px; }
.r18-resource-meta { display:flex; gap:12px; flex-wrap:wrap; color:var(--muted); font-size:12px; margin:10px 0; }.r18-resource-tag { color:var(--accent); }
.r18-magnet-text { display:block; padding:12px; border:1px solid var(--line); border-radius:6px; background:var(--bg); overflow-wrap:anywhere; white-space:pre-wrap; font-size:12px; line-height:1.8; user-select:text; }
.r18-resource-actions { display:flex; flex-wrap:wrap; gap:10px; margin-top:14px; }.r18-more { display:block; margin:24px auto 0; }
@media(max-width:720px) { .r18-detail-hero { grid-template-columns:1fr; gap:20px; }.r18-detail-hero .r18-detail-cover { width:120px; }.r18-detail-info h2 { font-size:19px; }.r18-gallery { grid-template-columns:1fr; }.r18-resource-head { flex-direction:column; gap:4px; }.r18-facts { gap:18px; } }
</style>
