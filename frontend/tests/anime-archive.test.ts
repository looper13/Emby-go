import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import ArchiveSync from '../src/components/anime/ArchiveSync.vue';
import { animeApi } from '../src/api/anime';
import { apiClient } from '../src/api/client';

type Archive = Awaited<ReturnType<typeof animeApi.archive>>;
const empty = (): Archive => ({
  snapshot: { name: '', digest: '', publishedAt: '', importedAt: '', subjects: 0, episodes: 0, characters: 0, persons: 0 },
  state: { status: '', error: '', updated_at: '', last_success_at: '' }, job: null,
});
const job = (changes: Partial<NonNullable<Archive['job']>> = {}): NonNullable<Archive['job']> => ({
  running: true, status: 'running', error: '', phase: 'download', bytes: 5 * 1024 * 1024, totalBytes: 10 * 1024 * 1024, rows: 0, upToDate: false, ...changes,
});
const ready = (): Archive => ({ ...empty(), snapshot: { name: 'dump-test.zip', digest: 'sha256:test', publishedAt: '2026-10-14T00:00:00Z', importedAt: '2026-10-14T01:00:00Z', subjects: 30000, episodes: 200000, characters: 1, persons: 1 } });
let wrapper: VueWrapper;
beforeEach(() => vi.useFakeTimers());
afterEach(() => { wrapper?.unmount(); vi.useRealTimers(); vi.restoreAllMocks(); });
async function mountSync() {
  wrapper = mount(ArchiveSync, { global: { stubs: { RouterLink: { template: '<a><slot /></a>' } } } });
  await flushPromises();
  return wrapper;
}
const button = (label: string) => wrapper.findAll('button').find(item => item.text() === label)!;

it('only starts the full download on click, then shows bytes, import stages and completion', async () => {
  let state = empty();
  vi.spyOn(animeApi, 'archive').mockImplementation(async () => structuredClone(state));
  const start = vi.spyOn(animeApi, 'importArchive').mockImplementation(async () => {
    state = { ...empty(), job: job() };
    return { task_id: 1 };
  });
  await mountSync();
  expect(start).not.toHaveBeenCalled();
  await button('下载全量目录').trigger('click');
  await flushPromises();
  expect(start).toHaveBeenCalledOnce();
  expect(wrapper.text()).toContain('下载全量归档');
  expect(wrapper.text()).toContain('5.0 MiB / 10.0 MiB · 50%');
  expect(wrapper.get('progress').attributes('value')).toBe(String(5 * 1024 * 1024));
  state.job = job({ phase: 'episode.jsonlines', rows: 10000, bytes: 0, totalBytes: 0 });
  await vi.advanceTimersByTimeAsync(1500);
  expect(wrapper.text()).toContain('导入章节');
  expect(wrapper.text()).toContain('已处理 10,000 条');
  expect(wrapper.get('progress').attributes('value')).toBeUndefined();
  state = { ...ready(), job: job({ running: false, status: 'success', phase: 'done' }) };
  await vi.advanceTimersByTimeAsync(1500);
  expect(wrapper.text()).toContain('目录更新完成');
  expect(wrapper.text()).toContain('30,000 部动画');
  expect(button('更新目录')).toBeDefined();
  expect(wrapper.emitted('updated')).toHaveLength(1);
});

it('keeps polling after the accepted start loses its first status response', async () => {
  const read = vi.spyOn(animeApi, 'archive').mockResolvedValueOnce(empty()).mockRejectedValueOnce(new Error('网络暂不可用')).mockResolvedValue({ ...empty(), job: job() });
  vi.spyOn(animeApi, 'importArchive').mockResolvedValue({ task_id: 1 });
  await mountSync();
  await button('下载全量目录').trigger('click'); await flushPromises();
  expect(wrapper.text()).toContain('读取任务状态失败');
  expect(button('取消任务')).toBeDefined();
  await vi.advanceTimersByTimeAsync(1500);
  expect(read).toHaveBeenCalledTimes(3);
  expect(wrapper.text()).toContain('下载全量归档');
  expect(wrapper.text()).not.toContain('读取任务状态失败');
});

it('blocks duplicate starts while the request is pending', async () => {
  vi.spyOn(animeApi, 'archive').mockResolvedValue(empty());
  let accept!: (value: unknown) => void;
  const start = vi.spyOn(animeApi, 'importArchive').mockImplementation(() => new Promise(resolve => { accept = resolve; }));
  await mountSync();
  await button('下载全量目录').trigger('click');
  expect(button('正在启动').attributes('disabled')).toBeDefined();
  await button('正在启动').trigger('click');
  expect(start).toHaveBeenCalledOnce();
  accept({ task_id: 1 }); await flushPromises();
});

it('resumes an existing task, waits for cancellation and offers retry', async () => {
  let state: Archive = { ...ready(), job: job() };
  vi.spyOn(animeApi, 'archive').mockImplementation(async () => structuredClone(state));
  const cancel = vi.spyOn(animeApi, 'cancelArchive').mockResolvedValue(undefined);
  await mountSync();
  await button('取消任务').trigger('click'); await flushPromises();
  expect(cancel).toHaveBeenCalledOnce();
  expect(button('正在取消').attributes('disabled')).toBeDefined();
  state = { ...ready(), job: job({ running: false, status: 'cancelled', error: '' }) };
  await vi.advanceTimersByTimeAsync(1500);
  expect(wrapper.text()).toContain('任务已取消');
  expect(button('重试下载 / 更新')).toBeDefined();
  expect(wrapper.find('[role="alert"]').exists()).toBe(false);
});

it('shows a failed update without hiding the old catalog and allows retry', async () => {
  let state: Archive = { ...ready(), job: job({ running: false, status: 'failed', error: 'archive_download_failed' }) };
  vi.spyOn(animeApi, 'archive').mockImplementation(async () => structuredClone(state));
  const start = vi.spyOn(animeApi, 'importArchive').mockImplementation(async () => { state = { ...ready(), job: job() }; return { task_id: 2 }; });
  await mountSync();
  expect(wrapper.text()).toContain('30,000 部动画');
  expect(wrapper.get('[role="alert"]').text()).toContain('归档下载失败');
  await button('重试下载 / 更新').trigger('click'); await flushPromises();
  expect(start).toHaveBeenCalledOnce();
  expect(wrapper.find('[role="alert"]').exists()).toBe(false);
});

it('reports no newer version and detects jobs started elsewhere while idle', async () => {
  let state: Archive = { ...ready(), job: job({ running: false, status: 'success', upToDate: true, phase: 'done' }) };
  vi.spyOn(animeApi, 'archive').mockImplementation(async () => structuredClone(state));
  await mountSync();
  expect(wrapper.text()).toContain('目录已是最新版本');
  state.job = job({ phase: 'manifest' });
  await vi.advanceTimersByTimeAsync(15000);
  expect(wrapper.text()).toContain('检查归档版本');
  expect(button('取消任务')).toBeDefined();
  wrapper.unmount();
  expect(vi.getTimerCount()).toBe(0);
});

it('treats an old persisted running state without a live job as interrupted', async () => {
  const state = ready(); state.state.status = 'running';
  vi.spyOn(animeApi, 'archive').mockResolvedValue(state);
  await mountSync();
  expect(wrapper.text()).toContain('上次任务已中断');
  expect(button('重试下载 / 更新')).toBeDefined();
});

it('does not enable a download until the initial status is known', async () => {
  const read = vi.spyOn(animeApi, 'archive').mockRejectedValueOnce(new Error('暂不可用')).mockResolvedValue(empty());
  const start = vi.spyOn(animeApi, 'importArchive');
  await mountSync();
  expect(button('下载全量目录').attributes('disabled')).toBeDefined();
  await button('重试状态查询').trigger('click'); await flushPromises();
  expect(read).toHaveBeenCalledTimes(2);
  expect(button('下载全量目录').attributes('disabled')).toBeUndefined();
  expect(start).not.toHaveBeenCalled();
});

it('preserves the server no-update result in the API contract', async () => {
  const value = ready();
  vi.spyOn(apiClient, 'request').mockResolvedValue({
    snapshot: { ...value.snapshot, imported_at: value.snapshot.importedAt, manifest: { name: value.snapshot.name, digest: value.snapshot.digest, created_at: value.snapshot.publishedAt } },
    state: value.state, job: { running: false, status: 'success', error: '', phase: 'done', up_to_date: true, bytes: 0, total_bytes: 0, rows: 0 },
  });
  expect((await animeApi.archive()).job?.upToDate).toBe(true);
});
