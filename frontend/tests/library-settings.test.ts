import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia';
import { adminApi } from '../src/api/admin';
import { ApiError, apiClient } from '../src/api/client';
import { libraryDirectoryPaths, parseLibraries, type LibraryDto } from '../src/api/contracts';
import LibraryForm from '../src/components/libraries/LibraryForm.vue';
import LibraryPlaybackHeaders from '../src/components/libraries/LibraryPlaybackHeaders.vue';
import LibrariesPage from '../src/pages/LibrariesPage.vue';

const library: LibraryDto = { Id: 7, Name: '多目录影片', Path: '/media/one', Paths: ['/media/one', '/media/two'],
  Type: 'movies', SourceType: 'nfo_directory', ProxyEnabled: true, ResolveRedirects: false, Revision: 3,
  ProxyHeaders: { Referer: 'https://source.example/watch/', Origin: 'https://source.example', 'User-Agent': 'SourcePlayer/1.0' } };
const wrappers: VueWrapper[] = [];
let pinia: Pinia;
function form(value?: LibraryDto) { const wrapper = mount(LibraryForm, { props: { submitting: false, library: value } }); wrappers.push(wrapper); return wrapper; }
function page() { vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [library], total: 1 }); const wrapper = mount(LibrariesPage, { global: { plugins: [pinia] } }); wrappers.push(wrapper); return wrapper; }
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done; }); return { resolve, promise }; }
beforeEach(() => { pinia = createPinia(); setActivePinia(pinia); localStorage.clear(); });
afterEach(() => { for (const wrapper of wrappers.splice(0)) wrapper.unmount(); disposePinia(pinia); vi.unstubAllGlobals(); localStorage.clear(); });

describe('多目录与请求头契约', () => {
  it('保留所有路径和请求头并兼容单目录及在线库', () => {
    const parsed = parseLibraries({ items: [library], total: 1 }).items![0]!;
    expect(parsed.Paths).toEqual(library.Paths); expect(parsed.ProxyHeaders).toEqual(library.ProxyHeaders);
    parsed.Paths!.push('/extra'); parsed.ProxyHeaders!.Origin = 'changed';
    expect(library.Paths).toHaveLength(2); expect(library.ProxyHeaders!.Origin).toBe('https://source.example');
    expect(libraryDirectoryPaths({ Id: 1, Name: '旧库', Path: '/old' })).toEqual(['/old']);
    expect(libraryDirectoryPaths(parseLibraries({ items: [{ Id: 2, Name: '在线库', Path: '', Paths: null, ProxyHeaders: null }], total: 1 }).items![0]!)).toEqual([]);
    for (const change of [{ Paths: 'bad' }, { Paths: [''] }, { Paths: ['/one', 3] }, { ProxyHeaders: [] }, { ProxyHeaders: { Origin: 3 } }]) {
      expect(() => parseLibraries({ items: [{ ...library, ...change }], total: 1 })).toThrow();
    }
  });

  it('以数组创建目录并将播放源请求头写入JSON正文', async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(library), { status: 200 })); vi.stubGlobal('fetch', fetcher);
    apiClient.setAuthHandlers({ getToken: () => 'fixture', onUnauthorized: () => {} });
    await adminApi.createLibrary({ Name: library.Name, Paths: library.Paths!, Type: 'movies', ProxyEnabled: true, ResolveRedirects: false, ProxyHeaders: library.ProxyHeaders! });
    const options = fetcher.mock.calls[0]![1]!;
    expect(JSON.parse(options.body)).toMatchObject({ Paths: ['/media/one', '/media/two'], ProxyHeaders: library.ProxyHeaders });
    expect(options.headers.get('Referer')).toBeNull(); expect(options.headers.get('Origin')).toBeNull();
  });
});

describe('目录与播放请求头表单', () => {
  it('添加多个目录、清理空白，并在开关代理后保留请求头草稿', async () => {
    const wrapper = form(); await wrapper.get('#lib-name').setValue('  影片  '); await wrapper.get('#lib-path').setValue(' /media/one ');
    await wrapper.get('[data-add-path]').trigger('click'); await wrapper.get('#lib-path-2').setValue('/media/two');
    await wrapper.get('#lib-proxy').setValue(true); await wrapper.get('#lib-referer').setValue(' https://source.example/watch/ ');
    await wrapper.get('#lib-origin').setValue('https://source.example'); await wrapper.get('[data-add-header]').trigger('click');
    await wrapper.get('[data-header-name]').setValue('User-Agent'); await wrapper.get('[data-header-value]').setValue('SourcePlayer/1.0');
    await wrapper.get('#lib-proxy').setValue(false); expect(wrapper.get('fieldset').isVisible()).toBe(false);
    await wrapper.get('#lib-proxy').setValue(true); expect(wrapper.get<HTMLInputElement>('#lib-referer').element.value).toContain('source.example');
    await wrapper.get('form').trigger('submit');
    expect(wrapper.emitted('submit')![0]![0]).toEqual({ Name: '影片', Paths: ['/media/one', '/media/two'], Type: 'movies',
      ProxyEnabled: true, ResolveRedirects: false, ProxyHeaders: library.ProxyHeaders });
  });

  it('拒绝空目录和重复请求头，允许清除已保存请求头', async () => {
    const wrapper = form(library); await wrapper.get('[data-add-path]').trigger('click'); await wrapper.get('form').trigger('submit');
    expect(wrapper.emitted('submit')).toBeUndefined(); expect(wrapper.get('[role="alert"]').text()).toContain('每个目录路径');
    await wrapper.get('button[aria-label="移除目录 3"]').trigger('click');
    await wrapper.get('[data-header-name]').setValue('referer'); await wrapper.get('form').trigger('submit');
    expect(wrapper.emitted('submit')).toBeUndefined(); expect(wrapper.get('[role="alert"]').text()).toContain('重复');
    await wrapper.get('[data-header-name]').setValue('X-Source');
    await wrapper.get('[data-header-value]').setValue(''); await wrapper.get('#edit-lib-referer').setValue(''); await wrapper.get('#edit-lib-origin').setValue('');
    await wrapper.get('form').trigger('submit'); expect(wrapper.emitted('submit')![0]![0]).toMatchObject({ ProxyHeaders: {} });
    await wrapper.setProps({ submitting: true }); expect(wrapper.get('#edit-lib-name').attributes('disabled')).toBeDefined();
    await wrapper.get('form').trigger('submit'); expect(wrapper.emitted('submit')).toHaveLength(1);
  });

  it('拒绝已有请求头值中的换行或控制字符', () => {
    const wrapper = mount(LibraryPlaybackHeaders, { props: { initialHeaders: { 'X-Source': 'bad\r\nheader' }, disabled: false, idPrefix: 'test' } }); wrappers.push(wrapper);
    expect(() => wrapper.vm.getHeaders()).toThrow('控制字符');
  });
});

describe('已有媒体库编辑', () => {
  it('卡片和列表显示所有目录，使用版本号保存并阻止重复提交', async () => {
    const pending = deferred<LibraryDto>(); const update = vi.spyOn(adminApi, 'updateLibrary').mockReturnValue(pending.promise);
    const wrapper = page(); await flushPromises(); expect(wrapper.text()).toContain('/media/one'); expect(wrapper.text()).toContain('/media/two');
    await wrapper.get('[aria-label="列表视图"]').trigger('click'); expect(wrapper.findAll('.lib-path')).toHaveLength(2);
    await wrapper.get('[data-lib-edit="7"]').trigger('click'); expect(wrapper.get<HTMLInputElement>('#edit-lib-origin').element.value).toBe('https://source.example');
    await wrapper.get('#edit-lib-name').setValue('新名称');
    await wrapper.get('#library-edit-form [data-add-path]').trigger('click');
    const editPaths = wrapper.findAll('#library-edit-form [name="Paths"]'); await editPaths[2]!.setValue('/media/three');
    await wrapper.get('#edit-lib-origin').setValue('https://next.example');
    await wrapper.get('#library-edit-form').trigger('submit'); await wrapper.get('#library-edit-form').trigger('submit');
    expect(update).toHaveBeenCalledTimes(1); expect(update.mock.calls[0]![1]).toMatchObject({ Name: '新名称', Paths: ['/media/one', '/media/two', '/media/three'], Revision: 3,
      ProxyHeaders: { ...library.ProxyHeaders, Origin: 'https://next.example' } });
    expect(wrapper.get<HTMLInputElement>('#edit-lib-name').element.disabled).toBe(true);
    pending.resolve({ ...library, Name: '新名称', Revision: 4 }); await flushPromises(); expect(wrapper.find('#library-edit-form').exists()).toBe(false);
    expect(adminApi.libraries).toHaveBeenCalledTimes(2);
  });

  it('移除目录先确认，取消时保留草稿且不提交', async () => {
    const update = vi.spyOn(adminApi, 'updateLibrary').mockResolvedValue({ ...library, Paths: ['/media/one'], Revision: 4 });
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValue(true); const wrapper = page(); await flushPromises();
    await wrapper.get('[data-lib-edit="7"]').trigger('click'); await wrapper.get('#library-edit-form button[aria-label="移除目录 2"]').trigger('click');
    await wrapper.get('#library-edit-form').trigger('submit'); expect(update).not.toHaveBeenCalled(); expect(wrapper.find('#library-edit-form').exists()).toBe(true);
    expect(confirm).toHaveBeenLastCalledWith('移除目录会清除不再属于此媒体库的影片索引及其播放进度/收藏。不会删除磁盘文件，确认保存？');
    await wrapper.get('#library-edit-form').trigger('submit'); await flushPromises(); expect(update.mock.calls[0]![1].Paths).toEqual(['/media/one']);
  });

  it('保存失败保留目录和请求头，版本冲突刷新后可核对重试', async () => {
    const update = vi.spyOn(adminApi, 'updateLibrary').mockRejectedValueOnce(new ApiError('媒体库已被修改', 409)).mockResolvedValueOnce({ ...library, Revision: 5 });
    const wrapper = page(); await flushPromises(); vi.mocked(adminApi.libraries).mockResolvedValue({ items: [{ ...library, Revision: 4 }], total: 1 });
    await wrapper.get('[data-lib-edit="7"]').trigger('click'); await wrapper.get('#edit-lib-name').setValue('保留草稿');
    await wrapper.get('#edit-lib-referer').setValue('https://draft.example'); await wrapper.get('#library-edit-form').trigger('submit'); await flushPromises();
    expect(wrapper.get<HTMLInputElement>('#edit-lib-name').element.value).toBe('保留草稿'); expect(wrapper.get<HTMLInputElement>('#edit-lib-referer').element.value).toBe('https://draft.example');
    expect(wrapper.text()).toContain('核对当前草稿'); expect(adminApi.libraries).toHaveBeenCalledTimes(2);
    await wrapper.get('#library-edit-form').trigger('submit'); await flushPromises();
    expect(update.mock.calls[1]![1]).toMatchObject({ Name: '保留草稿', Revision: 4, ProxyHeaders: { ...library.ProxyHeaders, Referer: 'https://draft.example' } });
  });

  it('离开页面后完成保存，停止GET且不刷新已卸载页面', async () => {
    const pending = deferred<LibraryDto>(); vi.spyOn(adminApi, 'updateLibrary').mockReturnValue(pending.promise); const wrapper = page(); await flushPromises();
    await wrapper.get('[data-lib-edit="7"]').trigger('click'); await wrapper.get('#library-edit-form').trigger('submit');
    const signal = vi.mocked(adminApi.libraries).mock.calls[0]![0]!; wrapper.unmount(); expect(signal.aborted).toBe(true);
    pending.resolve({ ...library, Revision: 4 }); await flushPromises(); expect(adminApi.libraries).toHaveBeenCalledTimes(1);
  });
});
