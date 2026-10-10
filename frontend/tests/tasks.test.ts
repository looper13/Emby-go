import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia';
import { flushPromises } from '@vue/test-utils';
import { taskApi, parseScanProgress } from '../src/api/tasks';
import { useTasksStore } from '../src/stores/tasks';
import { useToastStore } from '../src/stores/toasts';
import progress from '../../vue-migration/fixtures/json/scan-progress-running.json';
import ended from '../../vue-migration/fixtures/json/scan-progress.json';
import result from '../../vue-migration/fixtures/json/scan-result.json';
function deferred<T>() { let resolve!: (v: T) => void; const promise = new Promise<T>(r => { resolve = r; }); return { resolve, promise }; }
let pinia: Pinia;
beforeEach(() => { vi.useFakeTimers(); pinia = createPinia(); setActivePinia(pinia); });
afterEach(() => { disposePinia(pinia); vi.useRealTimers(); });
describe('application scan ownership', () => {
  it('accepts Go omitempty phase, cancellation and nil timestamps without inventing defaults for malformed counts', () => {
    expect(parseScanProgress({ ...progress, phase: undefined, cancelled: true }).phase).toBe('');
    expect(() => parseScanProgress({ ...progress, done: '99' })).toThrow();
    expect(() => parseScanProgress({ ...progress, running: 'false' })).toThrow();
  });
  it('does not show a previously finished scan when logging in', async () => {
    vi.spyOn(taskApi, 'progress').mockResolvedValue(ended);
    const tasks = useTasksStore(); await tasks.restore();
    expect(tasks.visible).toBe(false); expect(tasks.busy).toBe(false); expect(vi.getTimerCount()).toBe(0);
  });
  it('runs one long POST, survives page scopes, completes once, and renders final progress', async () => {
    const submit = deferred<typeof result>();
    const run = vi.spyOn(taskApi, 'run').mockReturnValue(submit.promise);
    vi.spyOn(taskApi, 'progress').mockResolvedValueOnce(progress).mockResolvedValue(ended);
    const tasks = useTasksStore(); const first = tasks.run('scan', 3);
    await flushPromises();
    expect(tasks.busy).toBe(true); expect(run.mock.calls[0]?.slice(0, 2)).toEqual(['scan', 3]);
    expect(await tasks.run('reindex')).toBe(false); expect(run).toHaveBeenCalledTimes(1);
    const signal = run.mock.calls[0]![2]!; expect(signal.aborted).toBe(false);
    submit.resolve(result); await first; expect(tasks.busy).toBe(false); expect(tasks.revision).toBe(1);
    expect(tasks.progress?.done).toBe(307); expect(tasks.visible).toBe(true);
    expect(useToastStore().items[0]?.message).toContain('增量扫描完成');
    await vi.advanceTimersByTimeAsync(4000); expect(tasks.visible).toBe(false);
  });
  it('keeps the last running progress after a network error, backs off and does not overlap reads', async () => {
    const pending = deferred<typeof progress>();
    const poll = vi.spyOn(taskApi, 'progress').mockResolvedValueOnce(progress).mockRejectedValueOnce(new TypeError('offline')).mockReturnValueOnce(pending.promise).mockResolvedValue(ended);
    const tasks = useTasksStore(); await tasks.restore(); expect(tasks.busy).toBe(true);
    await vi.advanceTimersByTimeAsync(600); expect(tasks.unavailable).toBe(true); expect(tasks.progress).toEqual(progress); expect(tasks.busy).toBe(true);
    await vi.advanceTimersByTimeAsync(1200); expect(poll).toHaveBeenCalledTimes(3);
    await vi.advanceTimersByTimeAsync(10_000); expect(poll).toHaveBeenCalledTimes(3);
    pending.resolve(progress); await flushPromises(); await vi.advanceTimersByTimeAsync(600);
    expect(tasks.busy).toBe(false); expect(tasks.revision).toBe(1); expect(tasks.unavailable).toBe(false);
  });
  it('recovers another running scan on 409 and retains the server error', async () => {
    vi.spyOn(taskApi, 'run').mockRejectedValue(new Error('扫描正在进行中'));
    const poll = vi.spyOn(taskApi, 'progress').mockResolvedValue(progress);
    const tasks = useTasksStore(); expect(await tasks.run()).toBe(false);
    expect(tasks.busy).toBe(true); expect(useToastStore().items[0]?.message).toBe('扫描正在进行中');
    expect(tasks.revision).toBe(0);
    poll.mockResolvedValue(ended); await vi.advanceTimersByTimeAsync(600);
    expect(tasks.busy).toBe(false); expect(tasks.revision).toBe(1);
  });
  it('preserves cancelled completion and resets POST, progress, late callbacks and timers on logout/disposal', async () => {
    const submit = deferred<typeof result>(); const read = deferred<typeof progress>();
    const run = vi.spyOn(taskApi, 'run').mockReturnValue(submit.promise); const poll = vi.spyOn(taskApi, 'progress').mockReturnValue(read.promise);
    const tasks = useTasksStore(); const job = tasks.run(); await flushPromises();
    tasks.reset(); expect(run.mock.calls[0]![2]!.aborted).toBe(true); expect(poll.mock.calls[0]![0]!.aborted).toBe(true);
    read.resolve(progress); submit.resolve(result); await job; await flushPromises();
    expect(tasks.progress).toBeNull(); expect(tasks.busy).toBe(false); expect(tasks.revision).toBe(0);
    expect(vi.getTimerCount()).toBe(0); expect(useToastStore().items).toEqual([]);
    poll.mockResolvedValue({ ...ended, cancelled: true, error: '扫描已取消' }); await tasks.restore();
    expect(tasks.progress?.cancelled).toBe(true); disposePinia(pinia); expect(vi.getTimerCount()).toBe(0);
  });
  it('uses reindex endpoint and exact completion counts', async () => {
    const run = vi.spyOn(taskApi, 'run').mockResolvedValue(result); vi.spyOn(taskApi, 'progress').mockResolvedValue(ended);
    const tasks = useTasksStore(); await tasks.run('reindex'); expect(run.mock.calls[0]?.[0]).toBe('reindex');
    expect(useToastStore().items[0]?.message).toBe(`重建完成：${result.success} 成功 / ${result.pending} 待补录 / ${result.incompatible} 不兼容`);
  });
});
