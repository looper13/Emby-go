import { apiClient } from './client';
import { boolean, count, record, string } from './admin';
import { list } from './scrape';

const req = (path: string, method = 'GET', json?: unknown, signal?: AbortSignal) =>
  apiClient.request(`/api/admin/anime${path}`, { auth: 'required', method, json, signal });

export function tmdbMapping(v: unknown) {
  const r = record(v);
  return { episode_id: count(r.episode_id), series_id: count(r.series_id), season: count(r.season), number: count(r.number),
    name: string(r.name), tmdb_id: count(r.tmdb_id), revision: count(r.revision), stills: list(r.stills).map(string) };
}
export function tmdbSeriesMapping(v: unknown) {
  const r = record(v);
  return { series_id: count(r.series_id), name: string(r.name), revision: count(r.revision) };
}
export type TMDBMapping = ReturnType<typeof tmdbMapping>;
export type TMDBSeriesMapping = ReturnType<typeof tmdbSeriesMapping>;
function config(v: unknown) { const r = record(v); return { configured: boolean(r.configured), revision: count(r.revision) }; }
function series(v: unknown) {
  const r = record(v);
  return { id: count(r.id), name: string(r.name), original_name: string(r.original_name), date: string(r.first_air_date), overview: string(r.overview) };
}
export const artworkApi = {
  async progress(signal?: AbortSignal) {
    const r = record(await req('/artwork', 'GET', undefined, signal)), j = r.job == null ? null : record(r.job);
    return { available: boolean(r.available), job: j == null ? null : { running: boolean(j.running), total: count(j.total), completed: count(j.completed), missing: count(j.missing), failed: count(j.failed), status: string(j.status), error: string(j.error) } };
  },
  start: (subject_ids: number[], people: boolean) => req('/artwork', 'POST', { subject_ids, people }),
  cancel: () => req('/artwork/cancel', 'POST'),
};
export const tmdbApi = {
  test: () => req('/tmdb/test','POST'),
  async config(signal?: AbortSignal) { return config(await req('/tmdb/settings', 'GET', undefined, signal)); },
  async saveConfig(api_key: string, clear: boolean, revision: number) { return config(await req('/tmdb/settings', 'PUT', { api_key, clear, revision })); },
  async search(q: string, signal?: AbortSignal) {
    const r = record(await req(`/tmdb/search?q=${encodeURIComponent(q)}`, 'GET', undefined, signal)); return list(r.items).map(series);
  },
  async series(id: number, signal?: AbortSignal) {
    const r = record(await req(`/tmdb/series/${id}`, 'GET', undefined, signal));
    return { ...series(r), seasons: list(r.seasons).map(v => { const s = record(v); return { number: count(s.season_number), name: string(s.name) }; }) };
  },
  async season(id: number, season: number, signal?: AbortSignal) {
    const r = record(await req(`/tmdb/series/${id}/seasons/${season}`, 'GET', undefined, signal));
    return list(r.episodes).map(v => { const e = record(v); return { id: count(e.id), number: count(e.episode_number), name: string(e.name), date: string(e.air_date), overview: string(e.overview) }; });
  },
  mapSeries: (subject: number, series_id: number, revision: number) => req(`/subjects/${subject}/tmdb`, 'PUT', { series_id, revision }),
  mapEpisode: (mapping: TMDBMapping, series_revision: number, season: number, number: number, remove = false) =>
    req(`/episodes/${mapping.episode_id}/tmdb`, remove ? 'DELETE' : 'PUT', { revision: mapping.revision, series_revision, season, number }),
};
