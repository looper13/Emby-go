import { afterEach, expect, it, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { createPinia, disposePinia } from 'pinia';
import PlayerDialog from '../src/components/media/PlayerDialog.vue';
import { apiClient } from '../src/api/client';
import * as player from '../src/lib/player';
import { bindNativeTracks, type PlaybackSource } from '../src/lib/player-tracks';

const cleanups: (() => void)[] = [];
afterEach(() => cleanups.splice(0).reverse().forEach(fn => fn()));

async function openDialog(source: PlaybackSource, native = false, options: Partial<player.PlayerOptions> = {}, remuxError?: Error) {
  const request = vi.spyOn(apiClient, 'request').mockImplementation(async path => {
    if (path.endsWith('/PlaybackInfo')) return { MediaSources: [source] };
    if (path.endsWith('/Playback')) { if (remuxError) throw remuxError; return { SessionId: 'remux', Url: '/Videos/1/hls/remux/index.m3u8', StartTimeTicks: 0 }; }
    return undefined;
  });
  const videos: HTMLVideoElement[] = [];
  const nativeAudio = vi.fn(), nativeSubtitle = vi.fn();
  vi.spyOn(player, 'loadPlayerFactory').mockResolvedValue((stage, options) => {
    const video = document.createElement('video');
    videos.push(video); stage.append(video);
    if (native && options.type === 'm3u8') {
      options.onTracks?.({ audio: () => [{ id: '0', label: 'Japanese' }, { id: '1', label: 'English' }], subtitles: () => [{ id: '0', label: 'Chinese' }], selectAudio: nativeAudio, selectSubtitle: nativeSubtitle, audioId: () => '0', subtitleId: () => '-1' });
    }
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
  const wrapper = mount(PlayerDialog, { attachTo: document.body, props: { options: { itemId: '1', title: 'film', url: '/Videos/1/stream', type: 'mp4', ...options } }, global: { plugins: [pinia] } });
  cleanups.push(() => { wrapper.unmount(); disposePinia(pinia); });
  await flushPromises();
  return { wrapper, request, videos, nativeAudio, nativeSubtitle };
}

it('applies detail external audio selection before mounting playback, preserving resume position', async () => {
  const { wrapper, request, videos } = await openDialog({ Id: 'source', DefaultAudioStreamIndex: 1, SupportsAudioSelection: true, MediaStreams: [
    { Type: 'Audio', Index: 1, ContainerIndex: 2, ContainerIndexKnown: true, DisplayTitle: '日语' },
    { Type: 'Audio', Index: 2, IsExternal: true, ContainerIndexKnown: true, DisplayTitle: '英语 FLAC' },
  ] }, true, { resumeTicks: 420000000, selection: { sourceId: 'source', audioIndex: 2, subtitleIndex: -1 } });
  expect(wrapper.find('select').exists()).toBe(false);
  expect(videos).toHaveLength(1);
  expect(request).toHaveBeenCalledWith('/Videos/1/Playback', expect.objectContaining({ json: expect.objectContaining({ AudioStreamIndex: 2, StartTimeTicks: 420000000 }) }));
});

it('does not automatically fetch an unsupported SMI default subtitle', async () => {
  const { wrapper, request, videos } = await openDialog({ Id: 'source', DefaultSubtitleStreamIndex: 1, MediaStreams: [
    { Type: 'Video', Index: 0 },
    { Type: 'Subtitle', Index: 1, IsExternal: true, IsDefault: true, IsTextSubtitleStream: true, Codec: 'smi', DeliveryUrl: '/Videos/1/mediasource_1/Subtitles/1/Stream.smi' },
  ] });
  videos[0]!.dispatchEvent(new Event('loadedmetadata')); await flushPromises();
  expect(wrapper.find('select').exists()).toBe(false);
  expect(request.mock.calls.every(([path]) => !path.includes('/Subtitles/'))).toBe(true);
});

it('uses defaults for quick play and ignores selections from a stale track revision', async () => {
  const source: PlaybackSource = { Id: 'source', TrackRevision: 'new', DefaultAudioStreamIndex: 1, SupportsAudioSelection: true, MediaStreams: [
    { Type: 'Audio', Index: 1 }, { Type: 'Audio', Index: 2 },
  ] };
  const quick = await openDialog(source);
  expect(quick.request.mock.calls.some(([path]) => path.endsWith('/Playback'))).toBe(false);
  quick.wrapper.unmount();
  const stale = await openDialog(source, false, { selection: { sourceId: 'source', revision: 'old', audioIndex: 2 } });
  expect(stale.request.mock.calls.some(([path]) => path.endsWith('/Playback'))).toBe(false);
  expect(document.body.textContent).toContain('媒体轨道已变化');
});

it('honors detail subtitle-off without fetching the default subtitle', async () => {
  const { request, videos } = await openDialog({ Id: 'source', DefaultSubtitleStreamIndex: 2, MediaStreams: [
    { Type: 'Subtitle', Index: 2, Codec: 'srt', IsExternal: true, DeliveryUrl: '/Videos/1/mediasource_1/Subtitles/2/Stream.srt' },
  ] }, false, { selection: { sourceId: 'source', subtitleIndex: -1 } });
  videos[0]!.dispatchEvent(new Event('loadedmetadata')); await flushPromises();
  expect(request.mock.calls.every(([path]) => !path.includes('/Subtitles/'))).toBe(true);
});

it('keeps an audio failure visible when loading the selected subtitle on the fallback stream', async () => {
  const { videos } = await openDialog({ Id: 'source', DefaultAudioStreamIndex: 1, DefaultSubtitleStreamIndex: 3, SupportsAudioSelection: true, MediaStreams: [
    { Type: 'Audio', Index: 1 }, { Type: 'Audio', Index: 2 },
    { Type: 'Subtitle', Index: 3, Codec: 'srt', IsExternal: true, DeliveryUrl: '/Videos/1/mediasource_1/Subtitles/3/Stream.srt' },
  ] }, false, { selection: { sourceId: 'source', audioIndex: 2, subtitleIndex: 3 } }, new Error('音轨处理超时'));
  videos[0]!.dispatchEvent(new Event('loadedmetadata')); await flushPromises();
  expect(document.querySelector('.player-track-error')?.textContent).toContain('音轨处理超时');
});

it('applies declared HLS audio and text subtitle indexes through the native engine', async () => {
  const { videos, request, nativeAudio, nativeSubtitle } = await openDialog({ Id: 'source', Container: 'm3u8', DefaultAudioStreamIndex: 1, MediaStreams: [
    { Type: 'Audio', Index: 1 }, { Type: 'Audio', Index: 2 }, { Type: 'Subtitle', Index: 3, Codec: 'webvtt' },
  ] }, true, { selection: { sourceId: 'source', audioIndex: 2, subtitleIndex: 3 } });
  videos[0]!.dispatchEvent(new Event('loadedmetadata')); await flushPromises();
  expect(nativeAudio).toHaveBeenCalledWith('1');
  expect(nativeSubtitle).toHaveBeenCalledWith('0');
  expect(request.mock.calls.some(([path]) => path.endsWith('/Playback') || path.includes('/Subtitles/'))).toBe(false);
});
