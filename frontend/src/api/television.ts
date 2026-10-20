import { apiClient } from './client';
import { record, count, string, boolean } from './admin';

export interface TelevisionItem {
  Id: string; Name: string; Type: 'Series' | 'Season' | 'Episode';
  ImageTags: Record<string, string>; ChildCount: number; RecursiveItemCount: number;
  IndexNumber: number; SeriesName: string; ProductionYear: number; Overview: string;
  Favorite?: boolean;
}
export function parseTelevision(value: unknown) {
  const result = record(value);
  if (!Array.isArray(result.Items)) throw new Error('服务响应格式不正确');
  const items = result.Items.map((item): TelevisionItem => {
    const r = record(item); const type = string(r.Type);
    if (type !== 'Series' && type !== 'Season' && type !== 'Episode') throw new Error('服务响应格式不正确');
    const tags = record(r.ImageTags ?? {}); if (Object.values(tags).some(v => typeof v !== 'string')) throw new Error('服务响应格式不正确');
    const id = string(r.Id);
    const validID = type === 'Episode' ? /^[1-9]\d*$/u.test(id) && Number.isSafeInteger(Number(id))
      : type === 'Series' ? /^tv-series:[1-9]\d*:[A-Za-z0-9_-]+$/u.test(id)
        : /^tv-season:[1-9]\d*:\d+:[A-Za-z0-9_-]+$/u.test(id);
    if (!validID) throw new Error('服务响应格式不正确');
    return { Id: id, Name: string(r.Name), Type: type, ImageTags: tags as Record<string,string>, ChildCount: count(r.ChildCount ?? 0), RecursiveItemCount: count(r.RecursiveItemCount ?? 0), IndexNumber: count(r.IndexNumber ?? 0), SeriesName: string(r.SeriesName ?? ''), ProductionYear: count(r.ProductionYear ?? 0), Overview: string(r.Overview ?? ''), Favorite: r.UserData == null ? false : boolean(record(r.UserData).IsFavorite ?? false) };
  });
  return { items, total: count(result.TotalRecordCount) };
}
const read = async (path: string, signal: AbortSignal) => parseTelevision(await apiClient.request(path, { auth:'required',signal }));
export const televisionApi = {
  list: (library: number, search: string, offset: number, signal: AbortSignal) => read(`/api/admin/shows?${new URLSearchParams({library_id:String(library),search,offset:String(offset),limit:'48'})}`, signal),
  series: async (id: string, signal: AbortSignal) => {
    const item = await apiClient.request(`/Items/${encodeURIComponent(id)}?UserId=1`, { auth: 'required', signal });
    const parsed = parseTelevision({ Items: [item], TotalRecordCount: 1 }).items[0]!;
    if (parsed.Type !== 'Series' || parsed.Id !== id) throw new Error('服务响应格式不正确');
    return parsed;
  },
  seasons: (series: string, signal: AbortSignal) => read(`/Shows/${encodeURIComponent(series)}/Seasons?UserId=1&Limit=1000`, signal),
  episodes: (series: string, season: string, offset: number, signal: AbortSignal) => read(`/Shows/${encodeURIComponent(series)}/Episodes?${new URLSearchParams({UserId:'1',SeasonId:season,StartIndex:String(offset),Limit:'100'})}`, signal),
};
