import { apiClient } from './client';
import { boolean, count, record, string } from './admin';
import { list, optionalString } from './scrape';
export type BackgroundKind = 'probe' | 'scrape';
export interface BackgroundProgress { running: boolean; total: number; done: number; success: number; skipped: number; failed: number; current: string; cancelled: boolean; aborted: boolean; kind: string; started_at: string; finished_at: string; error: string; failures: string[] }
export function parseBackgroundProgress(value: unknown): BackgroundProgress {
  const v = record(value); return { running: boolean(v.running), total: count(v.total), done: count(v.done), success: count(v.success), skipped: count(v.skipped), failed: count(v.failed), current: string(v.current), cancelled: v.cancelled == null ? false : boolean(v.cancelled), aborted: v.aborted == null ? false : boolean(v.aborted), kind: optionalString(v.kind), started_at: optionalString(v.started_at), finished_at: optionalString(v.finished_at), error: optionalString(v.error), failures: list(v.failures).map(string) };
}
const path = (kind: BackgroundKind) => kind === 'probe' ? 'probe/media' : 'scrape';
export const backgroundApi = {
  async progress(kind: BackgroundKind, signal: AbortSignal) { return parseBackgroundProgress(await apiClient.request(`/api/admin/${path(kind)}/progress`, { auth: 'required', signal })); },
  async start(kind: BackgroundKind, avatar: boolean, json: Record<string, unknown>, signal: AbortSignal) {
    const v = record(await apiClient.request(`/api/admin/${kind === 'probe' ? path(kind) : avatar ? 'scrape/avatars' : 'scrape/run'}`, { auth: 'required', method: 'POST', json, signal }));
    return { total: count(v.total), message: optionalString(v.message) };
  },
  cancel: (kind: BackgroundKind, signal: AbortSignal) => apiClient.request(`/api/admin/${path(kind)}/cancel`, { auth: 'required', method: 'POST', signal }),
};
