import { apiClient } from './client';
import { parseDetail, parseItemsTotal, parseLibraries, parseStatus, parseWall } from './contracts';

export const catalogApi = {
  async items(query: URLSearchParams, signal?: AbortSignal) {
    return parseWall(await apiClient.request(`/api/admin/items?${query}`, { auth: 'required', signal }));
  },
  async detail(id: string, signal?: AbortSignal) {
    return parseDetail(await apiClient.request(`/api/admin/items/${encodeURIComponent(id)}/detail`, { auth: 'required', signal }));
  },
  async libraries(signal?: AbortSignal) {
    return parseLibraries(await apiClient.request('/api/admin/libraries', { auth: 'required', signal }));
  },
  async status(signal?: AbortSignal) {
    return parseStatus(await apiClient.request('/api/admin/status', { auth: 'required', signal }));
  },
  async successTotal(signal?: AbortSignal) {
    return parseItemsTotal(await apiClient.request('/api/admin/items?status=success&limit=1', {
      auth: 'required', signal,
    }));
  },
};
