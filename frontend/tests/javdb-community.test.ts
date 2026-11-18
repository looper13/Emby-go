import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia, type Pinia } from 'pinia';
import { apiClient } from '../src/api/client';
import { javdbCommunityApi, parseJavdbListMovies, parseJavdbMatch, parseJavdbRelatedLists, parseJavdbReviews, type JavdbListMovies, type JavdbMatch, type JavdbPage, type JavdbReview } from '../src/api/javdb-community';
import { r18Api } from '../src/api/r18';
import JavdbCommunitySection from '../src/components/media/JavdbCommunitySection.vue';

const libraries = [{ Id: 2, Name: '在线 JavDB', Path: '', SourceType: 'avdb_javdb' as const }];
const match: JavdbMatch = { status: 'matched', number: 'TEST-001', library_id: 2, library_name: '在线 JavDB', video: { id: 'fixture', number: 'TEST-001', title: '匹配影片' } };
const review: JavdbReview = { id: 'first', username: '评论作者', content: '真实评论字段的隔离夹具', score: '4.5', status: '看过', created_at: '2026-11-18' };
const pageIdentity = { number: 'TEST-001', video_id: 'fixture', library_id: 2 };
const reviews: JavdbPage<JavdbReview> = { ...pageIdentity, items: [review], page: 1, page_size: 15, has_more: true, total: 16 };
const localMovies: JavdbListMovies = { ...pageIdentity, list_id: 'list1', local_library_id: 1, next_page: 2, has_more: false,
  items: [{ id: 11, number: 'TEST-002', title: '本库已有影片' }] };
let pinia: Pinia;
let wrapper: VueWrapper | undefined;
function component(number = 'TEST-001') {
  wrapper = mount(JavdbCommunitySection, { props: { itemId: 5, number, localLibraryId: 1 }, global: { plugins: [pinia], stubs: { RouterLink: { props: ['to'], template: '<a><slot /></a>' } } } });
  return wrapper;
}
function button(text: string) {
  return wrapper!.findAll('button').find(item => item.text().includes(text))!;
}

beforeEach(() => {
  pinia = createPinia();
  vi.spyOn(apiClient, 'request').mockRejectedValue(new Error('不应调用未隔离接口'));
  vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: libraries, total: libraries.length });
  vi.spyOn(javdbCommunityApi, 'match').mockResolvedValue(match);
  vi.spyOn(javdbCommunityApi, 'reviews').mockResolvedValue(reviews);
  vi.spyOn(javdbCommunityApi, 'lists').mockResolvedValue({ ...pageIdentity, items: [{ id: 'list1', name: '相关影片清单', movie_count: 32 }], page: 1, page_size: 15, has_more: false });
  vi.spyOn(javdbCommunityApi, 'listMovies').mockImplementation(async (_item, library, listId) => ({ ...localMovies, library_id: library, list_id: listId }));
});
afterEach(() => { wrapper?.unmount(); wrapper = undefined; disposePinia(pinia); vi.restoreAllMocks(); });

describe('普通影片详情的 JavDB 评论与清单', () => {
  it('按入库影片 ID 匹配，默认加载评论，清单在点击后加载', async () => {
    component(); await flushPromises();
    expect(javdbCommunityApi.match).toHaveBeenCalledWith(5, 2, expect.any(AbortSignal), false);
    expect(wrapper!.text()).toContain('评论作者');
    expect(wrapper!.text()).toContain('评分 4.5');
    expect(javdbCommunityApi.lists).not.toHaveBeenCalled();
    await button('相关清单').trigger('click'); await flushPromises();
    expect(javdbCommunityApi.lists).toHaveBeenCalledWith(5, 2, 1, expect.any(AbortSignal), false);
    expect(wrapper!.text()).toContain('清单 32 部');
    const list = wrapper!.get('.javdb-list');
    expect(list.element.tagName).toBe('BUTTON');
    expect(list.attributes('href')).toBeUndefined();
    await list.trigger('click'); await flushPromises();
    expect(javdbCommunityApi.listMovies).toHaveBeenCalledWith(5, 2, 'list1', 1, 1, expect.any(AbortSignal), false);
    expect(wrapper!.text()).toContain('本库已有影片');
    await wrapper!.get('[data-local-movie="11"]').trigger('click');
    expect(wrapper!.emitted('open')).toEqual([[11]]);
    await button('评论').trigger('click');
    expect(wrapper!.text()).toContain(review.content);
    expect(javdbCommunityApi.reviews).toHaveBeenCalledTimes(1);
    expect(apiClient.request).not.toHaveBeenCalled();
  });

  it('评论分页失败保留已加载内容，单独重试失败页并保留清单', async () => {
    vi.mocked(javdbCommunityApi.reviews).mockResolvedValueOnce(reviews).mockRejectedValueOnce(new Error('评论第 2 页读取失败')).mockResolvedValueOnce({ ...reviews, items: [{ ...review, id: 'second', content: '第二页评论' }], page: 2, has_more: false });
    component(); await flushPromises();
    await button('下一页评论').trigger('click'); await flushPromises();
    expect(wrapper!.get('[role="alert"]').text()).toContain('评论第 2 页读取失败');
    expect(wrapper!.text()).toContain(review.content);
    await button('相关清单').trigger('click'); await flushPromises();
    expect(wrapper!.text()).toContain('相关影片清单');
    await button('评论').trigger('click');
    await button('重试评论').trigger('click'); await flushPromises();
    expect(javdbCommunityApi.reviews).toHaveBeenLastCalledWith(5, 2, 2, expect.any(AbortSignal), true);
    expect(wrapper!.text()).toContain('第二页评论');
    expect(button('下一页评论').attributes('disabled')).toBeDefined();
    expect(javdbCommunityApi.match).toHaveBeenCalledTimes(1);
    expect(javdbCommunityApi.lists).toHaveBeenCalledTimes(1);
  });

  it('清单失败可以单独重试，未知数量不显示为零', async () => {
    vi.mocked(javdbCommunityApi.lists).mockRejectedValueOnce(new Error('上游 JavDB 登录状态失效')).mockResolvedValueOnce({ ...pageIdentity, items: [{ id: 'list2', name: '未知数量的清单' }], page: 1, page_size: 15, has_more: false });
    component(); await flushPromises();
    await button('相关清单').trigger('click'); await flushPromises();
    expect(wrapper!.text()).toContain('上游 JavDB 登录状态失效');
    await button('重试清单').trigger('click'); await flushPromises();
    expect(javdbCommunityApi.lists).toHaveBeenLastCalledWith(5, 2, 1, expect.any(AbortSignal), true);
    expect(wrapper!.text()).toContain('未知数量的清单');
    expect(wrapper!.text()).not.toContain('0 部影片');
    await button('评论').trigger('click');
    expect(wrapper!.text()).toContain(review.content);
  });

  it('切换影片取消旧请求并拒绝迟到的评论，卸载也取消读取', async () => {
    let resolve!: (page: JavdbPage<JavdbReview>) => void;
    vi.mocked(javdbCommunityApi.reviews).mockReturnValueOnce(new Promise(done => { resolve = done; })).mockResolvedValueOnce({ ...reviews, number: 'TEST-002', items: [{ ...review, content: '新影片的评论' }] });
    component(); await flushPromises();
    const oldSignal = vi.mocked(javdbCommunityApi.reviews).mock.calls[0]![3]!;
    vi.mocked(javdbCommunityApi.match).mockResolvedValueOnce({ ...match, number: 'TEST-002', video: { ...match.video!, number: 'TEST-002' } });
    await wrapper!.setProps({ itemId: 6, number: 'TEST-002' }); await flushPromises();
    expect(oldSignal.aborted).toBe(true);
    resolve(reviews); await flushPromises();
    expect(wrapper!.text()).toContain('新影片的评论');
    expect(wrapper!.text()).not.toContain(review.content);
    const newSignal = vi.mocked(javdbCommunityApi.reviews).mock.calls[1]![3]!;
    wrapper!.unmount(); wrapper = undefined;
    expect(newSignal.aborted).toBe(true);
  });

  it('切换 JavDB 来源重新匹配，不继续显示旧来源的评论', async () => {
    vi.mocked(r18Api.libraries).mockResolvedValue({ items: [...libraries, { ...libraries[0]!, Id: 3, Name: '另一来源' }], total: 2 });
    component(); await flushPromises();
    vi.mocked(javdbCommunityApi.match).mockResolvedValueOnce({ ...match, library_id: 3, library_name: '另一来源', status: 'not_found', video: undefined });
    await wrapper!.get('select[aria-label="JavDB 资料来源"]').setValue('3'); await flushPromises();
    expect(javdbCommunityApi.match).toHaveBeenLastCalledWith(5, 3, expect.any(AbortSignal), true);
    expect(wrapper!.text()).toContain('未找到与 TEST-001 完整匹配');
    expect(wrapper!.text()).not.toContain(review.content);
    expect(javdbCommunityApi.reviews).toHaveBeenCalledTimes(1);
  });

  it('入库番号或上游影片身份变化时拒绝将新评论放在旧标题下', async () => {
    vi.mocked(javdbCommunityApi.reviews).mockResolvedValueOnce({ ...reviews, number: 'TEST-002', video_id: 'changed-film' });
    component(); await flushPromises();
    expect(wrapper!.get('[role="alert"]').text()).toContain('已更新，请重新匹配');
    expect(wrapper!.find('.javdb-review').exists()).toBe(false);
    expect(wrapper!.text()).not.toContain(review.content);
    vi.mocked(javdbCommunityApi.lists).mockResolvedValueOnce({ ...pageIdentity, library_id: 3, items: [], page: 1, page_size: 15, has_more: false });
    await button('相关清单').trigger('click'); await flushPromises();
    expect(wrapper!.get('[role="alert"]').text()).toContain('已更新，请重新匹配');
  });

  it('没有番号或可访问来源时不搜索，不把缺失匹配作为评论空态', async () => {
    component(''); await flushPromises();
    expect(wrapper!.find('section').exists()).toBe(false);
    expect(r18Api.libraries).not.toHaveBeenCalled();
    vi.mocked(r18Api.libraries).mockResolvedValue({ items: [], total: 0 });
    await wrapper!.setProps({ number: 'TEST-002' }); await flushPromises();
    expect(wrapper!.text()).toContain('还没有可访问的 JavDB 资料库');
    expect(javdbCommunityApi.match).not.toHaveBeenCalled();
    vi.mocked(r18Api.libraries).mockResolvedValue({ items: libraries, total: 1 });
    vi.mocked(javdbCommunityApi.match).mockResolvedValue({ ...match, status: 'not_found', video: undefined });
    await button('重新匹配').trigger('click'); await flushPromises();
    expect(wrapper!.text()).toContain('完整匹配');
    expect(javdbCommunityApi.reviews).not.toHaveBeenCalled();
    expect(wrapper!.text()).not.toContain('当前影片暂无评论');
  });

  it('评论正文以纯文本显示，长评论提供可操作的展开与收起', async () => {
    const content = '<script>window.untrusted = true</script>\n' + '长评论内容'.repeat(180);
    vi.mocked(javdbCommunityApi.reviews).mockResolvedValue({ ...reviews, items: [{ ...review, content }] });
    component(); await flushPromises();
    expect(wrapper!.find('.javdb-review script').exists()).toBe(false);
    expect(wrapper!.get('.javdb-review-body').text()).toContain('<script>');
    expect(wrapper!.get('.javdb-review-body').text().length).toBeLessThan(content.length);
    await button('展开完整评论').trigger('click');
    expect(wrapper!.get('.javdb-review-body').text()).toBe(content);
    expect(button('收起评论').attributes('aria-expanded')).toBe('true');
    await button('收起评论').trigger('click');
    expect(button('展开完整评论').attributes('aria-expanded')).toBe('false');
  });

  it('跳过没有本库匹配的批次，后续匹配只显示本库条目', async () => {
    vi.mocked(javdbCommunityApi.listMovies).mockResolvedValueOnce({ ...localMovies, items: [], has_more: true, next_page: 5 }).mockResolvedValueOnce({ ...localMovies, next_page: 6 });
    component(); await flushPromises();
    await button('相关清单').trigger('click'); await flushPromises();
    await wrapper!.get('.javdb-list').trigger('click'); await flushPromises();
    expect(wrapper!.findAll('.javdb-local-movie')).toHaveLength(0);
    expect(wrapper!.text()).not.toContain('本库没有这份清单');
    await button('继续匹配本库影片').trigger('click'); await flushPromises();
    expect(javdbCommunityApi.listMovies).toHaveBeenLastCalledWith(5, 2, 'list1', 5, 1, expect.any(AbortSignal), false);
    expect(wrapper!.findAll('.javdb-local-movie')).toHaveLength(1);
    expect(wrapper!.text()).toContain('本库已有影片');
    expect(wrapper!.find('.javdb-list[href]').exists()).toBe(false);
  });

  it('整个清单都没有本库对应番号时只显示空态', async () => {
    vi.mocked(javdbCommunityApi.listMovies).mockResolvedValueOnce({ ...localMovies, items: [] });
    component(); await flushPromises();
    await button('相关清单').trigger('click'); await flushPromises();
    await wrapper!.get('.javdb-list').trigger('click'); await flushPromises();
    expect(wrapper!.text()).toContain('本库没有这份清单的对应影片');
    expect(wrapper!.findAll('.javdb-local-movie')).toHaveLength(0);
    expect(wrapper!.findAll('button').some(item => item.text().includes('继续匹配本库影片'))).toBe(false);
  });

  it('继续匹配去重本库条目，失败保留原结果并重试同一批次', async () => {
    vi.mocked(javdbCommunityApi.listMovies).mockResolvedValueOnce({ ...localMovies, has_more: true, next_page: 5 }).mockRejectedValueOnce(new Error('清单影片读取失败'))
      .mockResolvedValueOnce({ ...localMovies, next_page: 6, items: [{ ...localMovies.items[0]!, title: '更新后的本库标题' }, { id: 12, number: 'TEST-003', title: '另一部本库影片' }] });
    component(); await flushPromises();
    await button('相关清单').trigger('click'); await flushPromises();
    await wrapper!.get('.javdb-list').trigger('click'); await flushPromises();
    await button('继续匹配本库影片').trigger('click'); await flushPromises();
    expect(wrapper!.get('[role="alert"]').text()).toContain('清单影片读取失败');
    expect(wrapper!.findAll('.javdb-local-movie')).toHaveLength(1);
    await button('重试本库匹配').trigger('click'); await flushPromises();
    expect(javdbCommunityApi.listMovies).toHaveBeenLastCalledWith(5, 2, 'list1', 5, 1, expect.any(AbortSignal), true);
    expect(wrapper!.findAll('.javdb-local-movie')).toHaveLength(2);
    expect(wrapper!.text()).toContain('本库匹配 2 个条目');
    expect(wrapper!.text()).toContain('更新后的本库标题');
  });

  it('当前库身份变化或响应来自其他库时不展示清单影片', async () => {
    vi.mocked(javdbCommunityApi.listMovies).mockResolvedValueOnce({ ...localMovies, local_library_id: 99 });
    component(); await flushPromises();
    await button('相关清单').trigger('click'); await flushPromises();
    await wrapper!.get('.javdb-list').trigger('click'); await flushPromises();
    expect(wrapper!.get('[role="alert"]').text()).toContain('媒体库或清单已更新');
    expect(wrapper!.findAll('.javdb-local-movie')).toHaveLength(0);
  });

  it('切换清单及影片时取消旧匹配并忽略迟到条目', async () => {
    let resolve!: (page: JavdbListMovies) => void;
    vi.mocked(javdbCommunityApi.lists).mockResolvedValueOnce({ ...pageIdentity, items: [{ id: 'list1', name: '第一份清单' }, { id: 'list2', name: '第二份清单' }], page: 1, page_size: 15, has_more: false });
    vi.mocked(javdbCommunityApi.listMovies).mockReturnValueOnce(new Promise(done => { resolve = done; })).mockResolvedValueOnce({ ...localMovies, list_id: 'list2', items: [{ id: 12, number: 'TEST-003', title: '第二份清单的本库影片' }] });
    component(); await flushPromises();
    await button('相关清单').trigger('click'); await flushPromises();
    await wrapper!.findAll('.javdb-list')[0]!.trigger('click'); await flushPromises();
    const oldSignal = vi.mocked(javdbCommunityApi.listMovies).mock.calls[0]![5]!;
    await wrapper!.findAll('.javdb-list')[1]!.trigger('click'); await flushPromises();
    expect(oldSignal.aborted).toBe(true);
    resolve(localMovies); await flushPromises();
    expect(wrapper!.text()).toContain('第二份清单的本库影片');
    expect(wrapper!.text()).not.toContain('本库已有影片');
    const currentSignal = vi.mocked(javdbCommunityApi.listMovies).mock.calls[1]![5]!;
    await wrapper!.setProps({ itemId: 6, number: 'TEST-002' }); await flushPromises();
    expect(currentSignal.aborted).toBe(true);
    expect(wrapper!.find('#javdb-local-list').exists()).toBe(false);
  });

  it('清单头分页后重置旧匹配，使用新页核验关联清单', async () => {
    vi.mocked(javdbCommunityApi.lists).mockResolvedValueOnce({ ...pageIdentity, items: [{ id: 'list1', name: '第一页清单' }], page: 1, page_size: 15, has_more: true })
      .mockResolvedValueOnce({ ...pageIdentity, items: [{ id: 'list2', name: '第二页清单' }], page: 2, page_size: 15, has_more: false });
    component(); await flushPromises();
    await button('相关清单').trigger('click'); await flushPromises();
    await wrapper!.get('.javdb-list').trigger('click'); await flushPromises();
    await button('下一页清单').trigger('click'); await flushPromises();
    expect(wrapper!.find('#javdb-local-list').exists()).toBe(false);
    await wrapper!.get('.javdb-list').trigger('click'); await flushPromises();
    expect(javdbCommunityApi.listMovies).toHaveBeenLastCalledWith(5, 2, 'list2', 1, 2, expect.any(AbortSignal), false);
  });
});

describe('JavDB 社区 API 契约', () => {
  it('请求只发送本地 ID、来源和分页，不发送自选番号或上游影片 ID', async () => {
    vi.mocked(javdbCommunityApi.match).mockRestore();
    vi.mocked(javdbCommunityApi.reviews).mockRestore();
    vi.mocked(apiClient.request).mockResolvedValueOnce(match).mockResolvedValueOnce({ ...reviews, page: 2 });
    const signal = new AbortController().signal;
    await javdbCommunityApi.match(5, 2, signal);
    await javdbCommunityApi.reviews(5, 2, 2, signal, true);
    expect(apiClient.request).toHaveBeenNthCalledWith(1, '/api/r18/items/5/match?library_id=2', expect.objectContaining({ auth: 'required', signal, timeoutMs: 22000 }));
    expect(apiClient.request).toHaveBeenNthCalledWith(2, '/api/r18/items/5/reviews?library_id=2&page=2&limit=15&refresh=1', expect.objectContaining({ auth: 'required', signal }));
  });

  it('清单本库匹配接口不发送自选本库 ID、番号或路径，拒绝错误续页', async () => {
    vi.mocked(javdbCommunityApi.listMovies).mockRestore();
    vi.mocked(apiClient.request).mockResolvedValueOnce(localMovies).mockResolvedValueOnce({ ...localMovies, next_page: 1 });
    const signal = new AbortController().signal;
    await javdbCommunityApi.listMovies(5, 2, 'list1', 1, 2, signal, true);
    expect(apiClient.request).toHaveBeenCalledWith('/api/r18/items/5/related-lists/list1/movies?library_id=2&page=1&related_page=2&refresh=1', expect.objectContaining({ auth: 'required', signal, timeoutMs: 22000 }));
    await expect(javdbCommunityApi.listMovies(5, 2, 'list1', 1, 2, signal)).rejects.toThrow('来源或分页不一致');
    await expect(javdbCommunityApi.listMovies(5, 2, '../other', 1, 2, signal)).rejects.toThrow();
  });

  it('拒绝异常匹配、分页及清单标识，只保留本库展示字段', () => {
    expect(parseJavdbMatch(match)).toEqual(match);
    expect(() => parseJavdbMatch({ ...match, video: null })).toThrow();
    expect(() => parseJavdbReviews({ ...reviews, items: null })).toThrow();
    expect(() => parseJavdbReviews({ ...reviews, page: 0 })).toThrow();
    expect(() => parseJavdbReviews({ ...reviews, total: -1 })).toThrow();
    expect(parseJavdbRelatedLists({ ...pageIdentity, items: [{ id: 'valid', name: '清单', url: 'javascript:alert(1)' }], page: 1, page_size: 15, has_more: false }).items[0]).not.toHaveProperty('url');
    expect(() => parseJavdbRelatedLists({ items: [{ id: '../other', name: '清单', url: 'https://javdb.com/lists/../other' }], page: 1, page_size: 15, has_more: false })).toThrow();
    expect(parseJavdbListMovies({ ...localMovies, items: [{ ...localMovies.items[0], source_path: '/private/file' }] }).items[0]).not.toHaveProperty('source_path');
    expect(() => parseJavdbListMovies({ ...localMovies, local_library_id: 0 })).toThrow();
    expect(() => parseJavdbListMovies({ ...localMovies, has_more: true, next_page: 10000 })).toThrow();
  });
});
