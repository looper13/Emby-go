import { apiClient } from './client';
import { isRecord, parseLibraries, type LibraryDto } from './contracts';

export interface R18Video {
  id: string; content_id: string; service_code: string; number: string; title: string;
  original_title: string; release_date: string; cover_url: string; images: string[];
  duration: number; score: number; magnets_count: number; has_local: boolean;
  tags: string[]; has_preview: boolean; preview_url: string;
  rank?: number;
}
export interface R18Page {
  items: R18Video[]; page: number; page_size: number; has_more: boolean; has_previous: boolean;
  total?: number | null;
}
export interface R18Tag { id: string; name: string; type: string }
export interface R18TagCategory { id: string; name: string; type: string; tags: R18Tag[] }
export interface R18Tags { categories: R18TagCategory[] }
export interface R18Resource {
  id: string; title: string; number: string; date: string; magnet: string;
  website: string; detail_url: string; size_bytes: number; size_mb: number; tags: string[];
  origins: ('online' | 'library')[];
}
export interface R18LocalSource {
  id: string; item_id: string; name: string; library_name: string; container: string;
}
export interface R18Resources { items: R18Resource[]; has_more: boolean; next_offset: number }
export interface R18Connection {
  name: string; base_url: string; api_key?: string; revision?: number;
}
function record(v: unknown): Record<string, unknown> { if (!isRecord(v)) throw new Error('R18 响应格式不正确'); return v; }
function str(v: unknown): string { if (typeof v !== 'string') throw new Error('R18 响应格式不正确'); return v; }
function num(v: unknown): number { if (typeof v !== 'number' || !Number.isSafeInteger(v) || v < 0) throw new Error('R18 响应格式不正确'); return v; }
function positive(v: unknown): number { const n = num(v); if (!n) throw new Error('JavDB 分页格式不正确'); return n; }
function finite(v: unknown): number { if (typeof v !== 'number' || !Number.isFinite(v) || v < 0) throw new Error('JavDB 影片数值格式不正确'); return v; }
function bool(v: unknown): boolean { if (typeof v !== 'boolean') throw new Error('R18 响应格式不正确'); return v; }
function list(v: unknown): unknown[] { if (!Array.isArray(v)) throw new Error('R18 响应格式不正确'); return v; }
export function safeR18URL(raw: string): string {
  try { const u = new URL(raw); return ['http:', 'https:'].includes(u.protocol) && !u.username && !u.password ? u.href : ''; } catch { return ''; }
}
export function safeR18ImageURL(raw: string): string {
  return /^\/api\/r18\/libraries\/[1-9]\d*\/videos\/[A-Za-z0-9_-]{1,1024}\/images\/(poster|cover|[0-9]|[1-9][0-9])$/u.test(raw) ? raw : '';
}
export function safeR18PreviewURL(raw: string): string {
  return /^\/api\/r18\/libraries\/[1-9]\d*\/videos\/[A-Za-z0-9_-]{1,1024}\/preview\.(?:m3u8|mp4)$/u.test(raw) ? raw : '';
}
export function parseR18Video(v: unknown): R18Video {
  const r = record(v);
  return { id: str(r.id), content_id: str(r.content_id), service_code: str(r.service_code), number: str(r.number),
    title: str(r.title), original_title: str(r.original_title), release_date: str(r.release_date),
    cover_url: safeR18ImageURL(str(r.cover_url)), images: list(r.images).map(str).map(safeR18ImageURL).filter(Boolean),
    duration: finite(r.duration), score: finite(r.score), magnets_count: num(r.magnets_count), has_local: bool(r.has_local),
    tags: [...new Set(list(r.tags).map(str).map(tag => tag.trim()).filter(Boolean))], has_preview: bool(r.has_preview), preview_url: safeR18PreviewURL(str(r.preview_url)),
    rank: r.rank === undefined ? 0 : num(r.rank) };
}
export function parseR18LocalSource(v: unknown): R18LocalSource {
  const r = record(v), id = str(r.id), itemId = str(r.item_id);
  const match = /^strm-([1-9]\d*)(?:-p([2-9]|[1-9]\d+))?$/u.exec(id);
  if (!match || itemId !== (match[2] ? `part-${match[1]}-${match[2]}` : match[1])) throw new Error('本地 STRM 播放源格式不正确');
  return { id, item_id: itemId, name: str(r.name), library_name: str(r.library_name), container: str(r.container) };
}
export function parseR18Page(v: unknown): R18Page {
  const r = record(v);
  return { items: list(r.items).map(parseR18Video), page: positive(r.page), page_size: positive(r.page_size),
    has_more: bool(r.has_more), has_previous: bool(r.has_previous), total: r.total === undefined || r.total === null ? null : num(r.total) };
}
export function parseR18Tags(v: unknown): R18Tags {
  const r = record(v);
  function identity(value: unknown) { const id = str(value); if (!/^[A-Za-z0-9_-]{1,120}$/u.test(id)) throw new Error('JavDB 分类标识不正确'); return id; }
  function tagType(value: unknown) { const type = str(value); if (!/^[0-4]$/u.test(type)) throw new Error('JavDB 分类类型不正确'); return type; }
  return { categories: list(r.categories).map(value => {
    const category = record(value);
    return { id: identity(category.id), name: str(category.name), type: tagType(category.type), tags: list(category.tags).map(value => {
      const tag = record(value); return { id: identity(tag.id), name: str(tag.name), type: tagType(tag.type) };
    }) };
  }) };
}
export function parseR18Resources(v: unknown): R18Resources {
  const r = record(v);
  return { has_more: bool(r.has_more), next_offset: num(r.next_offset), items: list(r.items).map(v => {
    const a = record(v); const magnet = str(a.magnet);
    if (!magnet.startsWith('magnet:?')) throw new Error('R18 磁力资源格式不正确');
    const origins = list(a.origins).map(value => {
      if (value !== 'online' && value !== 'library') throw new Error('JavDB 磁力来源格式不正确');
      return value;
    });
    if (!origins.length) throw new Error('JavDB 磁力来源格式不正确');
    return { id: str(a.id), title: str(a.title), number: str(a.number), date: str(a.date), magnet,
      website: str(a.website), detail_url: safeR18URL(str(a.detail_url)), size_bytes: num(a.size_bytes), size_mb: num(a.size_mb), tags: list(a.tags).map(str), origins: [...new Set(origins)] };
  }) };
}
function base(id: string | number) { return `/api/r18/libraries/${encodeURIComponent(id)}/videos`; }
export const r18Api = {
  async libraries(signal?: AbortSignal) { return parseLibraries(await apiClient.request('/api/r18/libraries', { auth: 'required', signal, timeoutMs: 22000 })); },
  async tags(id: string, type = 'all', signal?: AbortSignal, refresh = false) {
    if (!['all', '0', '1', '2', '3', '4'].includes(type)) throw new Error('JavDB 分类类型不正确');
    const query = new URLSearchParams({ type }); if (refresh) query.set('refresh', '1');
    return parseR18Tags(await apiClient.request(`/api/r18/libraries/${encodeURIComponent(id)}/tags?${query}`, { auth: 'required', signal, timeoutMs: 22000 }));
  },
  async videos(id: string, query: URLSearchParams, signal?: AbortSignal) {
    // 路由用 page_size 保存选择，后端在线列表接口使用 limit。
    const params = new URLSearchParams(query);
    const pageSize = params.get('page_size');
    if (pageSize !== null) { params.set('limit', pageSize); params.delete('page_size'); }
    return parseR18Page(await apiClient.request(`${base(id)}?${params}`, { auth: 'required', signal, timeoutMs: 22000 }));
  },
  async detail(library: string, id: string, signal?: AbortSignal) {
    const r = record(await apiClient.request(`${base(library)}/${encodeURIComponent(id)}`, { auth: 'required', signal, timeoutMs: 22000 }));
    return { video: parseR18Video(r.video), library_name: str(r.library_name), playable: bool(r.playable), local_sources: list(r.local_sources).map(parseR18LocalSource) };
  },
  async localPlayback(library: string, id: string, sourceId: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ source_id: sourceId });
    const r = record(await apiClient.request(`${base(library)}/${encodeURIComponent(id)}/local-playback?${query}`, { auth: 'required', signal, timeoutMs: 22000 }));
    return { source: parseR18LocalSource(r.source) };
  },
  async resources(library: string, id: string, offset: number, signal?: AbortSignal) {
    return parseR18Resources(await apiClient.request(`${base(library)}/${encodeURIComponent(id)}/resources?offset=${offset}`, { auth: 'required', signal, timeoutMs: 22000 }));
  },
  async saveConnection(value: R18Connection, id?: number): Promise<LibraryDto> {
    const raw = await apiClient.request(`/api/admin/r18/libraries${id ? `/${id}` : ''}`, { auth: 'required', method: id ? 'PUT' : 'POST', json: value, timeoutMs: 25000 });
    return parseLibraries({ items: [raw], total: 1 }).items![0]!;
  },
  async connection(id: number, signal?: AbortSignal) {
    const r = record(await apiClient.request(`/api/admin/r18/libraries/${id}`, { auth: 'required', signal, timeoutMs: 22000 }));
    const lib = parseLibraries({ items: [r.library], total: 1 }).items![0]!; const c = record(r.connection);
    return { library: lib, connection: { name: lib.Name, base_url: str(c.base_url), revision: lib.Revision } as R18Connection, hasApiKey: bool(c.has_api_key) };
  },
};
