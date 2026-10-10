import { computed, onScopeDispose, ref, shallowRef } from 'vue';
import { defineStore } from 'pinia';
import { authApi } from '../api/auth';
import { ApiError, apiClient } from '../api/client';
import type { Credentials, UserDto } from '../api/contracts';

export const TOKEN_KEY = 'emby_token';

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(localStorage.getItem(TOKEN_KEY));
  const user = shallowRef<UserDto | null>(null);
  const initialized = ref<boolean | null>(null);
  const phase = ref<'idle' | 'checking' | 'authenticated' | 'anonymous' | 'unavailable'>('idle');
  const error = ref('');
  const submitting = ref(false);
  const expiration = ref(0);
  const authenticated = computed(() => Boolean(token.value && user.value && phase.value === 'authenticated'));
  let generation = 0;
  let check: { token: string; promise: Promise<boolean> } | undefined;
  let statusRequest: Promise<boolean> | undefined;

  function invalidate(sentToken: string) {
    if (token.value !== sentToken) return;
    generation += 1;
    user.value = null;
    const stored = localStorage.getItem(TOKEN_KEY);
    // A newer login in another tab must not be removed by this tab's old 401.
    if (stored && stored !== sentToken) {
      token.value = stored;
      phase.value = 'idle';
      return;
    }
    localStorage.removeItem(TOKEN_KEY);
    token.value = null;
    phase.value = 'anonymous';
    error.value = '登录已失效';
    expiration.value += 1;
  }

  onScopeDispose(apiClient.setAuthHandlers({ getToken: () => token.value, onUnauthorized: invalidate }));

  function checkSession(): Promise<boolean> {
    if (authenticated.value) return Promise.resolve(true);
    const sentToken = token.value;
    if (!sentToken) {
      phase.value = 'anonymous';
      return Promise.resolve(false);
    }
    if (check?.token === sentToken) return check.promise;
    const startedGeneration = generation;
    phase.value = 'checking';
    error.value = '';
    const promise = authApi.me().then((currentUser) => {
      if (startedGeneration !== generation || sentToken !== token.value) return authenticated.value;
      user.value = currentUser;
      phase.value = 'authenticated';
      return true;
    }).catch((cause: unknown) => {
      if (startedGeneration !== generation || sentToken !== token.value) return authenticated.value;
      if (cause instanceof ApiError && cause.status === 401) {
        invalidate(sentToken);
        return false;
      }
      phase.value = 'unavailable';
      error.value = '无法连接服务，请重试';
      throw cause;
    }).finally(() => {
      if (check?.promise === promise) check = undefined;
    });
    check = { token: sentToken, promise };
    return promise;
  }

  function loadStatus(): Promise<boolean> {
    if (initialized.value !== null) return Promise.resolve(initialized.value);
    if (statusRequest) return statusRequest;
    statusRequest = authApi.status().then((data) => {
      initialized.value = data.initialized;
      return data.initialized;
    }).finally(() => { statusRequest = undefined; });
    return statusRequest;
  }

  async function initialize(credentials: Credentials): Promise<void> {
    if (submitting.value) throw new Error('请求正在进行中');
    submitting.value = true;
    try {
      await authApi.initialize(credentials);
      initialized.value = true;
    } finally { submitting.value = false; }
  }

  async function login(credentials: Credentials): Promise<boolean> {
    if (submitting.value) throw new Error('请求正在进行中');
    submitting.value = true;
    try {
      const data = await authApi.login(credentials);
      localStorage.setItem(TOKEN_KEY, data.AccessToken);
      generation += 1;
      token.value = data.AccessToken;
      user.value = data.User;
      initialized.value = true;
      phase.value = 'authenticated';
      error.value = '';
      return true;
    } finally { submitting.value = false; }
  }

  return {
    token, user, initialized, phase, error, submitting, expiration, authenticated,
    checkSession, loadStatus, initialize, login,
  };
});
