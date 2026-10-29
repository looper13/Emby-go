import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia } from 'pinia';
import { reactive } from 'vue';
import { ApiError, apiClient } from '../src/api/client';
import { cloud115Api } from '../src/api/cloud115';
import { parseR18Page, parseR18Tags, parseR18Video, r18Api, type R18Page, type R18Tags, type R18Video } from '../src/api/r18';
import { readR18CatalogQuery, r18Modes, serializeR18CatalogQuery } from '../src/composables/r18-catalog';
import { applyMediaVisibility } from '../src/composables/useMediaVisibility';
import R18CatalogPage from '../src/pages/R18CatalogPage.vue';
import { useAuthStore } from '../src/stores/auth';

const route = reactive({ name: 'r18', fullPath: '/r18?library_id=2', query: { library_id: '2' } as Record<string, string> });
function navigate(destination: { path: string; query: Record<string, string> }) {
  route.query = destination.query;
  route.fullPath = destination.path + '?' + new URLSearchParams(destination.query);
  return Promise.resolve();
}
const router = { push: vi.fn(navigate), replace: vi.fn(navigate) };
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => router }));
const library = { Id: 2, Name: '隔离 JavDB 资料库', Path: '', SourceType: 'avdb_javdb' as const, Revision: 1 };
const video: R18Video = { id: 'fixture', content_id: 'fixture', service_code: 'javdb', number: 'TEST-001', title: '目录夹具', original_title: '', release_date: '2026-01-01', cover_url: '/api/r18/libraries/2/videos/fixture/images/poster', images: [], duration: 90, score: 4, magnets_count: 8, has_local: true, tags: [], has_preview: false, preview_url: '' };
const page: R18Page = { items: [video], page: 1, page_size: 24, has_previous: false, has_more: true };
const tags: R18Tags = { categories: [
  { id: 'subject', name: '題材', type: '0', tags: Array.from({ length: 7 }, (_, index) => ({ id: String(index + 1), name: `题材${index + 1}`, type: '0' })) },
  { id: 'subject', name: '題材', type: '1', tags: [{ id: '1', name: '无码题材', type: '1' }] },
  { id: 'main', name: '基本', type: '0', tags: [{ id: 'p', name: '可播放', type: '0' }, { id: 'm', name: '可下载', type: '0' }, { id: 'c', name: '含字幕', type: '0' }] },
  { id: 'year', name: '年份', type: '0', tags: [{ id: '2024', name: '2024年', type: '0' }] },
  { id: 'month', name: '月份', type: '0', tags: [{ id: '6', name: '六月', type: '0' }] },
  { id: 'duration', name: '时长', type: '0', tags: [{ id: 'lt-45', name: '45分钟以下', type: '0' }, { id: '45-90', name: '45至90分钟', type: '0' }, { id: '90-120', name: '90至120分钟', type: '0' }, { id: 'gt-120', name: '120分钟以上', type: '0' }] },
  { id: 'main', name: '基本', type: '3', tags: [{ id: 'm', name: '可下载', type: '3' }] },
  { id: 'year', name: '年份', type: '3', tags: [{ id: '2025', name: '2025年', type: '3' }] },
] };
const wrappers: VueWrapper[] = [], piniaStores: ReturnType<typeof createPinia>[] = [];
function render() {
  const pinia = createPinia(); piniaStores.push(pinia);
  const wrapper = mount(R18CatalogPage, { global: { plugins: [pinia], stubs: { RouterLink: { name: 'RouterLink', props: ['to'], template: '<a><slot /></a>' } } } });
  wrappers.push(wrapper); return { wrapper, pinia };
}
function setRoute(query: Record<string, string>) { void navigate({ path: '/r18', query }); }
function requestQuery(index = -1) { return Object.fromEntries(vi.mocked(r18Api.videos).mock.calls.at(index)![1]); }
function deferred<T>() { let resolve!: (value: T) => void, reject!: (cause: Error) => void; const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }
async function expand(wrapper: VueWrapper, key = '0-subject') {
  const group = wrapper.get(`[data-r18-group="${key}"]`);
  (group.element as HTMLDetailsElement).open = true; await group.trigger('toggle');
}
beforeEach(() => {
  applyMediaVisibility(false); setRoute({ library_id: '2' }); route.name = 'r18';
  vi.spyOn(apiClient, 'request').mockRejectedValue(new Error('不应发送未隔离请求'));
  vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [library], total: 1 });
  vi.spyOn(r18Api, 'videos').mockResolvedValue(page);
  vi.spyOn(r18Api, 'tags').mockResolvedValue(tags);
  for (const key of ['state', 'submit', 'prepare', 'select', 'change'] as const) vi.spyOn(cloud115Api, key).mockRejectedValue(new Error('目录不应读取115'));
});
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount()); piniaStores.splice(0).forEach(disposePinia);
  router.push.mockClear(); router.replace.mockClear(); vi.restoreAllMocks(); applyMediaVisibility(false);
});

describe('JavDB 目录参数与严格元数据', () => {
  it('默认最新、页码与每页大小保持现有路由契约', () => {
    expect(serializeR18CatalogQuery(readR18CatalogQuery({ library_id: '2' }))).toEqual({ library_id: '2', type: 'all', filter: 'magnets', sort: 'release', order: 'desc', page: '1', page_size: '24' });
    expect(readR18CatalogQuery({ page: '10000', page_size: '999' })).toMatchObject({ page: 1, page_size: '24' });
    expect(readR18CatalogQuery({ type: '4', sort: 'update' })).toMatchObject({ mode: 'latest', type: '4', sort: 'update' });
    expect(readR18CatalogQuery({ mode: 'categories', sort: 'update' }).sort).toBe('release');
    for (const invalid of [0, -1, 10000, 1.5, NaN]) expect(() => serializeR18CatalogQuery(readR18CatalogQuery({}), invalid)).toThrow();
  }, 60000);
  it('分类题材与基本条件稳定去重，特殊条件单独传递', () => {
    const query = readR18CatalogQuery({ library_id: '2', mode: 'categories', type: '0', tag_ids: '0:23,0:1,0:23', main_filter: 'm,p,p,c', year: '2024', month: '6', duration: '90-120', sort: 'score', order: 'asc', page: '4', page_size: '50' });
    expect(serializeR18CatalogQuery(query)).toEqual({ library_id: '2', mode: 'categories', type: '0', filter: 'magnets', sort: 'score', order: 'asc', page: '4', page_size: '50', tag_ids: '0:1,0:23', main_filter: 'c,m,p', year: '2024', month: '6', duration: '90-120' });
    expect(serializeR18CatalogQuery(readR18CatalogQuery({ mode: 'categories' }))).not.toHaveProperty('main_filter');
  }, 60000);
  it('普通题材最多5个，但basic6和年/月/时长不占题材名额', () => {
    const query = readR18CatalogQuery({ mode: 'categories', tag_ids: '0:1,0:2,0:3,0:4,0:5', main_filter: 'p,m,c,s,i,v', year: '2024', month: '12', duration: 'gt-120' });
    expect(serializeR18CatalogQuery(query)).toMatchObject({ tag_ids: '0:1,0:2,0:3,0:4,0:5', main_filter: 'c,i,m,p,s,v', year: '2024', month: '12', duration: 'gt-120' });
    for (const overrides of [{ tag_ids: ['0:1', '0:2', '0:3', '0:4', '0:5', '0:6'] }, { tag_ids: ['0:../'] }, { type: '1', tag_ids: ['0:1'] }, { main_filter: ['unsafe'] }, { month: '13' }, { duration: 'guessed' }, { year: '-1' }, { sort: 'update' }]) expect(() => serializeR18CatalogQuery({ ...query, ...overrides })).toThrow();
  }, 60000);
  for (const mode of ['top250', 'ranking', 'playback', 'popular']) {
    it(`${mode}不携带普通排序或分类条件`, () => {
      const query = serializeR18CatalogQuery(readR18CatalogQuery({ mode, type: 'all', filter: 'magnets', tag_ids: '0:23', main_filter: 'p', month: '6', duration: 'lt-45', sort: 'score', order: 'asc' }));
      expect(query).toMatchObject({ mode, type: mode === 'ranking' ? '0' : 'all', filter: 'all' });
      for (const key of ['sort', 'order', 'tag_ids', 'main_filter', 'year', 'month', 'duration']) expect(query).not.toHaveProperty(key);
      if (mode !== 'top250') expect(query.period).toBe('daily');
    }, 60000);
  }
  it('TOP年份榜与类型互斥，已看参数仅属于TOP且允许显式false', () => {
    expect(serializeR18CatalogQuery(readR18CatalogQuery({ mode: 'top250', year: '2024', ignore_watched: 'false' }))).toMatchObject({ type: 'all', year: '2024', ignore_watched: 'false' });
    expect(() => serializeR18CatalogQuery(readR18CatalogQuery({ mode: 'top250', type: '0', year: '2024' }))).toThrow(/互|同时/u);
    expect(serializeR18CatalogQuery(readR18CatalogQuery({ mode: 'ranking', ignore_watched: 'true', year: '2024' }))).not.toHaveProperty('ignore_watched');
  }, 60000);
  it('TOP页码以250上限和每页大小限制，最新更新不能叠年份', () => {
    for (const size of ['24', '40', '50']) {
      const state = readR18CatalogQuery({ mode: 'top250', page_size: size });
      const max = Math.ceil(250 / Number(size));
      expect(serializeR18CatalogQuery(state, max).page).toBe(String(max));
      expect(() => serializeR18CatalogQuery(state, max + 1)).toThrow(/TOP250/u);
    }
    expect(() => serializeR18CatalogQuery(readR18CatalogQuery({ mode: 'latest', sort: 'update', year: '2024' }))).toThrow(/更新排序不能叠加年份/u);
  }, 60000);
  it('搜索从任意模式切最新相关性且不携带类别、榜单或年份', () => {
    const query = serializeR18CatalogQuery(readR18CatalogQuery({ mode: 'top250', search: ' TEST ', type: '3', year: '2024', tag_ids: '0:23', period: 'monthly', ignore_watched: 'true', sort: 'score', order: 'asc' }));
    expect(query).toEqual({ library_id: '', type: '3', filter: 'magnets', page: '1', page_size: '24', search: 'TEST' });
  }, 60000);
  it('rank省略为0，total省略为null，只接受服务器真实数值', () => {
    expect(parseR18Video(video).rank).toBe(0); expect(parseR18Page(page).total).toBeNull();
    expect(parseR18Video({ ...video, rank: 251 }).rank).toBe(251);
    expect(parseR18Page({ ...page, total: 60 }).total).toBe(60);
    for (const invalid of [-1, NaN, 0.5, '1', null]) expect(() => parseR18Video({ ...video, rank: invalid })).toThrow();
    for (const invalid of [-1, NaN, 0.5, '60']) expect(() => parseR18Page({ ...page, total: invalid })).toThrow();
  }, 60000);
  it('分类DTO只接受字符串id/type与标签数组，不猜测对象字段', () => {
    expect(parseR18Tags(tags)).toEqual(tags);
    for (const invalid of [{ categories: null }, { categories: [{ ...tags.categories[0], id: 1 }] }, { categories: [{ ...tags.categories[0], type: 0 }] }, { categories: [{ ...tags.categories[0], type: '5' }] }, { categories: [{ ...tags.categories[0], tags: [{ id: 'unsafe/path', name: '标签', type: '0' }] }] }]) expect(() => parseR18Tags(invalid)).toThrow();
  }, 60000);
  it('tags真实API请求只走第一方、携带type并提供取消与超时', async () => {
    vi.mocked(r18Api.tags).mockRestore();
    vi.mocked(apiClient.request).mockResolvedValue(tags);
    const controller = new AbortController(); await r18Api.tags('2', 'all', controller.signal);
    expect(apiClient.request).toHaveBeenCalledExactlyOnceWith('/api/r18/libraries/2/tags?type=all', expect.objectContaining({ auth: 'required', signal: controller.signal, timeoutMs: 22000 }));
    await expect(r18Api.tags('2', '5')).rejects.toThrow(); expect(apiClient.request).toHaveBeenCalledTimes(1);
    await r18Api.tags('2', 'all', controller.signal, true);
    expect(apiClient.request).toHaveBeenLastCalledWith('/api/r18/libraries/2/tags?type=all&refresh=1', expect.objectContaining({ signal: controller.signal }));
  }, 60000);
});

describe('JavDB 模式与集中分类查询', () => {
  it('六种模式清晰可用，切换重置第一页而不冒充评分排行榜', async () => {
    setRoute({ library_id: '2', page: '7', page_size: '40', sort: 'score' });
    const { wrapper } = render(); await flushPromises();
    expect(wrapper.findAll('[data-r18-mode]').map(button => button.attributes('data-r18-mode'))).toEqual(r18Modes.map(mode => mode.value));
    await wrapper.get('#r18-mode-top250').trigger('click'); await flushPromises();
    expect(requestQuery()).toEqual({ mode: 'top250', type: 'all', filter: 'all', page: '1', page_size: '40' });
    expect(wrapper.get('#r18-mode-top250').attributes('aria-pressed')).toBe('true');
    expect(wrapper.find('#r18-sort').exists()).toBe(false); expect(wrapper.text()).toContain('250 是榜单上限');
  }, 60000);
  it('分类分组默认折叠，两题材、基础和发行条件集中提交，不逐项请求', async () => {
    setRoute({ library_id: '2', mode: 'categories' }); const { wrapper } = render(); await flushPromises();
    expect(r18Api.tags).toHaveBeenCalledExactlyOnceWith('2', 'all', expect.any(AbortSignal), false);
    expect(wrapper.findAll('.r18-tag-options input')).toHaveLength(0);
    expect(wrapper.get('#r18-sort').findAll('option').map(option => option.attributes('value'))).not.toContain('update');
    await expand(wrapper); await wrapper.get('#r18-tag-0-1').setValue(true); await wrapper.get('#r18-tag-0-2').setValue(true);
    await wrapper.get('#r18-main-p').setValue(true); await wrapper.get('#r18-main-m').setValue(true);
    await wrapper.get('#r18-year').setValue('2024'); await wrapper.get('#r18-month').setValue('6'); await wrapper.get('#r18-duration').setValue('90-120');
    expect(r18Api.videos).toHaveBeenCalledTimes(1);
    await wrapper.get('.r18-filters').trigger('submit'); await flushPromises();
    expect(requestQuery()).toEqual({ mode: 'categories', type: 'all', filter: 'magnets', page: '1', page_size: '24', sort: 'release', order: 'desc', year: '2024', month: '6', duration: '90-120', tag_ids: '0:1,0:2', main_filter: 'm,p' });
    expect(r18Api.tags).toHaveBeenCalledTimes(1); expect(r18Api.libraries).toHaveBeenCalledTimes(1);
    await wrapper.findAll('button').find(button => button.text().includes('下一页'))!.trigger('click'); await flushPromises();
    expect(requestQuery()).toEqual(expect.objectContaining({ page: '2', tag_ids: '0:1,0:2', year: '2024', month: '6', duration: '90-120', main_filter: 'm,p' }));
    expect(wrapper.getComponent({ name: 'RouterLink' }).props('to').query.from).toBe(route.fullPath);
  }, 60000);
  it('第六个题材不能选中，可移除再选；类型变更清理分类草稿', async () => {
    setRoute({ library_id: '2', mode: 'categories' }); const { wrapper } = render(); await flushPromises(); await expand(wrapper);
    for (let id = 1; id <= 6; id++) await wrapper.get(`#r18-tag-0-${id}`).setValue(true);
    expect(wrapper.findAll('.r18-tag-options input').filter(input => (input.element as HTMLInputElement).checked)).toHaveLength(5);
    expect(wrapper.get('#r18-category-warning').text()).toContain('最多选 5');
    await wrapper.get('#r18-tag-0-1').setValue(false); await wrapper.get('#r18-tag-0-6').setValue(true);
    expect(wrapper.findAll('.r18-selected-tags button')).toHaveLength(5);
    await wrapper.get('#r18-main-p').setValue(true); await wrapper.get('#r18-year').setValue('2024');
    await wrapper.get('#r18-type').setValue('1'); await flushPromises();
    expect(wrapper.find('.r18-selected-tags').exists()).toBe(false);
    expect(wrapper.find('#r18-main-p').exists()).toBe(false);
    expect((wrapper.get('#r18-year').element as HTMLSelectElement).value).toBe('');
    expect(wrapper.find('[data-r18-group="0-subject"]').exists()).toBe(false);
    expect(wrapper.find('[data-r18-group="1-subject"]').exists()).toBe(true);
    expect(r18Api.videos).toHaveBeenCalledTimes(1);
  }, 60000);
  it('特殊组按类型过滤，FC2/动漫不显示其他类型的基础或年份选项', async () => {
    setRoute({ library_id: '2', mode: 'categories', type: '3' }); const { wrapper } = render(); await flushPromises();
    expect(wrapper.find('#r18-main-p').exists()).toBe(false); expect(wrapper.find('#r18-main-m').exists()).toBe(true);
    expect(wrapper.get('#r18-year').findAll('option').map(option => option.attributes('value'))).toEqual(['', '2025']);
    await wrapper.get('#r18-type').setValue('4'); await flushPromises();
    expect(wrapper.find('.r18-basic-options').exists()).toBe(false);
    expect(wrapper.get('#r18-year').findAll('option').map(option => option.attributes('value'))).toEqual(['']);
  }, 60000);
  it('大标签集合按组折叠、分批展示、本地搜索，不铺满1061个控件', async () => {
    vi.mocked(r18Api.tags).mockResolvedValue({ categories: [{ id: 'subject', name: '题材', type: '0', tags: Array.from({ length: 1061 }, (_, index) => ({ id: String(index + 1), name: index === 1060 ? '唯一匹配词' : `夹具${index}`, type: '0' })) }] });
    setRoute({ library_id: '2', mode: 'categories' }); const { wrapper } = render(); await flushPromises();
    expect(wrapper.findAll('.r18-tag-options input')).toHaveLength(0); await expand(wrapper);
    expect(wrapper.findAll('.r18-tag-options input')).toHaveLength(40);
    await wrapper.get('.r18-tag-options button').trigger('click'); expect(wrapper.findAll('.r18-tag-options input')).toHaveLength(80);
    await wrapper.get('#r18-category-search').setValue('唯一匹配');
    expect(wrapper.findAll('.r18-tag-options input')).toHaveLength(1); expect(wrapper.find('#r18-tag-0-1061').exists()).toBe(true);
    expect(r18Api.tags).toHaveBeenCalledTimes(1); expect(r18Api.videos).toHaveBeenCalledTimes(1);
  }, 60000);
  it('TOP类型与年份双向互斥，已看开关明确是AVdb的JavDB账户', async () => {
    setRoute({ library_id: '2', mode: 'top250', type: '0' }); const { wrapper } = render(); await flushPromises();
    expect(wrapper.get('#r18-type').findAll('option').map(option => option.attributes('value'))).toEqual(['all', '0', '1', '2', '3']);
    await wrapper.get('#r18-year').setValue('2024'); await flushPromises();
    expect(requestQuery()).toMatchObject({ type: 'all', year: '2024' });
    await wrapper.get('#r18-type').setValue('1'); await flushPromises();
    expect(requestQuery()).toMatchObject({ type: '1' }); expect(requestQuery()).not.toHaveProperty('year');
    await wrapper.get('#r18-ignore-watched').setValue(true); await flushPromises(); expect(requestQuery().ignore_watched).toBe('true');
    await wrapper.get('#r18-ignore-watched').setValue(false); await flushPromises(); expect(requestQuery().ignore_watched).toBe('false');
    expect(wrapper.get('#r18-query-hint').text()).toContain('与本 Emby 用户已看无关');
  }, 60000);
  it('TOP跳页显示当前每页上限，拒绝越界跳转；换50条重置第一页', async () => {
    setRoute({ library_id: '2', mode: 'top250', page: '7', page_size: '40' }); const { wrapper } = render(); await flushPromises();
    expect(wrapper.get('#r18-jump-page').attributes('max')).toBe('7');
    await wrapper.get('#r18-jump-page').setValue('8'); await wrapper.get('.r18-page-jump').trigger('submit'); await flushPromises();
    expect(r18Api.videos).toHaveBeenCalledTimes(1); expect(wrapper.text()).toContain('TOP250 当前每页最多 7 页');
    await wrapper.get('#r18-page-size').setValue('50'); await flushPromises();
    expect(requestQuery()).toMatchObject({ mode: 'top250', page: '1', page_size: '50' });
    expect(wrapper.get('#r18-jump-page').attributes('max')).toBe('5');
  }, 60000);
  it('最新更新设置年份自动恢复发行排序，有年份时禁用更新选项', async () => {
    setRoute({ library_id: '2', sort: 'update' }); const { wrapper } = render(); await flushPromises();
    await wrapper.get('#r18-year').setValue('2024'); await flushPromises();
    expect(requestQuery()).toMatchObject({ sort: 'release', year: '2024' });
    expect(wrapper.get('#r18-sort option[value="update"]').attributes('disabled')).toBeDefined();
  }, 60000);
  for (const mode of ['ranking', 'playback', 'popular']) {
    it(`${mode}周期切换和分页保留模式，上游顺序不本地重排`, async () => {
      vi.mocked(r18Api.videos).mockResolvedValue({ ...page, items: [{ ...video, id: 'second', rank: 2, score: 1 }, { ...video, id: 'first', rank: 1, score: 5 }], total: 60 });
      setRoute({ library_id: '2', mode }); const { wrapper } = render(); await flushPromises();
      expect(requestQuery()).toMatchObject({ mode, type: mode === 'ranking' ? '0' : 'all', period: 'daily', filter: 'all' });
      expect(wrapper.findAll('.r18-rank').map(rank => rank.text())).toEqual(['#2', '#1']);
      expect(wrapper.get('.r18-list-meta').text()).toContain('榜单共 60 条');
      if (mode !== 'ranking') expect(wrapper.find('#r18-type').exists()).toBe(false);
      await wrapper.get('#r18-period').setValue('weekly'); await flushPromises();
      expect(requestQuery()).toMatchObject({ mode, period: 'weekly', page: '1' });
      await wrapper.findAll('button').find(button => button.text().includes('下一页'))!.trigger('click'); await flushPromises();
      expect(requestQuery()).toMatchObject({ mode, period: 'weekly', page: '2' });
      expect(r18Api.libraries).toHaveBeenCalledTimes(1);
    }, 60000);
  }
  it('榜单搜索统一返回latest，不读取115、详情或预览', async () => {
    setRoute({ library_id: '2', mode: 'top250', year: '2024', ignore_watched: 'true' }); const { wrapper } = render(); await flushPromises();
    await wrapper.get('#r18-search').setValue('TEST'); await wrapper.get('.r18-filters').trigger('submit'); await flushPromises();
    expect(requestQuery()).toEqual({ type: 'all', filter: 'all', page: '1', page_size: '24', search: 'TEST' });
    expect(wrapper.get('#r18-mode-latest').attributes('aria-pressed')).toBe('true'); expect(r18Api.tags).not.toHaveBeenCalled();
    for (const key of ['state', 'submit', 'prepare', 'select', 'change'] as const) expect(cloud115Api[key]).not.toHaveBeenCalled();
    expect(apiClient.request).not.toHaveBeenCalled();
  }, 60000);
  it('分类和排行隐藏图片时没有img，仍保留本地STRM文本标记', async () => {
    applyMediaVisibility(true); setRoute({ library_id: '2', mode: 'categories' }); const { wrapper } = render(); await flushPromises();
    expect(wrapper.findAll('img')).toHaveLength(0); expect(wrapper.get('.r18-card-local').text()).toBe('本地 STRM 可播');
    await wrapper.get('#r18-mode-top250').trigger('click'); await flushPromises(); expect(wrapper.findAll('img')).toHaveLength(0);
    for (const key of ['state', 'submit', 'prepare', 'select', 'change'] as const) expect(cloud115Api[key]).not.toHaveBeenCalled();
  }, 60000);
});

describe('JavDB 请求预算、授权和迟到响应', () => {
  it('首次默认资料库只读一次资料库列表，之后翻页只有影片GET', async () => {
    setRoute({}); const { wrapper } = render(); await flushPromises();
    expect(router.replace).toHaveBeenCalledTimes(1); expect(r18Api.libraries).toHaveBeenCalledTimes(1); expect(r18Api.videos).toHaveBeenCalledTimes(1);
    expect(requestQuery()).toMatchObject({ page: '1' });
    await wrapper.findAll('button').find(button => button.text().includes('下一页'))!.trigger('click'); await flushPromises();
    expect(r18Api.libraries).toHaveBeenCalledTimes(1); expect(r18Api.videos).toHaveBeenCalledTimes(2);
    setRoute({ ...route.query, irrelevant: 'ignored' }); await flushPromises(); expect(r18Api.videos).toHaveBeenCalledTimes(2);
  }, 60000);
  it('手动刷新重取权限/版本并传refresh=1，不把刷新参数持久化路由', async () => {
    const { wrapper } = render(); await flushPromises();
    await wrapper.findAll('button').find(button => button.text() === '刷新')!.trigger('click'); await flushPromises();
    expect(r18Api.libraries).toHaveBeenCalledTimes(2); expect(requestQuery().refresh).toBe('1'); expect(route.query).not.toHaveProperty('refresh');
  }, 60000);
  it('分类手动刷新标签也绕过服务器缓存，离开分类取消迟到元数据', async () => {
    setRoute({ library_id: '2', mode: 'categories' }); const pending = deferred<R18Tags>();
    vi.mocked(r18Api.tags).mockReturnValueOnce(pending.promise);
    const { wrapper } = render(); await flushPromises(); const signal = vi.mocked(r18Api.tags).mock.calls[0]![2]!;
    await wrapper.get('#r18-mode-latest').trigger('click'); await flushPromises(); expect(signal.aborted).toBe(true);
    pending.resolve(tags); await flushPromises(); expect(wrapper.find('.r18-category-panel').exists()).toBe(false);
    await wrapper.get('#r18-mode-categories').trigger('click'); await flushPromises();
    expect(r18Api.tags).toHaveBeenCalledTimes(2);
    await wrapper.findAll('button').find(button => button.text() === '刷新')!.trigger('click'); await flushPromises();
    expect(r18Api.tags).toHaveBeenLastCalledWith('2', 'all', expect.any(AbortSignal), true);
    expect(requestQuery().refresh).toBe('1');
  }, 60000);
  for (const state of ['已加载', '进行中'] as const) {
    it(`${state}分类切换到不存在的资料库时同步清理，迟到标签不回填`, async () => {
      setRoute({ library_id: '2', mode: 'categories' });
      const pending = deferred<R18Tags>();
      if (state === '进行中') vi.mocked(r18Api.tags).mockReturnValueOnce(pending.promise);
      const { wrapper } = render(); await flushPromises();
      const signal = vi.mocked(r18Api.tags).mock.calls[0]![2]!;
      if (state === '已加载') expect(wrapper.findAll('.r18-tag-group')).not.toHaveLength(0);
      else expect(wrapper.find('.r18-category-panel [role="status"]').exists()).toBe(true);
      setRoute({ library_id: '99', mode: 'categories' });
      expect(signal.aborted).toBe(true);
      await flushPromises();
      expect(wrapper.text()).toContain('资料库不存在或你没有访问权限');
      expect(wrapper.findAll('.r18-card')).toHaveLength(0);
      expect(wrapper.findAll('.r18-tag-group')).toHaveLength(0);
      expect(wrapper.find('.r18-basic-options').exists()).toBe(false);
      expect(wrapper.find('#r18-category-search').exists()).toBe(false);
      expect(wrapper.find('.r18-category-panel [role="status"]').exists()).toBe(false);
      pending.resolve(tags); await flushPromises();
      expect(wrapper.findAll('.r18-tag-group')).toHaveLength(0);
      expect(wrapper.find('#r18-main-p').exists()).toBe(false);
      expect(r18Api.libraries).toHaveBeenCalledTimes(1);
      expect(r18Api.tags).toHaveBeenCalledTimes(1);
      expect(r18Api.videos).toHaveBeenCalledTimes(1);
    }, 60000);
  }
  it('切条件取消旧请求，迟到响应不会覆盖新条件；旧卡片明确标记且保留原from', async () => {
    const stale = deferred<R18Page>(), fresh = deferred<R18Page>();
    vi.mocked(r18Api.videos).mockResolvedValueOnce(page).mockReturnValueOnce(stale.promise).mockReturnValueOnce(fresh.promise);
    const { wrapper } = render(); await flushPromises(); const originalFrom = route.fullPath;
    setRoute({ library_id: '2', type: '0' }); await flushPromises();
    const signal = vi.mocked(r18Api.videos).mock.calls[1]![2]!;
    expect(wrapper.get('.r18-stale-results').text()).toContain('正在加载新条件');
    expect(wrapper.getComponent({ name: 'RouterLink' }).props('to').query.from).toBe(originalFrom);
    setRoute({ library_id: '2', type: '1' }); await flushPromises(); expect(signal.aborted).toBe(true);
    fresh.resolve({ ...page, items: [{ ...video, number: 'NEW-001' }] }); await flushPromises();
    stale.resolve({ ...page, items: [{ ...video, number: 'OLD-001' }] }); await flushPromises();
    expect(wrapper.get('.r18-card').text()).toContain('NEW-001'); expect(wrapper.text()).not.toContain('OLD-001');
    expect(wrapper.find('.r18-stale-results').exists()).toBe(false); expect(r18Api.libraries).toHaveBeenCalledTimes(1);
  }, 60000);
  it('新条件失败保留有明确提示的旧结果，403则立即清除旧卡片', async () => {
    const { wrapper } = render(); await flushPromises();
    vi.mocked(r18Api.videos).mockRejectedValueOnce(new Error('隔离上游失败'));
    setRoute({ library_id: '2', type: '0' }); await flushPromises();
    expect(wrapper.findAll('.r18-card')).toHaveLength(1); expect(wrapper.get('.r18-stale-results').text()).toContain('当前是上次结果');
    vi.mocked(r18Api.videos).mockRejectedValueOnce(new ApiError('没有访问权限', 403));
    setRoute({ library_id: '2', type: '1' }); await flushPromises(); expect(wrapper.findAll('.r18-card')).toHaveLength(0);
  }, 60000);
  it('A第4页后B第1页失败保留旧结果，分页全部禁用且守卫不发送新条件第5或3页', async () => {
    setRoute({ library_id: '2', type: '0', page: '4' });
    vi.mocked(r18Api.videos).mockResolvedValueOnce({ ...page, page: 4, has_previous: true, has_more: true });
    const { wrapper } = render(); await flushPromises(); const originalFrom = route.fullPath;
    vi.mocked(r18Api.videos).mockRejectedValueOnce(new Error('隔离新条件失败'));
    setRoute({ library_id: '2', type: '1', page: '1' }); await flushPromises();
    expect(wrapper.get('.r18-page-number').text()).toBe('第 4 页');
    expect(wrapper.get('.r18-stale-results').text()).toContain('当前是上次结果');
    expect(wrapper.getComponent({ name: 'RouterLink' }).props('to').query.from).toBe(originalFrom);
    const previous = wrapper.findAll('.r18-pagination > button').find(button => button.text().includes('上一页'))!;
    const next = wrapper.findAll('.r18-pagination > button').find(button => button.text().includes('下一页'))!;
    const jumpInput = wrapper.get('#r18-jump-page'), jumpButton = wrapper.get('.r18-page-jump button');
    for (const control of [previous, next, jumpInput, jumpButton]) expect(control.attributes('disabled')).toBeDefined();
    await previous.trigger('click'); await next.trigger('click'); await jumpButton.trigger('click');
    await wrapper.get('.r18-page-jump').trigger('submit'); await flushPromises();
    expect(router.push).not.toHaveBeenCalled(); expect(r18Api.videos).toHaveBeenCalledTimes(2);
    // 模拟绕过 DOM 禁用，验证处理函数本身也拦截旧结果翻页，而非只依赖按钮状态。
    previous.element.removeAttribute('disabled'); next.element.removeAttribute('disabled'); jumpInput.element.removeAttribute('disabled');
    await previous.trigger('click'); await next.trigger('click'); await jumpInput.setValue('5');
    await wrapper.get('.r18-page-jump').trigger('submit'); await flushPromises();
    expect(router.push).not.toHaveBeenCalled(); expect(r18Api.videos).toHaveBeenCalledTimes(2);
    expect(requestQuery()).toMatchObject({ type: '1', page: '1' });
  }, 60000);
  it('刷新撤销权限或改变版本时不保留旧本地可播卡片', async () => {
    const { wrapper } = render(); await flushPromises();
    vi.mocked(r18Api.libraries).mockResolvedValueOnce({ items: [], total: 0 });
    await wrapper.findAll('button').find(button => button.text() === '刷新')!.trigger('click'); await flushPromises();
    expect(wrapper.findAll('.r18-card')).toHaveLength(0);
    expect(r18Api.videos).toHaveBeenCalledTimes(1);
  }, 60000);
  it('登出或切换token同步隔离资料库、标签与正在请求的影片', async () => {
    setRoute({ library_id: '2', mode: 'categories' }); const pending = deferred<R18Page>();
    const { wrapper, pinia } = render(); await flushPromises();
    const auth = useAuthStore(pinia); auth.token = 'fixture-session-one'; await flushPromises();
    vi.mocked(r18Api.videos).mockReturnValueOnce(pending.promise); setRoute({ library_id: '2', mode: 'categories', page: '2' }); await flushPromises();
    const signal = vi.mocked(r18Api.videos).mock.lastCall![2]!;
    auth.token = null; expect(signal.aborted).toBe(true); await flushPromises();
    expect(wrapper.findAll('.r18-card')).toHaveLength(0); expect(wrapper.findAll('.r18-tag-group')).toHaveLength(0);
    pending.resolve(page); await flushPromises(); expect(wrapper.findAll('.r18-card')).toHaveLength(0);
    auth.token = 'fixture-session-two'; await flushPromises();
    expect(r18Api.libraries).toHaveBeenCalledTimes(3); expect(r18Api.tags).toHaveBeenCalledTimes(3);
  }, 60000);
});
