import { computed, onScopeDispose, ref, watch, type Ref } from 'vue';
import { cloud115Api, type CloudSource, type OfflineState, type OfflineTask } from '../api/cloud115';
import type { R18Resource } from '../api/r18';
import { useAuthStore } from '../stores/auth';
import { usePlayerStore } from '../stores/player';
import { mainVideo, offlineActive, offlineLabels, playbackVersions, type R18PlaybackVersion } from '../lib/r18-playback';
import { playbackType } from '../lib/player';
import { usePageRequest } from './usePageRequest';

export function useR18Playback(item: () => { library: string; video: string; title: string }, resources: Ref<R18Resource[]> = ref([])) {
  const auth = useAuthStore(), player = usePlayerStore(), scope = usePageRequest();
  const state = ref<OfflineState>({ configured: false, tasks: [], sources: [] });
  const loaded = ref(false), busy = ref(false), error = ref(''), waiting = ref('');
  const selections = ref<Record<string, string>>({});
  const versions = computed(() => playbackVersions(resources.value, state.value));
  const pending = computed(() => versions.value.find(v => v.id === waiting.value));
  const message = computed(() => waiting.value ? busy.value ? '正在准备播放…' : `${offlineLabels[pending.value?.task?.status || 'queued']} · ${pending.value?.task?.progress || 0}% · 就绪后自动播放` : '');
  let timer: ReturnType<typeof setTimeout> | undefined;
  function accept(next: OfflineState) {
    state.value = next; loaded.value = true;
    for (const t of next.tasks) {
      if (!t.files.some(f => f.id === selections.value[t.id])) selections.value[t.id] = t.file_id || mainVideo(t.files)?.id || '';
    }
  }
  function schedule() {
    clearTimeout(timer);
    if (waiting.value || state.value.tasks.some(t => offlineActive(t) || t.status === 'cleanup_pending' || !!t.expires_at && t.status !== 'expired')) {
      timer = setTimeout(() => { if (document.hidden) schedule(); else void load(); }, 5000);
    }
  }
  function open(source: CloudSource) {
    waiting.value = '';
    player.open({ title: item().title, url: source.DirectStreamUrl, type: playbackType(source.Container) });
  }
  async function advance(signal: AbortSignal, current: () => boolean) {
    const version = pending.value;
    if (!version || !current()) return;
    if (version.source) {
      const next = await cloud115Api.prepare(item().library, item().video, version.source.Id, signal);
      if (!current()) return;
      accept(next);
      if (waiting.value === version.id && pending.value?.source) open(pending.value.source);
      return;
    }
    const task = version.task;
    if (!task) return;
    if (['waiting_selection', 'ready'].includes(task.status) && task.files.length && auth.isAdministrator) {
      const file = task.files.find(f => f.id === task.file_id) ?? mainVideo(task.files);
      if (!file) return;
      // Automatic binding keeps the library default unchanged; manual selection can change it.
      const next = await cloud115Api.select(item().library, item().video, task.id, file.id, signal, false);
      if (!current()) return;
      accept(next);
      if (waiting.value !== version.id) return;
      const source = pending.value?.source;
      if (source) await advance(signal, current);
      else { waiting.value = ''; error.value = '播放源尚未就绪，请刷新状态后重试'; }
    } else if (!offlineActive(task)) {
      waiting.value = ''; error.value = task.error || `${offlineLabels[task.status]}，请处理任务后重新播放`;
    }
  }
  async function run(work: (signal: AbortSignal, current: () => boolean) => Promise<void>) {
    if (busy.value) return;
    clearTimeout(timer); const request = scope.begin(); busy.value = true; error.value = '';
    try { await work(request.signal, request.isCurrent); }
    catch (cause) { if (request.isCurrent()) error.value = cause instanceof Error ? cause.message : '115 操作失败'; }
    finally { if (request.isCurrent()) { busy.value = false; schedule(); } }
  }
  async function load() {
    return run(async (signal, current) => {
      const next = await cloud115Api.state(item().library, item().video, signal);
      if (!current()) return;
      accept(next); await advance(signal, current);
    });
  }
  async function play(version: R18PlaybackVersion) {
    if (busy.value) return;
    waiting.value = version.id;
    return run(async (signal, current) => {
      try {
        // Re-read local state before acting: avoid stale sources/tickets and duplicate submissions.
        acceptIfCurrent(await cloud115Api.state(item().library, item().video, signal), current);
        if (!current() || waiting.value !== version.id) return;
        const chosen = versions.value.find(v => v.id === version.id) ?? version;
        if (chosen.source) { await advance(signal, current); return; }
        if (!auth.isAdministrator) throw new Error('此版本尚未就绪，请管理员先准备播放源');
        if (!state.value.configured) throw new Error('请先在设置中配置 115 连接');
        if (!chosen.task || chosen.task.status === 'expired') {
          const value = chosen.resourceId ? { resource_id: chosen.resourceId } : { magnet: chosen.magnet };
          acceptIfCurrent(await cloud115Api.submit(item().library, item().video, value, signal), current);
        } else if (chosen.task.retryable && !offlineActive(chosen.task) && !['ready', 'waiting_selection', 'cleanup_pending'].includes(chosen.task.status)) {
          acceptIfCurrent(await cloud115Api.change(item().library, item().video, chosen.task, 'retry', signal), current);
        }
        if (current() && waiting.value === version.id) {
          if (!pending.value?.task && !pending.value?.source) throw new Error('未找到此磁力的本地任务，请刷新状态后重试');
          await advance(signal, current);
        }
      } catch (cause) {
        if (current()) waiting.value = '';
        throw cause;
      }
    });
  }
  function acceptIfCurrent(next: OfflineState, current: () => boolean) { if (current()) accept(next); }
  function stopWaiting() { waiting.value = ''; schedule(); }
  async function select(task: OfflineTask) {
    return run(async (signal, current) => {
      acceptIfCurrent(await cloud115Api.select(item().library, item().video, task.id, selections.value[task.id]!, signal), current);
      if (current()) await advance(signal, current);
    });
  }
  async function change(task: OfflineTask, action: 'retry' | 'cancel') {
    if (action === 'cancel' && pending.value?.task?.id === task.id) stopWaiting();
    return run(async (signal, current) => { acceptIfCurrent(await cloud115Api.change(item().library, item().video, task, action, signal), current); });
  }
  watch(() => [item().library, item().video], () => {
    clearTimeout(timer); scope.cancel(); busy.value = false; loaded.value = false; waiting.value = ''; error.value = '';
    state.value = { configured: false, tasks: [], sources: [] }; selections.value = {}; void load();
  }, { immediate: true });
  onScopeDispose(() => clearTimeout(timer));
  return { state, loaded, busy, error, waiting, versions, pending, message, selections, load, play, select, change, stopWaiting };
}
export type R18Playback = ReturnType<typeof useR18Playback>;
