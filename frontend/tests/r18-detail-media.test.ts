import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia } from 'pinia';
import { reactive } from 'vue';
import { apiClient } from '../src/api/client';
import { cloud115Api } from '../src/api/cloud115';
import { parseR18Video, r18Api, safeR18PreviewURL, type R18Video } from '../src/api/r18';
import ArtworkGallery from '../src/components/media/ArtworkGallery.vue';
import ArtworkLightbox from '../src/components/media/ArtworkLightbox.vue';
import MetadataRow from '../src/components/media/MetadataRow.vue';
import TrailerSection from '../src/components/media/TrailerSection.vue';
import PlayerDialog from '../src/components/media/PlayerDialog.vue';
import * as playerLibrary from '../src/lib/player';
import R18DetailPage from '../src/pages/R18DetailPage.vue';
import { applyMediaVisibility } from '../src/composables/useMediaVisibility';
import { usePlayerStore } from '../src/stores/player';

const route = reactive({ name: 'r18-detail', fullPath: '/r18/2/item/fixture', params: { libraryId: '2', id: 'fixture' }, query: {} });
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push: vi.fn() }) }));
const urls = [0, 1, 2, 99].map(index => `/api/r18/libraries/2/videos/fixture/images/${index}`);
const video: R18Video = { id: 'fixture', content_id: 'fixture', service_code: 'javdb', number: 'TEST-001', title: '隔离媒体夹具', original_title: '', release_date: '', cover_url: '/api/r18/libraries/2/videos/fixture/images/poster', images: urls, duration: 90, score: 4, magnets_count: 0, has_local: true, tags: [], has_preview: false, preview_url: '' };
const wrappers: VueWrapper[] = [];
const stores: ReturnType<typeof createPinia>[] = [];
const containers: HTMLElement[] = [];

function attach() { const container = document.createElement('div'); document.body.append(container); containers.push(container); return container; }
function gallery(props: { urls?: string[]; id?: number; images?: { ImageType: string; ImageIndex: number; ImageTag: string }[] }) {
  const wrapper = mount(ArtworkGallery, { props, attachTo: attach() }); wrappers.push(wrapper); return wrapper;
}
function detail() {
  const pinia = createPinia(); stores.push(pinia);
  const wrapper = mount(R18DetailPage, { attachTo: attach(), global: { plugins: [pinia] } }); wrappers.push(wrapper); return { wrapper, pinia };
}
function expectNo115() { for (const method of ['state', 'submit', 'prepare', 'select', 'change'] as const) expect(cloud115Api[method]).not.toHaveBeenCalled(); }
function key(name: string, shiftKey = false) { window.dispatchEvent(new KeyboardEvent('keydown', { key: name, shiftKey, cancelable: true })); }
function unmount(wrapper: VueWrapper) { wrappers.splice(wrappers.indexOf(wrapper), 1); wrapper.unmount(); }

beforeEach(() => {
  applyMediaVisibility(false);
  route.name = 'r18-detail'; route.params = { libraryId: '2', id: 'fixture' }; route.fullPath = '/r18/2/item/fixture';
  vi.spyOn(apiClient, 'request').mockRejectedValue(new Error('不应发送未隔离的网络请求'));
  vi.spyOn(r18Api, 'detail').mockResolvedValue({ video, library_name: '隔离资料库', playable: true, local_sources: [{ id: 'strm-51', item_id: '51', name: '本地夹具.strm', library_name: '本地资料库', container: 'mp4' }] });
  vi.spyOn(r18Api, 'resources').mockResolvedValue({ items: [], next_offset: 0, has_more: false });
  vi.spyOn(r18Api, 'localPlayback').mockRejectedValue(new Error('查看剧照不应请求播放'));
  for (const method of ['state', 'submit', 'prepare', 'select', 'change'] as const) vi.spyOn(cloud115Api, method).mockResolvedValue({ configured: false, tasks: [], sources: [] });
});
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount());
  stores.splice(0).forEach(disposePinia);
  containers.splice(0).forEach(container => container.remove());
  applyMediaVisibility(false); document.body.style.overflow = '';
  vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.useRealTimers();
});

describe('JavDB 复用媒体库剧照组件', () => {
  for (const width of [1280, 390]) {
    it(`${width}px 使用共享缩略图和灯箱，不打开新标签页或触发影片播放`, async () => {
      vi.stubGlobal('innerWidth', width);
      const { wrapper, pinia } = detail(); await flushPromises();
      expect(wrapper.findComponent(ArtworkGallery).exists()).toBe(true);
      expect(wrapper.findAll('.detail-artwork-grid img').map(image => image.attributes('src'))).toEqual(urls);
      expect(wrapper.findAll('.detail-artwork-grid .artwork-thumb')).toHaveLength(urls.length);
      expect(wrapper.find('.r18-gallery').exists()).toBe(false);
      expect(wrapper.find('.detail-artwork-grid a').exists()).toBe(false);
      await wrapper.findAll('.artwork-thumb')[2]!.trigger('click');
      expect(wrapper.findComponent(ArtworkLightbox).exists()).toBe(true);
      expect(wrapper.get('[role="dialog"]').attributes('aria-modal')).toBe('true');
      expect(wrapper.get('.artwork-image').attributes('src')).toBe(urls[2]);
      expect(wrapper.findAll('.artwork-preload').map(image => image.attributes('src'))).toEqual([urls[3], urls[1]]);
      await wrapper.get('#artwork-next').trigger('click');
      expect(wrapper.get('.artwork-image').attributes('src')).toBe(urls[3]);
      key('Escape'); await flushPromises();
      expect(wrapper.find('#artwork-overlay').exists()).toBe(false);
      expect(document.activeElement).toBe(wrapper.findAll('.artwork-thumb')[2]!.element);
      expect(usePlayerStore(pinia).current).toBeNull(); expectNo115();
      expect(r18Api.localPlayback).not.toHaveBeenCalled(); expect(apiClient.request).not.toHaveBeenCalled();
    }, 60000);
  }

  it('媒体库 ImageDto 保留版本化缩略图与原图，并回到原始触发按钮', async () => {
    const wrapper = gallery({ id: 5, images: [3, 7].map(index => ({ ImageType: 'Backdrop', ImageIndex: index, ImageTag: `fixture-${index}` })) });
    expect(wrapper.findAll('.artwork-thumb img').map(image => image.attributes('src'))).toEqual(['/Items/5/Images/Backdrop/3?tag=fixture-3&maxWidth=480', '/Items/5/Images/Backdrop/7?tag=fixture-7&maxWidth=480']);
    await wrapper.findAll('.artwork-thumb')[0]!.trigger('click');
    await wrapper.get('#artwork-next').trigger('click');
    expect(wrapper.get('.artwork-image').attributes('src')).toBe('/Items/5/Images/Backdrop/7?tag=fixture-7');
    key('Escape'); await flushPromises();
    expect(document.activeElement).toBe(wrapper.findAll('.artwork-thumb')[0]!.element);
  }, 60000);

  it('键盘环绕、Tab焦点约束、Esc关闭并恢复页面原滚动状态', async () => {
    document.body.style.overflow = 'auto';
    const wrapper = gallery({ urls });
    const trigger = wrapper.findAll('.artwork-thumb')[0]!;
    await trigger.trigger('click');
    expect(document.body.style.overflow).toBe('hidden');
    expect(document.activeElement).toBe(wrapper.get('#artwork-close').element);
    key('ArrowLeft'); await flushPromises();
    expect(wrapper.get('.artwork-image').attributes('src')).toBe(urls[3]);
    key('ArrowRight'); await flushPromises();
    expect(wrapper.get('.artwork-image').attributes('src')).toBe(urls[0]);
    // jsdom 无布局，明确标记灯箱控件可见以验证真实焦点约束逻辑。
    for (const button of wrapper.findAll('.artwork-shell button')) vi.spyOn(button.element, 'getClientRects').mockReturnValue([{ width: 44, height: 44 } as DOMRect] as unknown as DOMRectList);
    key('Tab', true); await flushPromises();
    expect(document.activeElement).toBe(wrapper.get('#artwork-next').element);
    key('Tab'); await flushPromises();
    expect(document.activeElement).toBe(wrapper.get('#artwork-close').element);
    key('Escape'); await flushPromises();
    expect(wrapper.find('#artwork-overlay').exists()).toBe(false);
    expect(document.body.style.overflow).toBe('auto'); expect(document.activeElement).toBe(trigger.element);
  }, 60000);

  it('隐藏图片关闭当前灯箱并清除预取，再显示不会自动重开', async () => {
    const wrapper = gallery({ urls });
    await wrapper.findAll('.artwork-thumb')[1]!.trigger('click');
    expect(wrapper.findAll('.artwork-preload')).toHaveLength(2);
    applyMediaVisibility(true); await flushPromises();
    expect(wrapper.find('section').exists()).toBe(false); expect(wrapper.findAll('img')).toHaveLength(0);
    expect(document.body.style.overflow).toBe('');
    applyMediaVisibility(false); await flushPromises();
    expect(wrapper.findAll('.artwork-thumb')).toHaveLength(urls.length); expect(wrapper.find('#artwork-overlay').exists()).toBe(false);
    await wrapper.findAll('.artwork-thumb')[0]!.trigger('click');
    await wrapper.setProps({ urls: [urls[3]!] });
    expect(wrapper.find('#artwork-overlay').exists()).toBe(false); expect(document.body.style.overflow).toBe('');
    await wrapper.setProps({ urls: [] }); expect(wrapper.find('section').exists()).toBe(false);
  }, 60000);

  it('灯箱响应外部索引与空列表，卸载后移除键盘监听并恢复滚动', async () => {
    const wrapper = mount(ArtworkLightbox, { props: { urls, index: 99 }, attachTo: attach() }); wrappers.push(wrapper);
    expect(wrapper.get('.artwork-image').attributes('src')).toBe(urls[3]);
    await wrapper.setProps({ index: 1 }); expect(wrapper.get('.artwork-image').attributes('src')).toBe(urls[1]);
    await wrapper.setProps({ urls: [] });
    expect(wrapper.find('#artwork-overlay').exists()).toBe(false); expect(wrapper.emitted('close')).toEqual([[0]]);
    expect(document.body.style.overflow).toBe('');
    key('Escape'); await flushPromises(); expect(wrapper.emitted('close')).toHaveLength(1);
  }, 60000);

  it('父组件原地更新URL数组时也撤销旧灯箱状态', async () => {
    const mutable = reactive([...urls]);
    const wrapper = gallery({ urls: mutable });
    await wrapper.findAll('.artwork-thumb')[0]!.trigger('click');
    mutable.splice(0, 1); await flushPromises();
    expect(wrapper.find('#artwork-overlay').exists()).toBe(false); expect(document.body.style.overflow).toBe('');
    expect(wrapper.findAll('.artwork-thumb img').map(image => image.attributes('src'))).toEqual(urls.slice(1));
  }, 60000);

  it('卸载打开的图库时恢复滚动并移除灯箱键盘监听', async () => {
    document.body.style.overflow = 'auto';
    const wrapper = gallery({ urls });
    await wrapper.findAll('.artwork-thumb')[0]!.trigger('click');
    const remove = vi.spyOn(window, 'removeEventListener');
    unmount(wrapper);
    expect(document.body.style.overflow).toBe('auto');
    expect(remove).toHaveBeenCalledWith('keydown', expect.any(Function), true);
  }, 60000);

  it('路由更换影片时立即卸载旧剧照，不保留旧灯箱或触发115流程', async () => {
    const { wrapper } = detail(); await flushPromises();
    await wrapper.findAll('.artwork-thumb')[0]!.trigger('click');
    vi.spyOn(r18Api, 'detail').mockImplementationOnce(() => new Promise(() => {}));
    route.params = { libraryId: '3', id: 'next' }; route.fullPath = '/r18/3/item/next'; await flushPromises();
    expect(wrapper.findComponent(ArtworkGallery).exists()).toBe(false);
    expect(wrapper.find('#artwork-overlay').exists()).toBe(false); expect(document.body.style.overflow).toBe('');
    expectNo115(); expect(r18Api.localPlayback).not.toHaveBeenCalled();
  }, 60000);
});

describe('JavDB 影片标签与独立预览视频', () => {
  const preview = '/api/r18/libraries/2/videos/fixture/preview.m3u8';
  const mp4Preview = preview.replace('.m3u8', '.mp4');

  it('预览只接受严格第一方路径，拒绝远程、查询参数、片段和异常身份', () => {
    expect(safeR18PreviewURL(preview)).toBe(preview);
    expect(safeR18PreviewURL(mp4Preview)).toBe(mp4Preview);
    expect(safeR18PreviewURL('/api/r18/libraries/2/videos/' + 'a'.repeat(1024) + '/preview.m3u8')).not.toBe('');
    for (const raw of ['https://video.fixture.invalid/preview.m3u8', '//video.fixture.invalid/preview.m3u8', 'https://user:fixture-key@video.fixture.invalid/preview.m3u8', preview + '?token=fixture-key', preview + '#fragment', preview + '\n', ' ' + preview, preview.replace('/2/', '/0/'), preview.replace('/2/', '/02/'), preview.replace('/fixture/', '/../'), preview.replace('/fixture/', '/fixture%2Fother/'), preview.replace('.m3u8', '.webm'), '/api/r18/libraries/2/videos/' + 'a'.repeat(1025) + '/preview.m3u8']) expect(safeR18PreviewURL(raw)).toBe('');
  }, 60000);

  it('MP4元数据只保留合法第一方预览地址，仍拒绝查询参数、凭据和非法身份', () => {
    expect(parseR18Video({ ...video, has_preview: true, preview_url: mp4Preview })).toMatchObject({ has_preview: true, preview_url: mp4Preview });
    for (const raw of ['https://video.fixture.invalid/preview.mp4', 'https://user:fixture-key@video.fixture.invalid/preview.mp4', '//video.fixture.invalid/preview.mp4', mp4Preview + '?token=fixture-key', mp4Preview + '#fragment', mp4Preview + '\n', ' ' + mp4Preview, mp4Preview.replace('/2/', '/0/'), mp4Preview.replace('/2/', '/02/'), mp4Preview.replace('/fixture/', '/../'), mp4Preview.replace('/fixture/', '/fixture%2Fother/')]) {
      expect(safeR18PreviewURL(raw)).toBe('');
      expect(parseR18Video({ ...video, has_preview: true, preview_url: raw }).preview_url).toBe('');
    }
  }, 60000);

  it('严格读取影片标签与预览字段，不猜测上游对象或兼容缺失字段', () => {
    expect(parseR18Video({ ...video, tags: [' 标签一 ', '标签二', '标签一', ''], has_preview: true, preview_url: preview })).toMatchObject({ tags: ['标签一', '标签二'], has_preview: true, preview_url: preview });
    expect(parseR18Video(video)).toMatchObject({ tags: [], has_preview: false, preview_url: '' });
    for (const tags of [undefined, null, '标签', [1], [{ id: 'fixture', name: '标签' }]]) expect(() => parseR18Video({ ...video, tags })).toThrow();
    for (const has_preview of [undefined, null, 1, 'true']) expect(() => parseR18Video({ ...video, has_preview })).toThrow();
    for (const preview_url of [undefined, null, 1, { url: preview }]) expect(() => parseR18Video({ ...video, preview_url })).toThrow();
    expect(parseR18Video({ ...video, has_preview: true, preview_url: 'https://video.fixture.invalid/preview.m3u8' }).preview_url).toBe('');
  }, 60000);

  it('影片标签复用元数据行，与磁力资源标签分离并保持文本转义', async () => {
    vi.mocked(r18Api.detail).mockResolvedValue({ video: { ...video, tags: ['影片标签一', '影片标签二', '<script>fixture</script>'] }, library_name: '隔离资料库', playable: true, local_sources: [{ id: 'strm-51', item_id: '51', name: '本地夹具.strm', library_name: '本地资料库', container: 'mp4' }] });
    vi.mocked(r18Api.resources).mockResolvedValue({ items: [{ id: 'btih-' + 'a'.repeat(40), title: '磁力夹具', number: video.number, date: '', magnet: 'magnet:?xt=urn:btih:' + 'a'.repeat(40), website: '隔离来源', detail_url: '', size_bytes: 0, size_mb: 0, tags: ['磁力专属标签'], origins: ['online'] }], next_offset: 1, has_more: false });
    const { wrapper } = detail(); await flushPromises();
    expect(wrapper.findComponent(MetadataRow).exists()).toBe(true);
    const metadata = wrapper.get('.r18-film-metadata .detail-meta-row');
    expect(metadata.get('dt').text()).toBe('标签');
    expect(metadata.get('dd').text()).toBe('影片标签一 影片标签二 <script>fixture</script>');
    expect(metadata.text()).not.toContain('磁力专属标签');
    expect(wrapper.get('.r18-resource-meta').text()).toContain('磁力专属标签');
    expect(wrapper.find('script').exists()).toBe(false); expect(metadata.find('button').exists()).toBe(false);
    expectNo115();
  }, 60000);

  it('点击共享预告片控件才打开HLS预览，不携带正片ID或读取115与进度接口', async () => {
    vi.mocked(r18Api.detail).mockResolvedValue({ video: { ...video, has_preview: true, preview_url: preview }, library_name: '隔离资料库', playable: true, local_sources: [{ id: 'strm-51', item_id: '51', name: '本地夹具.strm', library_name: '本地资料库', container: 'mp4' }] });
    const { wrapper, pinia } = detail(); await flushPromises();
    const player = usePlayerStore(pinia);
    expect(wrapper.findComponent(TrailerSection).props('url')).toBe(preview);
    expect(wrapper.get('.r18-preview .trailer-thumb img').attributes('src')).toBe(urls[0]);
    expect(player.current).toBeNull(); expect(apiClient.request).not.toHaveBeenCalled(); expectNo115();
    const trigger = wrapper.get('#item-trailer'); (trigger.element as HTMLButtonElement).focus();
    await trigger.trigger('click'); await flushPromises();
    expect(player.current).toMatchObject({ title: '预览视频 · ' + video.title, url: preview, type: 'm3u8' });
    expect(player.current).not.toHaveProperty('itemId'); expect(player.current).not.toHaveProperty('sourceItemId'); expect(player.current).not.toHaveProperty('proxyURL');
    expect(r18Api.localPlayback).not.toHaveBeenCalled(); expectNo115(); expect(apiClient.request).not.toHaveBeenCalled();
    player.close(); await flushPromises(); expect(document.activeElement).toBe(trigger.element);
  }, 60000);

  it('隐藏图片仍允许显式预览播放，但没有缩略图或剧照预取', async () => {
    applyMediaVisibility(true);
    vi.mocked(r18Api.detail).mockResolvedValue({ video: { ...video, has_preview: true, preview_url: preview }, library_name: '隔离资料库', playable: true, local_sources: [{ id: 'strm-51', item_id: '51', name: '本地夹具.strm', library_name: '本地资料库', container: 'mp4' }] });
    const { wrapper, pinia } = detail(); await flushPromises();
    expect(wrapper.findAll('img')).toHaveLength(0); expect(wrapper.find('.detail-artwork-grid').exists()).toBe(false);
    await wrapper.get('#item-trailer').trigger('click'); await flushPromises();
    expect(usePlayerStore(pinia).current?.url).toBe(preview); expectNo115(); expect(apiClient.request).not.toHaveBeenCalled();
  }, 60000);

  it('真实共用播放器接收预览选项后也不会读PlaybackInfo或写进度与已看状态', async () => {
    vi.mocked(r18Api.detail).mockResolvedValue({ video: { ...video, has_preview: true, preview_url: preview }, library_name: '隔离资料库', playable: true, local_sources: [{ id: 'strm-51', item_id: '51', name: '本地夹具.strm', library_name: '本地资料库', container: 'mp4' }] });
    const { wrapper, pinia } = detail(); await flushPromises();
    await wrapper.get('#item-trailer').trigger('click'); await flushPromises();
    const callbacks = new Map<string, () => void>();
    const media = document.createElement('video'); media.currentTime = 42;
    const handle: playerLibrary.PlayerHandle = { url: preview, muted: false, video: media, on: (name, callback) => { callbacks.set(name, callback); }, play: vi.fn().mockResolvedValue(undefined), destroy: vi.fn() };
    const factory = vi.spyOn(playerLibrary, 'loadPlayerFactory').mockResolvedValue(() => handle);
    vi.useFakeTimers();
    const dialog = mount(PlayerDialog, { props: { options: usePlayerStore(pinia).current! }, global: { plugins: [pinia] } }); wrappers.push(dialog);
    await flushPromises(); expect(factory).toHaveBeenCalledExactlyOnceWith('m3u8');
    callbacks.get('video:playing')!(); callbacks.get('video:timeupdate')!(); callbacks.get('video:ended')!();
    await vi.advanceTimersByTimeAsync(20000); await flushPromises();
    unmount(dialog); await flushPromises();
    expect(handle.destroy).toHaveBeenCalledTimes(1); expect(apiClient.request).not.toHaveBeenCalled(); expectNo115();
  }, 60000);

  it('MP4预览点击后使用共用播放器，播放、结束与关闭都不触发正片或115副作用', async () => {
    vi.mocked(r18Api.detail).mockResolvedValue({ video: { ...video, has_preview: true, preview_url: mp4Preview }, library_name: '隔离资料库', playable: true, local_sources: [{ id: 'strm-51', item_id: '51', name: '本地夹具.strm', library_name: '本地资料库', container: 'mp4' }] });
    const { wrapper, pinia } = detail(); await flushPromises();
    const player = usePlayerStore(pinia);
    expect(wrapper.findComponent(TrailerSection).props('url')).toBe(mp4Preview);
    expect(player.current).toBeNull(); expectNo115(); expect(apiClient.request).not.toHaveBeenCalled();
    const trigger = wrapper.get('#item-trailer'); (trigger.element as HTMLButtonElement).focus();
    await trigger.trigger('click'); await flushPromises();
    expect(player.current).toEqual({ id: 1, title: '预览视频 · ' + video.title, url: mp4Preview, type: 'mp4' });
    const callbacks = new Map<string, () => void>();
    const media = document.createElement('video'); media.currentTime = 42;
    const handle: playerLibrary.PlayerHandle = { url: mp4Preview, muted: false, video: media, on: (name, callback) => { callbacks.set(name, callback); }, play: vi.fn().mockResolvedValue(undefined), destroy: vi.fn() };
    const factory = vi.spyOn(playerLibrary, 'loadPlayerFactory').mockResolvedValue(() => handle);
    vi.useFakeTimers();
    const dialog = mount(PlayerDialog, { props: { options: player.current! }, global: { plugins: [pinia] } }); wrappers.push(dialog);
    await flushPromises(); expect(factory).toHaveBeenCalledExactlyOnceWith('mp4');
    callbacks.get('video:playing')!(); callbacks.get('video:timeupdate')!(); callbacks.get('video:ended')!();
    await vi.advanceTimersByTimeAsync(20000); await flushPromises();
    unmount(dialog); player.close(); await flushPromises();
    expect(handle.destroy).toHaveBeenCalledTimes(1); expect(document.activeElement).toBe(trigger.element);
    expect(r18Api.localPlayback).not.toHaveBeenCalled(); expect(apiClient.request).not.toHaveBeenCalled(); expectNo115();
  }, 60000);

  it('无预览或不安全预览地址不显示入口，也不退回任意远程视频', async () => {
    for (const values of [{ has_preview: false, preview_url: preview }, { has_preview: true, preview_url: '' }, { has_preview: true, preview_url: 'https://video.fixture.invalid/preview.m3u8' }, { has_preview: true, preview_url: preview + '?token=fixture-key' }]) {
      vi.mocked(r18Api.detail).mockResolvedValue({ video: { ...video, ...values }, library_name: '隔离资料库', playable: true, local_sources: [{ id: 'strm-51', item_id: '51', name: '本地夹具.strm', library_name: '本地资料库', container: 'mp4' }] });
      const { wrapper, pinia } = detail(); await flushPromises();
      expect(wrapper.find('#item-trailer').exists()).toBe(false); expect(usePlayerStore(pinia).current).toBeNull();
    }
    expectNo115(); expect(apiClient.request).not.toHaveBeenCalled();
  }, 60000);
});
