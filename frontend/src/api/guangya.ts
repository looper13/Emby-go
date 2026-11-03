import { apiClient } from './client';
import { createOfflineApi, parseCloudConfig } from './cloud115';

export interface GuangyaConnectionPatch { access_token: string; refresh_token: string; device_id: string; revision: number }
export const guangyaApi = {
  ...createOfflineApi('guangya', '光鸭'),
  async config(signal?: AbortSignal) { return parseCloudConfig(await apiClient.request('/api/admin/guangya/connection', { auth: 'required', signal }), '光鸭'); },
  async save(value: GuangyaConnectionPatch, signal?: AbortSignal) {
    await apiClient.request('/api/admin/guangya/connection', { auth: 'required', method: 'PUT', json: value, signal, timeoutMs: 65000 });
  },
};
