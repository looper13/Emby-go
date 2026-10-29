import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia } from 'pinia';
import { reactive } from 'vue';
import MediaImage from '../src/components/media/MediaImage.vue';
import R18CatalogPage from '../src/pages/R18CatalogPage.vue';
import { r18Api, type R18Video } from '../src/api/r18';
import { applyMediaVisibility } from '../src/composables/useMediaVisibility';

const route = reactive({ name: 'r18', fullPath: '/r18?library_id=2', query: { library_id: '2' }, params: {} });
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push: vi.fn(), replace: vi.fn() }) }));
const wrappers: VueWrapper[] = [], stores: ReturnType<typeof createPinia>[] = [];
const src = '/api/r18/libraries/2/videos/fixture/images/poster';
function image(props: { src: string; fallback?: string; alt?: string } = { src }) { const wrapper = mount(MediaImage, { props }); wrappers.push(wrapper); return wrapper; }
async function exhaust(wrapper: VueWrapper) {
  await wrapper.get('img').trigger('error'); await vi.advanceTimersByTimeAsync(500); await wrapper.get('img').trigger('error');
}
beforeEach(async () => { vi.useFakeTimers(); applyMediaVisibility(false); await vi.advanceTimersByTimeAsync(0); });
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); stores.splice(0).forEach(disposePinia); vi.restoreAllMocks(); vi.useRealTimers(); applyMediaVisibility(false); });

describe('图片有界恢复', () => {
  it('短暂失败500ms后只重试同URL一次，成功恢复图片', async () => {
    const wrapper = image(), first = wrapper.get('img').element;
    await wrapper.get('img').trigger('error');
    expect(wrapper.find('img').exists()).toBe(false); expect(wrapper.text()).toBe('正在重试…');
    await vi.advanceTimersByTimeAsync(499); expect(wrapper.find('img').exists()).toBe(false);
    await vi.advanceTimersByTimeAsync(1);
    expect(wrapper.get('img').attributes('src')).toBe(src); expect(wrapper.get('img').element).not.toBe(first);
    await wrapper.get('img').trigger('load'); expect(wrapper.get('img').classes()).toContain('is-loaded');
    await vi.advanceTimersByTimeAsync(10000); expect(wrapper.get('img').attributes('src')).toBe(src); expect(vi.getTimerCount()).toBe(0);
  });
  it('第二次失败显示加载失败，不无限重发，也不假设401或502原因', async () => {
    const wrapper = image(); await exhaust(wrapper);
    expect(wrapper.find('img').exists()).toBe(false); expect(wrapper.text()).toBe('图片加载失败');
    expect(wrapper.get('[role="img"]').attributes('aria-label')).toBe('图片加载失败');
    await vi.advanceTimersByTimeAsync(10000); expect(wrapper.find('img').exists()).toBe(false); expect(vi.getTimerCount()).toBe(0);
    expect(wrapper.text()).not.toMatch(/401|502|Cookie|令牌/u);
  });
  it('真正没有地址显示暂无图片，不调度重试', async () => {
    const wrapper = image({ src: '', alt: '封面' });
    expect(wrapper.text()).toBe('暂无图片'); expect(wrapper.find('img').exists()).toBe(false);
    await vi.advanceTimersByTimeAsync(10000); expect(vi.getTimerCount()).toBe(0);
  });
  it('保留原fallback顺序，只切换一次后显示失败，不循环回主地址', async () => {
    const wrapper = image({ src, fallback: '/fallback.jpg', alt: '海报' });
    await wrapper.get('img').trigger('error'); expect(wrapper.get('img').attributes('src')).toBe('/fallback.jpg');
    expect(vi.getTimerCount()).toBe(0);
    await wrapper.get('img').trigger('error'); expect(wrapper.text()).toBe('图片加载失败');
    expect(wrapper.get('[role="img"]').attributes('aria-label')).toBe('海报');
    await vi.advanceTimersByTimeAsync(10000); expect(wrapper.find('img').exists()).toBe(false);
    await wrapper.setProps({ src: '/new.jpg' }); expect(wrapper.get('img').attributes('src')).toBe('/new.jpg');
  });
  it('同URL fallback不能造成失败循环，仍只有一次自动重试', async () => {
    const wrapper = image({ src, fallback: src }); await exhaust(wrapper);
    expect(wrapper.text()).toBe('图片加载失败'); expect(vi.getTimerCount()).toBe(0);
  });
  it('换地址取消旧timer并恢复完整重试预算，不加载迟到的旧URL', async () => {
    const wrapper = image(); await wrapper.get('img').trigger('error');
    await wrapper.setProps({ src: '/next.jpg' }); expect(vi.getTimerCount()).toBe(0);
    await vi.advanceTimersByTimeAsync(1000); expect(wrapper.get('img').attributes('src')).toBe('/next.jpg');
    await wrapper.get('img').trigger('error'); await vi.advanceTimersByTimeAsync(500);
    expect(wrapper.get('img').attributes('src')).toBe('/next.jpg');
  });
  it('隐藏图片取消待执行timer，隐藏期间不重发，显示后恢复唯一剩余重试', async () => {
    const wrapper = image(); await wrapper.get('img').trigger('error');
    applyMediaVisibility(true); await flushPromises(); await vi.advanceTimersByTimeAsync(0); expect(wrapper.find('.image-hidden').exists()).toBe(true); expect(vi.getTimerCount()).toBe(0);
    await vi.advanceTimersByTimeAsync(10000); expect(wrapper.find('img').exists()).toBe(false);
    applyMediaVisibility(false); await flushPromises(); await vi.advanceTimersByTimeAsync(500);
    expect(wrapper.get('img').attributes('src')).toBe(src);
    await wrapper.get('img').trigger('error'); expect(wrapper.text()).toBe('图片加载失败'); expect(vi.getTimerCount()).toBe(0);
  });
  it('初始隐藏不创建图片或重试timer，组件卸载也取消timer', async () => {
    applyMediaVisibility(true); const wrapper = image(); await vi.advanceTimersByTimeAsync(0); expect(wrapper.find('img').exists()).toBe(false); expect(vi.getTimerCount()).toBe(0);
    applyMediaVisibility(false); await flushPromises(); await vi.advanceTimersByTimeAsync(0); await wrapper.get('img').trigger('error'); expect(vi.getTimerCount()).toBe(1);
    wrapper.unmount(); wrappers.pop(); expect(vi.getTimerCount()).toBe(0); await vi.advanceTimersByTimeAsync(10000);
  });
});

describe('JavDB 手动刷新同URL封面', () => {
  it('只有手动刷新成功才重挂载同URL，失败刷新保留卡片且不重发图片', async () => {
    const video: R18Video = { id: 'fixture', content_id: 'fixture', service_code: 'javdb', number: 'TEST-001', title: '测试影片', original_title: '', release_date: '', cover_url: src, images: [], duration: 0, score: 0, magnets_count: 0, has_local: false, tags: [], has_preview: false, preview_url: '' };
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 2, Name: '在线库', Path: '', SourceType: 'avdb_javdb' }], total: 1 });
    const read = vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [video], page: 1, page_size: 24, has_more: false, has_previous: false });
    const pinia = createPinia(); stores.push(pinia);
    const wrapper = mount(R18CatalogPage, { global: { plugins: [pinia], stubs: { RouterLink: { template: '<a><slot /></a>' } } } }); wrappers.push(wrapper);
    await flushPromises(); const first = wrapper.get('.r18-cover img').element;
    await exhaust(wrapper); expect(wrapper.get('.r18-cover').text()).toContain('图片加载失败');
    read.mockRejectedValueOnce(new Error('列表暂时不可用'));
    await wrapper.get('.r18-header button').trigger('click'); await flushPromises();
    expect(wrapper.find('.r18-cover img').exists()).toBe(false); expect(wrapper.get('.r18-cover').text()).toContain('图片加载失败');
    await wrapper.get('.r18-header button').trigger('click'); await flushPromises();
    const recovered = wrapper.get('.r18-cover img'); expect(recovered.attributes('src')).toBe(src); expect(recovered.element).not.toBe(first);
    await recovered.trigger('load'); const loaded = recovered.element;
    await wrapper.get('#r18-search').setValue('修改草稿，不重新请求');
    expect(wrapper.get('.r18-cover img').element).toBe(loaded);
    applyMediaVisibility(true); await flushPromises(); applyMediaVisibility(false); await flushPromises(); await vi.advanceTimersByTimeAsync(0);
    expect(wrapper.get('.r18-cover img').attributes('src')).toBe(src);
    expect(wrapper.get('.r18-cover').text()).not.toContain('图片加载失败'); expect(read).toHaveBeenCalledTimes(3);
    expect(wrapper.get('.r18-cover img').element).not.toBe(loaded); expect(vi.getTimerCount()).toBe(0);
  });
});
