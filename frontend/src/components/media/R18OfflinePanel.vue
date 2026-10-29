<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue';
import { cloud115Api, type OfflineTask, type OfflineState } from '../../api/cloud115';
import { useAuthStore } from '../../stores/auth';
import { usePlayerStore } from '../../stores/player';
import { usePageRequest } from '../../composables/usePageRequest';
import { playbackType } from '../../lib/player';
import { externalPlayerURL } from '../../lib/external-player';
const props = defineProps<{ library: string; video: string; title: string }>();
const auth = useAuthStore(), player = usePlayerStore(), scope = usePageRequest();
const state = ref<OfflineState>({ configured: false, tasks: [], sources: [] }), loaded = ref(false), busy = ref(false), error = ref(''), magnet = ref('');
const selections = ref<Record<string, string>>({}), selectedSource = ref('');
let timer: ReturnType<typeof setTimeout> | undefined;
const labels: Record<string, string> = { queued: '排队中', submitting: '提交中 / 正在对账', downloading: '115 离线下载中', discovering: '正在确认视频文件', waiting_selection: '请选择正片文件', ready: '已就绪', failed: '失败', timed_out: '等待超时', needs_auth: '需要重新授权', cancelled: '已取消本地等待', cleanup_pending: '缓存到期 / 正在自动删除', expired: '缓存已自动删除' };
const stages: Record<string, string> = { submit: '提交', reconcile: '任务对账', discover: '文件确认', poll: '轮询', remote: '网盘下载', file: '文件检查', configuration: '凭证装载', cancel: '取消', cache: '缓存目录', cleanup: '自动清理' };
const active = (t: OfflineTask) => ['queued', 'submitting', 'downloading', 'discovering'].includes(t.status);
const source = computed(() => state.value.sources.find(s => s.Id === selectedSource.value) ?? state.value.sources[0]);
function accept(next: OfflineState) { state.value = next; loaded.value = true; for (const t of next.tasks) if (!(t.id in selections.value)) selections.value[t.id] = t.file_id || t.files[0]?.id || ''; }
function schedule() { clearTimeout(timer); if (state.value.tasks.some(t => active(t) || t.status === 'cleanup_pending' || !!t.expires_at && t.status !== 'expired')) timer = setTimeout(() => { if (document.hidden) schedule(); else void load(); }, 5000); }
async function load() {
  if (busy.value) return;
  const request = scope.begin(); error.value = '';
  try { const next = await cloud115Api.state(props.library, props.video, request.signal); if (request.isCurrent()) accept(next); }
  catch (cause) { if (request.isCurrent()) error.value = cause instanceof Error ? cause.message : '115 状态读取失败'; }
  finally { if (request.isCurrent()) schedule(); }
}
async function write(work: (signal: AbortSignal) => Promise<OfflineState>) {
  if (busy.value) return;
  clearTimeout(timer); const request = scope.begin(); busy.value = true; error.value = '';
  try { const next = await work(request.signal); if (request.isCurrent()) { accept(next); magnet.value = ''; } }
  catch (cause) { if (request.isCurrent()) error.value = cause instanceof Error ? cause.message : '115 操作失败'; }
  finally { if (request.isCurrent()) { busy.value = false; schedule(); } }
}
function submit(resourceId?: string) { return write(signal => cloud115Api.submit(props.library, props.video, resourceId ? { resource_id: resourceId } : { magnet: magnet.value }, signal)); }
function select(t: OfflineTask) { return write(signal => cloud115Api.select(props.library, props.video, t.id, selections.value[t.id]!, signal)); }
function change(t: OfflineTask, action: 'retry' | 'cancel') { return write(signal => cloud115Api.change(props.library, props.video, t, action, signal)); }
function play() { if (source.value) player.open({ title: props.title, url: source.value.DirectStreamUrl, type: playbackType(source.value.Container) }); }
function external() { return source.value ? externalPlayerURL('potplayer', new URL(source.value.DirectStreamUrl, location.origin).href) : ''; }
defineExpose({ submit });
watch(() => [props.library, props.video], () => { clearTimeout(timer); scope.cancel(); busy.value = false; loaded.value = false; state.value = { configured: false, tasks: [], sources: [] }; selections.value = {}; selectedSource.value = ''; void load(); }, { immediate: true });
onScopeDispose(() => clearTimeout(timer));
</script>
<template>
  <section class="panel offline-panel" :aria-busy="busy">
    <div class="panel-head"><h2>115 播放源</h2><button class="btn btn-sm" :disabled="busy" @click="load">刷新状态</button></div>
    <p v-if="error" class="offline-error" role="alert">{{ error }}</p>
    <p v-if="!loaded && !error" class="hint">正在读取本地任务…</p>
    <p v-else-if="!state.configured" class="hint">尚未配置 115 连接。<RouterLink v-if="auth.isAdministrator" to="/settings">前往设置</RouterLink></p>
    <div v-if="source" class="offline-play">
      <label for="r18-cloud-source">播放版本<select id="r18-cloud-source" v-model="selectedSource"><option value="">默认就绪版本</option><option v-for="s in state.sources" :key="s.Id" :value="s.Id">{{ s.Name }}{{ s.Default ? '（默认）' : '' }}</option></select></label>
      <button class="btn btn-accent" @click="play">播放</button><a class="btn" :href="external()">PotPlayer</a>
    </div>
    <p v-else-if="loaded && state.configured" class="hint">暂无就绪播放源。先离线下载，再选择正片；页面关闭或离开不会停止后台任务。</p>
    <p class="hint">播放按实际客户端 User-Agent 获取临时直链并 302 跳转；不转码、不记录 R18 观看进度。浏览器不支持的编码可使用外部播放器。</p>
    <article v-for="t in state.tasks" :key="t.id" class="offline-task">
      <div class="offline-status"><strong>{{ labels[t.status] }}</strong><span>{{ t.progress }}%</span></div>
      <p v-if="t.expires_at && t.status !== 'expired'" class="hint">自动删除时间：{{ new Date(t.expires_at * 1000).toLocaleString() }}（缓存保留 24 小时）</p>
      <progress v-if="active(t)" :value="t.progress" max="100" :aria-label="labels[t.status]" />
      <p v-if="t.error" class="offline-error">{{ t.stage ? `${stages[t.stage] || t.stage}：` : '' }}{{ t.error }}</p>
      <div v-if="t.files.length" class="offline-select"><label :for="`file-${t.id}`">正片文件<select :id="`file-${t.id}`" v-model="selections[t.id]" :disabled="busy"><option v-for="f in t.files" :key="f.id" :value="f.id">{{ f.path }} · {{ (f.size / 1024 ** 3).toFixed(2) }} GiB</option></select></label><button class="btn" :disabled="busy || !selections[t.id]" @click="select(t)">设为默认播放源</button></div>
      <div class="offline-actions"><button v-if="active(t)" class="btn btn-sm" :disabled="busy" @click="change(t, 'cancel')">取消本地等待</button><button v-if="t.retryable && !active(t) && !['ready', 'waiting_selection'].includes(t.status)" class="btn btn-sm" :disabled="busy" @click="change(t, 'retry')">重试当前阶段</button></div>
    </article>
    <form v-if="auth.isAdministrator && state.configured" class="offline-manual" @submit.prevent="submit()"><label for="r18-manual-magnet">手动磁力<input id="r18-manual-magnet" v-model="magnet" placeholder="magnet:?xt=urn:btih:…" required maxlength="8192" :disabled="busy" /></label><button class="btn" :disabled="busy">{{ busy ? '处理中…' : '提交到 115' }}</button></form>
    <p v-if="auth.isAdministrator" class="hint">缓存统一存放于「缓存-24h自动删除」，创建任务缓存 24 小时后自动删除。取消仅停止本地等待，不延长保留时间；清理完成后可重新提交同一磁力下载。不要在任务子目录中放入永久文件。</p>
  </section>
</template>
<style scoped>
.offline-panel { margin:24px 0; }.offline-panel .panel-head { display:flex; align-items:center; justify-content:space-between; gap:12px; }.offline-status { display:flex; justify-content:space-between; gap:12px; }.offline-task { border-top:1px solid var(--line); padding:18px 0; overflow-wrap:anywhere; }.offline-task progress { display:block; width:100%; height:8px; margin-top:12px; accent-color:var(--accent); }.offline-play,.offline-select,.offline-manual { display:flex; align-items:end; gap:12px; flex-wrap:wrap; margin:16px 0; }.offline-panel label { display:grid; gap:8px; flex:1; min-width:0; font-size:13px; }.offline-panel input,.offline-panel select { width:100%; min-width:0; }.offline-panel .btn { min-height:44px; }.offline-error { color:var(--danger); overflow-wrap:anywhere; }.offline-actions { display:flex; gap:10px; margin-top:12px; }@media(max-width:720px){.offline-play label,.offline-select label,.offline-manual label { flex-basis:100%; }.offline-panel h2 { font-size:18px; }}
</style>
