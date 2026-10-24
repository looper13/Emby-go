import { afterEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { configFixture } from './fixtures/system-config';
import { parseConfigState, systemConfigApi, type ConfigState } from '../src/api/system-config';
import SystemConfigForm from '../src/components/settings/SystemConfigForm.vue';
import SettingsPage from '../src/pages/SettingsPage.vue';
import { adminApi } from '../src/api/admin';
import settings from '../../vue-migration/fixtures/json/settings.json';

const wrappers: VueWrapper[] = [];
afterEach(() => { for (const w of wrappers.splice(0)) w.unmount(); });
function form() { const w = mount(SystemConfigForm, { props: { state: configFixture(), saving: false } }); wrappers.push(w); return w; }
function page() {
  vi.spyOn(adminApi, 'settings').mockResolvedValue(settings);
  vi.spyOn(systemConfigApi, 'read').mockImplementation(async () => configFixture());
  const w = mount(SettingsPage); wrappers.push(w); return w;
}
describe('instance configuration', () => {
  it('validates response types and monitor modes', () => {
    expect(parseConfigState(configFixture()).config.redis_password_configured).toBe(true);
    expect(() => parseConfigState({ ...configFixture(), restart_fields: 'port' })).toThrow();
    expect(() => parseConfigState({ ...configFixture(), config: { ...configFixture().config, library_monitor_mode: 'typo' } })).toThrow();
  });
  it('saves only explicit changes and restores the saved baseline without auto-saving', async () => {
    const w = form();
    expect(w.get<HTMLInputElement>('#cfg-password').element.value).toBe('');
    await w.get('#cfg-name').setValue('draft');
    await w.get('#cfg-port').setValue('18081');
    expect(w.emitted('save')).toBeUndefined();
    expect(w.get('#cfg-dirty').text()).toContain('未保存');
    expect(w.text()).toContain('本次修改中的 监听端口');
    await w.get('#cfg-restore').trigger('click');
    expect(w.get<HTMLInputElement>('#cfg-name').element.value).toBe('Emby-go');
    expect(w.get('#cfg-save').attributes('disabled')).toBeDefined();
    await w.get('#cfg-name').setValue('new name');
    await w.get('form').trigger('submit');
    expect(w.emitted('save')?.[0]).toEqual([{ server_name: 'new name' }]);
  });
  it('requires an explicit clear action and sends editable domains', async () => {
    const w = form();
    await w.get('#cfg-clear-password').setValue(true);
    await w.get('#cfg-add-domain').trigger('click');
    await w.get('#cfg-domain-name-0').setValue('内网');
    await w.get('#cfg-domain-url-0').setValue('http://localhost:18080/emby');
    await w.get('form').trigger('submit');
    expect(w.emitted('save')?.[0]).toEqual([{ clear_redis_password: true, server_domains: [{ name: '内网', url: 'http://localhost:18080/emby' }] }]);
    await w.get('[aria-label="删除线路 1"]').trigger('click');
    expect(w.find('#cfg-domain-name-0').exists()).toBe(false);
    await w.setProps({ state: { ...configFixture(), writable: false } });
    expect(w.get('fieldset').attributes('disabled')).toBeDefined();
    await w.get('form').trigger('submit');
    expect(w.emitted('save')).toHaveLength(1);
  });
  it('saves AList credentials explicitly without sending configuration flags and clears them on restore', async()=>{
    const w=form();await w.get('#cfg-alist-url').setValue('http://alist.test');await w.get('#cfg-alist-token').setValue('secret');await w.get('form').trigger('submit');
    expect(w.emitted('save')?.[0]).toEqual([{alist_url:'http://alist.test',alist_token:'secret'}]);
    await w.get('#cfg-restore').trigger('click');expect(w.get<HTMLInputElement>('#cfg-alist-token').element.value).toBe('');
    await w.get('#cfg-clear-alist-token').setValue(true);await w.get('form').trigger('submit');expect(w.emitted('save')?.[1]).toEqual([{clear_alist_token:true}]);
  });
  it('retains failed drafts, prevents duplicate saves, and reports saved versus effective values', async () => {
    let resolve!: (state: ConfigState) => void;
    const pending = new Promise<ConfigState>(r => { resolve = r; });
    const save = vi.spyOn(systemConfigApi, 'save').mockRejectedValueOnce(new Error('文件只读')).mockReturnValueOnce(pending);
    const w = page(); await flushPromises();
    await w.get('#cfg-port').setValue('18081');
    await w.get('form').trigger('submit'); await flushPromises();
    expect(w.text()).toContain('文件只读'); expect(w.text()).toContain('草稿已保留');
    expect(w.get<HTMLInputElement>('#cfg-port').element.value).toBe('18081');
    await w.get('form').trigger('submit'); await w.get('form').trigger('submit');
    expect(save).toHaveBeenCalledTimes(2); expect(w.get('fieldset').attributes('disabled')).toBeDefined();
    expect(save).toHaveBeenLastCalledWith('rev-1', { port: 18081 });
    const next = configFixture(); next.config.port = 18081; next.restart_fields = ['port']; next.revision = 'rev-2'; resolve(next);
    await flushPromises();
    expect(w.text()).toContain('已保存，等待重启：监听端口'); expect(w.text()).toContain('当前端口：18080');
    expect(w.get('#cfg-dirty').text()).toContain('与已保存配置一致');
  });
  it('requires confirmation before reloading a dirty form and clears it only after a successful read', async () => {
    const w = page(); await flushPromises();
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValue(true);
    await w.get('#cfg-name').setValue('unsaved'); await w.get('#cfg-reload').trigger('click');
    expect(systemConfigApi.read).toHaveBeenCalledTimes(1);
    await w.get('#cfg-reload').trigger('click'); await flushPromises();
    expect(confirm).toHaveBeenCalledTimes(2); expect(systemConfigApi.read).toHaveBeenCalledTimes(2);
    expect(w.get<HTMLInputElement>('#cfg-name').element.value).toBe('Emby-go');
  });
});
