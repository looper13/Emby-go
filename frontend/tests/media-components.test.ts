import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { createPinia, disposePinia, setActivePinia } from 'pinia';
import MediaCard from '../src/components/media/MediaCard.vue';
import MediaImage from '../src/components/media/MediaImage.vue';
import { useToastStore } from '../src/stores/toasts';
import { readViewPosition, saveViewPosition } from '../src/router/view-history';
import { imageURL } from '../src/lib/image-url';
import fixture from '../../vue-migration/fixtures/json/item-detail.json';
afterEach(() => vi.useRealTimers());
describe('declarative media components', () => {
  it('activates cards by click, Enter and Space while excluding nested action keys', async () => {
    const wrapper = mount(MediaCard, { props: { movie: fixture.movie } });
    await wrapper.trigger('keydown', { key: 'Enter' }); await wrapper.trigger('keydown', { key: ' ' }); await wrapper.trigger('click');
    expect(wrapper.emitted('open')).toEqual([[5], [5], [5]]);
    // P4 adds buttons here; the card must already ignore bubbled keys.
    await wrapper.find('strong').trigger('keydown', { key: 'Enter' });
    expect(wrapper.emitted('open')).toHaveLength(3); wrapper.unmount();
  });
  it('falls back once, then preserves a placeholder and resets for a new source', async () => {
    const wrapper = mount(MediaImage, { props: { src: '/broken', fallback: '/cover', alt: '海报' } });
    await wrapper.find('img').trigger('error'); expect(wrapper.find('img').attributes('src')).toBe('/cover');
    await wrapper.find('img').trigger('error'); expect(wrapper.find('[role=img]').attributes('aria-label')).toBe('海报');
    await wrapper.setProps({ src: '/new' }); expect(wrapper.find('img').attributes('src')).toBe('/new'); wrapper.unmount();
  });
  it('retains versioned thumbnail parameter order and original image links', () => {
    expect(imageURL(5, 'Primary', 320, 'version')).toBe('/Items/5/Images/Primary?tag=version&maxWidth=320');
    expect(imageURL(5, 'Primary')).toBe('/Items/5/Images/Primary');
  });
});
describe('history and notifications', () => {
  it('preserves Router-owned history fields in a separate validated namespace', () => {
    const original = { back: '/items', current: '/item/5', forward: null, position: 4, replaced: false, scroll: null };
    window.history.replaceState(original, '');
    const view = { scrollY: 1234, wallCount: 300, focusID: '5' }; saveViewPosition(view);
    expect(window.history.state).toEqual({ ...original, __embyView: view });
    expect(readViewPosition(window.history.state.__embyView)).toEqual(view);
    for (const invalid of [null, {}, { ...view, scrollY: NaN }, { ...view, wallCount: -1 }]) expect(readViewPosition(invalid)).toBeNull();
  });
  it('animates notification expiry and clears timers on disposal', async () => {
    vi.useFakeTimers(); const pinia = createPinia(); setActivePinia(pinia); const toasts = useToastStore();
    toasts.show('done'); await vi.advanceTimersByTimeAsync(3200); expect(toasts.items[0]?.out).toBe(true);
    await vi.advanceTimersByTimeAsync(260); expect(toasts.items).toEqual([]);
    toasts.show('pending'); disposePinia(pinia); expect(vi.getTimerCount()).toBe(0);
  });
});
