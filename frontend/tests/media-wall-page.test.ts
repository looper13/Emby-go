import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia';
import { createMemoryHistory, createRouter } from 'vue-router';
import MediaWallPage from '../src/pages/MediaWallPage.vue';
import { catalogApi } from '../src/api/catalog';
import type { WallDto } from '../src/api/contracts';
import { useMediaWallStore } from '../src/stores/media-wall';
import { viewHistoryKey } from '../src/router/view-history';
import detail from './fixtures/json/item-detail.json';

let pinia: Pinia, wrapper: VueWrapper | undefined;
function batch(offset: number): WallDto {
  return { total: 350, image_tags: {}, items: Array.from({ length: 100 }, (_, i) => ({ ...detail.movie, id: offset + i + 1 })) };
}
beforeEach(() => {
  pinia = createPinia(); setActivePinia(pinia);
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ top: 100000 } as DOMRect);
  vi.spyOn(catalogApi, 'libraries').mockResolvedValue({ items: [], total: 0 });
});
afterEach(() => { wrapper?.unmount(); wrapper = undefined; disposePinia(pinia); vi.restoreAllMocks(); });

async function restoredWall() {
  const api = vi.spyOn(catalogApi, 'items').mockImplementation(async q => batch(Number(q.get('offset'))));
  const wall = useMediaWallStore(), signal = new AbortController().signal;
  wall.prepare(new URLSearchParams(), false);
  for (let i = 0; i < 3; i++) await wall.loadMore(signal);
  wall.loadedAt = Date.now() - 60_000;
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/items', name: 'items', component: MediaWallPage }] });
  await router.push('/items');
  function open() {
    wrapper = mount(MediaWallPage, { attachTo: document.body, global: {
      plugins: [pinia, router],
      provide: { [viewHistoryKey as symbol]: { target: () => ({ scrollY: 1234, wallCount: 300, focusID: '250' }) } },
      stubs: {
        WallFilters: true, BackgroundTaskProgress: true,
        MediaGrid: { props: ['items'], template: '<div><article v-for="item in items" :key="item.id" class="wall-card" :data-play="item.id" tabindex="0">{{ item.Title }}</article></div>' },
      },
    } });
  }
  return { api, wall, open };
}

it('changes sort direction through the URL, resets pagination, and retains it after detail navigation', async () => {
  const api = vi.spyOn(catalogApi, 'items').mockImplementation(async q => batch(Number(q.get('offset'))));
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/items', name: 'items', component: MediaWallPage },
    { path: '/item/:id', name: 'item', component: { template: '<p>detail</p>' } },
  ] });
  await router.push('/items?sort=title&library_id=1&tag=keep&search=Movie');
  vi.mocked(catalogApi.libraries).mockResolvedValue({ items: [{ Id: 1, Name: 'Library', Path: '' }], total: 1 });
  let restore = false;
  wrapper = mount(MediaWallPage, { global: {
    plugins: [pinia, router],
    provide: { [viewHistoryKey as symbol]: { target: () => restore ? { scrollY: 0, wallCount: 100, focusID: '1' } : null } },
    stubs: { BackgroundTaskProgress: true, MediaGrid: true },
  } });
  await flushPromises();
  const wall = useMediaWallStore();
  expect((wrapper.get('#item-order').element as HTMLSelectElement).value).toBe('asc');
  await wall.loadMore(new AbortController().signal);
  expect(wall.items).toHaveLength(200);

  await wrapper.get('#item-order').setValue('desc'); await flushPromises();
  expect(router.currentRoute.value.query).toEqual({ sort: 'title', order: 'desc', library_id: '1', tag: 'keep', search: 'Movie' });
  expect(api.mock.lastCall![0].get('order')).toBe('desc'); expect(api.mock.lastCall![0].get('offset')).toBe('0');
  expect(wall.items).toHaveLength(100);

  wrapper.getComponent({ name: 'MediaGrid' }).vm.$emit('open', 1); await flushPromises();
  expect(router.currentRoute.value.path).toBe('/item/1'); expect(router.currentRoute.value.query.order).toBe('desc');
  const calls = api.mock.calls.length;
  restore = true; router.back(); await flushPromises();
  expect(router.currentRoute.value.path).toBe('/items');
  expect((wrapper.get('#item-order').element as HTMLSelectElement).value).toBe('desc');
  expect(api).toHaveBeenCalledTimes(calls);

  restore = false;
  await wrapper.get('#item-order').setValue('asc'); await flushPromises();
  expect(router.currentRoute.value.query.order).toBe('asc'); expect(api.mock.lastCall![0].get('order')).toBe('asc');
  await wrapper.get('#item-sort').setValue('year'); await flushPromises();
  expect(router.currentRoute.value.query.order).toBeUndefined();
  expect((wrapper.get('#item-order').element as HTMLSelectElement).value).toBe('desc');
  expect(api.mock.lastCall![0].get('order')).toBe('desc');
  await wrapper.get('#item-sort').setValue('random'); await flushPromises();
  expect(wrapper.find('#item-order').exists()).toBe(false); expect(wrapper.find('#wall-shuffle').exists()).toBe(true);
  await wrapper.get('#item-sort').setValue('title'); await flushPromises();
  expect((wrapper.get('#item-order').element as HTMLSelectElement).value).toBe('asc');
  expect(api.mock.lastCall![0].get('order')).toBe('asc');
});

it('restores count, focus and position before the background page request completes', async () => {
  const { api, open } = await restoredWall();
  let finish!: (value: WallDto) => void;
  api.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  open(); await flushPromises();
  expect(wrapper!.get('#wall-count').text()).toBe('共 350 条 · 已加载 300');
  expect(wrapper!.findAll('.wall-card')).toHaveLength(300);
  expect((document.activeElement as HTMLElement).dataset.play).toBe('250');
  expect(window.scrollTo).toHaveBeenCalledWith(0, 1234);
  expect(wrapper!.get('[role="status"]').text()).toBe('更新中…');
  finish(batch(0)); await flushPromises();
  expect(wrapper!.get('#wall-count').text()).toBe('共 350 条 · 已加载 300');
  expect(api).toHaveBeenCalledTimes(6);
});

it('keeps cached cards on background failure and retries refresh rather than appending pages', async () => {
  const { api, open } = await restoredWall();
  api.mockRejectedValueOnce(new Error('offline'));
  open(); await flushPromises();
  expect(wrapper!.get('[role="alert"]').text()).toContain('更新失败');
  expect(wrapper!.get('#wall-count').text()).toBe('共 350 条 · 已加载 300');
  expect(api).toHaveBeenCalledTimes(4);
  await wrapper!.get('[data-wall-retry]').trigger('click'); await flushPromises();
  expect(api.mock.calls.slice(4).map(([query]) => query.get('offset'))).toEqual(['0', '100', '200']);
  expect(wrapper!.find('[role="alert"]').exists()).toBe(false);
  expect(wrapper!.findAll('.wall-card')).toHaveLength(300);
});
