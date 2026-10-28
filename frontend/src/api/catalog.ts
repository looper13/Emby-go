import { apiClient } from './client';
import { parseDetail, parseItemsTotal, parseLibraries, parseStatus, parseWall } from './contracts';
import type { MovieDto, WallUserData } from './contracts';

export interface ResumeEntry {
  movie: MovieDto;
  tags?: Record<string, string>;
  userdata?: WallUserData;
}

export const catalogApi = {
  async items(query: URLSearchParams, signal?: AbortSignal) {
    return parseWall(await apiClient.request(`/api/admin/items?${query}`, { auth: 'required', signal }));
  },
  async detail(id: string, signal?: AbortSignal) {
    return parseDetail(await apiClient.request(`/api/admin/items/${encodeURIComponent(id)}/detail`, { auth: 'required', signal }));
  },
  async libraries(signal?: AbortSignal) {
    return parseLibraries(await apiClient.request('/api/browse/libraries', { auth: 'required', signal }));
  },
  async status(signal?: AbortSignal) {
    return parseStatus(await apiClient.request('/api/admin/status', { auth: 'required', signal }));
  },
  async successTotal(signal?: AbortSignal) {
    return parseItemsTotal(await apiClient.request('/api/admin/items?status=success&limit=1', {
      auth: 'required', signal,
    }));
  },
  async resume(userId: string | number = 1, signal?: AbortSignal): Promise<ResumeEntry[]> {
    try {
      const raw = await apiClient.request<{ Items?: any[] }>(`/Users/${encodeURIComponent(String(userId))}/Items/Resume?Limit=12`, { auth: 'required', signal });
      return (raw?.Items || []).map(item => ({
        movie: {
          id: Number(item.Id),
          library_id: 1,
          source_path: '',
          Status: 'success',
          Title: item.Name || '',
          OriginalTitle: item.OriginalTitle || '',
          Number: item.IndexNumber != null ? String(item.IndexNumber) : '',
          Plot: item.Overview || '',
          PosterPath: item.ImageTags?.Primary ? 'poster' : '',
          LandscapePath: item.ImageTags?.Thumb || item.ImageTags?.Backdrop ? 'thumb' : '',
          BackdropPath: '',
          cover_url: '',
          Year: item.ProductionYear || 0,
          Rating: item.CommunityRating || 0,
          RuntimeSeconds: item.RunTimeTicks ? Math.round(item.RunTimeTicks / 10000000) : 0,
          television: item.SeriesName ? {
            series_path: '',
            title: item.SeriesName,
            original_title: '',
            plot: '',
            year: 0,
            season: item.ParentIndexNumber || 1,
            episode: item.IndexNumber || 1,
          } : undefined,
        } as MovieDto,
        tags: (item.ImageTags || {}) as Record<string, string>,
        userdata: {
          played: item.UserData?.Played ?? false,
          favorite: item.UserData?.IsFavorite ?? false,
          position_ticks: item.UserData?.PlaybackPositionTicks ?? 0,
        } as WallUserData,
      }));
    } catch {
      return [];
    }
  },
  async latest(signal?: AbortSignal) {
    try {
      return await catalogApi.items(new URLSearchParams({ status: 'success', sort: 'datecreated', order: 'desc', limit: '12' }), signal);
    } catch {
      return { items: [], total: 0, image_tags: {} };
    }
  },
};
