import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia';
import { createMemoryHistory, createRouter } from 'vue-router';
import MediaWallPage from '../src/pages/MediaWallPage.vue';
import LibrariesPage from '../src/pages/LibrariesPage.vue';
import ItemDetailPage from '../src/pages/ItemDetailPage.vue';
import { catalogApi } from '../src/api/catalog';
import { adminApi } from '../src/api/admin';
import { mediaApi } from '../src/api/media';
import { useMediaWallStore } from '../src/stores/media-wall';
import { usePlayerStore } from '../src/stores/player';
import { useAuthStore } from '../src/stores/auth';
import { parseDetail, parseUser } from '../src/api/contracts';
import { viewHistoryKey } from '../src/router/view-history';
import detailFixture from './fixtures/json/item-detail.json';
import authFixture from './fixtures/json/auth-login.json';

let pinia: Pinia;
let wrapper: VueWrapper | undefined;
const cleanups: (() => void)[] = [];

beforeEach(() => {
  pinia = createPinia();
  setActivePinia(pinia);
  cleanups.splice(0).forEach(fn => fn());
});

afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  cleanups.splice(0).forEach(fn => fn());
  disposePinia(pinia);
  vi.restoreAllMocks();
});

describe('媒体库与媒体墙随机播放功能 (Shuffle Play)', () => {
  it('媒体墙提供随机播放按钮，点击后随机拉取一部影片并调起播放器', async () => {
    const randomMovie = { ...detailFixture.movie, id: 999, Title: 'Random Pick Movie' };
    vi.spyOn(catalogApi, 'libraries').mockResolvedValue({ items: [{ Id: 1, Name: '电影库', Path: '/movies' }], total: 1 });
    vi.spyOn(catalogApi, 'items').mockImplementation(async (q) => {
      if (q.get('sort') === 'random') {
        return { total: 100, items: [randomMovie], image_tags: {} };
      }
      return { total: 10, items: [{ ...detailFixture.movie, id: 1 }], image_tags: {} };
    });

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/items', name: 'items', component: MediaWallPage }],
    });
    await router.push('/items?library_id=1');

    wrapper = mount(MediaWallPage, {
      global: {
        plugins: [pinia, router],
        provide: { [viewHistoryKey as symbol]: { target: () => null } },
        stubs: { BackgroundTaskProgress: true, WallFilters: true, MediaGrid: true },
      },
    });
    await flushPromises();

    const randomPlayBtn = wrapper.find('#wall-random-play');
    expect(randomPlayBtn.exists()).toBe(true);
    expect(randomPlayBtn.text()).toContain('随机播放');

    const player = usePlayerStore(pinia);
    const mediaSpy = vi.spyOn(player, 'media');

    await randomPlayBtn.trigger('click');
    await flushPromises();

    expect(mediaSpy).toHaveBeenCalledTimes(1);
    expect(mediaSpy).toHaveBeenCalledWith(expect.objectContaining({ id: 999, Title: 'Random Pick Movie' }));
  });

  it('媒体库管理列表提供随机播放按钮，点击指定媒体库后播放该库内随机影片', async () => {
    const randomMovie = { ...detailFixture.movie, id: 888, Title: 'Library Random Movie' };
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({
      total: 1,
      items: [{ Id: 42, Name: '欧美电影', Path: '/western', Type: 'movies', ProxyEnabled: false, ResolveRedirects: false }],
    });
    const itemsSpy = vi.spyOn(catalogApi, 'items').mockResolvedValue({
      total: 50,
      items: [randomMovie],
      image_tags: {},
    });

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/libraries', name: 'libraries', component: LibrariesPage },
        { path: '/items', name: 'items', component: { template: '<p>wall</p>' } },
      ],
    });
    await router.push('/libraries');

    wrapper = mount(LibrariesPage, {
      global: {
        plugins: [pinia, router],
      },
    });
    await flushPromises();

    const libPlayBtn = wrapper.find('[data-lib-random-play="42"]');
    expect(libPlayBtn.exists()).toBe(true);
    expect(libPlayBtn.text()).toContain('随机播放');

    const player = usePlayerStore(pinia);
    const mediaSpy = vi.spyOn(player, 'media');

    await libPlayBtn.trigger('click');
    await flushPromises();

    expect(itemsSpy).toHaveBeenCalledWith(expect.any(URLSearchParams));
    const query = itemsSpy.mock.lastCall![0] as URLSearchParams;
    expect(query.get('library_id')).toBe('42');
    expect(query.get('sort')).toBe('random');
    expect(query.get('limit')).toBe('1');

    expect(mediaSpy).toHaveBeenCalledTimes(1);
    expect(mediaSpy).toHaveBeenCalledWith(expect.objectContaining({ id: 888, Title: 'Library Random Movie' }));
  });
});

describe('详情页前一部和后一部切换功能 (Detail Navigation)', () => {
  it('在媒体墙已有加载项时，正确识别前一部和后一部，并支持点击切换', async () => {
    const auth = useAuthStore(pinia);
    auth.user = parseUser(authFixture.User);

    const wall = useMediaWallStore(pinia);
    wall.items = [
      { ...detailFixture.movie, id: 101, Title: '第一部影片' },
      { ...detailFixture.movie, id: 102, Title: '第二部影片' },
      { ...detailFixture.movie, id: 103, Title: '第三部影片' },
    ];
    wall.done = true;

    vi.spyOn(catalogApi, 'detail').mockImplementation(async (id) => {
      const numId = Number(id);
      const title = numId === 101 ? '第一部影片' : numId === 102 ? '第二部影片' : '第三部影片';
      return parseDetail({ ...detailFixture, movie: { ...detailFixture.movie, id: numId, Title: title } });
    });
    vi.spyOn(mediaApi, 'similar').mockResolvedValue([]);

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/item/:id', name: 'item', component: ItemDetailPage },
        { path: '/items', name: 'items', component: { template: '<p>wall</p>' } },
      ],
    });

    // 从第一部影片进入
    await router.push('/item/101?library_id=1');
    wrapper = mount(ItemDetailPage, {
      global: {
        plugins: [pinia, router],
        provide: { [viewHistoryKey as symbol]: { target: () => null, backFromItem: vi.fn() } },
      },
    });
    await flushPromises();

    const prevBtn = wrapper.find('#item-prev');
    const nextBtn = wrapper.find('#item-next');
    expect(prevBtn.exists()).toBe(true);
    expect(nextBtn.exists()).toBe(true);

    // 第一部影片：没有前一部，按钮禁用；后一部为第二部影片
    expect(prevBtn.attributes('disabled')).toBeDefined();
    expect(nextBtn.attributes('disabled')).toBeUndefined();
    expect(nextBtn.attributes('title')).toBe('后一部：第二部影片');

    // 点击后一部切换到第二部影片
    await nextBtn.trigger('click');
    await flushPromises();

    expect(router.currentRoute.value.path).toBe('/item/102');
    expect(router.currentRoute.value.query.library_id).toBe('1');

    // 第二部影片：前一部与后一部均可用
    const prevBtn2 = wrapper.find('#item-prev');
    const nextBtn2 = wrapper.find('#item-next');
    expect(prevBtn2.attributes('disabled')).toBeUndefined();
    expect(prevBtn2.attributes('title')).toBe('前一部：第一部影片');
    expect(nextBtn2.attributes('disabled')).toBeUndefined();
    expect(nextBtn2.attributes('title')).toBe('后一部：第三部影片');

    // 点击后一部切换到第三部影片
    await nextBtn2.trigger('click');
    await flushPromises();

    expect(router.currentRoute.value.path).toBe('/item/103');
    const prevBtn3 = wrapper.find('#item-prev');
    const nextBtn3 = wrapper.find('#item-next');
    expect(prevBtn3.attributes('disabled')).toBeUndefined();
    expect(nextBtn3.attributes('disabled')).toBeDefined();

    // 点击前一部返回第二部影片
    await prevBtn3.trigger('click');
    await flushPromises();

    expect(router.currentRoute.value.path).toBe('/item/102');
  });

  it('支持键盘快捷键左右键切换前一部与后一部', async () => {
    const auth = useAuthStore(pinia);
    auth.user = parseUser(authFixture.User);

    const wall = useMediaWallStore(pinia);
    wall.items = [
      { ...detailFixture.movie, id: 201, Title: '影片A' },
      { ...detailFixture.movie, id: 202, Title: '影片B' },
      { ...detailFixture.movie, id: 203, Title: '影片C' },
    ];
    wall.done = true;

    vi.spyOn(catalogApi, 'detail').mockImplementation(async (id) => {
      const numId = Number(id);
      return parseDetail({ ...detailFixture, movie: { ...detailFixture.movie, id: numId, Title: `影片 ${id}` } });
    });
    vi.spyOn(mediaApi, 'similar').mockResolvedValue([]);

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/item/:id', name: 'item', component: ItemDetailPage },
        { path: '/items', name: 'items', component: { template: '<p>wall</p>' } },
      ],
    });

    await router.push('/item/202');
    wrapper = mount(ItemDetailPage, {
      global: {
        plugins: [pinia, router],
        provide: { [viewHistoryKey as symbol]: { target: () => null, backFromItem: vi.fn() } },
      },
    });
    await flushPromises();

    // 触发 ArrowLeft 键盘事件切换到前一部
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft' }));
    await flushPromises();
    expect(router.currentRoute.value.path).toBe('/item/201');

    // 触发 ArrowRight 键盘事件切换到后一部
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight' }));
    await flushPromises();
    expect(router.currentRoute.value.path).toBe('/item/202');
  });
});
