import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import type { Router } from 'vue-router';
import App from '../src/App.vue';
import { apiClient } from '../src/api/client';
import { createAppRouter } from '../src/router';
import { viewHistories, viewHistoryKey } from '../src/router/view-history';
import { TOKEN_KEY, useAuthStore } from '../src/stores/auth';
import authFixture from '../../vue-migration/fixtures/json/auth-login.json';
import librariesFixture from '../../vue-migration/fixtures/json/libraries.json';
import statusFixture from '../../vue-migration/fixtures/json/status.json';
import itemsFixture from '../../vue-migration/fixtures/json/items-wall.json';
import scanFixture from '../../vue-migration/fixtures/json/scan-progress.json';

const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), {
  status, headers: { 'Content-Type': 'application/json' },
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

type Handler = (path: string, init?: RequestInit) => Response | Promise<Response> | undefined;
function installServer(handler?: Handler) {
  const fetcher = vi.fn<typeof fetch>(async (input, init) => {
    const path = String(input);
    const custom = handler?.(path, init);
    if (custom !== undefined) return custom;
    switch (path) {
      case '/api/auth/status': return json({ initialized: true });
      case '/api/auth/initialize': return new Response(null, { status: 204 });
      case '/Users/AuthenticateByName': return json(authFixture);
      case '/Users/Me': return json(authFixture.User);
      case '/api/admin/libraries': return json(librariesFixture);
      case '/api/admin/status': return json(statusFixture);
      case '/api/admin/scan/progress': return json(scanFixture);
      case '/api/admin/items?status=success&limit=1':
        return json({ ...itemsFixture, items: itemsFixture.items.slice(0, 1), limit: 1, total: statusFixture.success });
      default: throw new Error(`Unexpected test request: ${path}`);
    }
  });
  vi.stubGlobal('fetch', fetcher);
  return fetcher;
}

const stores: Pinia[] = [];
const apps: { wrapper: VueWrapper; router: Router }[] = [];
function newStore() {
  const pinia = createPinia();
  stores.push(pinia);
  setActivePinia(pinia);
  return pinia;
}

async function openApp(path = '/login', entry = '/admin-vue') {
  window.history.replaceState({}, '', `${entry}#${path}`);
  const pinia = newStore();
  const router = createAppRouter(pinia);
  const wrapper = mount(App, { global: { plugins: [pinia, router], provide: { [viewHistoryKey]: viewHistories.get(router)! } } });
  apps.push({ wrapper, router });
  await router.isReady();
  await flushPromises();
  return { wrapper, router, auth: useAuthStore(pinia) };
}

async function fillLogin(wrapper: VueWrapper) {
  await wrapper.get('#auth-user').setValue('admin');
  await wrapper.get('#auth-pw').setValue('test-password');
}

beforeEach(() => { localStorage.clear(); });
afterEach(() => {
  for (const { wrapper, router } of apps.splice(0)) {
    wrapper.unmount();
    router.options.history.destroy();
  }
  for (const pinia of stores.splice(0)) disposePinia(pinia);
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.clear();
  window.history.replaceState({}, '', '/');
});

describe('authentication store', () => {
  it('shares an in-flight session check and caches the verified session', async () => {
    localStorage.setItem(TOKEN_KEY, 'stored-token');
    const response = deferred<Response>();
    const fetcher = installServer((path) => path === '/Users/Me' ? response.promise : undefined);
    const auth = useAuthStore(newStore());
    const first = auth.checkSession();
    const second = auth.checkSession();
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(auth.phase).toBe('checking');
    response.resolve(json(authFixture.User));
    expect(await first).toBe(true);
    expect(await second).toBe(true);
    expect(await auth.checkSession()).toBe(true);
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(auth.user?.Name).toBe(authFixture.User.Name);
  });

  it('retains token on a failed network check and retries only when asked', async () => {
    localStorage.setItem(TOKEN_KEY, 'stored-token');
    let offline = true;
    const fetcher = installServer((path) => path === '/Users/Me' && offline
      ? Promise.reject(new TypeError('Failed to fetch')) : undefined);
    const auth = useAuthStore(newStore());
    await expect(auth.checkSession()).rejects.toThrow();
    expect(localStorage.getItem(TOKEN_KEY)).toBe('stored-token');
    expect(auth.phase).toBe('unavailable');
    expect(auth.expiration).toBe(0);
    expect(fetcher).toHaveBeenCalledTimes(1);
    offline = false;
    expect(await auth.checkSession()).toBe(true);
  });

  it('removes a token only on an explicit protected 401', async () => {
    localStorage.setItem(TOKEN_KEY, 'expired-token');
    installServer((path) => path === '/Users/Me' ? json({ error: 'unauthorized' }, 401) : undefined);
    const auth = useAuthStore(newStore());
    expect(await auth.checkSession()).toBe(false);
    expect(auth.phase).toBe('anonymous');
    expect(auth.user).toBeNull();
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull();
    expect(auth.expiration).toBe(1);
  });

  it('does not erase a new login with an older request response', async () => {
    localStorage.setItem(TOKEN_KEY, 'old-token');
    const oldResponse = deferred<Response>();
    installServer((path) => path === '/api/admin/status' ? oldResponse.promise : undefined);
    const auth = useAuthStore(newStore());
    const oldRequest = apiClient.request('/api/admin/status', { auth: 'required' });
    await auth.login({ Username: 'admin', Pw: 'test-password' });
    oldResponse.resolve(json({ error: 'unauthorized' }, 401));
    await expect(oldRequest).rejects.toMatchObject({ status: 401 });
    expect(localStorage.getItem(TOKEN_KEY)).toBe(authFixture.AccessToken);
    expect(auth.authenticated).toBe(true);
    expect(auth.expiration).toBe(0);
  });

  it('also protects a newer token saved by another tab', async () => {
    localStorage.setItem(TOKEN_KEY, 'old-token');
    const oldResponse = deferred<Response>();
    installServer((path) => path === '/api/admin/status' ? oldResponse.promise : undefined);
    const auth = useAuthStore(newStore());
    const oldRequest = apiClient.request('/api/admin/status', { auth: 'required' });
    localStorage.setItem(TOKEN_KEY, 'other-tab-token');
    oldResponse.resolve(json({ error: 'unauthorized' }, 401));
    await expect(oldRequest).rejects.toMatchObject({ status: 401 });
    expect(localStorage.getItem(TOKEN_KEY)).toBe('other-tab-token');
    expect(auth.token).toBe('other-tab-token');
    expect(auth.expiration).toBe(0);
  });

  it('does not let a late Me response replace a newly authenticated user', async () => {
    localStorage.setItem(TOKEN_KEY, 'old-token');
    const response = deferred<Response>();
    installServer((path) => path === '/Users/Me' ? response.promise : undefined);
    const auth = useAuthStore(newStore());
    const check = auth.checkSession();
    await auth.login({ Username: 'admin', Pw: 'test-password' });
    response.resolve(json({ ...authFixture.User, Name: 'previous-user' }));
    await check;
    expect(auth.user?.Name).toBe(authFixture.User.Name);
    expect(auth.token).toBe(authFixture.AccessToken);
  });

  it('blocks duplicate submissions and never gives authentication writes a page signal', async () => {
    const response = deferred<Response>();
    const fetcher = installServer((path) => path === '/Users/AuthenticateByName' ? response.promise : undefined);
    const auth = useAuthStore(newStore());
    const credentials = { Username: 'admin', Pw: 'test-password' };
    const first = auth.login(credentials);
    await expect(auth.login(credentials)).rejects.toThrow('请求正在进行中');
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher.mock.calls[0]?.[1]?.signal).toBeUndefined();
    response.resolve(json(authFixture));
    await first;
    expect(auth.submitting).toBe(false);
  });
});

describe('login and minimal application', () => {
  it.each(['/admin-vue', '/admin'])('keeps a successful credential login inside %s', async (entry) => {
    const fetcher = installServer();
    const { wrapper, router } = await openApp('/login', entry);
    expect(wrapper.find('main.auth-root > section.auth-card > form.auth-form').exists()).toBe(true);
    await fillLogin(wrapper);
    await wrapper.get('#auth-form').trigger('submit');
    await flushPromises();
    expect(window.location.pathname + window.location.hash).toBe(`${entry}#/overview`);
    expect(router.currentRoute.value.path).toBe('/overview');
    expect(localStorage.getItem(TOKEN_KEY)).toBe(authFixture.AccessToken);
    const call = fetcher.mock.calls.find(([path]) => path === '/Users/AuthenticateByName');
    expect(call?.[1]?.body).toBe(JSON.stringify({ Username: 'admin', Pw: 'test-password' }));
    expect(new Headers(call?.[1]?.headers).has('X-Emby-Token')).toBe(false);
    expect(wrapper.find('#content > .cards').exists()).toBe(true);
    expect(wrapper.find('a[href="/admin"]').exists()).toBe(false);
    expect(wrapper.findAll('#nav [data-page]').map(item => item.attributes('data-page'))).toEqual(['overview', 'libraries', 'items', 'manual', 'settings', 'apikeys', 'scrape', 'scheduled', 'tasks', 'probe']);
    expect(wrapper.find('#scan').exists()).toBe(true);
    expect(wrapper.find('#reindex').exists()).toBe(true);
  });

  it.each(['/admin-vue', '/admin'])('keeps an existing-token login inside %s', async (entry) => {
    localStorage.setItem(TOKEN_KEY, 'stored-token');
    const fetcher = installServer();
    await openApp('/login', entry);
    expect(window.location.pathname + window.location.hash).toBe(`${entry}#/overview`);
    expect(fetcher.mock.calls.filter(([path]) => path === '/Users/Me')).toHaveLength(1);
    expect(fetcher.mock.calls.filter(([path]) => path === '/Users/AuthenticateByName')).toHaveLength(0);
    expect(localStorage.getItem(TOKEN_KEY)).toBe('stored-token');
  });

  it('initializes with 204, then logs in through the same entry-preserving path', async () => {
    const fetcher = installServer((path) => path === '/api/auth/status' ? json({ initialized: false }) : undefined);
    const { wrapper } = await openApp();
    expect(wrapper.get('h1').text()).toBe('建立管理员账户');
    await fillLogin(wrapper);
    await wrapper.get('#auth-confirm').setValue('test-password');
    await wrapper.get('#auth-form').trigger('submit');
    await flushPromises();
    expect(wrapper.get('h1').text()).toBe('欢迎回来');
    expect(wrapper.get('.auth-error').text()).toBe('初始化完成，请使用新账户登录');
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull();
    expect(fetcher.mock.calls.filter(([path]) => path === '/Users/AuthenticateByName')).toHaveLength(0);
    await wrapper.get('#auth-form').trigger('submit');
    await flushPromises();
    expect(window.location.pathname + window.location.hash).toBe('/admin-vue#/overview');
  });

  it('keeps all inputs when confirmation fails without submitting a request', async () => {
    const fetcher = installServer((path) => path === '/api/auth/status' ? json({ initialized: false }) : undefined);
    const { wrapper } = await openApp();
    await fillLogin(wrapper);
    await wrapper.get('#auth-confirm').setValue('other-password');
    await wrapper.get('#auth-form').trigger('submit');
    expect(wrapper.get('.auth-error').text()).toBe('两次输入的密码不一致');
    expect(wrapper.get<HTMLInputElement>('#auth-user').element.value).toBe('admin');
    expect(wrapper.get<HTMLInputElement>('#auth-pw').element.value).toBe('test-password');
    expect(wrapper.get<HTMLInputElement>('#auth-confirm').element.value).toBe('other-password');
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it('keeps failed credentials, displays server errors as text and allows retry', async () => {
    let fail = true;
    installServer((path) => path === '/Users/AuthenticateByName' && fail
      ? json({ error: '<b>账号或密码错误</b>' }, 401) : undefined);
    const { wrapper, auth } = await openApp();
    await fillLogin(wrapper);
    await wrapper.get('#auth-form').trigger('submit');
    await flushPromises();
    expect(wrapper.get<HTMLInputElement>('#auth-user').element.value).toBe('admin');
    expect(wrapper.get<HTMLInputElement>('#auth-pw').element.value).toBe('test-password');
    expect(wrapper.get('.auth-error').text()).toBe('<b>账号或密码错误</b>');
    expect(wrapper.find('.auth-error b').exists()).toBe(false);
    expect(auth.expiration).toBe(0);
    fail = false;
    await wrapper.get('#auth-form').trigger('submit');
    await flushPromises();
    expect(window.location.hash).toBe('#/overview');
  });

  it('offers explicit retry for a protected entry during a network failure', async () => {
    localStorage.setItem(TOKEN_KEY, 'stored-token');
    let offline = true;
    const fetcher = installServer((path) => path === '/Users/Me' && offline
      ? Promise.reject(new TypeError('Failed to fetch')) : undefined);
    const { wrapper } = await openApp('/overview');
    expect(wrapper.get('.auth-error').text()).toContain('请重试');
    expect(localStorage.getItem(TOKEN_KEY)).toBe('stored-token');
    expect(fetcher.mock.calls.filter(([path]) => path === '/Users/Me')).toHaveLength(1);
    offline = false;
    await wrapper.get('#auth-form button[type="button"]').trigger('click');
    await flushPromises();
    expect(window.location.pathname + window.location.hash).toBe('/admin-vue#/overview');
  });

  it('redirects concurrent protected 401s once and permits re-login', async () => {
    localStorage.setItem(TOKEN_KEY, 'expired-token');
    let expire = false;
    installServer((path) => expire && path.startsWith('/api/admin/')
      ? json({ error: 'unauthorized' }, 401) : undefined);
    const { router, auth, wrapper } = await openApp('/overview');
    const replace = vi.spyOn(router, 'replace');
    expire = true;
    await Promise.allSettled([
      apiClient.request('/api/admin/status', { auth: 'required' }),
      apiClient.request('/api/admin/libraries', { auth: 'required' }),
    ]);
    await flushPromises();
    expect(auth.expiration).toBe(1);
    expect(replace).toHaveBeenCalledTimes(1);
    expect(router.currentRoute.value.path).toBe('/login');
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull();
    expire = false;
    await fillLogin(wrapper);
    await wrapper.get('#auth-form').trigger('submit');
    await flushPromises();
    expect(window.location.pathname + window.location.hash).toBe('/admin-vue#/overview');
  });

  it('ignores an external returnTo after login', async () => {
    installServer();
    const { wrapper } = await openApp('/login?returnTo=%2F%2Fevil.example');
    await fillLogin(wrapper);
    await wrapper.get('#auth-form').trigger('submit');
    await flushPromises();
    expect(window.location.pathname + window.location.hash).toBe('/admin-vue#/overview');
  });

  it('preserves allowed overview query context through login', async () => {
    installServer();
    const { wrapper, router } = await openApp('/overview?library_id=3');
    await fillLogin(wrapper);
    await wrapper.get('#auth-form').trigger('submit');
    await flushPromises();
    expect(router.currentRoute.value.fullPath).toBe('/overview?library_id=3');
  });

  it('keeps the previous overview after a failed refresh and does not automatically retry', async () => {
    localStorage.setItem(TOKEN_KEY, 'stored-token');
    let fail = false;
    const fetcher = installServer((path) => path === '/api/admin/status' && fail
      ? json({ error: 'status unavailable' }, 503) : undefined);
    const { wrapper } = await openApp('/overview');
    const cards = wrapper.get('.cards').text();
    fail = true;
    await wrapper.get('button[aria-label="刷新"]').trigger('click');
    await flushPromises();
    expect(wrapper.get('.cards').text()).toBe(cards);
    expect(wrapper.get('[role="alert"]').text()).toBe('status unavailable');
    expect(fetcher.mock.calls.filter(([path]) => path === '/api/admin/status')).toHaveLength(2);
    fail = false;
    await wrapper.get('button[aria-label="刷新"]').trigger('click');
    await flushPromises();
    expect(wrapper.find('[role="alert"]').exists()).toBe(false);
  });

  it('renders an empty real Go catalog without requiring array-shaped empty slices', async () => {
    localStorage.setItem(TOKEN_KEY, 'stored-token');
    installServer((path) => {
      if (path === '/api/admin/libraries') return json({ items: null, total: 0 });
      if (path === '/api/admin/items?status=success&limit=1') {
        return json({ items: null, total: 0, limit: 1, offset: 0, userdata: {}, image_tags: {} });
      }
      if (path === '/api/admin/status') return json({ success: 0, manual: 0, pending: 0, incompatible: 0 });
      return undefined;
    });
    const { wrapper } = await openApp('/overview');
    expect(wrapper.get('.empty p').text()).toBe('档案为空');
    expect(wrapper.findAll('.cards .card strong').map((node) => node.text())).toEqual(['0', '0', '0', '0']);
  });

  it('prompts on preload error without losing input and unregisters the event on unmount', async () => {
    installServer();
    const remove = vi.spyOn(window, 'removeEventListener');
    const { wrapper } = await openApp();
    await fillLogin(wrapper);
    const event = new Event('vite:preloadError', { cancelable: true });
    window.dispatchEvent(event);
    await flushPromises();
    expect(event.defaultPrevented).toBe(true);
    expect(wrapper.get('#toasts').text()).toContain('刷新会丢失未保存的输入');
    expect(wrapper.get('#toasts button').text()).toBe('刷新页面');
    expect(wrapper.get<HTMLInputElement>('#auth-pw').element.value).toBe('test-password');
    expect(window.location.hash).toBe('#/login');
    wrapper.unmount();
    expect(remove.mock.calls.some(([name]) => name === 'vite:preloadError')).toBe(true);
    const afterUnmount = new Event('vite:preloadError', { cancelable: true });
    window.dispatchEvent(afterUnmount);
    expect(afterUnmount.defaultPrevented).toBe(false);
    const index = apps.findIndex((app) => app.wrapper === wrapper);
    apps[index]?.router.options.history.destroy();
    apps.splice(index, 1);
  });
});

describe('router fallbacks', () => {
  it('routes unknown paths to the overview without leaving the console shell', async () => {
    installServer();
    localStorage.setItem(TOKEN_KEY, 'stored-token');
    const { wrapper, router } = await openApp('/unknown');
    await flushPromises();
    expect(router.currentRoute.value.fullPath).toBe('/overview');
    expect(wrapper.find('#reindex').exists()).toBe(true);
    expect(wrapper.find('#content > .cards').exists()).toBe(true);
  });

  it('keeps invalid item IDs on the wall instead of opening a detail page', async () => {
    const fetcher = installServer((path) => path.startsWith('/api/admin/items?') ? json(itemsFixture) : undefined);
    localStorage.setItem(TOKEN_KEY, 'stored-token');
    const { wrapper, router } = await openApp('/item/0');
    await flushPromises();
    expect(router.currentRoute.value.path).toBe('/items');
    expect(fetcher.mock.calls.some(([path]) => String(path).startsWith('/api/admin/items?'))).toBe(true);
    expect(wrapper.find('#wall-count').exists()).toBe(true);
  });
});
