import { computed, onScopeDispose, ref, shallowRef } from 'vue';
import { defineStore } from 'pinia';
import { taskApi, type ScanProgress } from '../api/tasks';
import { useToastStore } from './toasts';
import { useBackgroundTask } from '../composables/useBackgroundTask';
export const useTasksStore = defineStore('tasks', () => {
  const progress = shallowRef<ScanProgress | null>(null); const submitting = ref(false);
  const kind = ref<'scan' | 'reindex'>('scan'); const visible = ref(false); const unavailable = ref(false);
  const revision = ref(0); const busy = computed(() => submitting.value || Boolean(progress.value?.running));
  const toasts = useToastStore();
  const probe = useBackgroundTask('probe', () => busy.value || scrape.busy, () => { revision.value++; });
  const scrape = useBackgroundTask('scrape', () => busy.value || probe.busy, () => { revision.value++; });
  const mutationBusy = computed(() => busy.value || probe.busy || scrape.busy);
  let generation = 0; let failures = 0;
  let timer: ReturnType<typeof setTimeout> | undefined; let hide: ReturnType<typeof setTimeout> | undefined;
  let readController: AbortController | undefined; let submitController: AbortController | undefined;
  let inFlight: Promise<boolean> | undefined;
  let completionAnnounced = false;
  let observedRunning = false;
  function stopTimers() { clearTimeout(timer); clearTimeout(hide); timer = undefined; hide = undefined; }
  function reset() {
    probe.reset(); scrape.reset();
    generation += 1; stopTimers(); readController?.abort(); submitController?.abort(); inFlight = undefined;
    progress.value = null; submitting.value = false; visible.value = false; unavailable.value = false; failures = 0; completionAnnounced = false; observedRunning = false;
  }
  function schedule() {
    clearTimeout(timer);
    timer = setTimeout(() => { timer = undefined; void tick(); }, Math.min(4800, 600 * 2 ** failures));
  }
  function readProgress(): Promise<boolean> {
    if (inFlight) return inFlight;
    const currentGeneration = generation; const controller = new AbortController(); readController = controller;
    const promise = taskApi.progress(controller.signal).then(p => {
      if (generation !== currentGeneration || controller.signal.aborted) return false;
      observedRunning ||= p.running;
      progress.value = p; unavailable.value = false; failures = 0;
      visible.value = submitting.value || p.running || (visible.value && Boolean(p.finished_at));
      if (observedRunning && !p.running && !submitting.value && !completionAnnounced) { revision.value += 1; completionAnnounced = true; }
      return true;
    }).catch(() => {
      if (generation !== currentGeneration || controller.signal.aborted) return false;
      unavailable.value = true; failures = Math.min(3, failures + 1);
      return false;
    }).finally(() => { if (inFlight === promise) inFlight = undefined; });
    inFlight = promise; return promise;
  }
  async function tick() {
    clearTimeout(timer); timer = undefined;
    const currentGeneration = generation; const ok = await readProgress();
    if (currentGeneration !== generation) return;
    if (!ok || busy.value) schedule();
    else {
      clearTimeout(hide);
      if (visible.value) hide = setTimeout(() => { visible.value = false; hide = undefined; }, 4000);
    }
  }
  async function restore() { await tick(); }
  async function restoreAll() { await Promise.allSettled([restore(), probe.restore(), scrape.restore()]); }
  async function run(operation: 'scan' | 'reindex' = 'scan', libraryId?: number) {
    if (mutationBusy.value) return false;
    generation += 1; stopTimers(); readController?.abort(); inFlight = undefined;
    const currentGeneration = generation; const controller = new AbortController(); submitController = controller;
    completionAnnounced = false; observedRunning = false;
    let succeeded = false;
    submitting.value = true; visible.value = true; unavailable.value = false; progress.value = null; kind.value = operation;
    void tick();
    try {
      const r = await taskApi.run(operation, libraryId, controller.signal);
      if (generation !== currentGeneration) return false;
      succeeded = true;
      toasts.show(operation === 'reindex' ? `重建完成：${r.success} 成功 / ${r.pending} 待补录 / ${r.incompatible} 不兼容`
        : `增量扫描完成：新增 ${r.added} / 更新 ${r.updated} / 跳过 ${r.skipped} / 删除 ${r.deleted} / 失败 ${r.failed}`);
      return true;
    } catch (cause) {
      if (generation === currentGeneration) toasts.show(cause instanceof Error ? cause.message : '扫描失败', 'error');
      return false;
    } finally {
      if (generation === currentGeneration) {
        submitting.value = false; submitController = undefined;
        if (succeeded) { completionAnnounced = true; revision.value += 1; }
        await tick();
      }
    }
  }
  onScopeDispose(reset);
  return { progress, submitting, kind, visible, unavailable, revision, busy, mutationBusy, probe, scrape, restore, restoreAll, run, reset };
});
