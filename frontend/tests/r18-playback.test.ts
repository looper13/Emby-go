import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { defineComponent, ref } from 'vue';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia, setActivePinia } from 'pinia';
import { cloud115Api, type OfflineState, type OfflineTask } from '../src/api/cloud115';
import type { R18Resource } from '../src/api/r18';
import { useAuthStore } from '../src/stores/auth';
import { usePlayerStore } from '../src/stores/player';
import { useR18Playback, type R18Playback } from '../src/composables/useR18Playback';
import { magnetHash, mainVideo, playbackVersions } from '../src/lib/r18-playback';

const hash = 'a'.repeat(40), otherHash = 'b'.repeat(40);
const resource: R18Resource = { id: '1', title: '4K version', number: 'TEST-001', date: '', magnet: `magnet:?xt=urn:btih:${hash}`, website: '', detail_url: '', size_bytes: 0, size_mb: 0, tags: [] };
const empty: OfflineState = { configured: true, tasks: [], sources: [] };
const files = [{ id: 'ad', name: 'attachment.mp4', path: 'attachment.mp4', size: 100 }, { id: 'main', name: 'movie.mp4', path: 'movie.mp4', size: 20_000_000_000 }];
const task: OfflineTask = { id: 'task', info_hash: hash, magnet: resource.magnet, resource_id: '1', status: 'downloading', progress: 40, files: [], file_id: '', stage: '', error: '', retryable: false, version: 1 };
const source = { Id: '115-task', InfoHash: hash, ResourceId: '1', Name: 'movie.mp4', DirectStreamUrl: '/Videos/r18-2-id/stream?MediaSourceId=115-task&playback_token=fixture', Container: 'mp4', Default: false };
let pinia: ReturnType<typeof createPinia>, wrapper: VueWrapper | undefined, workflow: R18Playback;
const item = ref({ library: '2', video: 'id', title: 'Movie' });
function start(admin = true) {
  useAuthStore().user = { Policy: { IsAdministrator: admin } } as never;
  wrapper = mount(defineComponent({ setup() { workflow = useR18Playback(() => item.value, ref([resource])); return {}; }, template: '<div />' }), { global: { plugins: [pinia] } });
}
beforeEach(() => {
  vi.useFakeTimers(); pinia = createPinia(); setActivePinia(pinia); item.value = { library: '2', video: 'id', title: 'Movie' };
  vi.spyOn(cloud115Api, 'state').mockResolvedValue(empty);
  vi.spyOn(cloud115Api, 'prepare').mockImplementation(async () => workflow.state.value);
});
afterEach(() => { wrapper?.unmount(); wrapper = undefined; disposePinia(pinia); vi.restoreAllMocks(); vi.useRealTimers(); });

describe('R18 one-click playback', () => {
  it('normalizes hex/Base32 magnets, merges duplicate records, and keeps different magnets as versions', () => {
    expect(magnetHash('magnet:?xt=urn:btih:' + 'A'.repeat(32))).toBe('0'.repeat(40));
    expect(magnetHash('magnet:?dn=name&xt=urn:btih:' + hash.toUpperCase())).toBe(hash);
    expect(magnetHash('https://example.com?xt=urn:btih:' + hash)).toBe('');
    const versions = playbackVersions([resource, { ...resource, id: 'alias', magnet: resource.magnet.toUpperCase().replace('MAGNET', 'magnet') }, { ...resource, id: '2', magnet: `magnet:?xt=urn:btih:${otherHash}` }], { ...empty, tasks: [{ ...task, resource_id: 'alias' }], sources: [source] });
    expect(versions).toHaveLength(2); expect(versions[0]?.task?.id).toBe(task.id); expect(versions[0]?.source?.Id).toBe(source.Id);
    expect(versions[1]?.source).toBeUndefined(); expect(mainVideo(files)?.id).toBe('main');
  });
  it('submits once, polls only local state, binds the largest movie, and automatically opens its source', async () => {
    const read = vi.spyOn(cloud115Api, 'state').mockResolvedValue(empty);
    const submit = vi.spyOn(cloud115Api, 'submit').mockResolvedValue({ ...empty, tasks: [task] });
    const select = vi.spyOn(cloud115Api, 'select').mockResolvedValue({ ...empty, tasks: [{ ...task, status: 'ready', files, file_id: 'main' }], sources: [source] });
    start(); await flushPromises(); expect(submit).not.toHaveBeenCalled();
    await workflow.play(workflow.versions.value[0]!);
    expect(submit).toHaveBeenCalledExactlyOnceWith('2', 'id', { resource_id: '1' }, expect.any(AbortSignal));
    expect(workflow.message.value).toContain('就绪后自动播放'); expect(usePlayerStore().current).toBeNull();
    read.mockResolvedValue({ ...empty, tasks: [{ ...task, status: 'waiting_selection', progress: 100, files }] });
    await vi.advanceTimersByTimeAsync(5000);
    expect(select).toHaveBeenCalledExactlyOnceWith('2', 'id', task.id, 'main', expect.any(AbortSignal), false);
    expect(usePlayerStore().current?.url).toBe(source.DirectStreamUrl); expect(usePlayerStore().current?.itemId).toBeUndefined();
    expect(workflow.waiting.value).toBe(''); expect(submit).toHaveBeenCalledTimes(1);
  });
  it('reuses a downloading task and never opens an unrelated ready version', async () => {
    const otherSource = { ...source, Id: '115-other', InfoHash: otherHash, ResourceId: '2' };
    vi.spyOn(cloud115Api, 'state').mockResolvedValue({ ...empty, tasks: [task], sources: [otherSource] });
    const submit = vi.spyOn(cloud115Api, 'submit');
    start(); await flushPromises(); await workflow.play(workflow.versions.value.find(v => v.id === hash)!);
    await vi.advanceTimersByTimeAsync(15000);
    expect(submit).not.toHaveBeenCalled(); expect(usePlayerStore().current).toBeNull(); expect(workflow.waiting.value).toBe(hash);
  });
  it('allows ordinary authorized users to play a fresh ready version without cloud writes', async () => {
    vi.spyOn(cloud115Api, 'state').mockResolvedValue({ ...empty, sources: [source] });
    const submit = vi.spyOn(cloud115Api, 'submit'), select = vi.spyOn(cloud115Api, 'select');
    start(false); await flushPromises(); await workflow.play(workflow.versions.value[0]!);
    expect(usePlayerStore().current?.url).toBe(source.DirectStreamUrl); expect(submit).not.toHaveBeenCalled(); expect(select).not.toHaveBeenCalled();
  });
  it('does not submit an unready version for ordinary users', async () => {
    const submit = vi.spyOn(cloud115Api, 'submit'); start(false); await flushPromises(); await workflow.play(workflow.versions.value[0]!);
    expect(workflow.error.value).toContain('管理员'); expect(workflow.waiting.value).toBe(''); expect(submit).not.toHaveBeenCalled();
  });
  it('retains visible state on refresh failure and stops automatic opening without cancelling a download', async () => {
    const read = vi.spyOn(cloud115Api, 'state').mockResolvedValue({ ...empty, tasks: [task] });
    const cancel = vi.spyOn(cloud115Api, 'change'); start(); await flushPromises(); await workflow.play(workflow.versions.value[0]!);
    read.mockRejectedValueOnce(new Error('network unavailable')); await workflow.load();
    expect(workflow.state.value.tasks).toHaveLength(1); expect(workflow.error.value).toBe('network unavailable');
    workflow.stopWaiting(); read.mockResolvedValue({ ...empty, tasks: [{ ...task, status: 'ready', files, file_id: 'main' }], sources: [source] });
    await vi.advanceTimersByTimeAsync(5000); expect(usePlayerStore().current).toBeNull(); expect(cancel).not.toHaveBeenCalled();
  });
  it('ignores a late file-selection result after stop-waiting or route change', async () => {
    const read = vi.spyOn(cloud115Api, 'state').mockResolvedValue({ ...empty, tasks: [{ ...task, status: 'waiting_selection', files }] });
    let finish!: (value: OfflineState) => void;
    vi.spyOn(cloud115Api, 'select').mockImplementation(() => new Promise(resolve => { finish = resolve; }));
    start(); await flushPromises(); const playing = workflow.play(workflow.versions.value[0]!); await flushPromises();
    workflow.stopWaiting(); finish({ ...empty, tasks: [{ ...task, status: 'ready', files, file_id: 'main' }], sources: [source] }); await playing;
    expect(usePlayerStore().current).toBeNull(); expect(workflow.error.value).toBe('');
    const again = workflow.play(workflow.versions.value[0]!); await flushPromises();
    read.mockResolvedValue(empty); item.value = { library: '2', video: 'new-id', title: 'Next movie' }; await flushPromises();
    finish({ ...empty, sources: [source] }); await again;
    expect(workflow.state.value.sources).toHaveLength(0); expect(usePlayerStore().current).toBeNull();
  });
  it('resubmits only an expired lifecycle and retries a failed stage without another submit', async () => {
    const read = vi.spyOn(cloud115Api, 'state').mockResolvedValue({ ...empty, tasks: [{ ...task, status: 'expired' }] });
    const submit = vi.spyOn(cloud115Api, 'submit').mockResolvedValue({ ...empty, tasks: [task] });
    const retry = vi.spyOn(cloud115Api, 'change').mockResolvedValue({ ...empty, tasks: [task] });
    start(); await flushPromises(); await workflow.play(workflow.versions.value[0]!); expect(submit).toHaveBeenCalledTimes(1);
    read.mockResolvedValue({ ...empty, tasks: [{ ...task, status: 'failed', retryable: true }] });
    workflow.stopWaiting(); await workflow.play(workflow.versions.value[0]!);
    expect(retry).toHaveBeenCalledWith('2', 'id', expect.objectContaining({ status: 'failed' }), 'retry', expect.any(AbortSignal)); expect(submit).toHaveBeenCalledTimes(1);
  });
  it('shows rejected submissions and does not leave a phantom queued playback intent', async () => {
    const submit = vi.spyOn(cloud115Api, 'submit').mockRejectedValue(new Error('quota exceeded'));
    start(); await flushPromises(); await workflow.play(workflow.versions.value[0]!);
    expect(workflow.error.value).toBe('quota exceeded'); expect(workflow.waiting.value).toBe('');
    await vi.advanceTimersByTimeAsync(15000); expect(submit).toHaveBeenCalledTimes(1);
  });
  it('preflights a ready file, keeps waiting if it is missing, and plays only the recovered source', async () => {
    const ready = { ...empty, tasks: [{ ...task, status: 'ready', files, file_id: 'main' }], sources: [source] };
    const read = vi.spyOn(cloud115Api, 'state').mockResolvedValue(ready);
    const prepare = vi.spyOn(cloud115Api, 'prepare').mockResolvedValueOnce({ ...empty, tasks: [{ ...task, status: 'discovering', stage: 'file' }] });
    const submit = vi.spyOn(cloud115Api, 'submit'); start(); await flushPromises();
    await workflow.play(workflow.versions.value[0]!);
    expect(prepare).toHaveBeenCalledWith('2', 'id', source.Id, expect.any(AbortSignal));
    expect(usePlayerStore().current).toBeNull(); expect(workflow.waiting.value).toBe(hash); expect(submit).not.toHaveBeenCalled();
    const replacement = { ...source, DirectStreamUrl: source.DirectStreamUrl + '&replacement=1' };
    read.mockResolvedValue({ ...ready, sources: [replacement] });
    prepare.mockResolvedValue({ ...ready, sources: [replacement] });
    await vi.advanceTimersByTimeAsync(5000);
    expect(usePlayerStore().current?.url).toBe(replacement.DirectStreamUrl); expect(submit).not.toHaveBeenCalled();
  });
  it('submits a manual magnet as a separate visible version and stops page polling on unmount', async () => {
    const manual = { ...task, id: 'manual', info_hash: otherHash, resource_id: '', magnet: `magnet:?xt=urn:btih:${otherHash}` };
    const submit = vi.spyOn(cloud115Api, 'submit').mockResolvedValue({ ...empty, tasks: [manual] });
    const read = vi.spyOn(cloud115Api, 'state').mockResolvedValue(empty);
    start(); await flushPromises(); await workflow.play({ id: otherHash, resourceId: '', name: 'Manual', magnet: manual.magnet! });
    expect(submit).toHaveBeenCalledWith('2', 'id', { magnet: manual.magnet }, expect.any(AbortSignal));
    expect(workflow.versions.value).toHaveLength(2); wrapper!.unmount(); wrapper = undefined;
    await vi.advanceTimersByTimeAsync(15000); expect(read).toHaveBeenCalledTimes(2);
  });
});
