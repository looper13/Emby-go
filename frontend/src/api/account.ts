import { apiClient } from './client';
import { parseUser } from './contracts';
import { record, string } from './admin';

export interface LibraryPolicy { EnableAllFolders: boolean; EnabledFolders: string[] }

export const accountApi = {
  async get(signal: AbortSignal) {
    const data = record(await apiClient.request('/api/admin/account', { auth: 'required', signal }));
    const user = parseUser(data.User);
    const rawPolicy = record(record(data.User).Policy);
    if (typeof rawPolicy.EnableAllFolders !== 'boolean' || !Array.isArray(rawPolicy.EnabledFolders)
      || rawPolicy.EnabledFolders.some(id => typeof id !== 'string') || !Array.isArray(data.Libraries)) throw new Error('账号数据格式错误');
    const policy: LibraryPolicy = { EnableAllFolders: rawPolicy.EnableAllFolders, EnabledFolders: [...rawPolicy.EnabledFolders] as string[] };
    const libraries = data.Libraries.map(value => { const lib = record(value); return { Id: string(lib.Id), Name: string(lib.Name), Type: string(lib.Type) }; });
    return { user, policy, libraries };
  },
  savePolicy(userId: string, policy: LibraryPolicy) {
    return apiClient.request(`/Users/${encodeURIComponent(userId)}/Policy`, { auth: 'required', method: 'POST', json: policy });
  },
  update(credentials: { Username: string; CurrentPassword: string; NewPassword: string }) {
    return apiClient.request('/api/admin/account', { auth: 'required', method: 'PUT', json: credentials });
  },
};
