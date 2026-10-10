import { apiClient } from './client';
import { boolean, count, record, string } from './admin';
import { list, optionalString } from './scrape';
export interface ScheduledTask { id: number; name: string; type: string; cron: string; params: string; enabled: boolean; next_run: string; last_status: string; last_run_at: string; last_message: string }
export interface ScheduledPayload { name: string; type: string; cron: string; enabled: boolean; params: Record<string, unknown> }
export function parseScheduled(value: unknown) {
  const v = record(value), types: Record<string, string> = {};
  for (const [key, value] of Object.entries(record(v.types))) Object.defineProperty(types, key, { enumerable: true, value: string(value) });
  return { total: count(v.total), types, items: list(v.items).map((item): ScheduledTask => { const t = record(item); return { id: count(t.id), name: string(t.name), type: string(t.type), cron: string(t.cron), params: optionalString(t.params), enabled: boolean(t.enabled), next_run: optionalString(t.next_run), last_status: optionalString(t.last_status), last_run_at: optionalString(t.last_run_at), last_message: optionalString(t.last_message) }; }) };
}
const request = (path = '', method = 'GET', json?: unknown, signal?: AbortSignal) => apiClient.request(`/api/admin/scheduled${path}`, { auth: 'required', method, json, signal, readOnly: path === '/validate' });
export const scheduledApi = {
  list: async (signal: AbortSignal) => parseScheduled(await request('', 'GET', undefined, signal)),
  async validate(cron: string, signal: AbortSignal) { const v = record(await request('/validate', 'POST', { cron }, signal)); return { valid: boolean(v.valid), error: optionalString(v.error), next_runs: list(v.next_runs).map(string) }; },
  save: (id: number | null, json: ScheduledPayload) => request(id ? `/${id}` : '', id ? 'PUT' : 'POST', json),
  action: (id: number, action: 'toggle' | 'run' | 'delete') => request(action === 'delete' ? `/${id}` : `/${id}/${action}`, action === 'delete' ? 'DELETE' : 'POST'),
};
