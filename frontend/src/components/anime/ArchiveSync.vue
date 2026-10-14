<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue';
import { CalendarClock, Download, LoaderCircle, RefreshCw, Square } from '@lucide/vue';
import { animeApi } from '../../api/anime';
import { useAdminRead } from '../../composables/useAdminRead';

const emit = defineEmits<{ updated: [] }>();
const archive = useAdminRead(animeApi.archive);
const command = ref('');
const accepted = ref(false);
const cancelRequested = ref(false);
const commandError = ref('');
const running = computed(() => accepted.value || archive.data.value?.job?.running === true);
const snapshot = computed(() => archive.data.value?.snapshot);
const job = computed(() => archive.data.value?.job);
const status = computed(() => job.value?.status || archive.data.value?.state.status || '');
const phases: Record<string, string> = {
  manifest: '检查归档版本', download: '下载全量归档', verify: '校验归档',
  'subject.jsonlines': '导入动画条目', 'episode.jsonlines': '导入章节',
  'character.jsonlines': '导入角色', 'person.jsonlines': '导入制作人员',
  'subject-characters.jsonlines': '导入角色关联', 'subject-persons.jsonlines': '导入制作人员关联',
  'person-characters.jsonlines': '导入声优关联', 'subject-relations.jsonlines': '导入作品关联',
  indexes: '建立索引', activate: '切换目录快照', done: '完成',
};
const phase = computed(() => cancelRequested.value ? '正在取消' : phases[job.value?.phase ?? ''] || '准备导入');
const downloading = computed(() => job.value?.phase === 'download' && job.value.totalBytes > 0);
const percent = computed(() => downloading.value ? Math.min(100, Math.floor(job.value!.bytes / job.value!.totalBytes * 100)) : 0);
const message = computed(() => {
  if (running.value) return phase.value;
  if (status.value === 'success') return job.value?.upToDate ? '目录已是最新版本' : '目录更新完成';
  if (status.value === 'cancelled') return '任务已取消';
  if (!job.value && status.value === 'running') return '上次任务已中断';
  return '';
});
const failure = computed(() => commandError.value || (status.value === 'failed' ? job.value?.error || archive.data.value?.state.error : '') || '');
const retry = computed(() => !running.value && (status.value === 'failed' || status.value === 'cancelled' || (!job.value && status.value === 'running')));

function errorMessage(raw: string) {
  if (/archive_manifest_(request_failed|http_\d+)/.test(raw)) return '无法获取官方归档版本，请检查服务器到 GitHub 的连接。';
  if (/archive_download_(failed|incomplete|http_\d+)/.test(raw)) return '归档下载失败，请检查网络后重试。';
  if (/archive_(digest|size)_mismatch/.test(raw)) return '归档校验失败，请重新下载。';
  if (raw.includes('context deadline exceeded')) return '目录更新超时，请稍后重试。';
  return raw;
}
function size(bytes: number) { return `${(bytes / 1024 / 1024).toFixed(1)} MiB`; }
function date(value?: string) {
  if (!value) return '';
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

let timer: ReturnType<typeof setInterval> | undefined;
watch(running, value => {
  clearInterval(timer);
  timer = setInterval(() => { if (!archive.loading.value) void archive.load(); }, value ? 1500 : 15000);
}, { immediate: true });
onScopeDispose(() => clearInterval(timer));
watch(archive.data, (current, previous) => {
  if (!current) return;
  accepted.value = false;
  if (!current.job?.running) cancelRequested.value = false;
  if ((current.snapshot.digest && current.snapshot.digest !== previous?.snapshot.digest) || (previous?.job?.running && !current.job?.running)) emit('updated');
});

async function update() {
  if (command.value || running.value || !archive.data.value) return;
  command.value = 'start';
  commandError.value = '';
  try {
    await animeApi.importArchive();
    if (!archive.active()) return;
    accepted.value = true;
    await archive.load();
  } catch (cause) {
    if (archive.active()) commandError.value = cause instanceof Error ? cause.message : '启动下载失败';
  } finally { command.value = ''; }
}
async function cancel() {
  if (command.value || cancelRequested.value) return;
  command.value = 'cancel';
  commandError.value = '';
  try {
    await animeApi.cancelArchive();
    if (!archive.active()) return;
    cancelRequested.value = true;
    await archive.load();
  } catch (cause) {
    if (archive.active()) commandError.value = cause instanceof Error ? cause.message : '取消失败';
  } finally { command.value = ''; }
}
</script>

<template>
  <div class="archive-sync">
    <div class="archive-head">
      <div class="archive-summary">
        <strong>Bangumi Archive</strong>
        <template v-if="snapshot?.name">
          <span>{{ snapshot.subjects.toLocaleString() }} 部动画</span>
          <span>{{ snapshot.episodes.toLocaleString() }} 个章节</span>
        </template>
        <span v-else>{{ archive.loading.value && !archive.data.value ? '读取目录状态' : '尚未下载目录' }}</span>
      </div>
      <div class="archive-actions">
        <RouterLink class="btn" to="/scheduled"><CalendarClock :size="16" />计划任务</RouterLink>
        <button v-if="running" class="btn" :disabled="!!command || cancelRequested" @click="cancel">
          <LoaderCircle v-if="cancelRequested" :size="16" class="archive-spinner" /><Square v-else :size="16" />
          {{ cancelRequested ? '正在取消' : '取消任务' }}
        </button>
        <button v-else class="btn btn-accent" :disabled="!!command || !archive.data.value || !!archive.error.value" @click="update">
          <LoaderCircle v-if="command" :size="16" class="archive-spinner" /><RefreshCw v-else-if="snapshot?.name || retry" :size="16" /><Download v-else :size="16" />
          {{ command ? '正在启动' : retry ? '重试下载 / 更新' : snapshot?.name ? '更新目录' : '下载全量目录' }}
        </button>
      </div>
    </div>
    <div v-if="snapshot?.name" class="archive-meta">
      <span>{{ snapshot.name }}</span><span>导入时间：{{ date(snapshot.importedAt) }}</span>
    </div>
    <div v-if="running" class="archive-progress" role="status">
      <span>{{ phase }}</span>
      <template v-if="downloading">
        <progress aria-label="全量归档下载进度" :value="job!.bytes" :max="job!.totalBytes" />
        <span>{{ size(job!.bytes) }} / {{ size(job!.totalBytes) }} · {{ percent }}%</span>
      </template>
      <template v-else><progress aria-label="目录导入进度" /><span v-if="job?.rows">已处理 {{ job.rows.toLocaleString() }} 条</span></template>
    </div>
    <p v-else-if="message" class="archive-result" role="status">{{ message }}</p>
    <p v-if="failure" class="archive-error" role="alert">{{ errorMessage(failure) }}</p>
    <div v-if="archive.error.value" class="archive-error" role="alert">
      <span>读取任务状态失败：{{ archive.error.value }}</span>
      <button class="btn" :disabled="archive.loading.value" @click="archive.load"><RefreshCw :size="16" />重试状态查询</button>
    </div>
  </div>
</template>

<style scoped>
.archive-sync { padding: 12px 0; border-bottom: 1px solid var(--line, #ddd); margin-bottom: 16px; }
.archive-head, .archive-summary, .archive-actions, .archive-progress, .archive-meta, .archive-error { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; min-width: 0; }
.archive-head { justify-content: space-between; }
.archive-actions .btn { display: inline-flex; align-items: center; justify-content: center; gap: 6px; min-height: 36px; }
.archive-meta { margin-top: 10px; font-size: 12px; opacity: .75; overflow-wrap: anywhere; }
.archive-meta span, .archive-error span { min-width: 0; overflow-wrap: anywhere; }
.archive-progress, .archive-result, .archive-error { margin: 12px 0 0; }
.archive-progress progress { width: 240px; max-width: 100%; height: 8px; accent-color: var(--accent); }
.archive-error { color: var(--danger, #ef4444); overflow-wrap: anywhere; }
.archive-spinner { animation: archive-spin 1s linear infinite; }
@keyframes archive-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .archive-spinner { animation: none; } }
</style>
