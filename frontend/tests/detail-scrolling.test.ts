import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import SimilarItems from '../src/components/media/SimilarItems.vue';

const items = Array.from({ length: 12 }, (_, index) => ({ Id: String(index + 1), Name: `Movie ${index + 1}` }));
const cleanups: (() => void)[] = [];
afterEach(() => { cleanups.splice(0).reverse().forEach(cleanup => cleanup()); vi.unstubAllGlobals(); });

function gallery(width = 400, contentWidth = 1738) {
  let resize!: () => void;
  const disconnect = vi.fn();
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false })));
  vi.stubGlobal('ResizeObserver', class {
    constructor(callback: () => void) { resize = callback; }
    observe = vi.fn();
    disconnect = disconnect;
  });
  const wrapper = mount(SimilarItems, { props: { items }, attachTo: document.body });
  cleanups.push(() => wrapper.unmount());
  function measure() {
    const element = wrapper.find('.similar-row').element as HTMLDivElement;
    Object.defineProperties(element, {
      clientWidth: { configurable: true, get: () => width },
      scrollWidth: { configurable: true, get: () => contentWidth },
    });
    element.scrollBy = vi.fn();
    return element;
  }
  const row = measure();
  return { wrapper, row, measure, resize: () => resize(), disconnect, setWidth: (value: number) => { width = value; resize(); } };
}

describe('detail recommendations scrolling', () => {
  it('shows navigation only for overflow and disables the arrow at each end', async () => {
    const { wrapper, row, resize } = gallery(); resize(); await nextTick();
    expect(wrapper.find('.similar-nav').exists()).toBe(true);
    expect(wrapper.find('[aria-label="上一组相似影片"]').attributes('disabled')).toBeDefined();
    expect(wrapper.find('[aria-label="下一组相似影片"]').attributes('disabled')).toBeUndefined();
    row.scrollLeft = row.scrollWidth - row.clientWidth; await wrapper.find('.similar-row').trigger('scroll');
    expect(wrapper.find('[aria-label="下一组相似影片"]').attributes('disabled')).toBeDefined();
    expect(wrapper.find('[aria-label="上一组相似影片"]').attributes('disabled')).toBeUndefined();
  });

  it('scrolls in both directions without opening a recommendation', async () => {
    const { wrapper, row, resize } = gallery(); resize(); await nextTick();
    await wrapper.find('[aria-label="下一组相似影片"]').trigger('click');
    expect(row.scrollBy).toHaveBeenLastCalledWith({ left: 400, behavior: 'smooth' });
    row.scrollLeft = 400; await wrapper.find('.similar-row').trigger('scroll');
    await wrapper.find('[aria-label="上一组相似影片"]').trigger('click');
    expect(row.scrollBy).toHaveBeenLastCalledWith({ left: -400, behavior: 'smooth' });
    expect(wrapper.emitted('open')).toBeUndefined();
    await wrapper.find('.similar-card').trigger('click'); expect(wrapper.emitted('open')).toEqual([['1']]);
  });

  it('updates navigation when the container resizes and disconnects on unmount', async () => {
    const { wrapper, resize, setWidth, disconnect } = gallery(); resize(); await nextTick();
    expect(wrapper.find('.similar-nav').exists()).toBe(true);
    setWidth(2000); await nextTick(); expect(wrapper.find('.similar-nav').exists()).toBe(false);
    setWidth(390); await nextTick(); expect(wrapper.find('.similar-nav').exists()).toBe(true);
    disconnect.mockClear();
    wrapper.unmount(); expect(disconnect).toHaveBeenCalledOnce();
  });

  it('handles empty recommendations followed by asynchronously loaded items', async () => {
    const { wrapper, resize, measure } = gallery();
    await wrapper.setProps({ items: [] }); expect(wrapper.find('.similar-row').exists()).toBe(false);
    await wrapper.setProps({ items }); measure(); resize(); await nextTick();
    expect(wrapper.findAll('.similar-card')).toHaveLength(12);
    expect(wrapper.find('.similar-nav').exists()).toBe(true);
  });

  it('respects the reduced motion preference when scrolling', async () => {
    const { wrapper, row, resize } = gallery(); resize(); await nextTick();
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true })));
    await wrapper.find('[aria-label="下一组相似影片"]').trigger('click');
    expect(row.scrollBy).toHaveBeenLastCalledWith({ left: 400, behavior: 'instant' });
  });
});
