import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia';
import { catalogApi } from '../src/api/catalog';
import { useMediaWallStore } from '../src/stores/media-wall';
import { parseDetail, parseWall, type MovieDto } from '../src/api/contracts';
import { apiClient } from '../src/api/client';
import wallFixture from './fixtures/json/items-wall.json';
import detailFixture from './fixtures/json/item-detail.json';
function batch(offset: number, count = 100) {
  return { total: 350, image_tags: {}, items: Array.from({ length: count }, (_, i) => ({ ...detailFixture.movie, id: offset + i + 1 } as MovieDto)) };
}
let pinia: Pinia;
beforeEach(() => { pinia = createPinia(); setActivePinia(pinia); });
afterEach(() => { disposePinia(pinia); vi.useRealTimers(); });
describe('media wall state and request ownership', () => {
  it('invalidates on successful short writes including 204, but not reads or failed writes', async () => {
    const wall = useMediaWallStore();
    apiClient.setAuthHandlers({ getToken: () => 'test', onUnauthorized: () => {} });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(new Response('{}'))
      .mockResolvedValueOnce(new Response('{"error":"failed"}', { status: 409 }))
      .mockResolvedValueOnce(new Response(null, { status: 204 })));
    try {
      await apiClient.request('/api/admin/status', { auth: 'required' }); expect(wall.dirty).toBe(false);
      await expect(apiClient.request('/api/admin/items/5', { auth: 'required', method: 'DELETE' })).rejects.toThrow(); expect(wall.dirty).toBe(false);
      await apiClient.request('/api/admin/items/5', { auth: 'required', method: 'DELETE' }); expect(wall.dirty).toBe(true);
    } finally { vi.unstubAllGlobals(); }
  });
  it('consumes the real mixed-case fixtures and rejects malformed responses', () => {
    expect(parseWall(wallFixture).items.length).toBe(100);
    expect(parseDetail(detailFixture).movie.id).toBe(5);
    expect(parseWall({ items: null, total: 0 }).items).toEqual([]);
    for (const invalid of [null, { items: [], total: -1 }, { items: [{}], total: 1 }, { ...wallFixture, image_tags: { 1: { Primary: 2 } } }]) expect(() => parseWall(invalid)).toThrow();
    expect(() => parseDetail({ ...detailFixture, movie: { ...detailFixture.movie, id: Number.MAX_SAFE_INTEGER + 1 } })).toThrow();
  });
  it('restores 300 entries immediately, including dirty, expired and failed snapshots', async () => {
    const api = vi.spyOn(catalogApi, 'items').mockImplementation(async q => batch(Number(q.get('offset'))));
    const wall = useMediaWallStore(); const query = new URLSearchParams('status=success');
    wall.prepare(query, false); const signal = new AbortController().signal;
    for (let i = 0; i < 3; i++) await wall.loadMore(signal);
    expect(wall.items).toHaveLength(300); expect(api).toHaveBeenCalledTimes(3);
    expect(wall.prepare(query, true)).toBe(true); expect(api).toHaveBeenCalledTimes(3);
    wall.dirty = true; expect(wall.prepare(query, true)).toBe(true);
    expect(wall.items).toHaveLength(300); expect(wall.total).toBe(350);
    expect(wall.needsRefresh()).toBe(true);
    await wall.refresh(signal); expect(wall.dirty).toBe(false);
    wall.loadedAt = Date.now() - 60_000; expect(wall.prepare(query, true)).toBe(true);
    expect(wall.items).toHaveLength(300); expect(wall.needsRefresh()).toBe(true);
    wall.error = 'failure'; expect(wall.prepare(query, true)).toBe(true);
    expect(wall.items).toHaveLength(300);
  });
  it('publishes a refreshed snapshot only after every previously loaded page is ready', async () => {
    const api = vi.spyOn(catalogApi, 'items').mockImplementation(async q => batch(Number(q.get('offset'))));
    const wall = useMediaWallStore(), signal = new AbortController().signal;
    wall.prepare(new URLSearchParams(), false);
    for (let i = 0; i < 3; i++) await wall.loadMore(signal);
    const previous = wall.items;
    let finish!: (value: ReturnType<typeof batch>) => void;
    api.mockImplementationOnce(async () => ({ ...batch(1000), total: 301 }))
      .mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }))
      .mockResolvedValueOnce({ ...batch(1200), total: 301 });
    const refreshing = wall.refresh(signal);
    await vi.waitFor(() => expect(finish).toBeTypeOf('function'));
    expect(wall.refreshing).toBe(true); expect(wall.items).toBe(previous); expect(wall.total).toBe(350);
    expect(await wall.loadMore(signal)).toBe(false);
    finish({ ...batch(1100), total: 301 });
    expect(await refreshing).toBe(true);
    expect(wall.items).toHaveLength(300); expect(wall.items[0]?.id).toBe(1001); expect(wall.total).toBe(301);
    expect(wall.refreshing).toBe(false);
  });
  it('retains the whole snapshot after refresh failure and retries from the first page', async () => {
    const api = vi.spyOn(catalogApi, 'items').mockImplementation(async q => batch(Number(q.get('offset'))));
    const wall = useMediaWallStore(), signal = new AbortController().signal;
    wall.prepare(new URLSearchParams(), false);
    await wall.loadMore(signal); await wall.loadMore(signal);
    const previous = wall.items;
    api.mockResolvedValueOnce(batch(1000)).mockRejectedValueOnce(new Error('offline'));
    expect(await wall.refresh(signal)).toBe(false);
    expect(wall.items).toBe(previous); expect(wall.total).toBe(350); expect(wall.refreshError).toBe('offline');
    const calls = api.mock.calls.length;
    expect(await wall.loadMore(signal)).toBe(false); expect(api).toHaveBeenCalledTimes(calls);
    api.mockResolvedValueOnce({ ...batch(2000, 2), total: 2 });
    expect(await wall.refresh(signal)).toBe(true);
    expect(api.mock.lastCall![0].get('offset')).toBe('0');
    expect(wall.items).toHaveLength(2); expect(wall.total).toBe(2); expect(wall.done).toBe(true); expect(wall.refreshError).toBe('');
  });
  it('cancels background replacement without losing the snapshot or accepting late results', async () => {
    const api = vi.spyOn(catalogApi, 'items').mockResolvedValue(batch(0));
    const wall = useMediaWallStore(), query = new URLSearchParams('sort=random'), signal = new AbortController().signal;
    wall.prepare(query, false); await wall.loadMore(signal);
    let finish!: (value: ReturnType<typeof batch>) => void;
    api.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
    wall.dirty = true; const refreshing = wall.refresh(signal);
    wall.cancel(); expect(wall.items).toHaveLength(100); expect(wall.dirty).toBe(true);
    wall.prepare(new URLSearchParams('search=new'), false);
    api.mockResolvedValueOnce({ ...batch(500, 1), total: 1 }); await wall.loadMore(signal);
    finish(batch(1000)); await refreshing;
    expect(wall.items.map(item => item.id)).toEqual([501]); expect(wall.total).toBe(1); expect(wall.dirty).toBe(false);
  });
  it('ignores late results and errors even if the transport ignores cancellation', async () => {
    let resolve!: (v: ReturnType<typeof batch>) => void;
    const api = vi.spyOn(catalogApi, 'items').mockImplementationOnce(() => new Promise(r => { resolve = r; }))
      .mockResolvedValueOnce(batch(200));
    const wall = useMediaWallStore(); const signal = new AbortController().signal;
    wall.prepare(new URLSearchParams('search=old'), false); const old = wall.loadMore(signal);
    const oldSignal = api.mock.calls[0]![1]!;
    wall.prepare(new URLSearchParams('search=new'), false); expect(oldSignal.aborted).toBe(true);
    await wall.loadMore(signal); resolve(batch(0)); await old;
    expect(wall.items[0]?.id).toBe(201); expect(wall.items).toHaveLength(100); expect(wall.error).toBe('');
  });
  it('locks duplicate loads and stops after failure until explicit retry', async () => {
    let reject!: (e: Error) => void;
    const api = vi.spyOn(catalogApi, 'items').mockImplementationOnce(() => new Promise((_r, j) => { reject = j; })).mockResolvedValueOnce(batch(0));
    const wall = useMediaWallStore(); const signal = new AbortController().signal;
    wall.prepare(new URLSearchParams(), false); const first = wall.loadMore(signal);
    expect(await wall.loadMore(signal)).toBe(false); expect(api).toHaveBeenCalledTimes(1);
    reject(new Error('offline')); await first;
    expect(await wall.loadMore(signal)).toBe(false); expect(api).toHaveBeenCalledTimes(1);
    await wall.loadMore(signal, true); expect(api).toHaveBeenCalledTimes(2); expect(wall.error).toBe('');
  });
  it('keeps a partially loaded snapshot from publishing after leaving the page or logout', async () => {
    let resolve!: (v: ReturnType<typeof batch>) => void;
    vi.spyOn(catalogApi, 'items').mockImplementation(() => new Promise(r => { resolve = r; }));
    const wall = useMediaWallStore(); wall.prepare(new URLSearchParams(), false);
    const controller = new AbortController(); const load = wall.loadMore(controller.signal);
    controller.abort(); wall.clear(); resolve(batch(0)); await load;
    expect(wall.items).toEqual([]); expect(wall.loading).toBe(false); expect(wall.loadedAt).toBe(0);
  });
  it('forces 100-entry batches and derives default sort order from the URL', async () => {
    const api = vi.spyOn(catalogApi, 'items').mockResolvedValue({ ...batch(0, 0), total: 0 });
    const wall = useMediaWallStore(); wall.prepare(new URLSearchParams('sort=title&limit=9000&offset=99'), false);
    await wall.loadMore(new AbortController().signal);
    const q = api.mock.calls[0]![0]; expect(q.get('limit')).toBe('100'); expect(q.get('offset')).toBe('0'); expect(q.get('order')).toBe('asc');
    expect(wall.done).toBe(true);
  });
  it.each(['datecreated', 'title', 'year', 'communityrating', 'date'])('keeps explicit %s directions across pagination and restored refreshes', async sort => {
    const api = vi.spyOn(catalogApi, 'items').mockImplementation(async q => batch(Number(q.get('offset'))));
    const wall = useMediaWallStore(), signal = new AbortController().signal;
    for (const order of ['asc', 'desc']) {
      const query = new URLSearchParams({ sort, order, tag: 'keep' });
      expect(wall.prepare(query, true)).toBe(false);
      expect(wall.items).toEqual([]);
      api.mockClear();
      await wall.loadMore(signal); await wall.loadMore(signal);
      expect(wall.prepare(query, true)).toBe(true);
      await wall.refresh(signal);
      expect(api.mock.calls.map(([q]) => q.get('offset'))).toEqual(['0', '100', '0', '100']);
      for (const [q] of api.mock.calls) {
        expect(q.get('sort')).toBe(sort); expect(q.get('order')).toBe(order); expect(q.get('tag')).toBe('keep');
        expect(q.has('seed')).toBe(false);
      }
    }
  });
  it('keeps random pagination and restored snapshots on the same seed, and reshuffles on a fresh browse', async () => {
    vi.spyOn(crypto, 'getRandomValues').mockReturnValueOnce(new Uint32Array([12345])).mockReturnValueOnce(new Uint32Array([98765]));
    const api = vi.spyOn(catalogApi, 'items').mockImplementation(async q => batch(Number(q.get('offset'))));
    const wall = useMediaWallStore(); const query = new URLSearchParams('sort=random&tag=keep');
    const signal = new AbortController().signal;
    wall.prepare(query, false);
    await wall.loadMore(signal); await wall.loadMore(signal);
    expect(api.mock.calls.slice(0, 2).map(([q]) => q.get('seed'))).toEqual(['12345', '12345']);
    expect(api.mock.calls.slice(0, 2).map(([q]) => q.get('offset'))).toEqual(['0', '100']);
    expect(wall.prepare(query, true)).toBe(true);
    wall.loadedAt = Date.now() - 60_000;
    expect(wall.prepare(query, true)).toBe(true);
    await wall.refresh(signal);
    expect(api.mock.lastCall![0].get('seed')).toBe('12345');
    wall.clear(); wall.prepare(query, true); await wall.loadMore(signal);
    expect(api.mock.lastCall![0].get('seed')).toBe('98765');
    expect(api.mock.lastCall![0].get('tag')).toBe('keep');
  });
  it('updates playback and favorite fields in place without invalidating an unfiltered wall', async () => {
    vi.spyOn(catalogApi, 'items').mockResolvedValue({ ...batch(0), userdata: { '1': { favorite: false, played: false, position_ticks: 0 } } });
    const wall = useMediaWallStore(), signal = new AbortController().signal;
    wall.prepare(new URLSearchParams(), false); await wall.loadMore(signal);
    const previous = wall.items;
    apiClient.setAuthHandlers({ getToken: () => 'test', onUnauthorized: () => {} });
    vi.stubGlobal('fetch', vi.fn(async () => new Response(null, { status: 204 })));
    try {
      await apiClient.request('/Sessions/Playing/Progress', { auth: 'required', method: 'POST', json: { ItemId: '1', PositionTicks: 12300000 } });
      await apiClient.request('/Users/1/PlayedItems/1', { auth: 'required', method: 'POST' });
      await apiClient.request('/Users/1/FavoriteItems/1', { auth: 'required', method: 'POST' });
      expect(wall.items).toBe(previous); expect(wall.total).toBe(350); expect(wall.dirty).toBe(false);
      expect(wall.userdata['1']).toEqual({ position_ticks: 12300000, played: true, favorite: true });
      expect(wall.prepare(new URLSearchParams(), true)).toBe(true); expect(wall.needsRefresh()).toBe(false);
      await apiClient.request('/Users/1/PlayedItems/1', { auth: 'required', method: 'DELETE' });
      expect(wall.userdata['1']?.played).toBe(false);
      await apiClient.request('/Users/1/FavoriteItems/person%3Aexample', { auth: 'required', method: 'POST' });
      expect(wall.dirty).toBe(true);
    } finally { vi.unstubAllGlobals(); }
  });
  it('does not restore another filter and cannot republish a refresh after the account is cleared', async () => {
    const api = vi.spyOn(catalogApi, 'items').mockResolvedValue(batch(0));
    const wall = useMediaWallStore(), signal = new AbortController().signal;
    wall.prepare(new URLSearchParams('status=success'), false); await wall.loadMore(signal);
    expect(wall.prepare(new URLSearchParams('status=pending'), true)).toBe(false);
    expect(wall.items).toEqual([]); expect(wall.total).toBe(0);
    await wall.loadMore(signal);
    let finish!: (value: ReturnType<typeof batch>) => void;
    api.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
    const refreshing = wall.refresh(signal);
    wall.clear(); finish(batch(1000)); await refreshing;
    expect(wall.items).toEqual([]); expect(wall.userdata).toEqual({}); expect(wall.total).toBe(0);
    expect(wall.refreshing).toBe(false); expect(wall.dirty).toBe(false);
  });
  it('revalidates favorite membership, and preserves newer userdata and invalidations during refresh', async () => {
    const api = vi.spyOn(catalogApi, 'items').mockResolvedValue({ ...batch(0), userdata: { '1': { favorite: true, played: false, position_ticks: 0 } } });
    const wall = useMediaWallStore(), signal = new AbortController().signal;
    wall.prepare(new URLSearchParams('favorite=true'), false); await wall.loadMore(signal);
    apiClient.setAuthHandlers({ getToken: () => 'test', onUnauthorized: () => {} });
    vi.stubGlobal('fetch', vi.fn(async () => new Response(null, { status: 204 })));
    try {
      await apiClient.request('/Users/1/FavoriteItems/1', { auth: 'required', method: 'DELETE' });
      expect(wall.dirty).toBe(true); expect(wall.userdata['1']?.favorite).toBe(false);
      let finish!: (value: ReturnType<typeof batch>) => void;
      api.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
      const refreshing = wall.refresh(signal);
      await apiClient.request('/Sessions/Playing/Stopped', { auth: 'required', method: 'POST', json: { ItemId: '1', PositionTicks: 999 } });
      await apiClient.request('/api/admin/items/1/reread', { auth: 'required', method: 'POST' });
      finish(batch(0)); await refreshing;
      expect(wall.userdata['1']?.position_ticks).toBe(999); expect(wall.dirty).toBe(true);
    } finally { vi.unstubAllGlobals(); }
  });
  it('uses newest premiere dates first and removes random seeds from nonrandom requests', async () => {
    const api = vi.spyOn(catalogApi, 'items').mockResolvedValue(batch(0));
    const wall = useMediaWallStore(); wall.prepare(new URLSearchParams('sort=date&seed=12'), false);
    await wall.loadMore(new AbortController().signal);
    expect(api.mock.lastCall![0].get('sort')).toBe('date');
    expect(api.mock.lastCall![0].get('order')).toBe('desc');
    expect(api.mock.lastCall![0].has('seed')).toBe(false);
  });
});
