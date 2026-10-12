import { apiClient } from './client';
import { boolean, count, record, string } from './admin';
import { list } from './scrape';

export type ActorStatus = 'all' | 'missing' | 'ready';
export interface ActorEntry { name: string; movie_count: number; has_image: boolean; image_tag: string }
export interface ActorsResult { items: ActorEntry[]; total: number; stats: { total: number; ready: number; missing: number } }
export function parseActors(value: unknown): ActorsResult {
  const v = record(value), stats = record(v.stats);
  return {
    items: list(v.items).map(item => { const a = record(item); return { name: string(a.name), movie_count: count(a.movie_count), has_image: boolean(a.has_image), image_tag: string(a.image_tag) }; }),
    total: count(v.total), stats: { total: count(stats.total), ready: count(stats.ready), missing: count(stats.missing) },
  };
}
export const actorsApi = {
  async list(query: { library_id: number; search: string; status: ActorStatus; offset: number; limit: number }, signal?: AbortSignal) {
    const params = new URLSearchParams(Object.entries(query).map(([key, value]) => [key, String(value)]));
    return parseActors(await apiClient.request(`/api/admin/actors?${params}`, { auth: 'required', signal }));
  },
};
