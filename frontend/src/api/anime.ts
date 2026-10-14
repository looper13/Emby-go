import { apiClient } from './client';
import { boolean, count, record, string } from './admin';
import { parseLibraries, type LibraryDto } from './contracts';
import { list } from './scrape';
import { tmdbMapping, tmdbSeriesMapping } from './anime-artwork';

export interface AnimeEntry {
  id: number;
  subject_id: number;
  bangumi_id: number;
  name: string;
  image: string;
  year: number;
  season: number;
  weekday: number;
  manual: boolean;
  revision: number;
  updated_at: string;
}
export interface AnimeSubject {
  id: number;
  bangumi_id: number;
  name: string;
  original_name: string;
  summary: string;
  platform: string;
  image: string;
  date: string;
  rating: number;
  episode_count: number;
}
export interface AnimeBinding {
  subject_id: number;
  episode_id: number;
  movie_id: number;
  revision: number;
  status: string;
  checked_at: string;
  error: string;
}
export interface AnimeEpisode {
  id: number;
  name: string;
  original_name: string;
  sort: number;
  season: number;
  index: number;
  summary: string;
  air_date: string;
  active: boolean;
}
export interface AnimeRelease {
  id: string;
  title: string;
  group: string;
  size: number;
  size_text: string;
  published_at: string;
  url: string;
  torrent: string;
  magnet: string;
  resolution: string;
  subtitle: string;
  episode_hint: string;
  ambiguous: boolean;
}
export interface AnimeState {
  status: string;
  updated_at: string;
  last_success_at: string;
  error: string;
}
export interface AnimeSettings {
  source: { mikan_url: string; revision: number };
  library: LibraryDto;
}
function number(v: unknown) {
  if (typeof v !== 'number' || !Number.isFinite(v))
    throw new Error('服务响应格式不正确');
  return v;
}
function entry(v: unknown): AnimeEntry {
  const r = record(v);
  return {
    id: count(r.id),
    subject_id: count(r.subject_id),
    bangumi_id: count(r.bangumi_id),
    name: string(r.name),
    image: string(r.image),
    year: count(r.year),
    season: count(r.season),
    weekday: number(r.weekday),
    manual: boolean(r.manual),
    revision: count(r.revision),
    updated_at: string(r.updated_at),
  };
}
function subject(v: unknown): AnimeSubject {
  const r = record(v);
  return {
    id: count(r.id),
    bangumi_id: count(r.bangumi_id),
    name: string(r.name),
    original_name: string(r.original_name),
    summary: string(r.summary),
    platform: string(r.platform),
    image: string(r.image),
    date: string(r.date),
    rating: number(r.rating),
    episode_count: count(r.episode_count),
  };
}
function binding(v: unknown): AnimeBinding {
  const r = record(v);
  return {
    subject_id: count(r.subject_id),
    episode_id: count(r.episode_id),
    movie_id: count(r.movie_id),
    revision: count(r.revision),
    status: string(r.status),
    checked_at: string(r.checked_at),
    error: string(r.error),
  };
}
function state(v: unknown): AnimeState {
  const r = record(v);
  return {
    status: string(r.status),
    updated_at: string(r.updated_at),
    last_success_at: string(r.last_success_at),
    error: string(r.error),
  };
}
function release(v: unknown): AnimeRelease {
  const r = record(v);
  return {
    id: string(r.id),
    title: string(r.title),
    group: string(r.group),
    size: count(r.size),
    size_text: string(r.size_text),
    published_at: string(r.published_at),
    url: string(r.url),
    torrent: string(r.torrent),
    magnet: string(r.magnet),
    resolution: string(r.resolution),
    subtitle: string(r.subtitle),
    episode_hint: string(r.episode_hint),
    ambiguous: boolean(r.ambiguous),
  };
}
function settings(v: unknown): AnimeSettings {
  const r = record(v),
    s = record(r.source);
  return {
    source: { mikan_url: string(s.mikan_url), revision: count(s.revision) },
    library: parseLibraries({ items: [r.library], total: 1 }).items![0]!,
  };
}
const req = (
  path: string,
  method = 'GET',
  json?: unknown,
  signal?: AbortSignal,
) =>
  apiClient.request(`/api/admin/anime${path}`, {
    auth: 'required',
    method,
    json,
    signal,
    readOnly: path === '/test',
  });
function page<T>(v: unknown, parse: (x: unknown) => T) {
  const r = record(v);
  return { items: list(r.items).map(parse), total: count(r.total) };
}
export const animeApi = {
	async bangumi(query: URLSearchParams, signal: AbortSignal) {
		const r = record(await req(`/bangumi?${query}`, 'GET', undefined, signal));
		return { ...page(r, subject), snapshot: archiveSnapshot(r.snapshot) };
	},
	async archive(signal?: AbortSignal) {
		const r = record(await req('/archive', 'GET', undefined, signal));
		const j = r.job == null ? null : record(r.job);
		return { snapshot: archiveSnapshot(r.snapshot), state: state(r.state), job: j == null ? null : {
			running: boolean(j.running), status: string(j.status), error: string(j.error), phase: string(j.phase), upToDate: boolean(j.up_to_date),
			bytes: count(j.bytes), totalBytes: count(j.total_bytes), rows: count(j.rows),
		} };
	},
	importArchive: () => req('/archive', 'POST'),
	cancelArchive: () => req('/archive/cancel', 'POST'),
	async bangumiSubject(id: string, signal: AbortSignal) {
		const r = record(await req(`/subjects/${encodeURIComponent(id)}`, 'GET', undefined, signal));
		return { subject: subject(r.subject), binding: r.binding == null ? null : binding(r.binding), entries: list(r.entries).map(entry), tmdb: tmdbSeriesMapping(r.tmdb),
			episodes: list(r.episodes).map(v => { const row = record(v), e = record(row.episode); return {
				episode: { id: count(e.id), name: string(e.name), original_name: string(e.original_name), sort: number(e.sort), season: count(e.season), index: count(e.index), summary: string(e.summary), air_date: string(e.air_date), active: boolean(e.active) } as AnimeEpisode,
				binding: binding(row.binding), item_id: string(row.item_id), tmdb: tmdbMapping(row.tmdb),
			}; }),
			people: list(r.people).map(v => { const p = record(v); return { id: count(p.id), kind: string(p.kind), name: string(p.name), summary: string(p.summary), role: count(p.role) }; }),
		};
	},
	refreshSubject: (id: string) => req(`/subjects/${encodeURIComponent(id)}/refresh`, 'POST'),
  async catalog(query: URLSearchParams, signal: AbortSignal) {
    const r = record(await req(`/catalog?${query}`, 'GET', undefined, signal));
    return {
      ...page(r, entry),
      state: state(r.state),
      stale: boolean(r.stale),
    };
  },
  async detail(id: string, signal: AbortSignal) {
    const r = record(await req(`/entries/${id}`, 'GET', undefined, signal));
    return {
      entry: entry(r.entry),
      subject: r.subject == null ? null : subject(r.subject),
      binding: r.binding == null ? null : binding(r.binding),
      state: state(r.state),
    };
  },
  async episodes(id: string, offset: number, signal: AbortSignal) {
    return page(
      await req(
        `/entries/${id}/episodes?limit=50&offset=${offset}`,
        'GET',
        undefined,
        signal,
      ),
      (v) => {
        const r = record(v),
          e = record(r.episode);
        return {
          episode: {
            id: count(e.id),
            name: string(e.name),
            original_name: string(e.original_name),
            sort: number(e.sort),
            season: count(e.season),
            index: count(e.index),
            summary: string(e.summary),
            air_date: string(e.air_date),
            active: boolean(e.active),
          } as AnimeEpisode,
          binding: binding(r.binding),
          item_id: string(r.item_id),
        };
      },
    );
  },
  async releases(
    id: string,
    offset: number,
    filter: string,
    signal: AbortSignal,
  ) {
    return page(
      await req(
        `/entries/${id}/releases?limit=50&offset=${offset}&filter=${encodeURIComponent(filter)}`,
        'GET',
        undefined,
        signal,
      ),
      release,
    );
  },
  async search(q: string, offset: number, signal: AbortSignal) {
    return page(
      await req(
        `/search?q=${encodeURIComponent(q)}&offset=${offset}`,
        'GET',
        undefined,
        signal,
      ),
      release,
    );
  },
  async candidates(id: string, q: string, signal: AbortSignal) {
    return page(
      await req(
        `/entries/${id}/mapping-candidates?search=${encodeURIComponent(q)}`,
        'GET',
        undefined,
        signal,
      ),
      subject,
    );
  },
  map: (id: string, bangumi_id: number, revision: number) =>
    req(`/entries/${id}/mapping`, 'PUT', { bangumi_id, revision }),
  async media(q: string, offset: number, signal: AbortSignal) {
    return page(
      await req(
        `/media-candidates?search=${encodeURIComponent(q)}&offset=${offset}`,
        'GET',
        undefined,
        signal,
      ),
      (v) => {
        const r = record(v);
        return {
          id: count(r.id),
          name: string(r.name),
          path: string(r.path),
          library_id: count(r.library_id),
          library_name: string(r.library_name),
          eligible: boolean(r.eligible),
          reason: string(r.reason),
          occupied: boolean(r.occupied),
        };
      },
    );
  },
  async bind(target: string, movie_id: number, revision: number) {
    return binding(
      await req(
        `/bindings/${encodeURIComponent(target)}`,
        movie_id ? 'PUT' : 'DELETE',
        { movie_id, revision },
      ),
    );
  },
  async check(target: string) {
    return binding(
      await req(`/bindings/${encodeURIComponent(target)}/check`, 'POST'),
    );
  },
  refresh: (json: { id?: number; year?: number; season?: number }) =>
    req('/refresh', 'POST', json),
  async progress(signal?: AbortSignal) {
    const r = record(await req('/progress', 'GET', undefined, signal));
    return {
      id: count(r.id),
      scope: string(r.scope),
      running: boolean(r.running),
      error: string(r.error),
    };
  },
  cancel: () => req('/cancel', 'POST'),
  async settings(signal: AbortSignal) {
    return settings(await req('/settings', 'GET', undefined, signal));
  },
  async saveSettings(json: AnimeSettings) {
    return settings(await req('/settings', 'PUT', json));
  },
  test: (mikan_url: string) => req('/test', 'POST', { mikan_url }),
  async playback(id: string) {
    const r = record(
      await apiClient.request(`/Items/${encodeURIComponent(id)}/PlaybackInfo`, {
        auth: 'required',
        method: 'POST',
      }),
    );
    const sources = list(r.MediaSources);
    if (!sources.length) throw new Error('没有可播放资源');
    return record(sources[0]);
  },
};

function archiveSnapshot(v: unknown) {
	const r = record(v), m = record(r.manifest);
	return { name: string(m.name), digest: string(m.digest), publishedAt: string(m.created_at), importedAt: string(r.imported_at),
		subjects: count(r.subjects), episodes: count(r.episodes), characters: count(r.characters), persons: count(r.persons) };
}
export const animeStatus = (status: string) =>
  ({
    unbound: '未关联',
    unchecked: '待检查',
    checking: '检查中',
    playable: '可播放',
    unavailable: '暂不可用',
  })[status] ?? status;
export function safeExternal(raw: string, magnet = false): string | undefined {
  try {
    const u = new URL(raw);
    return ['http:', 'https:', ...(magnet ? ['magnet:'] : [])].includes(
      u.protocol,
    )
      ? raw
      : undefined;
  } catch {
    return undefined;
  }
}
