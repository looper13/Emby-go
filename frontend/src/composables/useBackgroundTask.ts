import { computed, reactive, shallowRef } from 'vue';
import { ApiError } from '../api/client';
import { backgroundApi, type BackgroundKind, type BackgroundProgress } from '../api/background';
import { useToastStore } from '../stores/toasts';

// One application-owned lifetime per backend job family. Page unmount never resets it.
export function useBackgroundTask(kind: BackgroundKind, blocked: () => boolean, completed: () => void) {
  const progress = shallowRef<BackgroundProgress | null>(null); const toasts = useToastStore();
  const state = reactive({ progress, visible: false, unavailable: false, submitting: false, cancelling: false,
    busy: computed((): boolean => state.submitting || Boolean(progress.value?.running)) });
  let generation = 0, failures = 0, tracking = false;
  let timer: ReturnType<typeof setTimeout> | undefined, hide: ReturnType<typeof setTimeout> | undefined;
  let reading: Promise<void> | undefined;
  let readController: AbortController | undefined, writeController: AbortController | undefined, cancelController: AbortController | undefined;
  const interval = kind === 'probe' ? 1000 : 1500;
  function clearTimers() { clearTimeout(timer); clearTimeout(hide); timer = hide = undefined; }
  function reset() {
    generation++; clearTimers(); readController?.abort(); writeController?.abort(); cancelController?.abort(); reading = undefined;
    progress.value = null; state.visible = state.unavailable = state.submitting = state.cancelling = false; failures = 0; tracking = false;
  }
  function announce(p: BackgroundProgress) {
    const label = kind === 'probe' ? '探测' : p.kind === 'scrape_avatars' ? '头像任务' : '刮削';
    toasts.show(`${label}${p.cancelled ? '已中止' : p.aborted ? '已因连续失败中止' : '完成'}：成功 ${p.success} / 跳过 ${p.skipped} / 失败 ${p.failed}${p.error ? ' · ' + p.error : ''}`, p.failed || (p.error && !p.cancelled) ? 'error' : 'ok');
    if (p.failures[0]) toasts.show(`失败/待确认示例：${p.failures[0]}`, 'error');
    completed();
  }
  function poll(): Promise<void> {
    if (reading) return reading;
    clearTimeout(timer); timer = undefined;
    const epoch = generation, controller = new AbortController(); readController = controller;
    const promise = backgroundApi.progress(kind, controller.signal).then(p => {
      if (epoch !== generation || controller.signal.aborted) return;
      progress.value = p; state.unavailable = false; failures = 0;
      if (p.running) { tracking = true; state.visible = true; clearTimeout(hide); hide = undefined; }
      else if (tracking && !state.submitting) {
        tracking = false; state.visible = true;
        if (!p.finished_at) { progress.value = {...p,error:'任务状态已重置，服务可能已重启；请检查已有结果后重新启动。'}; toasts.show(progress.value.error, 'error'); completed(); }
        else { announce(p); if (kind === 'probe' && !p.failed && !p.error) hide = setTimeout(() => { state.visible = false; hide = undefined; }, 8000); }
      }
    }).catch(() => {
      if (epoch !== generation || controller.signal.aborted) return;
      state.unavailable = true; failures = Math.min(failures + 1, 3);
    }).finally(() => {
      if (reading === promise) reading = undefined;
      if (epoch !== generation) return;
      if (state.busy || state.unavailable) timer = setTimeout(() => { timer = undefined; void poll(); }, Math.min(interval * 2 ** failures, 8000));
    });
    reading = promise; return promise;
  }
  async function run(payload: Record<string, unknown>, avatar = false) {
    if (state.busy || blocked()) return false;
    generation++; clearTimers(); readController?.abort(); reading = undefined;
    const epoch = generation, controller = new AbortController(); writeController = controller;
    state.submitting = state.visible = true; state.unavailable = false; progress.value = null; tracking = false;
    try {
      const result = await backgroundApi.start(kind, avatar, payload, controller.signal);
      if (epoch !== generation) return false;
      if (!result.total) { state.visible = false; toasts.show(result.message || '没有符合条件的条目'); }
      else { tracking = true; toasts.show(`${kind === 'probe' ? '探测' : avatar ? '头像任务' : '刮削'}已启动（${result.total} ${avatar ? '个演员' : '条'}）`); }
      return true;
    } catch (cause) {
      if (epoch === generation) { state.visible = false; toasts.show(cause instanceof Error ? cause.message : '启动失败', 'error'); }
      return false;
    } finally {
      if (epoch === generation) { state.submitting = false; writeController = undefined; await poll(); }
    }
  }
  async function cancel() {
    if (!state.busy || state.submitting || state.cancelling) return;
    const epoch = generation, controller = new AbortController(); cancelController = controller; state.cancelling = true;
    try { await backgroundApi.cancel(kind, controller.signal); if (epoch === generation) toasts.show('已请求中止'); }
    catch (cause) { if (epoch === generation) { toasts.show(cause instanceof Error ? cause.message : '中止失败', 'error'); if (cause instanceof ApiError && cause.status === 409) await poll(); } }
    finally { if (epoch === generation) { state.cancelling = false; cancelController = undefined; await poll(); } }
  }
  return Object.assign(state, { restore: poll, run, cancel, reset });
}
