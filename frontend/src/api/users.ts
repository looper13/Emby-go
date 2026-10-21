import { apiClient } from './client';
import { parseUser, type UserDto } from './contracts';
import { boolean, record, string } from './admin';
import { accountApi, type LibraryPolicy } from './account';

export interface ManagedUser extends UserDto {
  Policy: UserDto['Policy'] & LibraryPolicy & { IsDisabled: boolean };
}
export interface UserLibrary { Id: string; Name: string; Type: string }
export function parseManagedUser(value: unknown): ManagedUser {
  const user = parseUser(value), policy = record(record(value).Policy);
  if (!Array.isArray(policy.EnabledFolders)) throw new Error('用户权限格式错误');
  return { ...user, Policy: { ...user.Policy, IsDisabled: boolean(policy.IsDisabled),
    EnableAllFolders: boolean(policy.EnableAllFolders), EnabledFolders: policy.EnabledFolders.map(string) } };
}
export const usersApi = {
  async list(signal: AbortSignal) {
    const data = record(await apiClient.request('/api/admin/users', { auth: 'required', signal }));
    if (!Array.isArray(data.Users) || !Array.isArray(data.Libraries)) throw new Error('用户列表格式错误');
    return { users: data.Users.map(parseManagedUser), libraries: data.Libraries.map((value): UserLibrary => {
      const lib = record(value); return { Id: string(lib.Id), Name: string(lib.Name), Type: string(lib.Type) };
    }) };
  },
  async create(Username: string, Pw: string) {
    return parseManagedUser(await apiClient.request('/api/admin/users', { auth: 'required', method: 'POST', json: { Username, Pw } }));
  },
  update(id: string, payload: { Username: string; NewPassword: string; IsDisabled: boolean }) {
    return apiClient.request(`/api/admin/users/${encodeURIComponent(id)}`, { auth: 'required', method: 'PUT', json: payload });
  },
  remove(id: string) {
    return apiClient.request(`/api/admin/users/${encodeURIComponent(id)}`, { auth: 'required', method: 'DELETE' });
  },
  savePolicy: accountApi.savePolicy,
};
