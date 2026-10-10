import { apiClient } from './client';
import { parseAuthStatus, parseLogin, parseUser, type Credentials } from './contracts';

export const authApi = {
  async status(signal?: AbortSignal) {
    return parseAuthStatus(await apiClient.request('/api/auth/status', { auth: 'none', signal }));
  },
  async initialize(credentials: Credentials) {
    await apiClient.request('/api/auth/initialize', { auth: 'none', method: 'POST', json: credentials });
  },
  async login(credentials: Credentials) {
    return parseLogin(await apiClient.request('/Users/AuthenticateByName', {
      auth: 'none', method: 'POST', json: credentials,
    }));
  },
  async me() {
    return parseUser(await apiClient.request('/Users/Me', { auth: 'required' }));
  },
};
