import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia } from 'pinia';
import { reactive } from 'vue';
import { apiClient } from '../src/api/client';
import { cloud115Api, type OfflineState } from '../src/api/cloud115';
import { parseR18LocalSource, parseR18Resources, r18Api, type R18LocalSource, type R18Video } from '../src/api/r18';
import R18DetailPage from '../src/pages/R18DetailPage.vue';
import { usePlayerStore } from '../src/stores/player';

const route = reactive({ name: 'r18-detail', fullPath: '/r18/2/item/video', params: { libraryId: '2', id: 'video' }, query: {} });
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push: vi.fn() }) }));
const video: R18Video = { id: 'video', content_id: 'fixture', service_code: 'javdb', number: 'TEST-001', title: '隔离测试影片', original_title: '', release_date: '', cover_url: '', images: [], duration: 90, score: 0, magnets_count: 1, has_local: true, tags: [], has_preview: false, preview_url: '' };
const sources: R18LocalSource[] = [
  { id: 'strm-51', item_id: '51', name: '已有影片-高清.strm', library_name: '本地资料库', container: 'mp4' },
  { id: 'strm-52', item_id: '52', name: '已有影片-另一版.strm', library_name: '另一资料库', container: 'mkv' },
  { id: 'strm-51-p2', item_id: 'part-51-2', name: '已有影片-CD2.strm', library_name: '本地资料库', container: 'm3u8' },
];
const resource = { id: 'btih-' + 'a'.repeat(40), title: '资料库与在线同一磁力', number: 'TEST-001', date: '', magnet: 'magnet:?xt=urn:btih:' + 'a'.repeat(40), website: 'JavDB', detail_url: '', size_bytes: 0, size_mb: 0, tags: [], origins: ['online' as const, 'library' as const] };
const empty: OfflineState = { configured: false, tasks: [], sources: [] };
let wrapper: VueWrapper | undefined, pinia: ReturnType<typeof createPinia>;
function render() { wrapper = mount(R18DetailPage, { global: { plugins: [pinia], stubs: { RouterLink: true } } }); return wrapper; }
function nextVideo(id: string) { route.params = { libraryId: '3', id }; route.fullPath = `/r18/3/item/${id}`; }
function expectNo115() { for (const method of ['state', 'submit', 'prepare', 'select', 'change'] as const) expect(cloud115Api[method]).not.toHaveBeenCalled(); }
beforeEach(() => {
  pinia = createPinia(); route.name = 'r18-detail'; route.params = { libraryId: '2', id: 'video' }; route.fullPath = '/r18/2/item/video';
  vi.spyOn(r18Api, 'detail').mockResolvedValue({ video, library_name: '在线资料库', playable: true, local_sources: sources });
  vi.spyOn(r18Api, 'resources').mockResolvedValue({ items: [resource], has_more: false, next_offset: 1 });
  vi.spyOn(r18Api, 'localPlayback').mockImplementation(async (_library, _video, sourceId) => ({ source: sources.find(source => source.id === sourceId)! }));
  vi.spyOn(cloud115Api, 'state').mockResolvedValue(empty);
  for (const method of ['submit', 'prepare', 'select', 'change'] as const) vi.spyOn(cloud115Api, method).mockResolvedValue(empty);
});
afterEach(() => { wrapper?.unmount(); wrapper = undefined; disposePinia(pinia); vi.restoreAllMocks(); vi.useRealTimers(); });

describe('JavDB 本地 STRM 优先播放', () => {
  it('本地版本无需115配置，浏览与播放不读取任何115接口', async () => {
    vi.useFakeTimers(); const w = render(); await flushPromises();
    expect(w.get('#detail-play').text()).toBe('播放本地 STRM'); expect(w.get('#detail-play').attributes('disabled')).toBeUndefined();
    expect(w.find('.offline-panel').exists()).toBe(false); expect(w.find('#r18-play-version').exists()).toBe(false);
    expect(w.get('.r18-resource-meta').text()).toContain('JavDB 在线磁力'); expect(w.get('.r18-resource-meta').text()).toContain('AVdb 本地资源库磁力');
    expect(w.get('.r18-resources').text()).toContain('AVdb 本地资源库磁力不代表本服务已有 STRM');
    await w.get('#detail-play').trigger('click'); await flushPromises();
    expect(r18Api.localPlayback).toHaveBeenCalledWith('2', 'video', 'strm-51', expect.any(AbortSignal));
    expect(usePlayerStore(pinia).current).toMatchObject({ title: sources[0]!.name, url: '/Videos/51/stream', proxyURL: '/Videos/51/proxy', type: 'mp4', itemId: '51', sourceItemId: '51' });
    await vi.advanceTimersByTimeAsync(15000); expectNo115();
  });
  it('多个本地版本与分段选择使用选中版本的标准影片ID', async () => {
    const w = render(); await flushPromises(); expect(w.findAll('#r18-local-version option')).toHaveLength(3);
    for (const source of sources.slice(1)) {
      await w.get('#r18-local-version').setValue(source.id); await w.get('#detail-play').trigger('click'); await flushPromises();
      expect(r18Api.localPlayback).toHaveBeenLastCalledWith('2', 'video', source.id, expect.any(AbortSignal));
      expect(usePlayerStore(pinia).current).toMatchObject({ url: `/Videos/${source.item_id}/stream`, proxyURL: `/Videos/${source.item_id}/proxy`, itemId: source.item_id, sourceItemId: source.item_id });
    }
    expect(usePlayerStore(pinia).current?.type).toBe('m3u8'); expectNo115();
  });
  it('本地复查失败明确提示，不自动转入115或打开播放器', async () => {
    vi.spyOn(r18Api, 'localPlayback').mockRejectedValueOnce(new Error('STRM 已移动或没有访问权限'));
    const w = render(); await flushPromises(); await w.get('#detail-play').trigger('click'); await flushPromises();
    expect(w.get('[role="alert"]').text()).toContain('STRM 已移动或没有访问权限'); expect(w.text()).toContain('不会自动转入磁力下载');
    expect(usePlayerStore(pinia).current).toBeNull(); expectNo115();
  });
  it('只在用户手动选择磁力流程后启用115，返回本地时停止轮询', async () => {
    vi.useFakeTimers(); const w = render(); await flushPromises(); expectNo115();
    await w.get('#r18-use-magnets').trigger('click'); await flushPromises();
    expect(cloud115Api.state).toHaveBeenCalledTimes(1); expect(w.find('.offline-panel').exists()).toBe(true);
    await w.get('#r18-use-local').trigger('click'); await flushPromises();
    expect(w.get('#detail-play').text()).toBe('播放本地 STRM'); expect(w.find('.offline-panel').exists()).toBe(false);
    await vi.advanceTimersByTimeAsync(15000); expect(cloud115Api.state).toHaveBeenCalledTimes(1);
    expect(cloud115Api.submit).not.toHaveBeenCalled();
  });
  it('详情尚未解析时不读取115，无本地版本解析完成后沿用115流程', async () => {
    let finish!: (value: Awaited<ReturnType<typeof r18Api.detail>>) => void;
    vi.spyOn(r18Api, 'detail').mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
    const w = render(); await flushPromises(); expectNo115();
    finish({ video: { ...video, has_local: false }, library_name: '在线库', playable: false, local_sources: [] }); await flushPromises();
    expect(cloud115Api.state).toHaveBeenCalledTimes(1); expect(w.find('.offline-panel').exists()).toBe(true); expect(w.get('#detail-play').text()).toBe('播放');
  });
  it('切换影片同步撤销本地复查，迟到结果不能打开旧版本', async () => {
    let finishPlayback!: (value: { source: R18LocalSource }) => void;
    const preflight = vi.spyOn(r18Api, 'localPlayback').mockImplementationOnce(() => new Promise(resolve => { finishPlayback = resolve; }));
    const w = render(); await flushPromises(); await w.get('#detail-play').trigger('click');
    const signal = preflight.mock.lastCall![3]!;
    let finishDetail!: (value: Awaited<ReturnType<typeof r18Api.detail>>) => void;
    vi.spyOn(r18Api, 'detail').mockImplementationOnce(() => new Promise(resolve => { finishDetail = resolve; }));
    nextVideo('next'); expect(signal.aborted).toBe(true); await flushPromises();
    expect(w.find('#r18-local-version').exists()).toBe(false); expectNo115();
    finishPlayback({ source: sources[0]! }); await flushPromises(); expect(usePlayerStore(pinia).current).toBeNull();
    finishDetail({ video: { ...video, id: 'next' }, library_name: '新在线库', playable: true, local_sources: [sources[1]!] }); await flushPromises();
    expect((w.get('#r18-local-version').element as HTMLSelectElement).value).toBe('strm-52'); expectNo115();
  });
  it('从无本地影片切换时先中断旧115请求，未解析的新影片不发115请求', async () => {
    vi.spyOn(r18Api, 'detail').mockResolvedValueOnce({ video: { ...video, has_local: false }, library_name: '在线库', playable: false, local_sources: [] });
    let finishState!: (value: OfflineState) => void;
    const state = vi.spyOn(cloud115Api, 'state').mockImplementationOnce(() => new Promise(resolve => { finishState = resolve; }));
    const w = render(); await flushPromises(); const signal = state.mock.lastCall![2]!;
    let finishDetail!: (value: Awaited<ReturnType<typeof r18Api.detail>>) => void;
    vi.spyOn(r18Api, 'detail').mockImplementationOnce(() => new Promise(resolve => { finishDetail = resolve; }));
    nextVideo('local-next'); expect(signal.aborted).toBe(true); await flushPromises(); expect(state).toHaveBeenCalledTimes(1);
    finishState({ configured: true, tasks: [], sources: [] }); await flushPromises();
    finishDetail({ video: { ...video, id: 'local-next' }, library_name: '新在线库', playable: true, local_sources: sources }); await flushPromises();
    expect(w.get('#detail-play').text()).toBe('播放本地 STRM'); expect(state).toHaveBeenCalledTimes(1); expect(w.find('.offline-panel').exists()).toBe(false);
  });
  it('复查返回其它版本时拒绝播放，也不自动使用磁力', async () => {
    vi.spyOn(r18Api, 'localPlayback').mockResolvedValueOnce({ source: sources[1]! });
    const w = render(); await flushPromises(); await w.get('#detail-play').trigger('click'); await flushPromises();
    expect(w.text()).toContain('本地播放版本已变化'); expect(usePlayerStore(pinia).current).toBeNull(); expectNo115();
  });
});

describe('JavDB 本地来源与多来源资源契约', () => {
  it('验证本地影片与分段ID，不接收原始远程地址', () => {
    for (const source of sources) expect(parseR18LocalSource(source)).toEqual(source);
    for (const itemId of ['0', '-1', 'r18-2-video', 'part-51-1', 'https://media.fixture.invalid/film.mp4', '../51']) expect(() => parseR18LocalSource({ ...sources[0], item_id: itemId })).toThrow();
    expect(() => parseR18LocalSource({ ...sources[0], id: 'strm-51-p1' })).toThrow();
    expect(() => parseR18LocalSource({ ...sources[0], item_id: '52' })).toThrow();
  });
  it('保留同一磁力的多来源标记，拒绝未知或缺失来源', () => {
    const page = { items: [resource], has_more: false, next_offset: 1 };
    expect(parseR18Resources(page).items[0]?.origins).toEqual(['online', 'library']);
    for (const origins of [undefined, [], ['strm'], 'library']) expect(() => parseR18Resources({ ...page, items: [{ ...resource, origins }] })).toThrow();
  });
  it('本地复查使用内部GET接口与长超时，不返回或请求原始STRM地址', async () => {
    vi.mocked(r18Api.localPlayback).mockRestore();
    const request = vi.spyOn(apiClient, 'request').mockResolvedValue({ source: sources[2] });
    expect(await r18Api.localPlayback('2', 'video', 'strm-51-p2')).toEqual({ source: sources[2] });
    expect(request).toHaveBeenCalledWith('/api/r18/libraries/2/videos/video/local-playback?source_id=strm-51-p2', expect.objectContaining({ auth: 'required', timeoutMs: 22000 }));
    vi.mocked(r18Api.detail).mockRestore(); request.mockResolvedValue({ video, library_name: '在线库', playable: true });
    await expect(r18Api.detail('2', 'video')).rejects.toThrow();
  });
});
