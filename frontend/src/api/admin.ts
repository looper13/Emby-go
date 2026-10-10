import { apiClient } from './client';
import { isRecord, parseLibraries, type LibraryDto } from './contracts';

export interface ApiKeyDto { key: string; name: string; created_at: string }
export interface TaskDto { id: number; type: string; status: string; started_at: string; ended_at?: string; error?: string }
export interface ProbeDto { id: number; method: string; path: string; created_at: string }
export interface SettingsDto {
  listen: string; db_path: string; cache: string; redis_addr: string; redis_db: number;
  redis_online: boolean; redis_failures: number; library_monitor_mode: string; disable_library_monitor: boolean;
  cache_stats: Record<string, { hits: number; misses: number; shared: number }>;
}
export interface ManualPayload {
  source_path: string; source_url: string; title: string; year: number; number: string;
  original_title: string; plot: string; genres: string[]; tags: string[]; studios: string[];
}
export function invalid(): never { throw new Error('服务响应格式不正确'); }
export function record(value: unknown): Record<string, unknown> { return isRecord(value) ? value : invalid(); }
export function count(value: unknown): number { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 ? value : invalid(); }
export function string(value: unknown): string { return typeof value === 'string' ? value : invalid(); }
export function boolean(value: unknown): boolean { return typeof value === 'boolean' ? value : invalid(); }
function slice(value: unknown): unknown[] { return value === null ? [] : Array.isArray(value) ? value : invalid(); }
export function parseKey(value: unknown): ApiKeyDto {
  const v = record(value); return { key: string(v.key), name: string(v.name), created_at: string(v.created_at) };
}
export function parseKeys(value: unknown) {
  const v = record(value); return { items: slice(v.items).map(parseKey), total: count(v.total) };
}
export function parseTasks(value: unknown) {
  const v = record(value);
  return { running: boolean(v.running), items: slice(v.items).map((item): TaskDto => {
    const t = record(item);
    return { id: count(t.id), type: string(t.type), status: string(t.status), started_at: string(t.started_at),
      ended_at: t.ended_at == null ? undefined : string(t.ended_at), error: t.error == null ? undefined : string(t.error) };
  }) };
}
export function parseProbes(value: unknown) {
  const v = record(value);
  return { items: slice(v.items).map((item): ProbeDto => {
    const p = record(item); return { id: count(p.id), method: string(p.method), path: string(p.path), created_at: string(p.created_at) };
  }) };
}
export function parseSettings(value: unknown): SettingsDto {
  const v = record(value); const stats: SettingsDto['cache_stats'] = {};
  for (const [key, item] of Object.entries(record(v.cache_stats))) {
    const s = record(item); Object.defineProperty(stats, key, { enumerable: true,
      value: { hits: count(s.hits), misses: count(s.misses), shared: count(s.shared) } });
  }
  return { listen: string(v.listen), db_path: string(v.db_path), cache: string(v.cache), redis_addr: string(v.redis_addr),
    redis_db: count(v.redis_db), redis_online: boolean(v.redis_online), redis_failures: count(v.redis_failures),
    library_monitor_mode: string(v.library_monitor_mode), disable_library_monitor: boolean(v.disable_library_monitor), cache_stats: stats };
}
const read = (path: string, signal?: AbortSignal) => apiClient.request(`/api/admin/${path}`, { auth: 'required', signal });
const write = (path: string, method: string, json?: unknown) => apiClient.request(`/api/admin/${path}`, { auth: 'required', method, json });
export const adminApi = {
  libraries: async (signal?: AbortSignal) => parseLibraries(await read('libraries', signal)),
  async createLibrary(payload: { Name: string; Path: string }): Promise<LibraryDto> {
    const result = await write('libraries', 'POST', payload);
    return parseLibraries({ items: [result], total: 1 }).items![0]!;
  },
  deleteLibrary: (id: number) => write(`libraries/${id}`, 'DELETE'),
  keys: async (signal?: AbortSignal) => parseKeys(await read('apikeys', signal)),
  createKey: async (name: string) => parseKey(await write('apikeys', 'POST', { name })),
  deleteKey: (key: string) => write(`apikeys/${encodeURIComponent(key)}`, 'DELETE'),
  tasks: async (signal?: AbortSignal) => parseTasks(await read('tasks', signal)),
  probes: async (signal?: AbortSignal) => parseProbes(await read('probe', signal)),
  clearProbes: () => write('probe', 'DELETE'),
  settings: async (signal?: AbortSignal) => parseSettings(await read('settings', signal)),
  async manual(payload: ManualPayload) {
    const v = record(await write('items/manual', 'POST', payload));
    return { id: count(v.id), status: string(v.status) };
  },
};
