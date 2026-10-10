import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia';
import { apiClient } from '../src/api/client';
import { adminApi, parseKeys, parseProbes, parseSettings, parseTasks } from '../src/api/admin';
import { manualPayload } from '../src/lib/manual';
import LibrariesPage from '../src/pages/LibrariesPage.vue';
import ManualPage from '../src/pages/ManualPage.vue';
import ApiKeysPage from '../src/pages/ApiKeysPage.vue';
import ProbePage from '../src/pages/ProbePage.vue';
import SettingsPage from '../src/pages/SettingsPage.vue';
import { useToastStore } from '../src/stores/toasts';
import libraries from '../../vue-migration/fixtures/json/libraries.json';
import settings from '../../vue-migration/fixtures/json/settings.json';
import probes from '../../vue-migration/fixtures/json/probe.json';
import tasks from '../../vue-migration/fixtures/json/tasks.json';
let pinia: Pinia; const wrappers: VueWrapper[] = [];
function page(component: Parameters<typeof mount>[0]) { const w = mount(component, { global: { plugins: [pinia] } }); wrappers.push(w); return w; }
function deferred<T>() { let resolve!: (v: T) => void; const promise = new Promise<T>(r => { resolve = r; }); return { resolve, promise }; }
beforeEach(() => { pinia = createPinia(); setActivePinia(pinia); });
afterEach(() => { for (const w of wrappers.splice(0)) w.unmount(); disposePinia(pinia); vi.unstubAllGlobals(); });
describe('management contracts', () => {
  it('parses collected DTOs, permits nil slices, rejects empty or malformed responses', () => {
    expect(parseTasks(tasks).items[0]?.type).toBe('scan'); expect(parseProbes(probes).items[0]?.path).toBe('/no-such-path');
    expect(parseSettings(settings).cache).toBe('redis'); expect(parseKeys({ items: null, total: 0 }).items).toEqual([]);
    for (const parse of [parseTasks, parseProbes, parseKeys, parseSettings]) expect(() => parse(null)).toThrow();
    expect(() => parseKeys({ items: [{ Key: 'wrong' }], total: 1 })).toThrow();
  });
  it('encodes API keys in delete paths and uses 204 without a page cancellation signal', async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(null, { status: 204 })); vi.stubGlobal('fetch', fetcher);
    apiClient.setAuthHandlers({ getToken: () => 'fixture', onUnauthorized: () => {} });
    await adminApi.deleteKey('中文 /+&?');
    expect(fetcher.mock.calls[0]?.[0]).toBe('/api/admin/apikeys/%E4%B8%AD%E6%96%87%20%2F%2B%26%3F');
    expect(fetcher.mock.calls[0]?.[1].signal).toBeUndefined(); expect(fetcher.mock.calls[0]?.[1].method).toBe('DELETE');
  });
  it('builds exact manual fields, numbers and Chinese comma arrays and rejects non-http sources', () => {
    const body = manualPayload({ source_path: '/media/a.strm', source_url: 'https://example.com/a.mp4', title: '测试', year: '2024', genres: '剧情， 偶像, ', tags: 'a,b', studios: 'Studio' });
    expect(body.year).toBe(2024); expect(body.genres).toEqual(['剧情', '偶像']); expect(body.tags).toEqual(['a', 'b']); expect(body).not.toHaveProperty('library_id');
    expect(manualPayload({ ...body, year: '', genres: '', tags: '', studios: '' }).year).toBe(0);
    expect(() => manualPayload({ source_path: '/a', source_url: 'ftp://example.com/a', title: 'a' })).toThrow('http/https');
    expect(() => manualPayload({ source_path: '/a', source_url: 'https://example.com/a', title: 'a', year: 'not-a-number' })).toThrow('年份');
  });
});
describe('management interaction ownership', () => {
  it('retains failed library input, locks duplicate submission, resets only on success', async () => {
    vi.spyOn(adminApi, 'libraries').mockResolvedValue(libraries); const pending = deferred<(typeof libraries.items)[number]>();
    const create = vi.spyOn(adminApi, 'createLibrary').mockRejectedValueOnce(new Error('bad path')).mockReturnValueOnce(pending.promise);
    const w = page(LibrariesPage); await flushPromises(); await w.get('#lib-name').setValue('new'); await w.get('#lib-path').setValue('/media');
    await w.get('#library-form').trigger('submit'); await flushPromises(); expect(w.get<HTMLInputElement>('#lib-name').element.value).toBe('new');
    await w.get('#library-form').trigger('submit'); await w.get('#library-form').trigger('submit'); expect(create).toHaveBeenCalledTimes(2);
    pending.resolve(libraries.items[0]!); await flushPromises(); expect(w.get<HTMLInputElement>('#lib-name').element.value).toBe('');
  });
  it('respects library deletion cancellation and exact index/userdata warning', async () => {
    vi.spyOn(adminApi, 'libraries').mockResolvedValue(libraries); const remove = vi.spyOn(adminApi, 'deleteLibrary').mockResolvedValue(null);
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValue(true); const w = page(LibrariesPage); await flushPromises();
    await w.get('[data-lib-delete]').trigger('click'); expect(remove).not.toHaveBeenCalled();
    await w.get('[data-lib-delete]').trigger('click'); await flushPromises(); expect(remove).toHaveBeenCalledTimes(1);
    expect(confirm).toHaveBeenLastCalledWith('删除该媒体库及其影片索引？不会删除磁盘文件，但该库影片的播放进度/收藏会一并清除。');
  });
  it('lets a short create finish after leaving, but cancels its GET and never refreshes the dead page', async () => {
    const read = vi.spyOn(adminApi, 'libraries').mockResolvedValue(libraries); const pending = deferred<(typeof libraries.items)[number]>();
    vi.spyOn(adminApi, 'createLibrary').mockReturnValue(pending.promise); const w = page(LibrariesPage); await flushPromises();
    await w.get('#lib-name').setValue('new'); await w.get('#lib-path').setValue('/media'); await w.get('#library-form').trigger('submit');
    const signal = read.mock.calls[0]![0]!; w.unmount(); expect(signal.aborted).toBe(true);
    pending.resolve(libraries.items[0]!); await flushPromises(); expect(read).toHaveBeenCalledTimes(1); expect(useToastStore().items[0]?.message).toContain('已添加');
  });
  it('preserves manual draft on failure, sends numeric/array fields, prevents duplicates and resets on success', async () => {
    vi.spyOn(adminApi, 'libraries').mockResolvedValue(libraries); const pending = deferred<{ id: number; status: string }>();
    const manual = vi.spyOn(adminApi, 'manual').mockRejectedValueOnce(new Error('write failed')).mockReturnValueOnce(pending.promise);
    const w = page(ManualPage); await flushPromises();
    for (const [id, value] of Object.entries({ title: '测试', path: '/media/a.strm', url: 'https://example.com/a.mp4', year: '2024', genres: 'a，b' })) await w.get(`#m-${id}`).setValue(value);
    await w.get('#manual-form').trigger('submit'); await flushPromises(); expect(w.get<HTMLInputElement>('#m-title').element.value).toBe('测试');
    expect(manual.mock.calls[0]?.[0]).toMatchObject({ year: 2024, genres: ['a', 'b'] });
    await w.get('#manual-form').trigger('submit'); await w.get('#manual-form').trigger('submit'); expect(manual).toHaveBeenCalledTimes(2);
    pending.resolve({ id: 1, status: 'success' }); await flushPromises(); expect(w.get<HTMLInputElement>('#m-title').element.value).toBe('');
  });
  it('retains key input on create failure, displays clipboard failure, and requires deletion confirmation', async () => {
    const item = { key: 'fixture-secret', name: 'test', created_at: '' }; vi.spyOn(adminApi, 'keys').mockResolvedValue({ items: [item], total: 1 });
    const create = vi.spyOn(adminApi, 'createKey').mockRejectedValue(new Error('failed')); const remove = vi.spyOn(adminApi, 'deleteKey').mockResolvedValue(null);
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValue(true);
    const w = page(ApiKeysPage); await flushPromises(); await w.get('#key-name').setValue('draft'); await w.get('#apikey-form').trigger('submit'); await flushPromises();
    expect(create).toHaveBeenCalledWith('draft'); expect(w.get<HTMLInputElement>('#key-name').element.value).toBe('draft');
    await w.get('[data-copy]').trigger('click'); await flushPromises(); expect(useToastStore().items.some(t => t.message.includes('复制失败'))).toBe(true);
    await w.get('[data-key-delete]').trigger('click'); expect(remove).not.toHaveBeenCalled(); await w.get('[data-key-delete]').trigger('click'); await flushPromises();
    expect(confirm).toHaveBeenLastCalledWith('删除该 API 密钥？使用它的客户端将立即失效。'); expect(remove).toHaveBeenCalledWith('fixture-secret');
  });
  it('clears probes with 204 and rereads once; duplicate clicks stay locked', async () => {
    const read = vi.spyOn(adminApi, 'probes').mockResolvedValueOnce(probes).mockResolvedValue({ items: [] }); const pending = deferred<null>();
    const clear = vi.spyOn(adminApi, 'clearProbes').mockReturnValue(pending.promise); const w = page(ProbePage); await flushPromises();
    await w.get('#clear-probes').trigger('click'); await w.get('#clear-probes').trigger('click'); expect(clear).toHaveBeenCalledTimes(1);
    pending.resolve(null); await flushPromises(); expect(read).toHaveBeenCalledTimes(2); expect(w.text()).toContain('暂无探针记录');
  });
  it('keeps settings read-only and excludes authentication cache statistics', async () => {
    vi.spyOn(adminApi, 'settings').mockResolvedValue({ ...settings, cache_stats: { token: { hits: 99, misses: 0, shared: 9 }, apikey: { hits: 99, misses: 0, shared: 9 }, items: { hits: 3, misses: 1, shared: 2 } } });
    const w = page(SettingsPage); await flushPromises(); expect(w.text()).toContain('75.0% · 命中 3 / 未命中 1'); expect(w.text()).toContain('2 次');
    expect(w.find('form').exists()).toBe(false); expect(w.find('input').exists()).toBe(false); expect(w.find('button').exists()).toBe(false);
  });
});
