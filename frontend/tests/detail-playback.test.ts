import { afterEach, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import { createPinia, disposePinia } from 'pinia';
import DetailPlaybackControls from '../src/components/media/DetailPlaybackControls.vue';
import DetailPlayButton from '../src/components/media/DetailPlayButton.vue';
import { apiClient } from '../src/api/client';
import { usePlayerStore } from '../src/stores/player';
import { externalPlayerURL } from '../src/lib/external-player';
import { resolveTrackSelection, type PlaybackSource } from '../src/lib/player-tracks';
import movie from '../../vue-migration/fixtures/json/item-detail.json';

const source: PlaybackSource = {
  Id: 'mediasource_5', TrackRevision: 'r1', Container: 'mkv', SupportsAudioSelection: true,
  ExternalStreamUrl: 'https://media.example.test/film.mkv?sig=a%2Bb&part=1',
  DefaultAudioStreamIndex: 1, DefaultSubtitleStreamIndex: 3,
  MediaStreams: [
    { Type: 'Video', Index: 0 },
    { Type: 'Audio', Index: 1, DisplayTitle: 'Japanese', IsDefault: true },
    { Type: 'Audio', Index: 2, DisplayTitle: 'English', ContainerIndexKnown: true },
    { Type: 'Subtitle', Index: 3, DisplayTitle: 'Chinese', Codec: 'srt', IsExternal: true, DeliveryUrl: '/Videos/5/mediasource_5/Subtitles/3/Stream.srt' },
    { Type: 'Subtitle', Index: 4, Codec: 'pgs', IsTextSubtitleStream: false },
  ],
};
const cleanups: (() => void)[] = [];
afterEach(() => cleanups.splice(0).reverse().forEach(fn => fn()));

it('selects tracks on detail and keeps quick playback independent of that selection', async () => {
  vi.spyOn(apiClient, 'request').mockResolvedValue({ MediaSources: [source] });
  const pinia = createPinia();
  const wrapper = mount(DetailPlaybackControls, { props: { itemId: '5' }, global: { plugins: [pinia] } });
  cleanups.push(() => { wrapper.unmount(); disposePinia(pinia); });
  await flushPromises();
  expect((wrapper.get('select[aria-label="选择音轨"]').element as HTMLSelectElement).value).toBe('1');
  expect((wrapper.get('select[aria-label="选择字幕"]').element as HTMLSelectElement).value).toBe('3');
  expect(wrapper.get('option[value="4"]').attributes('disabled')).toBeDefined();
  await wrapper.get('select[aria-label="选择音轨"]').setValue('2');
  await wrapper.get('select[aria-label="选择字幕"]').setValue('-1');
  const selection = wrapper.emitted('selection')!.at(-1)![0];
  expect(selection).toEqual({ sourceId: source.Id, revision: 'r1', audioIndex: 2, subtitleIndex: -1 });
  const store = usePlayerStore(pinia);
  store.media(movie.movie, selection as Parameters<typeof store.media>[1]);
  expect(store.current?.selection).toEqual(selection);
  store.media(movie.movie);
  expect(store.current?.selection).toBeUndefined();
});

it('does not load a player or start remuxing while browsing detail and exposes three native links', async () => {
  const request = vi.spyOn(apiClient, 'request').mockResolvedValue({ MediaSources: [source] });
  const wrapper = mount(DetailPlaybackControls, { props: { itemId: '5' } });
  cleanups.push(() => wrapper.unmount());
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(1);
  expect(request).toHaveBeenCalledWith('/Items/5/PlaybackInfo', expect.objectContaining({ readOnly: true, signal: expect.any(AbortSignal) }));
  await wrapper.get('button[aria-label="外部播放器"]').trigger('click');
  for (const [scheme, name] of [['potplayer', 'PotPlayer'], ['vlc', 'VLC'], ['mpv', 'MPV']]) {
    const link = wrapper.get(`a[data-player="${scheme}"]`);
    expect(link.text()).toBe(name);
    expect(link.attributes('href')).toBe(`${scheme}://${source.ExternalStreamUrl}`);
  }
  expect(wrapper.find('video').exists()).toBe(false);
});

it('discards late detail responses and cancels requests when changing items or leaving', async () => {
  let resolve!: (value: unknown) => void;
  const request = vi.spyOn(apiClient, 'request').mockReturnValueOnce(new Promise(r => { resolve = r; }))
    .mockResolvedValue({ MediaSources: [{ ...source, Id: 'other', TrackRevision: 'r2', MediaStreams: [] }] });
  const wrapper = mount(DetailPlaybackControls, { props: { itemId: '5' } });
  const signal = request.mock.calls[0]![1].signal!;
  await wrapper.setProps({ itemId: '6' }); await flushPromises();
  expect(signal.aborted).toBe(true);
  resolve({ MediaSources: [source] }); await flushPromises();
  expect(wrapper.emitted('selection')!.at(-1)![0]).toMatchObject({ sourceId: 'other' });
  wrapper.unmount();
  expect(request.mock.calls[1]![1].signal!.aborted).toBe(true);
});

it('falls back to defaults for stale or unavailable choices without leaking a previous source', () => {
  expect(resolveTrackSelection(source)).toEqual({ audioIndex: 1, subtitleIndex: 3 });
  expect(resolveTrackSelection(source, { sourceId: source.Id, revision: 'r1', audioIndex: 2, subtitleIndex: -1 })).toEqual({ audioIndex: 2, subtitleIndex: -1 });
  expect(resolveTrackSelection(source, { sourceId: source.Id, revision: 'old', audioIndex: 2, subtitleIndex: -1 })).toEqual({ audioIndex: 1, subtitleIndex: 3 });
  expect(resolveTrackSelection(source, { sourceId: source.Id, revision: 'r1', audioIndex: 99, subtitleIndex: 4 })).toEqual({ audioIndex: 1, subtitleIndex: 3 });
});

it('only opens HTTP media and preserves escaped signed query strings', () => {
  expect(externalPlayerURL('mpv', source.ExternalStreamUrl!)).toBe(`mpv://${source.ExternalStreamUrl}`);
  for (const url of ['file:///C:/secret', 'javascript:alert(1)', '//media.test/x', 'https://media.test/x" --script=bad', 'https://media.test/x\n--bad']) {
    expect(() => externalPlayerURL('vlc', url)).toThrow();
  }
});

it('loads episode playback options only when expanded and preserves selection on collapse', async () => {
  const request = vi.spyOn(apiClient, 'request').mockResolvedValue({ MediaSources: [source] });
  const wrapper = mount(DetailPlayButton, { props: { itemId: 'anime-episode:7' } });
  cleanups.push(() => wrapper.unmount());
  await wrapper.get('.btn-accent').trigger('click');
  expect(wrapper.emitted('play')).toEqual([[undefined]]);
  expect(request).not.toHaveBeenCalled();
  const toggle = wrapper.get('button[aria-label="播放设置与外部播放器"]');
  await toggle.trigger('click'); await flushPromises();
  await wrapper.get('select[aria-label="选择音轨"]').setValue('2');
  await toggle.trigger('click'); await toggle.trigger('click');
  expect(request).toHaveBeenCalledTimes(1);
  await wrapper.get('.btn-accent').trigger('click');
  expect(wrapper.emitted('play')!.at(-1)![0]).toMatchObject({ audioIndex: 2 });
});

it('keeps known HLS text subtitles and alternate audio available without FFmpeg', () => {
  const hls: PlaybackSource = { Id: 'hls', Container: 'm3u8', DefaultAudioStreamIndex: 1, MediaStreams: [
    { Type: 'Audio', Index: 1 }, { Type: 'Audio', Index: 2 }, { Type: 'Subtitle', Index: 3, Codec: 'webvtt' },
  ] };
  expect(resolveTrackSelection(hls, { sourceId: 'hls', audioIndex: 2, subtitleIndex: 3 }, true)).toEqual({ audioIndex: 2, subtitleIndex: 3 });
});
