import { afterEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia, type Pinia } from 'pinia';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import AccountPage from '../src/pages/AccountPage.vue';
import { accountApi } from '../src/api/account';
import { apiClient } from '../src/api/client';
import { parseUser } from '../src/api/contracts';
import { TOKEN_KEY, useAuthStore } from '../src/stores/auth';
import { useMediaWallStore } from '../src/stores/media-wall';
import authFixture from '../../vue-migration/fixtures/json/auth-login.json';

function fixture() {
  return {
    user: parseUser(authFixture.User),
    policy: { EnableAllFolders: true, EnabledFolders: [] as string[] },
    libraries: [{ Id: '1000000001', Name: 'Films', Type: 'movies' }, { Id: '1000000002', Name: 'Series', Type: 'tvshows' }],
  };
}
const apps: { wrapper: VueWrapper; pinia: Pinia; router: Router }[] = [];
afterEach(() => {
  for (const { wrapper, pinia, router } of apps.splice(0)) {
    wrapper.unmount(); router.options.history.destroy(); disposePinia(pinia);
  }
  localStorage.clear();
});
async function page() {
  localStorage.setItem(TOKEN_KEY, 'test-token');
  const pinia = createPinia();
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/', redirect: '/account' },
    { path: '/account', component: AccountPage },
    { path: '/login', component: { template: '<div>Login</div>' } },
    { path: '/other', component: { template: '<div>Other</div>' } },
  ] });
  const wrapper = mount({ template: '<RouterView />' }, { global: { plugins: [pinia, router] } });
  apps.push({ wrapper, pinia, router });
  const auth = useAuthStore(pinia);
  auth.user = fixture().user; auth.phase = 'authenticated';
  vi.spyOn(accountApi, 'get').mockResolvedValue(fixture());
  await router.replace('/account'); await router.isReady(); await flushPromises();
  return { wrapper, auth, router, wall: useMediaWallStore(pinia) };
}

describe('account API', () => {
  it('validates Emby policy fields and sends explicit authenticated mutations', async () => {
    const data = fixture();
    const request = vi.spyOn(apiClient, 'request').mockResolvedValue({ User: { ...authFixture.User, Policy: { ...authFixture.User.Policy, ...data.policy } }, Libraries: data.libraries });
    const signal = new AbortController().signal;
    expect(await accountApi.get(signal)).toEqual(data);
    expect(request).toHaveBeenLastCalledWith('/api/admin/account', { auth: 'required', signal });
    request.mockResolvedValue(null);
    await accountApi.savePolicy('1', { EnableAllFolders: false, EnabledFolders: [] });
    expect(request).toHaveBeenLastCalledWith('/Users/1/Policy', { auth: 'required', method: 'POST', json: { EnableAllFolders: false, EnabledFolders: [] } });
    request.mockResolvedValue({ User: { ...authFixture.User, Policy: { EnableAllFolders: false, EnabledFolders: null } }, Libraries: [] });
    await expect(accountApi.get(signal)).rejects.toThrow();
  });
});

describe('account settings', () => {
  it('does not sign out a newer session when an old credential update completes', async () => {
    let finish!: (value: null) => void;
    vi.spyOn(accountApi, 'update').mockReturnValue(new Promise(resolve => { finish = resolve; }));
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    const { wrapper, auth, router } = await page();
    await wrapper.get('#account-current').setValue('current-password');
    await wrapper.get('.account-credentials').trigger('submit');
    auth.token = 'newer-token';
    finish(null); await flushPromises();
    expect(auth.token).toBe('newer-token');
    expect(auth.authenticated).toBe(true);
    expect(router.currentRoute.value.path).toBe('/account');
  });
  it('saves explicit grants, clears the media snapshot, and restores unsaved changes', async () => {
    const save = vi.spyOn(accountApi, 'savePolicy').mockResolvedValue(null);
    const { wrapper, wall } = await page();
    const clear = vi.spyOn(wall, 'clear');
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined();
    await wrapper.get('.account-choice input').setValue(false);
    await wrapper.get('fieldset input[value="1000000001"]').setValue(true);
    expect(save).not.toHaveBeenCalled();
    await wrapper.get('form').trigger('submit'); await flushPromises();
    expect(save).toHaveBeenCalledWith('1', { EnableAllFolders: false, EnabledFolders: ['1000000001'] });
    expect(clear).toHaveBeenCalledOnce();
    await wrapper.get('fieldset input[value="1000000002"]').setValue(true);
    await wrapper.get('button[type="button"]').trigger('click');
    expect(wrapper.get<HTMLInputElement>('fieldset input[value="1000000002"]').element.checked).toBe(false);
    expect(save).toHaveBeenCalledOnce();
  });

  it('retains failed drafts and prevents concurrent policy and credential saves', async () => {
    let finish!: (value: null) => void;
    const pending = new Promise<null>(resolve => { finish = resolve; });
    const save = vi.spyOn(accountApi, 'savePolicy').mockRejectedValueOnce(new Error('save failed')).mockReturnValueOnce(pending);
    const update = vi.spyOn(accountApi, 'update').mockResolvedValue(null);
    const { wrapper } = await page();
    await wrapper.get('.account-choice input').setValue(false);
    await wrapper.get('form').trigger('submit'); await flushPromises();
    expect(wrapper.text()).toContain('save failed');
    expect(wrapper.get<HTMLInputElement>('.account-choice input').element.checked).toBe(false);
    await wrapper.get('form').trigger('submit');
    await wrapper.get('form').trigger('submit');
    await wrapper.get('.account-credentials').trigger('submit');
    expect(save).toHaveBeenCalledTimes(2);
    expect(update).not.toHaveBeenCalled();
    expect(wrapper.get('#account-current').attributes('disabled')).toBeDefined();
    finish(null); await flushPromises();
    expect(wrapper.get('#account-current').attributes('disabled')).toBeUndefined();
  });

  it('requires confirmation to discard changes and validates matching passwords', async () => {
    const update = vi.spyOn(accountApi, 'update').mockResolvedValue(null);
    const { wrapper, router } = await page();
    await wrapper.get('#account-name').setValue('owner');
    vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValue(true);
    await router.push('/other');
    expect(router.currentRoute.value.path).toBe('/account');
    await wrapper.get('#account-current').setValue('current-password');
    await wrapper.get('#account-new').setValue('new-password');
    await wrapper.get('#account-confirm').setValue('different-password');
    await wrapper.get('.account-credentials').trigger('submit');
    expect(wrapper.text()).toContain('两次输入的新密码不一致');
    expect(update).not.toHaveBeenCalled();
    await router.push('/other');
    expect(router.currentRoute.value.path).toBe('/other');
  });

  it('keeps failed credentials and signs out after a successful update', async () => {
    const update = vi.spyOn(accountApi, 'update').mockRejectedValueOnce(new Error('wrong password')).mockResolvedValue(null);
    const { wrapper, auth, router } = await page();
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    await wrapper.get('#account-name').setValue('owner');
    await wrapper.get('#account-current').setValue('current-password');
    await wrapper.get('.account-credentials').trigger('submit'); await flushPromises();
    expect(wrapper.text()).toContain('wrong password');
    expect(wrapper.get<HTMLInputElement>('#account-current').element.value).toBe('current-password');
    expect(auth.authenticated).toBe(true);
    await wrapper.get('.account-credentials').trigger('submit'); await flushPromises();
    expect(update).toHaveBeenLastCalledWith({ Username: 'owner', CurrentPassword: 'current-password', NewPassword: '' });
    expect(auth.authenticated).toBe(false);
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull();
    expect(router.currentRoute.value.path).toBe('/login');
  });
});
