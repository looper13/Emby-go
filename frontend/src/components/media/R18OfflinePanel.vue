<script setup lang="ts">
import { computed, ref } from 'vue';
import { Play } from '@lucide/vue';
import { useAuthStore } from '../../stores/auth';
import { useR18Playback, type R18Playback } from '../../composables/useR18Playback';
import { magnetHash, offlineLabels as labels, offlineActive as active, mainVideo } from '../../lib/r18-playback';
import { externalPlayerURL } from '../../lib/external-player';
const props = defineProps<{ library: string; video: string; title: string; playback?: R18Playback }>();
const auth = useAuthStore(), workflow = props.playback ?? useR18Playback(() => props);
const { state, loaded, busy, error, waiting, message, selections, load, select, change, stopWaiting } = workflow;
const magnet = ref(''), selectedSource = ref('');
const stages: Record<string, string> = { submit: '提交', reconcile: '任务对账', discover: '文件确认', poll: '轮询', remote: '网盘下载', file: '文件检查', configuration: '凭证装载', cancel: '取消', cache: '缓存目录', cleanup: '自动清理' };
const source = computed(() => state.value.sources.find(s => s.Id === selectedSource.value) ?? state.value.sources[0]);
async function playManual() {
  const value = magnet.value.trim(), hash = magnetHash(value);
  if (!hash) { error.value = '请输入有效的 BTIH 磁力链接'; return; }
  await workflow.play({ id: hash, name: '手动磁力版本', magnet: value, resourceId: '' });
  if (!error.value) magnet.value = '';
}
function play() { const version = workflow.versions.value.find(v => v.source?.Id === source.value?.Id); if (version) void workflow.play(version); }
function external() { return source.value ? externalPlayerURL('potplayer', new URL(source.value.DirectStreamUrl, location.origin).href) : ''; }
</script>
<template>
  <section class="panel offline-panel" :aria-busy="busy">
    <div class="panel-head"><h2>115 播放源</h2><button class="btn btn-sm" :disabled="busy" @click="load">刷新状态</button></div>
    <p v-if="error" class="offline-error" role="alert">{{ error }}</p>
    <p v-if="!loaded && !error" class="hint">正在读取本地任务…</p>
    <p v-else-if="loaded && !state.configured" class="hint">尚未配置 115 连接。<RouterLink v-if="auth.isAdministrator" to="/settings">前往设置</RouterLink></p>
    <div v-if="waiting" class="offline-wait" role="status"><span>{{ message }}</span><button class="btn btn-sm" @click="stopWaiting">停止自动播放</button></div>
    <div v-if="source" class="offline-play">
      <label for="r18-cloud-source">播放版本<select id="r18-cloud-source" v-model="selectedSource"><option value="">默认就绪版本</option><option v-for="s in state.sources" :key="s.Id" :value="s.Id">{{ s.Name }}{{ s.Default ? '（默认）' : '' }}</option></select></label>
      <button class="btn btn-accent" :disabled="busy" @click="play"><Play :size="17" fill="currentColor" aria-hidden="true" /> 播放</button><a class="btn" :href="external()">PotPlayer</a>
    </div>
    <p v-else-if="loaded && state.configured" class="hint">选择磁力版本并点击播放，自动添加磁力、等待下载并获取播放直链。页面关闭或离开不会停止后台下载，但会停止自动打开播放器。</p>
    <p class="hint">播放按实际客户端 User-Agent 获取临时直链并 302 跳转；不转码、不记录 R18 观看进度。浏览器不支持的编码可使用外部播放器。</p>
    <article v-for="t in state.tasks" :key="t.id" class="offline-task">
      <p class="offline-version">{{ workflow.versions.value.find(v => v.task?.id === t.id)?.name || '磁力版本' }}<span v-if="t.info_hash" class="mono">{{ t.info_hash.slice(0, 12) }}</span></p>
      <div class="offline-status"><strong>{{ labels[t.status] }}</strong><span>{{ t.progress }}%</span></div>
      <p v-if="t.expires_at && t.status !== 'expired'" class="hint">自动删除时间：{{ new Date(t.expires_at * 1000).toLocaleString() }}（缓存保留 24 小时）</p>
      <progress v-if="active(t)" :value="t.progress" max="100" :aria-label="labels[t.status]" />
      <p v-if="t.error" class="offline-error">{{ t.stage ? `${stages[t.stage] || t.stage}：` : '' }}{{ t.error }}</p>
      <div v-if="t.files.length" class="offline-select"><label :for="`file-${t.id}`">正片文件<select :id="`file-${t.id}`" v-model="selections[t.id]" :disabled="busy"><option v-for="f in t.files" :key="f.id" :value="f.id">{{ f.path }} · {{ (f.size / 1024 ** 3).toFixed(2) }} GiB{{ f.id === mainVideo(t.files)?.id ? '（推荐正片）' : '' }}</option></select></label><button class="btn" :disabled="busy || !selections[t.id]" @click="select(t)">设为默认播放源</button></div>
      <div class="offline-actions"><button v-if="active(t)" class="btn btn-sm" :disabled="busy" @click="change(t, 'cancel')">取消本地等待</button><button v-if="t.retryable && !active(t) && !['ready', 'waiting_selection'].includes(t.status)" class="btn btn-sm" :disabled="busy" @click="change(t, 'retry')">重试当前阶段</button></div>
    </article>
    <form v-if="auth.isAdministrator && state.configured" class="offline-manual" @submit.prevent="playManual"><label for="r18-manual-magnet">添加其它磁力版本<input id="r18-manual-magnet" v-model="magnet" placeholder="magnet:?xt=urn:btih:…" required maxlength="8192" :disabled="busy" /></label><button class="btn" :disabled="busy"><Play :size="17" aria-hidden="true" /> {{ busy ? '处理中…' : '添加并播放' }}</button></form>
    <p v-if="auth.isAdministrator" class="hint">缓存统一存放于「缓存-24h自动删除」，创建任务缓存 24 小时后自动删除。取消仅停止本地等待，不延长保留时间；清理完成后可重新提交同一磁力下载。不要在任务子目录中放入永久文件。</p>
  </section>
</template>
<style scoped>
.offline-panel { margin:24px 0; }.offline-panel .panel-head { display:flex; align-items:center; justify-content:space-between; gap:12px; }.offline-status { display:flex; justify-content:space-between; gap:12px; }.offline-task { border-top:1px solid var(--line); padding:18px 0; overflow-wrap:anywhere; }.offline-task progress { display:block; width:100%; height:8px; margin-top:12px; accent-color:var(--accent); }.offline-play,.offline-select,.offline-manual { display:flex; align-items:end; gap:12px; flex-wrap:wrap; margin:16px 0; }.offline-panel label { display:grid; gap:8px; flex:1; min-width:0; font-size:13px; }.offline-panel input,.offline-panel select { width:100%; min-width:0; }.offline-panel .btn { min-height:44px; }.offline-error { color:var(--danger); overflow-wrap:anywhere; }.offline-actions { display:flex; gap:10px; margin-top:12px; }@media(max-width:720px){.offline-play label,.offline-select label,.offline-manual label { flex-basis:100%; }.offline-panel h2 { font-size:18px; }}
.offline-wait { display:flex; align-items:center; justify-content:space-between; gap:12px; flex-wrap:wrap; padding:12px; background:var(--bg); border-left:3px solid var(--accent); font-size:13px; }
.offline-version { display:flex; gap:12px; flex-wrap:wrap; font-size:13px; }.offline-version span { color:var(--muted); font-size:12px; }
</style>
