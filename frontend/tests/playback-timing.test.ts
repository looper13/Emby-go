import { afterEach, expect, it, vi } from 'vitest';
import { startPlaybackTiming } from '../src/lib/playback-timing';
import { managePlayer } from '../src/lib/player';

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

it('records distinct local timing stages without identifiers, URLs or duplicate measures', () => {
  const mark = vi.fn(), measure = vi.fn();
  vi.stubGlobal('performance', { mark, measure });
  const first = startPlaybackTiming(); first('prepared'); first('prepared'); first('first_frame');
  const second = startPlaybackTiming(); second('prepared');
  expect(measure).toHaveBeenCalledTimes(3);
  expect(mark.mock.calls.every(([name]) => /^r18-play:\d+:(click|prepared|first_frame)$/u.test(name))).toBe(true);
  expect(measure.mock.calls[0]![0]).not.toBe(measure.mock.calls[2]![0]);
  vi.stubGlobal('performance', {});
  expect(() => startPlaybackTiming()('prepared')).not.toThrow();
});

it('reports the first presented frame and cancels late callbacks on player disposal', () => {
  const video = document.createElement('video');
  let frame!: VideoFrameRequestCallback;
  video.requestVideoFrameCallback = vi.fn(callback => { frame = callback; return 7; });
  video.cancelVideoFrameCallback = vi.fn();
  const loaded = vi.fn(), first = vi.fn();
  const player = { video, url: '', muted: false, on: vi.fn(), play: vi.fn(), destroy: vi.fn() };
  const session = managePlayer(document.createElement('div'), { title: 'x', url: '/fixture', type: 'mp4', onLoaded: loaded, onFirstFrame: first }, () => player, vi.fn());
  expect(loaded).toHaveBeenCalledTimes(1); expect(first).not.toHaveBeenCalled();
  frame(1, {} as VideoFrameCallbackMetadata); expect(first).toHaveBeenCalledTimes(1);
  session.destroy(); frame(2, {} as VideoFrameCallbackMetadata);
  expect(first).toHaveBeenCalledTimes(1); expect(video.cancelVideoFrameCallback).toHaveBeenCalledWith(7);
});

it('uses a one-time playing fallback on browsers without frame callbacks', () => {
  const video = document.createElement('video'), first = vi.fn();
  const player = { video, url: '', muted: false, on: vi.fn(), play: vi.fn(), destroy: vi.fn() };
  const session = managePlayer(document.createElement('div'), { title: 'x', url: '/fixture', type: 'mp4', onFirstFrame: first }, () => player, vi.fn());
  video.dispatchEvent(new Event('playing')); video.dispatchEvent(new Event('playing'));
  expect(first).toHaveBeenCalledTimes(1); session.destroy();
});
