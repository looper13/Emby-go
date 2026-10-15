import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, disposePinia, setActivePinia } from 'pinia';
import { createHlsPlayback } from '../src/lib/player-hls';
import { loadPlayerFactory, managePlayer, playbackType } from '../src/lib/player';
import { usePlayerStore } from '../src/stores/player';
import type { PlayerHandle } from '../src/lib/player';
import movie from '../../vue-migration/fixtures/json/item-detail.json';

const mocks = vi.hoisted(() => {
  const engines: {
    loadSource: ReturnType<typeof vi.fn>; attachMedia: ReturnType<typeof vi.fn>;
    destroy: ReturnType<typeof vi.fn>; recoverMediaError: ReturnType<typeof vi.fn>;
    error: (event: string, data: { fatal: boolean; type: string }) => void;
    attached: () => void;
  }[] = [];
  return { supported: true, engines, constructorOptions: vi.fn() };
});
vi.mock('hls.js', () => ({ default: class {
  static isSupported = () => mocks.supported;
  static Events = { ERROR: 'hlsError', MEDIA_ATTACHED: 'hlsMediaAttached' };
  static ErrorTypes = { MEDIA_ERROR: 'mediaError', NETWORK_ERROR: 'networkError' };
  loadSource = vi.fn(); attachMedia = vi.fn(); destroy = vi.fn(); recoverMediaError = vi.fn();
  error = (_event: string, _data: { fatal: boolean; type: string }) => {};
  attached = () => {};
  constructor() { mocks.engines.push(this); }
  on(event: string, callback: typeof this.error) {
    if (event === 'hlsError') this.error = callback;
    else this.attached = callback as unknown as () => void;
  }
} }));
vi.mock('../src/vendor/artplayer.min.js', () => ({ default: class {
  url: string;
  constructor(options: { url: string }) { this.url = options.url; mocks.constructorOptions(options); }
} }));

beforeEach(() => { mocks.supported = true; mocks.engines.length = 0; });
afterEach(() => vi.restoreAllMocks());

function host() {
  const video = document.createElement('video');
  const events = new Map<string, () => void>();
  const art = { on: vi.fn((name: string, fn: () => void) => { events.set(name, fn); }),
    emit: vi.fn(), notice: { show: '' }, option: { autoplay: true }, play: vi.fn().mockResolvedValue(undefined) };
  return { video, art, events };
}
function error(index: number, type = 'networkError', fatal = true) {
  mocks.engines[index]!.error('hlsError', { type, fatal });
}

describe('HLS playback', () => {
  it('recognizes containers and signed URLs and preserves MP4/WebM', () => {
    for (const value of ['m3u8', 'M3U8', 'm3u', 'https://cdn.test/INDEX.M3U8?token=x#t=3']) expect(playbackType(value)).toBe('m3u8');
    expect(playbackType('https://cdn.test/a.webm?token=x')).toBe('webm');
    expect(playbackType('webm')).toBe('webm');
    for (const value of ['', 'mp4', 'mkv', 'https://cdn.test/a.mp4?next=.m3u8']) expect(playbackType(value)).toBe('mp4');
  });
  it('selects HLS for movie and trailer entry points', () => {
    const pinia = createPinia(); setActivePinia(pinia);
    try {
      const store = usePlayerStore();
      store.media({ ...movie.movie, source_container: 'M3U8' });
      expect(store.current).toMatchObject({ type: 'm3u8', proxyURL: '/Videos/5/proxy' });
      store.trailer(movie.movie, 'https://cdn.test/trailer.m3u8?token=x');
      expect(store.current?.type).toBe('m3u8');
      store.media({ ...movie.movie, source_container: 'webm' });
      expect(store.current?.type).toBe('webm');
      store.close();
    } finally { disposePinia(pinia); }
  });
  it('registers the HLS custom type only when opening HLS', async () => {
    const options = { title: 'test', url: '/Videos/5/stream', type: 'm3u8' };
    const factory = await loadPlayerFactory('m3u8');
    factory(document.createElement('div'), options);
    const config = mocks.constructorOptions.mock.lastCall![0];
    expect(config.type).toBe('m3u8');
    const { video, art } = host();
    config.customType.m3u8(video, options.url, art);
    expect(mocks.engines[0]!.loadSource).toHaveBeenCalledWith(options.url);
    const mp4 = await loadPlayerFactory(); mp4(document.createElement('div'), { ...options, type: 'mp4' });
    expect(mocks.constructorOptions.mock.lastCall![0].customType).toEqual({});
  });
  it('releases the previous engine on URL switches and the current engine on close', () => {
    const play = createHlsPlayback(); const { video, art, events } = host();
    play(video, '/direct', art); const first = mocks.engines[0]!;
    expect(first.attachMedia).toHaveBeenCalledWith(video);
    play(video, '/proxy', art); const second = mocks.engines[1]!;
    expect(first.destroy).toHaveBeenCalledTimes(1); expect(second.loadSource).toHaveBeenCalledWith('/proxy');
    expect(art.on).toHaveBeenCalledTimes(1);
    error(0); expect(art.emit).not.toHaveBeenCalled();
    events.get('destroy')!(); events.get('destroy')!();
    expect(second.destroy).toHaveBeenCalledTimes(1);
    error(1); play(video, '/late', art);
    expect(art.emit).not.toHaveBeenCalled(); expect(mocks.engines).toHaveLength(2);
  });
  it('starts autoplay after attachment, tolerates gesture restrictions, and ignores stale attachments', async () => {
    const play = createHlsPlayback(); const { video, art } = host();
    art.play.mockRejectedValue(new Error('gesture required'));
    play(video, '/direct', art); expect(art.play).not.toHaveBeenCalled();
    mocks.engines[0]!.attached(); await Promise.resolve(); expect(art.play).toHaveBeenCalledTimes(1);
    play(video, '/proxy', art); mocks.engines[0]!.attached(); expect(art.play).toHaveBeenCalledTimes(1);
    art.option.autoplay = false; mocks.engines[1]!.attached(); expect(art.play).toHaveBeenCalledTimes(1);
  });
  it('does not create an engine if the player closed before the deferred first handler', () => {
    const { video, art } = host();
    createHlsPlayback()(video, '/late', { ...art, isDestroy: true });
    expect(mocks.engines).toHaveLength(0); expect(art.on).not.toHaveBeenCalled();
  });
  it('routes fatal errors into the existing single proxy fallback and reports once', () => {
    const hlsPlay = createHlsPlayback(); const { video, art, events } = host();
    let url = '/direct'; const report = vi.fn();
    const player: PlayerHandle = { get url() { return url; }, set url(value: string) { url = value; hlsPlay(video, value, art); },
      muted: false, video, on: (name, callback) => { events.set(name, callback); },
      play: vi.fn().mockResolvedValue(undefined), destroy: () => events.get('destroy')!() };
    art.emit.mockImplementation(name => { events.get(name)?.(); });
    const session = managePlayer(document.createElement('div'), { title: 'HLS', url, type: 'm3u8', proxyURL: '/proxy' }, () => player, report);
    hlsPlay(video, url, art);
    error(0, 'networkError', false); expect(player.url).toBe('/direct');
    error(0); expect(player.url).toBe('/proxy'); expect(mocks.engines).toHaveLength(2);
    expect(mocks.engines[0]!.destroy).toHaveBeenCalledTimes(1);
    error(1); error(1); expect(report).toHaveBeenCalledTimes(1);
    session.destroy(); expect(mocks.engines[1]!.destroy).toHaveBeenCalledTimes(1);
  });
  it('tries media error recovery once, then stops instead of looping', () => {
    const play = createHlsPlayback(); const { video, art } = host(); play(video, '/index.m3u8', art);
    error(0, 'mediaError'); expect(mocks.engines[0]!.recoverMediaError).toHaveBeenCalledTimes(1); expect(art.emit).not.toHaveBeenCalled();
    error(0, 'mediaError'); expect(art.emit).toHaveBeenCalledWith('error'); expect(mocks.engines[0]!.destroy).toHaveBeenCalledTimes(1);
  });
  it('uses native HLS when MSE is unavailable and explains unsupported browsers', () => {
    mocks.supported = false;
    const { video, art } = host(); vi.spyOn(video, 'canPlayType').mockReturnValue('probably');
    createHlsPlayback()(video, 'https://cdn.test/native.m3u8', art);
    expect(video.src).toBe('https://cdn.test/native.m3u8'); expect(mocks.engines).toHaveLength(0);
    vi.mocked(video.canPlayType).mockReturnValue(''); createHlsPlayback()(video, '/unsupported', art);
    expect(art.notice.show).toContain('不支持 HLS');
  });
});
