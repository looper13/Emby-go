import { afterEach, expect, it, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { createPinia, disposePinia } from 'pinia';
import PlayerDialog from '../src/components/media/PlayerDialog.vue';
import { apiClient } from '../src/api/client';
import * as player from '../src/lib/player';
import { bindNativeTracks, type PlaybackSource } from '../src/lib/player-tracks';

const cleanups: (() => void)[] = [];
afterEach(() => cleanups.splice(0).reverse().forEach(fn => fn()));

async function openDialog(source: PlaybackSource, native = false) {
  const request = vi.spyOn(apiClient, 'request').mockImplementation(async path => {
    if (path.endsWith('/PlaybackInfo')) return { MediaSources: [source] };
    if (path.endsWith('/Playback')) return { SessionId: 'remux', Url: '/Videos/1/hls/remux/index.m3u8', StartTimeTicks: 0 };
    return undefined;
  });
  const videos: HTMLVideoElement[] = [];
  vi.spyOn(player, 'loadPlayerFactory').mockResolvedValue((stage, options) => {
    const video = document.createElement('video');
    videos.push(video); stage.append(video);
    if (native && options.type !== 'm3u8') {
      Object.defineProperty(video, 'textTracks', { value: Object.assign(new EventTarget(), { length: 0 }) });
      Object.defineProperty(video, 'audioTracks', { value: Object.assign(new EventTarget(), {
        0: { enabled: true, label: '原生日语', language: 'jpn' }, length: 1,
      }) });
      cleanups.push(bindNativeTracks(video, options.onTracks!));
    }
    return { video, url: options.url, muted: false, play: vi.fn().mockResolvedValue(undefined),
      on: (name, fn) => video.addEventListener(name.replace('video:', ''), fn), destroy: () => video.remove() };
  });
  const pinia = createPinia();
  const wrapper = mount(PlayerDialog, { attachTo: document.body, props: { options: { itemId: '1', title: 'film', url: '/Videos/1/stream', type: 'mp4' } }, global: { plugins: [pinia] } });
  cleanups.push(() => { wrapper.unmount(); disposePinia(pinia); });
  await flushPromises();
  return { wrapper, request, videos };
}

it('keeps external audio selectable when Safari exposes native audio tracks', async () => {
  const { wrapper, request } = await openDialog({ Id: 'source', DefaultAudioStreamIndex: 1, SupportsAudioSelection: true, MediaStreams: [
    { Type: 'Audio', Index: 1, ContainerIndex: 2, ContainerIndexKnown: true, DisplayTitle: '日语' },
    { Type: 'Audio', Index: 2, IsExternal: true, ContainerIndexKnown: true, DisplayTitle: '英语 FLAC' },
  ] }, true);
  const audio = wrapper.get('select[aria-label="选择音轨"]');
  expect(audio.findAll('option').map(option => option.attributes('value'))).toEqual(['hls:0', 'server:2']);
  expect(audio.get('option[value="server:2"]').attributes('disabled')).toBeUndefined();
  await audio.setValue('server:2'); await flushPromises();
  expect(request).toHaveBeenCalledWith('/Videos/1/Playback', expect.objectContaining({ json: expect.objectContaining({ AudioStreamIndex: 2 }) }));
});

it('does not automatically fetch an unsupported SMI default subtitle', async () => {
  const { wrapper, request, videos } = await openDialog({ Id: 'source', DefaultSubtitleStreamIndex: 1, MediaStreams: [
    { Type: 'Video', Index: 0 },
    { Type: 'Subtitle', Index: 1, IsExternal: true, IsDefault: true, IsTextSubtitleStream: true, Codec: 'smi', DeliveryUrl: '/Videos/1/mediasource_1/Subtitles/1/Stream.smi' },
  ] });
  videos[0]!.dispatchEvent(new Event('loadedmetadata')); await flushPromises();
  const subtitles = wrapper.get('select[aria-label="选择字幕"]');
  expect((subtitles.element as HTMLSelectElement).value).toBe('-1');
  expect(subtitles.get('option[value="server:1"]').attributes('disabled')).toBeDefined();
  expect(request.mock.calls.every(([path]) => !path.includes('/Subtitles/'))).toBe(true);
});
