import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia';
import { catalogApi } from '../src/api/catalog';
import { useMediaWallStore } from '../src/stores/media-wall';
import { parseDetail, parseWall, type MovieDto } from '../src/api/contracts';
import { apiClient } from '../src/api/client';
import wallFixture from '../../vue-migration/fixtures/json/items-wall.json';
import detailFixture from '../../vue-migration/fixtures/json/item-detail.json';
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
  it('restores 300 entries without pagination, but dirty/expired/failed snapshots reload', async () => {
    const api = vi.spyOn(catalogApi, 'items').mockImplementation(async q => batch(Number(q.get('offset'))));
    const wall = useMediaWallStore(); const query = new URLSearchParams('status=success');
    wall.prepare(query, false); const signal = new AbortController().signal;
    for (let i = 0; i < 3; i++) await wall.loadMore(signal);
    expect(wall.items).toHaveLength(300); expect(api).toHaveBeenCalledTimes(3);
    expect(wall.prepare(query, true)).toBe(true); expect(api).toHaveBeenCalledTimes(3);
    wall.dirty = true; expect(wall.prepare(query, true)).toBe(false);
    for (let i = 0; i < 3; i++) await wall.loadMore(signal);
    expect(wall.items).toHaveLength(300);
    wall.loadedAt = Date.now() - 60_000; expect(wall.prepare(query, true)).toBe(false);
    await wall.loadMore(signal); wall.error = 'failure'; expect(wall.prepare(query, true)).toBe(false);
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
});
