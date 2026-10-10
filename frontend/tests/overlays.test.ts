import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia';
import * as playerLibrary from '../src/lib/player';
import type { PlayerHandle } from '../src/lib/player';
import PlayerDialog from '../src/components/media/PlayerDialog.vue';
import ScrapePreviewDialog from '../src/components/scrape/ScrapePreviewDialog.vue';
import { scrapeApi, type Preview } from '../src/api/scrape';
import { useScrapePreviewStore } from '../src/stores/scrape-preview';

// GL-07/GL-08: top-layer order, Escape arbitration and backdrop-only closing.
let pinia: Pinia;
const cleanups: (() => void)[] = [];
beforeEach(() => { pinia = createPinia(); setActivePinia(pinia); });
afterEach(() => { cleanups.splice(0).reverse().forEach(fn => fn()); disposePinia(pinia); vi.restoreAllMocks(); });

function handle(): PlayerHandle {
  return { url: '/direct', muted: false, on: vi.fn(), play: vi.fn(), destroy: vi.fn() };
}
function mountPlayer() {
  vi.spyOn(playerLibrary, 'loadPlayerFactory').mockResolvedValue(() => handle());
  const w = mount(PlayerDialog, {
    props: { options: { title: '播放', url: '/direct', type: 'mp4' } },
    attachTo: document.body, global: { plugins: [pinia] },
  });
  cleanups.push(() => w.unmount());
  return w;
}

describe('overlay top layer', () => {
  it('Escape closes the player without reaching the page-level back handler', async () => {
    const page = vi.fn(); window.addEventListener('keydown', page);
    cleanups.push(() => window.removeEventListener('keydown', page));
    const w = mountPlayer(); await flushPromises();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    expect(w.emitted('close')).toHaveLength(1);
    expect(page).not.toHaveBeenCalled();
  });

  it('a player underneath the scrape preview leaves Escape to the preview', async () => {
    const overlay = document.createElement('div'); overlay.id = 'scrape-overlay'; document.body.append(overlay);
    cleanups.push(() => overlay.remove());
    const w = mountPlayer(); await flushPromises();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    expect(w.emitted('close')).toBeUndefined();
  });

  it('closes the player by backdrop click and ignores clicks inside its shell', async () => {
    const w = mountPlayer(); await flushPromises();
    await w.get('.player-shell').trigger('click');
    expect(w.emitted('close')).toBeUndefined();
    await w.get('#player-overlay').trigger('click');
    expect(w.emitted('close')).toHaveLength(1);
  });

  it('closes the scrape preview by backdrop click and ignores clicks inside its modal', async () => {
    const preview: Preview = { movie_id: 5, query: 'ABF-005', expected_number: '', recommended: -1, candidates: [] };
    vi.spyOn(scrapeApi, 'preview').mockResolvedValue(preview);
    const write = vi.spyOn(scrapeApi, 'confirm');
    const store = useScrapePreviewStore();
    await store.open(5, vi.fn());
    const w = mount(ScrapePreviewDialog, { attachTo: document.body, global: { plugins: [pinia] } });
    cleanups.push(() => w.unmount());
    await w.get('.scrape-modal').trigger('click');
    expect(store.current).not.toBeNull();
    await w.get('#scrape-overlay').trigger('click');
    expect(store.current).toBeNull();
    expect(write).not.toHaveBeenCalled();
  });
});