import { apiClient } from './client';
import { boolean, count, invalid, record, string } from './admin';
export const optionalString = (v: unknown) => v == null ? '' : string(v);
export const list = (v: unknown): unknown[] => v == null ? [] : Array.isArray(v) ? v : invalid();
const finite = (v: unknown): number => typeof v === 'number' && Number.isFinite(v) ? v : invalid();
export interface TranslateSettings { title: boolean; summary: boolean; target_lang: string; api_url: string; api_key: string; timeout_seconds: number; configured?: boolean }
export interface ScrapeSettings { metatube_url: string; metatube_token: string; timeout_seconds: number; concurrency: number; image_quality: number; avatars_dir: string; download_images: boolean; overwrite: boolean; configured?: boolean; translate: TranslateSettings }
export interface Candidate { provider: string; id: string; number: string; title: string; score: number; thumb: string; exact: boolean }
export interface Preview { movie_id: number; query: string; expected_number: string; recommended: number; candidates: Candidate[] }
export interface FieldDiff { key: string; label: string; old: string; next: string; change: boolean }
export interface Inspect { provider: string; id: string; title: string; number: string; summary: string; poster: string; score: number; release_date: string; runtime: number; overwrite: boolean; fields: FieldDiff[]; images: { name: string; preview: string; exists: boolean }[] }
export function parseScrapeSettings(value: unknown): ScrapeSettings {
  const v = record(value), t = record(v.translate);
  return { metatube_url: string(v.metatube_url), metatube_token: string(v.metatube_token), timeout_seconds: count(v.timeout_seconds), concurrency: count(v.concurrency), image_quality: count(v.image_quality), avatars_dir: string(v.avatars_dir), download_images: boolean(v.download_images), overwrite: boolean(v.overwrite), configured: boolean(v.configured),
    translate: { title: boolean(t.title), summary: boolean(t.summary), target_lang: string(t.target_lang), api_url: string(t.api_url), api_key: string(t.api_key), timeout_seconds: count(t.timeout_seconds), configured: boolean(t.configured) } };
}
export function parsePreview(value: unknown): Preview {
  const v = record(value); const recommended = finite(v.recommended); if (!Number.isInteger(recommended) || recommended < -1) return invalid();
  const candidates = list(v.candidates).map(item => { const c = record(item); return { provider: string(c.provider), id: string(c.id), number: string(c.number), title: string(c.title), score: finite(c.score), thumb: optionalString(c.thumb), exact: boolean(c.exact) }; });
  if (recommended >= candidates.length) return invalid();
  return { movie_id: count(v.movie_id), query: string(v.query), expected_number: optionalString(v.expected_number), recommended, candidates };
}
export function parseInspect(value: unknown): Inspect {
  const v = record(value); return { provider: string(v.provider), id: string(v.id), title: string(v.title), number: string(v.number), summary: string(v.summary), poster: string(v.poster), score: finite(v.score), release_date: optionalString(v.release_date), runtime: v.runtime == null ? 0 : count(v.runtime), overwrite: boolean(v.overwrite),
    fields: list(v.fields).map(item => { const f = record(item); return { key: string(f.key), label: string(f.label), old: string(f.old), next: string(f.next), change: boolean(f.change) }; }),
    images: list(v.images).map(item => { const i = record(item); return { name: string(i.name), preview: optionalString(i.preview), exists: boolean(i.exists) }; }) };
}
const request = (path: string, method = 'GET', json?: unknown, signal?: AbortSignal) => apiClient.request(`/api/admin/${path}`, { auth: 'required', method, json, signal, readOnly: path.endsWith('/inspect') || path === 'scrape/test' });
export const scrapeApi = {
  settings: async (signal?: AbortSignal) => parseScrapeSettings(await request('scrape/settings', 'GET', undefined, signal)),
  save: async (json: ScrapeSettings) => parseScrapeSettings(await request('scrape/settings', 'PUT', json)),
  async test(target: 'metatube' | 'translate') { const v = record(await request('scrape/test', 'POST', { target })); return { ok: boolean(v.ok), detail: optionalString(v.detail), error: optionalString(v.error), elapsed_ms: v.elapsed_ms == null ? 0 : count(v.elapsed_ms) }; },
  async candidates(library_id: number, only_missing: boolean, signal?: AbortSignal) { return count(record(await request(`scrape/candidates?${new URLSearchParams({ library_id: String(library_id), only_missing: String(only_missing) })}`, 'GET', undefined, signal)).total); },
  preview: async (id: number, signal: AbortSignal) => parsePreview(await request(`items/${id}/scrape/preview`, 'GET', undefined, signal)),
  inspect: async (id: number, provider: string, candidateId: string, signal: AbortSignal) => parseInspect(await request(`items/${id}/scrape/inspect`, 'POST', { provider, id: candidateId }, signal)),
  async confirm(id: number, provider: string, candidateId: string, overwrite: boolean) { const v = record(await request(`items/${id}/scrape`, 'POST', { provider, id: candidateId, overwrite })); return { title: string(v.title), images: count(v.images) }; },
};
