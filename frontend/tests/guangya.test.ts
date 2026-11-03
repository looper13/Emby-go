import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { defineComponent, ref } from 'vue';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia, setActivePinia } from 'pinia';
import { apiClient } from '../src/api/client';
import { cloud115Api, parseCloudConfig, parseOfflineState, type CloudProvider, type OfflineState, type OfflineTask } from '../src/api/cloud115';
import { guangyaApi } from '../src/api/guangya';
import type { R18Resource } from '../src/api/r18';
import CloudGuangyaSettings from '../src/components/settings/CloudGuangyaSettings.vue';
import { useR18Playback, type R18Playback } from '../src/composables/useR18Playback';
import { playbackVersions } from '../src/lib/r18-playback';
import { useAuthStore } from '../src/stores/auth';
import { usePlayerStore } from '../src/stores/player';
import * as playerRuntime from '../src/lib/player';

const hash = 'a'.repeat(40);
const resource: R18Resource = { id: 'resource', title: '测试影片', number: 'TEST-001', date: '', magnet: `magnet:?xt=urn:btih:${hash}`, website: '', detail_url: '', size_bytes: 0, size_mb: 0, tags: [], origins: ['online'] };
const empty: OfflineState = { configured: true, tasks: [], sources: [] };
const task: OfflineTask = { id: 'task', resource_id: 'resource', info_hash: hash, magnet: resource.magnet, status: 'downloading', progress: 30, stage: '', error: '', retryable: false, version: 1, files: [], file_id: '' };
const source = { Id: 'guangya-task', InfoHash: hash, ResourceId: 'resource', Name: '正片.mp4', DirectStreamUrl: '/Videos/r18-2-video/stream?MediaSourceId=guangya-task&playback_token=fixture', Container: 'mp4', Default: false };
const ready: OfflineState = { ...empty, tasks: [{ ...task, status: 'ready', progress: 100, file_id: 'main', files: [{ id: 'main', name: source.Name, path: source.Name, size: 1024 ** 3 }] }], sources: [source] };
const provider = ref<CloudProvider>('115'), enabled = ref(true);
let pinia: ReturnType<typeof createPinia>, wrapper: VueWrapper | undefined, workflow: R18Playback;
function start(admin = true) {
  useAuthStore().user = { Policy: { IsAdministrator: admin } } as never;
  wrapper = mount(defineComponent({
    setup() {
      workflow = useR18Playback(() => ({ library: '2', video: 'video', title: '测试影片' }), ref([resource]), () => enabled.value,
        () => provider.value === 'guangya' ? { id: 'guangya', name: '光鸭', api: guangyaApi } : { id: '115', name: '115', api: cloud115Api });
      return {};
    }, template: '<div />',
  }), { global: { plugins: [pinia] } });
}
beforeEach(() => {
  vi.useFakeTimers(); pinia = createPinia(); setActivePinia(pinia); provider.value = '115'; enabled.value = true;
  vi.spyOn(cloud115Api, 'state').mockResolvedValue(empty);
  vi.spyOn(guangyaApi, 'state').mockResolvedValue(empty);
  vi.spyOn(playerRuntime, 'loadPlayerFactory').mockResolvedValue(() => ({} as never));
});
afterEach(() => { wrapper?.unmount(); wrapper = undefined; disposePinia(pinia); vi.restoreAllMocks(); vi.useRealTimers(); });

describe('光鸭配置与 API 契约', () => {
  it('配置读取只保留公开字段，不回显令牌或设备标识', () => {
    const config = parseCloudConfig({ configured: true, account_id: 'fixture-account', folder_id: 'fixture-folder', folder_name: '缓存-24h自动删除', revision: 3, access_token: 'private-fixture', refresh_token: 'private-refresh', device_id: 'private-device' }, '光鸭');
    expect(config).toEqual({ configured: true, account_id: 'fixture-account', folder_id: 'fixture-folder', folder_name: '缓存-24h自动删除', revision: 3 });
    expect(() => parseCloudConfig({ ...config, revision: '3' }, '光鸭')).toThrow('光鸭 响应格式不正确');
  });
  it('光鸭接口使用独立路径，参数编码且保持现有115入口', async () => {
    vi.mocked(cloud115Api.state).mockRestore(); vi.mocked(guangyaApi.state).mockRestore();
    const request = vi.spyOn(apiClient, 'request').mockResolvedValue(empty);
    await guangyaApi.state('2', 'video/next');
    expect(request).toHaveBeenLastCalledWith('/api/r18/libraries/2/videos/video%2Fnext/offline/guangya', expect.objectContaining({ auth: 'required' }));
    await guangyaApi.prepare('2', 'video', source.Id);
    expect(request).toHaveBeenLastCalledWith('/api/r18/libraries/2/videos/video/offline/guangya/prepare?source_id=guangya-task', expect.objectContaining({ auth: 'required' }));
    await guangyaApi.select('2', 'video', 'task', 'main', undefined, false, true);
    expect(request).toHaveBeenLastCalledWith('/api/r18/libraries/2/videos/video/offline/guangya/selection', expect.objectContaining({ method: 'POST', json: { task_id: 'task', file_id: 'main', default: false, prepare: true } }));
    await guangyaApi.change('2', 'video', task, 'retry');
    expect(request).toHaveBeenLastCalledWith('/api/r18/libraries/2/videos/video/offline/guangya/tasks/task/retry', expect.objectContaining({ method: 'POST', json: { version: 1 } }));
    await guangyaApi.submit('2', 'video', { resource_id: 'resource' });
    expect(request).toHaveBeenLastCalledWith('/api/r18/libraries/2/videos/video/offline/guangya', expect.objectContaining({ method: 'POST', json: { resource_id: 'resource' } }));
    await cloud115Api.state('2', 'video');
    expect(request).toHaveBeenLastCalledWith('/api/r18/libraries/2/videos/video/offline', expect.objectContaining({ auth: 'required' }));
    request.mockResolvedValue({ configured: true, account_id: 'fixture-account', folder_id: 'fixture-folder', revision: 2 });
    await guangyaApi.config();
    expect(request).toHaveBeenLastCalledWith('/api/admin/guangya/connection', expect.objectContaining({ auth: 'required' }));
    await guangyaApi.save({ access_token: 'fixture-access', refresh_token: 'fixture-refresh', device_id: '0123456789abcdef0123456789abcdef', revision: 2 });
    expect(request).toHaveBeenLastCalledWith('/api/admin/guangya/connection', expect.objectContaining({ method: 'PUT', json: { access_token: 'fixture-access', refresh_token: 'fixture-refresh', device_id: '0123456789abcdef0123456789abcdef', revision: 2 } }));
  });
  it('光鸭播放地址保持服务端鉴权入口，并拒绝其它网盘的播放源', () => {
    expect(parseOfflineState(ready, '光鸭', 'guangya').sources[0]?.Id).toBe(source.Id);
    expect(() => parseOfflineState({ ...ready, sources: [{ ...source, DirectStreamUrl: 'https://untrusted.example/video' }] }, '光鸭', 'guangya')).toThrow('光鸭 播放地址不正确');
    expect(() => parseOfflineState({ ...ready, sources: [{ ...source, Id: '115-task' }] }, '光鸭', 'guangya')).toThrow('光鸭 播放源标识不正确');
    expect(playbackVersions([resource], { ...ready, sources: [{ ...source, Id: '115-task' }, source] }, 'guangya')[0]?.source?.Id).toBe(source.Id);
    expect(playbackVersions([resource], { ...ready, sources: [{ ...source, InfoHash: undefined, ResourceId: undefined }] }, 'guangya')[0]?.source?.Id).toBe(source.Id);
  });
  it('保存后的三种敏感输入均清空，已配置凭据可留空保留', async () => {
    vi.spyOn(guangyaApi, 'config').mockResolvedValue({ configured: false, account_id: '', folder_id: '', revision: 0 });
    const save = vi.spyOn(guangyaApi, 'save').mockResolvedValue(undefined);
    wrapper = mount(CloudGuangyaSettings, { global: { plugins: [pinia] } }); await flushPromises();
    await wrapper.get('#guangya-access-token').setValue(' fixture-access ');
    await wrapper.get('#guangya-refresh-token').setValue('fixture-refresh');
    await wrapper.get('#guangya-device-id').setValue('0123456789abcdef0123456789abcdef');
    vi.mocked(guangyaApi.config).mockResolvedValue({ configured: true, account_id: 'fixture-account', folder_id: 'fixture-folder', revision: 1 });
    await wrapper.get('form').trigger('submit'); await flushPromises();
    expect(save).toHaveBeenLastCalledWith({ access_token: 'fixture-access', refresh_token: 'fixture-refresh', device_id: '0123456789abcdef0123456789abcdef', revision: 0 }, expect.any(AbortSignal));
    for (const id of ['guangya-access-token', 'guangya-refresh-token', 'guangya-device-id']) {
      expect((wrapper.get(`#${id}`).element as HTMLInputElement).value).toBe(''); expect(wrapper.get(`#${id}`).attributes('type')).toBe('password');
    }
    expect(wrapper.text()).toContain('缓存-24h自动删除'); expect(wrapper.find('#guangya-folder-id').exists()).toBe(false);
    await wrapper.get('form').trigger('submit'); await flushPromises();
    expect(save).toHaveBeenLastCalledWith({ access_token: '', refresh_token: '', device_id: '', revision: 1 }, expect.any(AbortSignal));
  });
  it('连接校验失败保留输入，显示可处理的错误', async () => {
    vi.spyOn(guangyaApi, 'config').mockResolvedValue({ configured: false, account_id: '', folder_id: '', revision: 0 });
    vi.spyOn(guangyaApi, 'save').mockRejectedValue(new Error('光鸭登录已过期，请重新获取令牌'));
    wrapper = mount(CloudGuangyaSettings, { global: { plugins: [pinia] } }); await flushPromises();
    await wrapper.get('#guangya-access-token').setValue('fixture-access'); await wrapper.get('form').trigger('submit'); await flushPromises();
    expect(wrapper.get('[role="alert"]').text()).toContain('登录已过期');
    expect((wrapper.get('#guangya-access-token').element as HTMLInputElement).value).toBe('fixture-access');
  });
});

describe('网盘选择与播放隔离', () => {
  it('默认115，切换网盘只读取各自状态，不自动提交磁力', async () => {
    const submit115 = vi.spyOn(cloud115Api, 'submit'), submitGuangya = vi.spyOn(guangyaApi, 'submit');
    start(); await flushPromises(); expect(cloud115Api.state).toHaveBeenCalledTimes(1); expect(guangyaApi.state).not.toHaveBeenCalled();
    provider.value = 'guangya'; await flushPromises();
    expect(guangyaApi.state).toHaveBeenCalledTimes(1); expect(workflow.providerName.value).toBe('光鸭');
    expect(workflow.versions.value[0]?.provider).toBe('guangya'); expect(submit115).not.toHaveBeenCalled(); expect(submitGuangya).not.toHaveBeenCalled();
  });
  it('光鸭下载就绪后通过自己的文件选择接口自动播放，115不参与', async () => {
    provider.value = 'guangya';
    const read = vi.spyOn(guangyaApi, 'state').mockResolvedValue(empty);
    const submit = vi.spyOn(guangyaApi, 'submit').mockResolvedValue({ ...empty, tasks: [task] });
    const select = vi.spyOn(guangyaApi, 'select').mockResolvedValue(ready);
    const prepare115 = vi.spyOn(cloud115Api, 'prepare');
    start(); await flushPromises(); await workflow.play(workflow.versions.value[0]!);
    expect(submit).toHaveBeenCalledExactlyOnceWith('2', 'video', { resource_id: 'resource' }, expect.any(AbortSignal));
    expect(workflow.message.value).toContain('光鸭 离线下载中');
    read.mockResolvedValue({ ...ready, sources: [], tasks: ready.tasks.map(t => ({ ...t, status: 'waiting_selection', file_id: '' })) });
    await vi.advanceTimersByTimeAsync(1000);
    expect(select).toHaveBeenCalledExactlyOnceWith('2', 'video', task.id, 'main', expect.any(AbortSignal), false, true);
    expect(usePlayerStore().current?.url).toBe(source.DirectStreamUrl); expect(cloud115Api.state).not.toHaveBeenCalled(); expect(prepare115).not.toHaveBeenCalled();
  });
  it.each([
    ['queued', 'cache', '排队中 · 正在核验缓存目录 · 就绪后自动播放'],
    ['queued', 'submit', '排队中 · 正在提交到网盘 · 就绪后自动播放'],
    ['downloading', 'poll', '光鸭 离线下载中 · 正在查询网盘下载状态 · 30% · 就绪后自动播放'],
  ])('光鸭 %s 的 %s 阶段显示操作说明并保持网盘隔离', async (status, stage, message) => {
    provider.value = 'guangya';
    vi.spyOn(guangyaApi, 'state').mockResolvedValue({ ...empty, tasks: [{ ...task, status, stage, progress: status === 'downloading' ? task.progress : 0 }] });
    const submit = vi.spyOn(guangyaApi, 'submit');
    start(); await flushPromises(); await workflow.play(workflow.versions.value[0]!);
    expect(workflow.message.value).toBe(message);
    expect(workflow.providerName.value).toBe('光鸭'); expect(workflow.waiting.value).toBe(hash);
    expect(submit).not.toHaveBeenCalled(); expect(cloud115Api.state).not.toHaveBeenCalled(); expect(usePlayerStore().current).toBeNull();
  });
  it('切换时立即撤销旧请求并清空等待，迟到115结果不能覆盖光鸭或打开播放器', async () => {
    const oldSource = { ...source, Id: '115-task' };
    const read115 = vi.spyOn(cloud115Api, 'state').mockResolvedValue({ ...empty, tasks: [task] });
    vi.spyOn(guangyaApi, 'state').mockResolvedValue({ ...empty, tasks: [{ ...task, id: 'guangya-download' }] });
    const prepare = vi.spyOn(cloud115Api, 'prepare');
    start(); await flushPromises(); await workflow.play(workflow.versions.value[0]!); expect(workflow.waiting.value).toBe(hash);
    let finish!: (value: OfflineState) => void;
    read115.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
    const oldLoad = workflow.load(); await flushPromises(); const signal = read115.mock.lastCall![2]!;
    const oldVersion = workflow.versions.value[0]!;
    provider.value = 'guangya';
    expect(signal.aborted).toBe(true); expect(workflow.waiting.value).toBe(''); expect(workflow.state.value.tasks).toEqual([]); expect(workflow.selections.value).toEqual({});
    await flushPromises(); finish({ ...ready, sources: [oldSource] }); await oldLoad;
    expect(workflow.state.value.tasks[0]?.id).toBe('guangya-download'); expect(workflow.state.value.sources).toEqual([]);
    await workflow.play(oldVersion); expect(prepare).not.toHaveBeenCalled(); expect(usePlayerStore().current).toBeNull();
    const oldReadCount = read115.mock.calls.length; await vi.advanceTimersByTimeAsync(15000); expect(read115).toHaveBeenCalledTimes(oldReadCount);
  });
  it('准备直链期间切换，迟到光鸭结果不会打开原影片', async () => {
    provider.value = 'guangya'; vi.spyOn(guangyaApi, 'state').mockResolvedValue(ready);
    let finish!: (value: OfflineState) => void;
    const prepare = vi.spyOn(guangyaApi, 'prepare').mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
    start(false); await flushPromises(); const playing = workflow.play(workflow.versions.value[0]!); await flushPromises();
    const signal = prepare.mock.lastCall![3]!; provider.value = '115'; expect(signal.aborted).toBe(true);
    finish(ready); await playing; await flushPromises();
    expect(workflow.state.value.sources).toEqual([]); expect(workflow.waiting.value).toBe(''); expect(usePlayerStore().current).toBeNull();
  });
  it('普通用户可播放光鸭就绪源，未下载版本仍需管理员准备', async () => {
    provider.value = 'guangya'; const read = vi.spyOn(guangyaApi, 'state').mockResolvedValue(ready);
    vi.spyOn(guangyaApi, 'prepare').mockResolvedValue(ready);
    const submit = vi.spyOn(guangyaApi, 'submit'), select = vi.spyOn(guangyaApi, 'select');
    start(false); await flushPromises(); await workflow.play(workflow.versions.value[0]!);
    expect(usePlayerStore().current?.url).toBe(source.DirectStreamUrl); expect(submit).not.toHaveBeenCalled(); expect(select).not.toHaveBeenCalled();
    read.mockResolvedValue(empty); await workflow.load(); await workflow.play(workflow.versions.value[0]!);
    expect(workflow.error.value).toContain('管理员'); expect(submit).not.toHaveBeenCalled(); expect(select).not.toHaveBeenCalled();
  });
  it('本地模式禁用工作流时，选择光鸭也不发任何云盘请求', async () => {
    enabled.value = false; start(); await flushPromises(); provider.value = 'guangya'; await flushPromises();
    await workflow.load(); await workflow.play(workflow.versions.value[0]!); await vi.advanceTimersByTimeAsync(15000);
    expect(cloud115Api.state).not.toHaveBeenCalled(); expect(guangyaApi.state).not.toHaveBeenCalled();
  });
});
