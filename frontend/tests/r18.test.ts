import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia } from 'pinia';
import { reactive } from 'vue';
import { r18Api, parseR18Page, parseR18Resources, safeR18URL, type R18Video } from '../src/api/r18';
import { parseLibraries } from '../src/api/contracts';
import R18CatalogPage from '../src/pages/R18CatalogPage.vue';
import R18DetailPage from '../src/pages/R18DetailPage.vue';
import R18LibraryForm from '../src/components/libraries/R18LibraryForm.vue';
import { applyMediaVisibility } from '../src/composables/useMediaVisibility';
import { sanitizeReturnTo } from '../src/router/legacy-url';
import { viewHistoryKey } from '../src/router/view-history';

const route = reactive({ name: 'r18', fullPath: '/r18?library_id=2', query: { library_id: '2' } as Record<string, string>, params: {} as Record<string, string> });
const router = { push: vi.fn(), replace: vi.fn() };
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => router }));
const wrappers: VueWrapper[] = [], stores: ReturnType<typeof createPinia>[] = [];
const lib = { Id: 2, Name: 'R18', Path: '', SourceType: 'postgres_r18' as const, Revision: 1 };
const video: R18Video = { id: 'WyJ0ZXN0IiwiZGlnaXRhbCJd', content_id: 'test', service_code: 'digital', number: 'TEST-001', title: 'Catalog title', original_title: 'Original', release_date: '2026-01-01', cover_url: 'https://example.com/cover.jpg', images: [] };
const page = { items: [video], has_more: true, has_previous: false, next_cursor: video.id, previous_cursor: video.id, estimated_total: 1900000 };
function render(component: typeof R18CatalogPage | typeof R18DetailPage | typeof R18LibraryForm, props = {}, history?: object) {
  const pinia = createPinia(); stores.push(pinia);
  const w = mount(component, { props, global: { plugins: [pinia], provide: history ? { [viewHistoryKey as symbol]: history } : {}, stubs: { RouterLink: { template: '<a><slot /></a>' } } } }); wrappers.push(w); return w;
}
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); stores.splice(0).forEach(disposePinia); vi.restoreAllMocks(); router.push.mockReset(); router.replace.mockReset(); applyMediaVisibility(false); route.name = 'r18'; route.fullPath = '/r18?library_id=2'; route.query = { library_id: '2' }; route.params = {}; });

describe('R18 library presentation', () => {
  it('keeps external source types and safe URLs, without inventing playback', () => {
    expect(parseLibraries({ items: [lib], total: 1 }).items?.[0]?.SourceType).toBe('postgres_r18');
    expect(parseR18Page(page).items[0]?.id).toBe(video.id);
    expect(safeR18URL('javascript:alert(1)')).toBe('');
    expect(safeR18URL('https://user:secret@example.com/')).toBe('');
    expect(() => parseR18Resources({ items: [{ magnet: 'javascript:alert(1)' }], has_more: false, next_offset: 0 })).toThrow();
    expect(sanitizeReturnTo('/r18/2/item/' + video.id + '?from=%2Fr18%3Flibrary_id%3D2')).toContain('/r18/2/item/');
  });
  it('shows the catalog, preserves it on failed refresh, and uses cursor navigation', async () => {
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [lib], total: 1 });
    const reads = vi.spyOn(r18Api, 'videos').mockResolvedValue(page);
    const w = render(R18CatalogPage); await flushPromises();
    expect(w.findAll('.r18-card')).toHaveLength(1);
    expect(w.text()).toContain('全库约 1,900,000 条');
    const next = w.findAll('button').find(b => b.text().includes('下一页'))!; await next.trigger('click');
    expect(router.push).toHaveBeenCalledWith(expect.objectContaining({ query: expect.objectContaining({ cursor: video.id, direction: 'next' }) }));
    reads.mockRejectedValueOnce(new Error('Source unavailable'));
    await w.findAll('button').find(b => b.text() === '刷新')!.trigger('click'); await flushPromises();
    expect(w.findAll('.r18-card')).toHaveLength(1); expect(w.text()).toContain('Source unavailable');
  });
  it('loads article resources on detail entry and copies magnets without opening a player', async () => {
    route.name = 'r18-detail'; route.fullPath = '/r18/2/item/' + video.id; route.params = { libraryId: '2', id: video.id };
    vi.spyOn(r18Api, 'detail').mockResolvedValue({ video, library_name: 'R18', playable: false });
    const magnet = 'magnet:?xt=urn:btih:0123456789012345678901234567890123456789';
    const resources = vi.spyOn(r18Api, 'resources').mockResolvedValue({ items: [{ id: '7', title: 'Article resource', number: 'TEST-001', date: '', magnet, website: 'Source', detail_url: 'https://example.com/', size_bytes: 0, size_mb: 1024, tags: [] }], has_more: false, next_offset: 1 });
    const write = vi.fn().mockResolvedValue(undefined); Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: write } });
    const w = render(R18DetailPage); await flushPromises();
    expect(resources).toHaveBeenCalledWith('2', video.id, 0, expect.any(AbortSignal));
    expect(w.text()).toContain('Article resource'); expect(w.text()).toContain('TODO');
    await w.findAll('button').find(b => b.text() === '复制磁力')!.trigger('click');
    expect(write).toHaveBeenCalledWith(magnet);
    expect(w.findAll('a[href^="magnet:"]')).toHaveLength(0);
  });
  it('restores the catalog position after detail return and uses only one back navigation', async () => {
    const backTo = vi.fn();
    const history = { target: () => ({ scrollY: 1234, wallCount: 0, focusID: video.id }), backTo };
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [lib], total: 1 });
    vi.spyOn(r18Api, 'videos').mockResolvedValue(page);
    render(R18CatalogPage, {}, history); await flushPromises();
    expect(window.scrollTo).toHaveBeenCalledWith(0, 1234);
    route.name = 'r18-detail'; route.fullPath = '/r18/2/item/' + video.id; route.params = { libraryId: '2', id: video.id };
    route.query = { from: '/r18?library_id=2&order=desc' };
    vi.spyOn(r18Api, 'detail').mockResolvedValue({ video, library_name: 'R18', playable: false });
    vi.spyOn(r18Api, 'resources').mockResolvedValue({ items: [], has_more: false, next_offset: 0 });
    const w = render(R18DetailPage, {}, history); await flushPromises();
    await w.get('.r18-back').trigger('click');
    expect(backTo).toHaveBeenCalledExactlyOnceWith('/r18?library_id=2&order=desc');
    expect(router.push).not.toHaveBeenCalled();
  });
  it('keeps video detail visible when article fails and retries only the resources', async () => {
    route.name = 'r18-detail'; route.fullPath = '/r18/2/item/' + video.id; route.params = { libraryId: '2', id: video.id };
    const detail = vi.spyOn(r18Api, 'detail').mockResolvedValue({ video, library_name: 'R18', playable: false });
    const resources = vi.spyOn(r18Api, 'resources').mockRejectedValueOnce(new Error('Article unavailable'))
      .mockResolvedValue({ items: [], has_more: false, next_offset: 0 });
    const w = render(R18DetailPage); await flushPromises();
    expect(w.text()).toContain(video.title); expect(w.text()).toContain('Article unavailable');
    await w.get('.r18-resources').findAll('button').find(b => b.text().includes('重试'))!.trigger('click'); await flushPromises();
    expect(resources).toHaveBeenCalledTimes(2); expect(detail).toHaveBeenCalledTimes(1);
    expect(w.text()).toContain('暂无匹配的磁力资源');
  });
  it('retains the password on an empty connection edit and never receives it from GET', async () => {
    vi.spyOn(r18Api, 'connection').mockResolvedValue({ library: lib, connection: { name: 'R18', host: 'db.example', port: 5432, database: 'catalog', username: 'reader', ssl_mode: 'prefer', revision: 1 }, hasPassword: true });
    const save = vi.spyOn(r18Api, 'saveConnection').mockResolvedValue(lib);
    const w = render(R18LibraryForm, { libraryId: 2 }); await flushPromises();
    expect((w.get('input[type="password"]').element as HTMLInputElement).value).toBe('');
    await w.get('form').trigger('submit'); await flushPromises();
    expect(save).toHaveBeenCalledWith(expect.not.objectContaining({ password: expect.anything() }), 2);
    expect(w.emitted('saved')).toHaveLength(1);
  });
});
