import { apiClient } from './client';
import { parseSimilar } from './contracts';
import { record, string } from './admin';
export const mediaApi = {
  async similar(id: string, signal: AbortSignal) { return parseSimilar(await apiClient.request(`/Items/${id}/Similar?Limit=12`, { auth: 'required', signal, timeoutMs: 5000 })); },
  async reread(id: number) { return string(record(await apiClient.request(`/api/admin/items/${id}/reread`, { auth: 'required', method: 'POST' })).status); },
  remove: (id: number) => apiClient.request(`/api/admin/items/${id}`, { auth: 'required', method: 'DELETE' }),
  async probe(id: number) { return record(record(await apiClient.request(`/api/admin/items/${id}/probe`, { auth: 'required', method: 'POST' })).info); },
};
