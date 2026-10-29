import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia, setActivePinia } from 'pinia';
import { cloud115Api, parseOfflineState, type OfflineState } from '../src/api/cloud115';
import R18OfflinePanel from '../src/components/media/R18OfflinePanel.vue';
import Cloud115Settings from '../src/components/settings/Cloud115Settings.vue';
import { useAuthStore } from '../src/stores/auth';
import { usePlayerStore } from '../src/stores/player';
const empty: OfflineState = { configured: true, tasks: [], sources: [] };
const task = { id: 'task', resource_id: '1', status: 'waiting_selection', progress: 100, stage: '', error: '', retryable: false, version: 2, file_id: '', files: [{ id: 'file', name: 'movie.mkv', path: 'result/movie.mkv', size: 1024 ** 3 }] };
const source = { Id: '115-task', Name: 'movie.mkv', Container: 'mkv', DirectStreamUrl: '/Videos/r18-2-id/stream?MediaSourceId=115-task&playback_token=ticket', Default: true };
let pinia: ReturnType<typeof createPinia>; let wrapper: VueWrapper | undefined;
beforeEach(() => { pinia = createPinia(); setActivePinia(pinia); vi.spyOn(cloud115Api, 'state').mockResolvedValue(empty); });
afterEach(() => { wrapper?.unmount(); wrapper = undefined; disposePinia(pinia); vi.restoreAllMocks(); vi.useRealTimers(); });
function panel(admin = true) {
  useAuthStore().user = { Policy: { IsAdministrator: admin } } as never;
  wrapper = mount(R18OfflinePanel, { props: { library: '2', video: 'id', title: 'Movie' }, global: { plugins: [pinia], stubs: { RouterLink: true } } }); return wrapper;
}
describe('115 workflow', () => {
  it('submits only on user action, binds a candidate, and opens the stable playback URL', async () => {
    vi.spyOn(cloud115Api, 'state').mockResolvedValue({ ...empty, tasks: [task] });
    const submit = vi.spyOn(cloud115Api, 'submit').mockResolvedValue({ ...empty, tasks: [task] });
    const select = vi.spyOn(cloud115Api, 'select').mockResolvedValue({ ...empty, tasks: [{ ...task, status: 'ready', file_id: 'file' }], sources: [source] });
    vi.spyOn(cloud115Api, 'prepare').mockResolvedValue({ ...empty, tasks: [{ ...task, status: 'ready', file_id: 'file' }], sources: [source] });
    const w = panel(); await flushPromises(); expect(submit).not.toHaveBeenCalled(); expect(w.text()).toContain('请选择正片文件');
    await w.findAll('button').find(b => b.text() === '设为默认播放源')!.trigger('click'); await flushPromises();
    expect(select).toHaveBeenCalledWith('2', 'id', 'task', 'file', expect.any(AbortSignal), true, false);
    await w.findAll('button').find(b => b.text() === '播放')!.trigger('click');
    await flushPromises();
    expect(usePlayerStore().current?.url).toBe(source.DirectStreamUrl); expect(usePlayerStore().current?.itemId).toBeUndefined();
  });
  it('preserves visible tasks on refresh failure and hides writes from ordinary users', async () => {
    const read = vi.spyOn(cloud115Api, 'state').mockResolvedValueOnce({ ...empty, sources: [source] }).mockRejectedValueOnce(new Error('state unavailable'));
    const w = panel(false); await flushPromises(); await w.findAll('button').find(b => b.text() === '刷新状态')!.trigger('click'); await flushPromises();
    expect(w.text()).toContain('state unavailable'); expect(w.text()).toContain('播放'); expect(w.find('#r18-manual-magnet').exists()).toBe(false); expect(read).toHaveBeenCalledTimes(2);
  });
  it('polls local status and cancels the page timer on unmount', async () => {
    vi.useFakeTimers(); const read = vi.spyOn(cloud115Api, 'state').mockResolvedValue({ ...empty, tasks: [{ ...task, status: 'downloading', files: [] }] });
    const w = panel(); await flushPromises(); await vi.advanceTimersByTimeAsync(5000); expect(read).toHaveBeenCalledTimes(2); w.unmount(); wrapper = undefined; await vi.advanceTimersByTimeAsync(15000); expect(read).toHaveBeenCalledTimes(2);
  });
  it('needs only a masked Cookie and clears it after saving without a folder input', async () => {
    vi.spyOn(cloud115Api, 'config').mockResolvedValue({ configured: false, account_id: '', folder_id: '', revision: 0 });
    const save = vi.spyOn(cloud115Api, 'save').mockResolvedValue(undefined);
    wrapper = mount(Cloud115Settings, { global: { plugins: [pinia] } }); await flushPromises();
    await wrapper.get('#cloud-cookie').setValue('UID=1; CID=2; SEID=3'); await wrapper.get('form').trigger('submit'); await flushPromises();
    expect(save).toHaveBeenCalledWith({ cookie: 'UID=1; CID=2; SEID=3', revision: 0 }, expect.any(AbortSignal)); expect((wrapper.get('#cloud-cookie').element as HTMLInputElement).value).toBe(''); expect(wrapper.get('#cloud-cookie').attributes('type')).toBe('password');
    expect(wrapper.find('#cloud-folder').exists()).toBe(false); expect(wrapper.find('#cloud-access').exists()).toBe(false); expect(wrapper.text()).toContain('缓存-24h自动删除');
  });
  it('rejects untrusted source addresses and unknown task states', () => {
    expect(() => parseOfflineState({ ...empty, sources: [{ ...source, DirectStreamUrl: 'https://evil.example' }] })).toThrow();
    expect(() => parseOfflineState({ ...empty, tasks: [{ ...task, status: 'unknown' }] })).toThrow();
  });
  it('shows expiry and cleanup status without playback, file selection or renewal actions', async () => {
    const pending = { ...task, status: 'cleanup_pending', stage: 'cleanup', expires_at: 1793240000, error: '缓存已到期，自动删除失败：请更新 Cookie', files: [] };
    vi.spyOn(cloud115Api, 'state').mockResolvedValue(parseOfflineState({ ...empty, tasks: [pending] }));
    const w = panel(); await flushPromises();
    expect(w.text()).toContain('缓存到期 / 正在自动删除'); expect(w.text()).toContain('自动删除时间'); expect(w.text()).toContain('请更新 Cookie');
    expect(w.findAll('button').some(b => ['播放', '取消本地等待', '重试当前阶段', '设为默认播放源'].includes(b.text()))).toBe(false);
    expect(parseOfflineState({ ...empty, tasks: [{ ...pending, status: 'expired' }] }).tasks[0]!.status).toBe('expired');
  });
});
