import { apiClient } from './client';
import { boolean, count, record, string } from './admin';
import { entityIdOf } from '../lib/media';
import { currentUserPath } from './user-context';

export interface FavoriteItem {
  Id: string; Name: string; Type: string; ImageTags: Record<string, string>;
  UserData: { IsFavorite: boolean }; ParentId: string; SeriesId: string;
  bangumiSubjectId?: string;
}
export function parseFavoriteItem(value: unknown): FavoriteItem {
  const item = record(value), tags = record(item.ImageTags ?? {}), data = record(item.UserData);
  if (Object.values(tags).some(tag => typeof tag !== 'string')) throw new Error('服务响应格式不正确');
  const id = string(item.Id);
  const bangumi = record(item.ProviderIds ?? {}).Bangumi;
  const bangumiSubjectId = /^anime-(movie|series|season):/u.test(id) && typeof bangumi === 'string' && /^[1-9]\d*$/u.test(bangumi) ? bangumi : undefined;
  return { Id: id, Name: string(item.Name), Type: string(item.Type), ImageTags: tags as Record<string, string>,
    UserData: { IsFavorite: boolean(data.IsFavorite) }, ParentId: string(item.ParentId ?? ''), SeriesId: string(item.SeriesId ?? ''), ...(bangumiSubjectId ? { bangumiSubjectId } : {}) };
}
export const favoritesApi = {
  async set(id: string, favorite: boolean) {
    const data = record(await apiClient.request(currentUserPath(`FavoriteItems/${encodeURIComponent(id)}`), { auth: 'required', method: favorite ? 'POST' : 'DELETE' }));
    if (data.ItemId !== id) throw new Error('收藏响应条目不一致');
    return boolean(data.IsFavorite);
  },
  async item(id: string, signal?: AbortSignal) {
    return parseFavoriteItem(await apiClient.request(`/Items/${encodeURIComponent(id)}`, { auth: 'required', signal }));
  },
  async list(type: string, search: string, offset: number, signal?: AbortSignal) {
    const query = new URLSearchParams({ Recursive: 'true', IncludeItemTypes: type, Filters: 'IsFavorite', SearchTerm: search, SortBy: 'SortName', SortOrder: 'Ascending', StartIndex: String(offset), Limit: '48' });
    const data = record(await apiClient.request(`/Items?${query}`, { auth: 'required', signal }));
    if (!Array.isArray(data.Items)) throw new Error('服务响应格式不正确');
    const items = data.Items.map(parseFavoriteItem);
    // Emby episode ProviderIds belong to the episode, while the catalog route
    // uses the series' external Bangumi ID, not the local anime item ID.
    const series = [...new Set(items.filter(item => item.Id.startsWith('anime-episode:')).map(item => item.SeriesId))];
    const subjects = new Map(await Promise.all(series.map(async id => [id, (await favoritesApi.item(id, signal)).bangumiSubjectId] as const)));
    for (const item of items) if (item.Id.startsWith('anime-episode:')) item.bangumiSubjectId = subjects.get(item.SeriesId);
    return { items, total: count(data.TotalRecordCount) };
  },
};

export function favoriteEntityId(key: string, name: string) {
  return entityIdOf(key === 'collection' ? 'boxset' : key, name);
}

export function favoriteDestination(item: FavoriteItem) {
  if (/^[1-9]\d*$/u.test(item.Id)) return { path: `/item/${item.Id}` };
  if (item.Id.startsWith('anime-')) return { path: item.bangumiSubjectId ? `/anime/subjects/${item.bangumiSubjectId}` : '/anime' };
  const tv = /^tv-series:([1-9]\d*):/u.exec(item.Type === 'Season' ? item.SeriesId : item.Id);
  if (tv) return { path: '/items', query: { library_id: tv[1]!, tv_series: item.Type === 'Season' ? item.SeriesId : item.Id, ...(item.Type === 'Season' ? { tv_season: item.Id } : {}) } };
  const key = ({ Person: 'person', Genre: 'genre', Studio: 'studio', Tag: 'tag', BoxSet: 'collection' } as Record<string, string>)[item.Type];
  return { path: '/items', query: key ? { [key]: item.Name } : {} };
}
