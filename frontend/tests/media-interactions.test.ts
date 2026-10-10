import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { createPinia, disposePinia, setActivePinia } from 'pinia';
import { createRouter, createMemoryHistory } from 'vue-router';
import { titleCarriesNumber, numberUnlessInTitle, entityIdOf } from '../src/lib/media';
import { managePlayer, playbackErrorMessage, loadPlayerFactory, type PlayerHandle, type PlayerFactory } from '../src/lib/player';
import * as playerLibrary from '../src/lib/player';
import PlayerDialog from '../src/components/media/PlayerDialog.vue';
import { parseDetail, parseWall, parseSimilar } from '../src/api/contracts';
import { catalogApi } from '../src/api/catalog'; import { mediaApi } from '../src/api/media'; import { apiClient } from '../src/api/client';
import { viewHistoryKey } from '../src/router/view-history';
import ItemDetailPage from '../src/pages/ItemDetailPage.vue';
import MediaCard from '../src/components/media/MediaCard.vue';
import MetadataRow from '../src/components/media/MetadataRow.vue';
import WallFilters from '../src/components/media/WallFilters.vue';
import ArtworkGallery from '../src/components/media/ArtworkGallery.vue';
import TrailerSection from '../src/components/media/TrailerSection.vue';
import { imageURL } from '../src/lib/image-url';
import ct1 from '../../vue-migration/fixtures/ct/ct-01-number.json'; import ct2 from '../../vue-migration/fixtures/ct/ct-02-entity-id.json';
import detail from '../../vue-migration/fixtures/json/item-detail.json'; import wall from '../../vue-migration/fixtures/json/items-wall.json'; import similar from '../../vue-migration/fixtures/json/similar.json';
const cleanups: (() => void)[] = [];
vi.mock('../src/vendor/artplayer.min.js', () => ({ default: class { url: string; constructor(options: { url: string }) { this.url = options.url; } } }));
afterEach(() => { cleanups.splice(0).reverse().forEach(fn => fn()); vi.useRealTimers(); });
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r; }); return { resolve, promise }; }
describe('shared contracts CT-01/02', () => {
  for (const c of ct1.cases) it(`CT-01 ${c.id}`, () => { expect(titleCarriesNumber(c.input.title, c.input.number)).toBe(c.expected_carries); expect(numberUnlessInTitle(c.input.title, c.input.number)).toBe(c.expected_carries ? '' : c.input.number); });
  for (const c of ct2.cases) it(`CT-02 ${c.id}`, () => expect(entityIdOf(c.input.kind, c.input.name)).toBe(c.expected));
  it('consumes real DTO sections and rejects malformed optional data', () => {
    expect(parseDetail(detail).files?.[0]?.streams[0]?.Type).toBe('Video'); expect(parseWall(wall).items.length).toBeGreaterThan(0); expect(parseSimilar(similar)[0]?.Id).toBe('3');
    expect(() => parseDetail({ ...detail, actors: [{ name: 'x', has_image: 'false' }] })).toThrow(); expect(() => parseWall({ ...wall, userdata: { '5': { played: false, favorite: false, position_ticks: '5' } } })).toThrow();
    expect(imageURL(5, 'Backdrop', 480, 'tag +/', 7)).toBe('/Items/5/Images/Backdrop/7?tag=tag+%2B%2F&maxWidth=480');
  });
});
describe('media controls', () => {
  it('separates metadata values and emits each entity without merging their labels', async () => {
    const w = mount(MetadataRow, { props: { label: '标签', value: ['Tag-A', 'Rare series'], entityKey: 'tag' } }); cleanups.push(() => w.unmount());
    expect(w.find('dd').text()).toBe('Tag-A Rare series'); await w.findAll('button')[1]!.trigger('click'); expect(w.emitted('entity')).toEqual([['tag', 'Rare series']]);
  });
  it('does not navigate for nested buttons and renders progress, played/favorite/multipart badges', async () => {
    const w = mount(MediaCard, { props: { movie: { ...detail.movie, AdditionalParts: ['cd2'] }, userdata: { played: true, favorite: true, position_ticks: detail.movie.RuntimeSeconds * 5000000 } } }); cleanups.push(() => w.unmount());
    await w.find('[data-reread]').trigger('keydown', { key: 'Enter' }); await w.find('[data-reread]').trigger('click'); await w.find('[data-quickplay]').trigger('click');
    expect(w.emitted('open')).toBeUndefined(); expect(w.emitted('reread')).toEqual([[5]]); expect(w.emitted('quickplay')).toEqual([[5]]);
    expect(w.find('.wall-progress i').attributes('style')).toContain('50%'); expect(w.text()).toContain('已看'); expect(w.text()).toContain('CD×2'); expect(w.text()).toContain('♥');
  });
  it('uses select for nine libraries, emits toggle scrape and entity clear, keeps draft search controlled', async () => {
    const w = mount(WallFilters, { props: { query: { genre: '剧情', scrape: 'failed' }, search: 'draft', libraries: Array.from({ length: 9 }, (_, i) => ({ Id: i + 1, Name: `L${i}`, Path: '' })) } }); cleanups.push(() => w.unmount());
    expect(w.find('#item-lib').exists()).toBe(true); expect(w.find('[data-lib]').exists()).toBe(false);
    await w.find('[data-scrape=failed]').trigger('click'); expect(w.emitted('filter')).toEqual([['scrape', 'failed', true]]);
    await w.find('#entity-clear').trigger('click'); expect(w.emitted('clearEntity')).toHaveLength(1); await w.find('input').setValue('new'); expect(w.emitted('search')).toEqual([['new']]);
  });
  it('keeps trailer and stills separate with versioned nonzero indices and original links', () => {
    const gallery = mount(ArtworkGallery, { props: { id: 5, images: [{ ImageType: 'Backdrop', ImageIndex: 7, ImageTag: 'new' }] } }); cleanups.push(() => gallery.unmount());
    expect(gallery.find('a').attributes('href')).toBe('/Items/5/Images/Backdrop/7?tag=new'); expect(gallery.find('img').attributes('src')).toBe('/Items/5/Images/Backdrop/7?tag=new&maxWidth=480');
    const trailer = mount(TrailerSection, { props: { url: '', thumb: '', fallback: '' } }); cleanups.push(() => trailer.unmount()); expect(trailer.find('section').exists()).toBe(false);
  });
});
describe('player lifetime', () => {
  it('loads the Vite default export without requiring a vendor global', async () => {
    const factory = await loadPlayerFactory(); const player = factory(document.createElement('div'), { title: 'module', url: '/default-export', type: 'mp4' }); expect(player.url).toBe('/default-export');
  });
  it('ignores a delayed vendor import after the dialog has unmounted', async () => {
    const incoming = deferred<PlayerFactory>(); vi.spyOn(playerLibrary, 'loadPlayerFactory').mockReturnValue(incoming.promise);
    const pinia = createPinia(); const wrapper = mount(PlayerDialog, { props: { options: { title: 'late', url: '/late', type: 'mp4' } }, global: { plugins: [pinia] } });
    const factory = vi.fn(); expect(document.body.classList.contains('player-open')).toBe(true); wrapper.unmount(); disposePinia(pinia); incoming.resolve(factory); await flushPromises(); expect(factory).not.toHaveBeenCalled(); expect(document.body.classList.contains('player-open')).toBe(false);
  });
  it('falls back once, mutes asynchronous playback and ignores callbacks after destroy', async () => {
    let callback!: () => void; const report = vi.fn(); const play = vi.fn().mockRejectedValue(new Error('gesture'));
    const player: PlayerHandle = { url: '/direct', muted: false, on: (_, fn) => { callback = fn; }, play, destroy: vi.fn(), video: { error: { code: 2 } } as HTMLVideoElement };
    const session = managePlayer(document.createElement('div'), { title: 'x', url: '/direct', type: 'mp4', proxyURL: '/proxy' }, () => player, report);
    callback(); await flushPromises(); expect(player.url).toBe('/proxy'); expect(player.muted).toBe(true); expect(play).toHaveBeenCalledTimes(1);
    callback(); callback(); expect(report).toHaveBeenCalledTimes(1); session.destroy(); callback(); expect(report).toHaveBeenCalledTimes(1); expect(play).toHaveBeenCalledTimes(1); expect(player.destroy).toHaveBeenCalledTimes(1);
  });
  it('reports trailer errors directly and classifies network/decode/format/mixed content', () => {
    for (const [code, word] of [[2, '网络'], [3, '解码'], [4, '格式']] as const) expect(playbackErrorMessage(code, '/x', 'http:')).toContain(word);
    expect(playbackErrorMessage(4, 'http://source/x', 'https:')).toContain('HTTPS'); expect(playbackErrorMessage(undefined, '/x')).toContain('播放失败');
    let callback!: () => void; const report = vi.fn(); const p = { url: '', muted: false, on: (_: string, fn: () => void) => { callback = fn; }, play: vi.fn(), destroy: vi.fn() };
    const session = managePlayer(document.createElement('div'), { title: 'trailer', url: '/t', type: 'mp4' }, () => p, report); callback(); callback(); expect(report).toHaveBeenCalledTimes(1); expect(p.play).not.toHaveBeenCalled(); session.destroy();
  });
});
async function detailPage() {
  const pinia = createPinia(); setActivePinia(pinia); const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/item/:id', name: 'item', component: ItemDetailPage }, { path: '/items', component: { template: '<p>wall</p>' } }] });
  const backFromItem = vi.fn();
  await router.push('/item/5?library_id=1'); const w = mount(ItemDetailPage, { global: { plugins: [pinia, router], provide: { [viewHistoryKey as symbol]: { target: () => null, backFromItem } } } });
  cleanups.push(() => { w.unmount(); disposePinia(pinia); }); await flushPromises(); return { w, router, backFromItem };
}
describe('detail reading and writes', () => {
  it('recommendations do not block detail; reread preserves expanded state, loaded recommendations and failed refresh callbacks', async () => {
    const incoming = deferred<ReturnType<typeof parseSimilar>>(); const recommend = vi.spyOn(mediaApi, 'similar').mockReturnValue(incoming.promise);
    const get = vi.spyOn(catalogApi, 'detail').mockResolvedValue(parseDetail({ ...detail, movie: { ...detail.movie, Plot: '剧情'.repeat(200) } }));
    vi.spyOn(mediaApi, 'reread').mockResolvedValue('success'); const { w } = await detailPage(); expect(w.find('.item-hero-title').exists()).toBe(true); expect(w.find('#item-similar').attributes('hidden')).toBeDefined();
    incoming.resolve(parseSimilar(similar)); await flushPromises(); await w.find('#plot-toggle').trigger('click'); (w.find('details').element as HTMLDetailsElement).open = true; await w.find('details').trigger('toggle');
    get.mockRejectedValueOnce(new Error('refresh failed')); await w.find('#detail-reread').trigger('click'); await flushPromises(); expect(w.find('.item-hero-title').exists()).toBe(true); expect(w.find('#plot-toggle').attributes('aria-expanded')).toBe('true');
    expect(w.find('#detail-reread').attributes('disabled')).toBeUndefined(); expect(w.findAll('[data-similar]').length).toBeGreaterThan(0); expect(recommend).toHaveBeenCalledTimes(1);
    expect(w.find('details').attributes('open')).toBeDefined();
    await w.find('#detail-reread').trigger('click'); await flushPromises(); expect(w.find('#plot-toggle').attributes('aria-expanded')).toBe('true'); expect(recommend).toHaveBeenCalledTimes(1);
  });
  it('Escape returns from detail unless the user is typing in a control', async () => {
    vi.spyOn(catalogApi, 'detail').mockResolvedValue(parseDetail(detail)); vi.spyOn(mediaApi, 'similar').mockResolvedValue([]);
    const { w, backFromItem } = await detailPage();
    const input = document.createElement('input'); w.element.append(input); cleanups.push(() => input.remove());
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); expect(backFromItem).not.toHaveBeenCalled();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })); expect(backFromItem).toHaveBeenCalledTimes(1);
  });
  it('recommends within a 5 second budget and hides the section when it times out', async () => {
    const request = vi.spyOn(apiClient, 'request').mockResolvedValue(similar);
    await mediaApi.similar('5', new AbortController().signal);
    expect(request.mock.calls[0]![1]).toMatchObject({ timeoutMs: 5000 });
    vi.spyOn(catalogApi, 'detail').mockResolvedValue(parseDetail(detail));
    vi.spyOn(mediaApi, 'similar').mockRejectedValue(new DOMException('请求超时', 'TimeoutError'));
    const { w } = await detailPage();
    expect(w.find('.item-hero-title').exists()).toBe(true);
    expect(w.find('#item-similar').attributes('hidden')).toBeDefined();
  });
  it('late short writes and recommendations cannot refresh or replace another detail', async () => {
    const late = deferred<ReturnType<typeof parseSimilar>>(); const write = deferred<string>();
    const get = vi.spyOn(catalogApi, 'detail').mockImplementation(async id => parseDetail({ ...detail, movie: { ...detail.movie, id: Number(id), Title: `movie-${id}` } }));
    vi.spyOn(mediaApi, 'similar').mockReturnValueOnce(late.promise).mockResolvedValue([]); vi.spyOn(mediaApi, 'reread').mockReturnValue(write.promise);
    const { w, router } = await detailPage(); await w.find('#detail-reread').trigger('click'); await router.push('/item/6'); await flushPromises(); write.resolve('success'); late.resolve(parseSimilar(similar)); await flushPromises();
    expect(w.find('.item-hero-title').text()).toBe('movie-6'); expect(get).toHaveBeenCalledTimes(2); expect(w.findAll('[data-similar]')).toHaveLength(0);
  });
});
