import { apiClient } from './client';
import { isRecord } from './contracts';
import { safeR18ImageURL } from './r18';

export interface JavdbMatch {
  status: 'matched' | 'not_found' | 'no_number';
  number: string;
  library_id: number;
  library_name: string;
  video?: { id: string; number: string; title: string };
}
export interface JavdbReview {
  id: string; username: string; content: string; score: string; status: string; created_at: string;
}
export interface JavdbRelatedList { id: string; name: string; movie_count?: number }
export interface JavdbPage<T> {
  items: T[]; page: number; page_size: number; has_more: boolean; total?: number;
  number: string; video_id: string; library_id: number;
}
export interface JavdbLocalMovie { id: number; number: string; title: string }
export interface JavdbListMovie {
  id: string; number: string; title: string; cover_url: string;
  has_local: boolean; local_movies: JavdbLocalMovie[];
}
export interface JavdbListMovies {
  items: JavdbListMovie[]; number: string; video_id: string; library_id: number;
  list_id: string; local_library_id: number; next_page: number; has_more: boolean;
}

function record(value: unknown): Record<string, unknown> {
  if (!isRecord(value)) throw new Error('JavDB 评论与清单响应格式不正确');
  return value;
}
function text(value: unknown): string {
  if (typeof value !== 'string') throw new Error('JavDB 评论与清单字段格式不正确');
  return value;
}
function count(value: unknown, positive = false): number {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < (positive ? 1 : 0)) {
    throw new Error('JavDB 评论与清单数量格式不正确');
  }
  return value;
}
function identity(value: unknown): string {
  const id = text(value);
  if (!/^[A-Za-z0-9_-]{1,1024}$/u.test(id)) throw new Error('JavDB 评论与清单标识格式不正确');
  return id;
}
export function parseJavdbMatch(value: unknown): JavdbMatch {
  const r = record(value);
  if (!['matched', 'not_found', 'no_number'].includes(text(r.status))) throw new Error('JavDB 番号匹配状态不正确');
  const result: JavdbMatch = {
    status: r.status as JavdbMatch['status'], number: text(r.number),
    library_id: count(r.library_id, true), library_name: text(r.library_name),
  };
  if (result.status === 'matched') {
    const video = record(r.video);
    result.video = { id: identity(video.id), number: text(video.number), title: text(video.title) };
  }
  return result;
}
function parsePage<T>(value: unknown, parseItem: (value: unknown) => T): JavdbPage<T> {
  const r = record(value);
  if (!Array.isArray(r.items) || typeof r.has_more !== 'boolean') throw new Error('JavDB 评论与清单分页格式不正确');
  const page = count(r.page, true), pageSize = count(r.page_size, true);
  if (page > 9999 || pageSize > 50 || r.items.length > pageSize) throw new Error('JavDB 评论与清单分页格式不正确');
  return { items: r.items.map(parseItem), page, page_size: pageSize, has_more: r.has_more,
    total: r.total === undefined ? undefined : count(r.total),
    number: text(r.number), video_id: identity(r.video_id), library_id: count(r.library_id, true) };
}
export function parseJavdbReviews(value: unknown): JavdbPage<JavdbReview> {
  return parsePage(value, item => {
    const r = record(item);
    return { id: identity(r.id), username: text(r.username), content: text(r.content),
      score: text(r.score), status: text(r.status), created_at: text(r.created_at) };
  });
}
export function parseJavdbRelatedLists(value: unknown): JavdbPage<JavdbRelatedList> {
  return parsePage(value, item => {
    const r = record(item), id = identity(r.id);
    if (!/^[A-Za-z0-9_-]{1,120}$/u.test(id)) throw new Error('JavDB 清单标识格式不正确');
    return { id, name: text(r.name), movie_count: r.movie_count === undefined ? undefined : count(r.movie_count) };
  });
}
export function parseJavdbListMovies(value: unknown): JavdbListMovies {
  const r = record(value);
  if (!Array.isArray(r.items) || r.items.length > 50 || typeof r.has_more !== 'boolean') throw new Error('清单影片格式不正确');
  const listId = identity(r.list_id), nextPage = count(r.next_page, true);
  if (!/^[A-Za-z0-9_-]{1,120}$/u.test(listId) || nextPage > 10000 || r.has_more && nextPage > 9999) throw new Error('清单影片分页格式不正确');
  return { items: r.items.map(item => {
    const movie = record(item);
    if (!Array.isArray(movie.local_movies) || movie.local_movies.length > 2000 || typeof movie.has_local !== 'boolean'
      || movie.has_local !== (movie.local_movies.length > 0)) throw new Error('清单影片本地播放状态不正确');
    return { id: identity(movie.id), number: text(movie.number), title: text(movie.title), cover_url: safeR18ImageURL(text(movie.cover_url)),
      has_local: movie.has_local, local_movies: movie.local_movies.map(item => {
        const local = record(item);
        return { id: count(local.id, true), number: text(local.number), title: text(local.title) };
      }) };
  }), number: text(r.number), video_id: identity(r.video_id), library_id: count(r.library_id, true),
  list_id: listId, local_library_id: count(r.local_library_id, true), next_page: nextPage, has_more: r.has_more };
}
function path(item: number, library: number, endpoint: string, page?: number, refresh = false): string {
  count(item, true); count(library, true);
  const query = new URLSearchParams({ library_id: String(library) });
  if (page !== undefined) {
    count(page, true);
    if (page > 9999) throw new Error('JavDB 页码超过范围');
    query.set('page', String(page)); query.set('limit', '15');
  }
  if (refresh) query.set('refresh', '1');
  return `/api/r18/items/${item}/${endpoint}?${query}`;
}
export const javdbCommunityApi = {
  async match(item: number, library: number, signal?: AbortSignal, refresh = false) {
    const result = parseJavdbMatch(await apiClient.request(path(item, library, 'match', undefined, refresh), { auth: 'required', signal, timeoutMs: 22000 }));
    if (result.library_id !== library) throw new Error('JavDB 匹配来源不一致');
    return result;
  },
  async reviews(item: number, library: number, page: number, signal?: AbortSignal, refresh = false) {
    const result = parseJavdbReviews(await apiClient.request(path(item, library, 'reviews', page, refresh), { auth: 'required', signal, timeoutMs: 22000 }));
    if (result.page !== page || result.page_size !== 15 || result.library_id !== library) throw new Error('JavDB 评论分页或来源不一致');
    return result;
  },
  async lists(item: number, library: number, page: number, signal?: AbortSignal, refresh = false) {
    const result = parseJavdbRelatedLists(await apiClient.request(path(item, library, 'related-lists', page, refresh), { auth: 'required', signal, timeoutMs: 22000 }));
    if (result.page !== page || result.page_size !== 15 || result.library_id !== library) throw new Error('JavDB 清单分页或来源不一致');
    return result;
  },
  async listMovies(item: number, library: number, listId: string, page: number, relatedPage: number, signal?: AbortSignal, refresh = false) {
    count(item, true); count(library, true); count(page, true); count(relatedPage, true);
    if (!/^[A-Za-z0-9_-]{1,120}$/u.test(listId) || page > 9999 || relatedPage > 9999) throw new Error('关联清单查询条件不正确');
    const query = new URLSearchParams({ library_id: String(library), page: String(page), related_page: String(relatedPage) });
    if (refresh) query.set('refresh', '1');
    const result = parseJavdbListMovies(await apiClient.request(`/api/r18/items/${item}/related-lists/${listId}/movies?${query}`, { auth: 'required', signal, timeoutMs: 22000 }));
    if (result.list_id !== listId || result.library_id !== library || result.next_page !== page + 1) throw new Error('清单影片来源或分页不一致');
    return result;
  },
};
