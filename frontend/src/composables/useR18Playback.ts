import { computed, onScopeDispose, ref, watch, type Ref } from 'vue';
import { cloud115Api, type CloudProvider, type CloudSource, type OfflineApi, type OfflineState, type OfflineTask } from '../api/cloud115';
import type { R18Resource } from '../api/r18';
import { useAuthStore } from '../stores/auth';
import { usePlayerStore } from '../stores/player';
import { mainVideo, offlineActive, offlineStatusLabel, playbackVersions, type R18PlaybackVersion } from '../lib/r18-playback';
import { offlineProgressVisible, offlineStageMessage } from '../lib/offline-stage';
import { loadPlayerFactory, playbackType } from '../lib/player';
import { startPlaybackTiming, type PlaybackStage } from '../lib/playback-timing';
import { usePageRequest } from './usePageRequest';

export interface R18CloudConnection { id: CloudProvider; name: string; api: OfflineApi }
const defaultConnection: R18CloudConnection = { id: '115', name: '115', api: cloud115Api };
export function useR18Playback(item: () => { library: string; video: string; title: string }, resources: Ref<R18Resource[]> = ref([]), enabled: () => boolean = () => true, connection: () => R18CloudConnection = () => defaultConnection) {
  const auth = useAuthStore(), player = usePlayerStore(), scope = usePageRequest();
  const state = ref<OfflineState>({ configured: false, tasks: [], sources: [] });
  const loaded = ref(false), busy = ref(false), error = ref(''), waiting = ref('');
  const selections = ref<Record<string, string>>({});
  const providerName = computed(() => connection().name);
  const statusLabel = (status: string) => offlineStatusLabel(status, providerName.value);
  const versions = computed(() => playbackVersions(resources.value, state.value, connection().id));
  const pending = computed(() => versions.value.find(v => v.id === waiting.value));
  const message = computed(() => {
    if (!waiting.value) return '';
    const task = pending.value?.task;
    // 轮询期间保留最近的任务阶段，避免每次读取状态都退回准备播放提示。
    if (busy.value && (!task || !offlineActive(task))) return '正在准备播放…';
    return [statusLabel(task?.status || 'queued'), offlineStageMessage(task), offlineProgressVisible(task) ? `${task!.progress}%` : '', '就绪后自动播放'].filter(Boolean).join(' · ');
  });
  let timer: ReturnType<typeof setTimeout> | undefined;
  let timing: (stage: PlaybackStage) => void = () => {};
  function accept(next: OfflineState) {
    state.value = next; loaded.value = true;
    for (const t of next.tasks) {
      if (!t.files.some(f => f.id === selections.value[t.id])) selections.value[t.id] = t.file_id || mainVideo(t.files)?.id || '';
    }
  }
  function schedule() {
    clearTimeout(timer);
    if (!enabled()) return;
    if (waiting.value || state.value.tasks.some(t => offlineActive(t) || t.status === 'cleanup_pending' || !!t.expires_at && t.status !== 'expired')) {
      timer = setTimeout(() => { if (document.hidden) schedule(); else void load(); }, waiting.value ? 1000 : 5000);
    }
  }
  function open(source: CloudSource) {
    waiting.value = '';
    const trace = timing;
    player.open({ title: item().title, url: source.DirectStreamUrl, type: playbackType(source.Container), onLoaded: () => trace('player_loaded'), onFirstFrame: () => trace('first_frame') });
  }
  async function advance(signal: AbortSignal, current: () => boolean, api: OfflineApi) {
    const version = pending.value;
    if (!version || !current()) return;
    if (version.source) {
      timing('source_ready');
      const next = await api.prepare(item().library, item().video, version.source.Id, signal);
      if (!current()) return;
      accept(next);
      if (waiting.value === version.id && pending.value?.source) { timing('prepared'); open(pending.value.source); }
      return;
    }
    const task = version.task;
    if (!task) return;
    if (['waiting_selection', 'ready'].includes(task.status) && task.files.length && auth.isAdministrator) {
      const file = task.files.find(f => f.id === task.file_id) ?? mainVideo(task.files);
      if (!file) return;
      // 自动绑定保留资料库默认版本，手动选择才修改默认播放源。
      timing('source_ready');
      const next = await api.select(item().library, item().video, task.id, file.id, signal, false, true);
      if (!current()) return;
      accept(next);
      if (waiting.value !== version.id) return;
      const source = pending.value?.source;
      if (source) { timing('prepared'); open(source); }
      else if (!pending.value?.task || !offlineActive(pending.value.task)) { waiting.value = ''; error.value = '播放源尚未就绪，请刷新状态后重试'; }
    } else if (!offlineActive(task)) {
      waiting.value = ''; error.value = task.error || `${statusLabel(task.status)}，请处理任务后重新播放`;
    }
  }
  async function run(work: (signal: AbortSignal, current: () => boolean, cloud: R18CloudConnection) => Promise<void>) {
    if (!enabled() || busy.value) return;
    clearTimeout(timer); const request = scope.begin(), cloud = connection(); busy.value = true; error.value = '';
    try { await work(request.signal, () => enabled() && connection().id === cloud.id && request.isCurrent(), cloud); }
    catch (cause) { if (request.isCurrent()) error.value = cause instanceof Error ? cause.message : `${cloud.name} 操作失败`; }
    finally { if (request.isCurrent()) { busy.value = false; schedule(); } }
  }
  async function load() {
    return run(async (signal, current, cloud) => {
      const next = await cloud.api.state(item().library, item().video, signal);
      if (!current()) return;
      accept(next); await advance(signal, current, cloud.api);
    });
  }
  async function play(version: R18PlaybackVersion) {
    if (!enabled() || busy.value || version.provider && version.provider !== connection().id) return;
    timing = startPlaybackTiming();
    void loadPlayerFactory().catch(() => {});
    waiting.value = version.id;
    return run(async (signal, current, cloud) => {
      try {
        // 操作前重读当前网盘的本地状态，避免使用过期播放源或重复提交。
        acceptIfCurrent(await cloud.api.state(item().library, item().video, signal), current);
        if (current()) timing('local_state');
        if (!current() || waiting.value !== version.id) return;
        const chosen = versions.value.find(v => v.id === version.id) ?? version;
        if (chosen.source) { await advance(signal, current, cloud.api); return; }
        if (!auth.isAdministrator) throw new Error('此版本尚未就绪，请管理员先准备播放源');
        if (!state.value.configured) throw new Error(`请先在设置中配置 ${cloud.name} 连接`);
        if (!chosen.task || chosen.task.status === 'expired') {
          const value = chosen.resourceId ? { resource_id: chosen.resourceId } : { magnet: chosen.magnet };
          acceptIfCurrent(await cloud.api.submit(item().library, item().video, value, signal), current);
          if (current()) timing('task_submitted');
        } else if (chosen.task.retryable && !offlineActive(chosen.task) && !['ready', 'waiting_selection', 'cleanup_pending'].includes(chosen.task.status)) {
          acceptIfCurrent(await cloud.api.change(item().library, item().video, chosen.task, 'retry', signal), current);
        }
        if (current() && waiting.value === version.id) {
          if (!pending.value?.task && !pending.value?.source) throw new Error('未找到此磁力的本地任务，请刷新状态后重试');
          await advance(signal, current, cloud.api);
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
    return run(async (signal, current, cloud) => {
      const preparing = pending.value?.task?.id === task.id;
      acceptIfCurrent(await cloud.api.select(item().library, item().video, task.id, selections.value[task.id]!, signal, true, preparing), current);
      if (current() && preparing && pending.value?.task?.id === task.id && pending.value.source) { timing('prepared'); open(pending.value.source); }
    });
  }
  async function change(task: OfflineTask, action: 'retry' | 'cancel') {
    if (action === 'cancel' && pending.value?.task?.id === task.id) stopWaiting();
    return run(async (signal, current, cloud) => { acceptIfCurrent(await cloud.api.change(item().library, item().video, task, action, signal), current); });
  }
  // 路由、播放模式或网盘一变就同步撤销旧请求，防止迟到结果打开错误网盘的影片。
  watch([enabled, () => item().library, () => item().video, () => connection().id], () => {
    clearTimeout(timer); scope.cancel(); busy.value = false; loaded.value = false; waiting.value = ''; error.value = '';
    state.value = { configured: false, tasks: [], sources: [] }; selections.value = {};
    if (enabled()) void load();
  }, { immediate: true, flush: 'sync' });
  onScopeDispose(() => clearTimeout(timer));
  return { state, loaded, busy, error, waiting, versions, pending, message, selections, providerName, statusLabel, load, play, select, change, stopWaiting };
}
export type R18Playback = ReturnType<typeof useR18Playback>;
