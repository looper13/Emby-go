import { apiClient } from './client';
import { isRecord, parseLibraries, type LibraryDto } from './contracts';

export interface R18Video {
  id: string; content_id: string; service_code: string; number: string; title: string;
  original_title: string; release_date: string; cover_url: string; images: string[];
}
export interface R18Page {
  items: R18Video[]; next_cursor: string; previous_cursor: string;
  has_more: boolean; has_previous: boolean; estimated_total: number;
}
export interface R18Resource {
  id: string; title: string; number: string; date: string; magnet: string;
  website: string; detail_url: string; size_bytes: number; size_mb: number; tags: string[];
}
export interface R18Resources { items: R18Resource[]; has_more: boolean; next_offset: number }
export interface R18Connection {
  name: string; host: string; port: number; database: string; username: string;
  ssl_mode: string; password?: string; revision?: number;
}
function record(v: unknown): Record<string, unknown> { if (!isRecord(v)) throw new Error('R18 响应格式不正确'); return v; }
function str(v: unknown): string { if (typeof v !== 'string') throw new Error('R18 响应格式不正确'); return v; }
function num(v: unknown): number { if (typeof v !== 'number' || !Number.isSafeInteger(v) || v < 0) throw new Error('R18 响应格式不正确'); return v; }
function bool(v: unknown): boolean { if (typeof v !== 'boolean') throw new Error('R18 响应格式不正确'); return v; }
function list(v: unknown): unknown[] { if (!Array.isArray(v)) throw new Error('R18 响应格式不正确'); return v; }
export function safeR18URL(raw: string): string {
  try { const u = new URL(raw); return ['http:', 'https:'].includes(u.protocol) && !u.username && !u.password ? u.href : ''; } catch { return ''; }
}
export function parseR18Video(v: unknown): R18Video {
  const r = record(v);
  return { id: str(r.id), content_id: str(r.content_id), service_code: str(r.service_code), number: str(r.number),
    title: str(r.title), original_title: str(r.original_title), release_date: str(r.release_date),
    cover_url: safeR18URL(str(r.cover_url)), images: list(r.images).map(str).map(safeR18URL).filter(Boolean) };
}
export function parseR18Page(v: unknown): R18Page {
  const r = record(v);
  return { items: list(r.items).map(parseR18Video), next_cursor: str(r.next_cursor), previous_cursor: str(r.previous_cursor),
    has_more: bool(r.has_more), has_previous: bool(r.has_previous), estimated_total: num(r.estimated_total) };
}
export function parseR18Resources(v: unknown): R18Resources {
  const r = record(v);
  return { has_more: bool(r.has_more), next_offset: num(r.next_offset), items: list(r.items).map(v => {
    const a = record(v); const magnet = str(a.magnet);
    if (!magnet.startsWith('magnet:?')) throw new Error('R18 磁力资源格式不正确');
    return { id: str(a.id), title: str(a.title), number: str(a.number), date: str(a.date), magnet,
      website: str(a.website), detail_url: safeR18URL(str(a.detail_url)), size_bytes: num(a.size_bytes), size_mb: num(a.size_mb), tags: list(a.tags).map(str) };
  }) };
}
function base(id: string | number) { return `/api/r18/libraries/${encodeURIComponent(id)}/videos`; }
export const r18Api = {
  async libraries(signal?: AbortSignal) { return parseLibraries(await apiClient.request('/api/r18/libraries', { auth: 'required', signal })); },
  async videos(id: string, query: URLSearchParams, signal?: AbortSignal) {
    return parseR18Page(await apiClient.request(`${base(id)}?${query}`, { auth: 'required', signal, timeoutMs: 12000 }));
  },
  async detail(library: string, id: string, signal?: AbortSignal) {
    const r = record(await apiClient.request(`${base(library)}/${encodeURIComponent(id)}`, { auth: 'required', signal, timeoutMs: 12000 }));
    return { video: parseR18Video(r.video), library_name: str(r.library_name), playable: bool(r.playable) };
  },
  async resources(library: string, id: string, offset: number, signal?: AbortSignal) {
    return parseR18Resources(await apiClient.request(`${base(library)}/${encodeURIComponent(id)}/resources?offset=${offset}`, { auth: 'required', signal, timeoutMs: 12000 }));
  },
  async saveConnection(value: R18Connection, id?: number): Promise<LibraryDto> {
    const raw = await apiClient.request(`/api/admin/r18/libraries${id ? `/${id}` : ''}`, { auth: 'required', method: id ? 'PUT' : 'POST', json: value, timeoutMs: 15000 });
    return parseLibraries({ items: [raw], total: 1 }).items![0]!;
  },
  async connection(id: number, signal?: AbortSignal) {
    const r = record(await apiClient.request(`/api/admin/r18/libraries/${id}`, { auth: 'required', signal }));
    const lib = parseLibraries({ items: [r.library], total: 1 }).items![0]!; const c = record(r.connection);
    return { library: lib, connection: { name: lib.Name, host: str(c.host), port: num(c.port), database: str(c.database), username: str(c.username), ssl_mode: str(c.ssl_mode), revision: lib.Revision } as R18Connection, hasPassword: bool(c.has_password) };
  },
};
