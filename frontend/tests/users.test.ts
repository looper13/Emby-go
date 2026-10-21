import { afterEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia, type Pinia } from 'pinia';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import UsersPage from '../src/pages/UsersPage.vue';
import { usersApi, parseManagedUser } from '../src/api/users';
import { apiClient } from '../src/api/client';
import { useAuthStore } from '../src/stores/auth';
import authFixture from '../../vue-migration/fixtures/json/auth-login.json';

function user(id: string, name: string) {
  return parseManagedUser({ ...authFixture.User, Id: id, Name: name, Policy: {
    IsAdministrator: id === '1', IsDisabled: false, EnableAllFolders: id === '1', EnabledFolders: [],
  } });
}
const apps: { wrapper: VueWrapper; pinia: Pinia; router: Router }[] = [];
afterEach(() => {
  for (const { wrapper, pinia, router } of apps.splice(0)) { wrapper.unmount(); router.options.history.destroy(); disposePinia(pinia); }
  localStorage.clear(); vi.restoreAllMocks();
});
async function page() {
  const pinia = createPinia(), auth = useAuthStore(pinia);
  auth.user = user('1', 'admin'); auth.token = 'test-token'; auth.phase = 'authenticated';
  vi.spyOn(usersApi, 'list').mockResolvedValue({ users: [user('1', 'admin'), user('2', 'viewer')], libraries: [{ Id: '1000000001', Name: 'Films', Type: 'movies' }] });
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/users', component: UsersPage }, { path: '/account', component: { template: '<p>Account</p>' } }, { path: '/login', component: { template: '<p>Login</p>' } },
  ] });
  await router.push('/users');
  const wrapper = mount({ template: '<RouterView />' }, { global: { plugins: [pinia, router] } });
  apps.push({ wrapper, pinia, router }); await router.isReady(); await flushPromises();
  return { wrapper, auth, router };
}

describe('user management', () => {
  it('creates a user with no access, then grants the selected libraries to that user', async () => {
    const create = vi.spyOn(usersApi, 'create').mockResolvedValue(user('3', 'family'));
    const grant = vi.spyOn(usersApi, 'savePolicy').mockResolvedValue(null);
    const { wrapper } = await page();
    expect(wrapper.find('input[name=disabled]').exists()).toBe(false);
    expect(wrapper.find('button.danger').exists()).toBe(false);
    await wrapper.findAll('button').find(button => button.text() === '新增用户')!.trigger('click');
    await wrapper.get('input[name=username]').setValue('family');
    await wrapper.get('input[name=password]').setValue('password-1234');
    await wrapper.get('input[name=confirmation]').setValue('different');
    await wrapper.get('[data-form=credentials]').trigger('submit');
    expect(create).not.toHaveBeenCalled(); expect(wrapper.text()).toContain('两次输入的密码不一致');
    await wrapper.get('input[name=confirmation]').setValue('password-1234');
    await wrapper.get('[data-form=credentials]').trigger('submit'); await flushPromises();
    expect(create).toHaveBeenCalledExactlyOnceWith('family', 'password-1234');
    expect(wrapper.get('[data-user-id="3"]').attributes('aria-pressed')).toBe('true');
    expect(wrapper.text()).toContain('未授权任何媒体库'); expect(grant).not.toHaveBeenCalled();
    await wrapper.get('fieldset input').setValue(true);
    await wrapper.get('[data-form=policy]').trigger('submit'); await flushPromises();
    expect(grant).toHaveBeenCalledExactlyOnceWith('3', { EnableAllFolders: false, EnabledFolders: ['1000000001'] });
  });
  it('retains failed permission drafts and guards duplicate submissions and unsaved navigation', async () => {
    let finish!: (value: null) => void;
    const grant = vi.spyOn(usersApi, 'savePolicy').mockRejectedValueOnce(new Error('save failed'))
      .mockReturnValueOnce(new Promise(resolve => { finish = resolve; }));
    const { wrapper, router } = await page();
    await wrapper.get('[data-user-id="2"]').trigger('click');
    await wrapper.get('fieldset input').setValue(true);
    await wrapper.get('[data-form=policy]').trigger('submit'); await flushPromises();
    expect(wrapper.text()).toContain('save failed');
    expect(wrapper.get<HTMLInputElement>('fieldset input').element.checked).toBe(true);
    vi.spyOn(window, 'confirm').mockReturnValue(false);
    await router.push('/account'); expect(router.currentRoute.value.path).toBe('/users');
    await wrapper.get('[data-form=policy]').trigger('submit');
    await wrapper.get('[data-form=policy]').trigger('submit');
    expect(grant).toHaveBeenCalledTimes(2); expect(wrapper.get('[data-user-id="1"]').attributes('disabled')).toBeDefined();
    finish(null); await flushPromises(); expect(wrapper.get('[data-user-id="1"]').attributes('disabled')).toBeUndefined();
  });
  it('disables and deletes another account, preserving the administrator session', async () => {
    const update = vi.spyOn(usersApi, 'update').mockResolvedValue(null);
    const remove = vi.spyOn(usersApi, 'remove').mockResolvedValue(null);
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    const { wrapper, auth } = await page();
    await wrapper.get('[data-user-id="2"]').trigger('click');
    await wrapper.get('input[name=disabled]').setValue(true);
    await wrapper.get('[data-form=credentials]').trigger('submit'); await flushPromises();
    expect(update).toHaveBeenCalledWith('2', { Username: 'viewer', NewPassword: '', IsDisabled: true });
    expect(auth.authenticated).toBe(true);
    expect(wrapper.get('[data-user-id="2"]').text()).toContain('已禁用');
    await wrapper.get('button.danger').trigger('click'); await flushPromises();
    expect(remove).toHaveBeenCalledWith('2'); expect(wrapper.find('[data-user-id="2"]').exists()).toBe(false);
    expect(wrapper.find('button.danger').exists()).toBe(false);
  });
  it('signs out when editing the current account and preserves rejected creation input', async () => {
    const create = vi.spyOn(usersApi, 'create').mockRejectedValue(new Error('用户名已存在'));
    const update = vi.spyOn(usersApi, 'update').mockResolvedValue(null);
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    const { wrapper, auth, router } = await page();
    await wrapper.findAll('button').find(button => button.text() === '新增用户')!.trigger('click');
    await wrapper.get('input[name=username]').setValue('duplicate');
    await wrapper.get('input[name=password]').setValue('password-1234');
    await wrapper.get('input[name=confirmation]').setValue('password-1234');
    await wrapper.get('[data-form=credentials]').trigger('submit'); await flushPromises();
    expect(create).toHaveBeenCalledOnce(); expect(wrapper.text()).toContain('用户名已存在');
    expect(wrapper.get<HTMLInputElement>('input[name=username]').element.value).toBe('duplicate');
    await wrapper.get('[data-user-id="1"]').trigger('click');
    await wrapper.get('input[name=username]').setValue('owner');
    await wrapper.get('[data-form=credentials]').trigger('submit'); await flushPromises();
    expect(update).toHaveBeenCalledWith('1', { Username: 'owner', NewPassword: '', IsDisabled: false });
    expect(auth.authenticated).toBe(false); expect(router.currentRoute.value.path).toBe('/login');
  });
});

describe('user API contracts', () => {
  it('uses the selected identity for edits and rejects malformed policy data', async () => {
    const request = vi.spyOn(apiClient, 'request').mockResolvedValue(null);
    await usersApi.savePolicy('7', { EnableAllFolders: false, EnabledFolders: [] });
    expect(request).toHaveBeenLastCalledWith('/Users/7/Policy', { auth: 'required', method: 'POST', json: { EnableAllFolders: false, EnabledFolders: [] } });
    expect(() => parseManagedUser({ ...user('2', 'viewer'), Policy: { IsAdministrator: false, IsDisabled: false, EnableAllFolders: false, EnabledFolders: [1] } })).toThrow();
  });
});
