import { afterEach, expect, it, vi } from 'vitest';
import { apiClient } from '../src/api/client';
import { tmdbApi, tmdbMapping } from '../src/api/anime-artwork';

afterEach(() => vi.restoreAllMocks());
it('writes API keys without keeping secret response fields', async () => {
  const request = vi.spyOn(apiClient, 'request').mockResolvedValue({ configured: true, revision: 2, api_key: 'must-not-survive' });
  const cfg = await tmdbApi.saveConfig('fixture-key', false, 1);
  expect(cfg).toEqual({ configured: true, revision: 2 });
  expect(request).toHaveBeenCalledWith('/api/admin/anime/tmdb/settings', expect.objectContaining({ method: 'PUT', json: { api_key: 'fixture-key', clear: false, revision: 1 } }));
});
it('preserves explicit specials mapping and an empty still list', () => {
  const mapping = tmdbMapping({ episode_id: 10, series_id: 20, season: 0, number: 3, name: 'Special', tmdb_id: 30, revision: 1, stills: [] });
  expect(mapping.season).toBe(0);
  expect(mapping.stills).toEqual([]);
  expect(() => tmdbMapping({ ...mapping, stills: ['image', 3] })).toThrow();
});
