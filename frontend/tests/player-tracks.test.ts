import { describe, expect, it, vi } from 'vitest';
import { bindNativeTracks, offsetVtt, parsePlaybackSource, rememberTrack, subtitlePath, trackLabel, trackPreference } from '../src/lib/player-tracks';

describe('playback track delivery', () => {
  it('retains actual stream indexes and rejects missing sources', () => {
    const source = parsePlaybackSource({ MediaSources: [{ Id: 'x', MediaStreams: [{ Type: 'Audio', Index: 7 }, { Type: 'Audio', Index: 'wrong' }] }] });
    expect(source.MediaStreams).toEqual([{ Type: 'Audio', Index: 7 }]);
    expect(() => parsePlaybackSource({})).toThrow('没有可播放资源');
  });
  it('requests VTT only from local subtitle endpoints without forwarding credentials to another origin', () => {
    expect(subtitlePath('/Videos/1/mediasource_1/Subtitles/23/Stream.ass')).toBe('/Videos/1/mediasource_1/Subtitles/23/Stream.vtt');
    expect(() => subtitlePath('https://other.test/Videos/1/x/Subtitles/23/Stream.ass')).toThrow('字幕地址无效');
    expect(() => subtitlePath('/api/admin/config')).toThrow('字幕地址无效');
  });
  it('keeps subtitles aligned when remux playback restarts after an absolute seek', () => {
    const vtt = 'WEBVTT\n\n00:00:01.000 --> 00:00:03.000\nold\n\n00:00:05.000 --> 00:00:08.000\ncurrent\n\n00:01:00.000 --> 00:01:01.000\nnext';
    const shifted = offsetVtt(vtt, 6);
    expect(shifted).not.toContain('old');
    expect(shifted).toContain('00:00:00.000 --> 00:00:02.000\ncurrent');
    expect(shifted).toContain('00:00:54.000 --> 00:00:55.000\nnext');
    expect(trackLabel({ Type: 'Subtitle', Index: 7, DisplayTitle: '简体中文', IsExternal: true, IsDefault: true })).toBe('简体中文 · 外挂 · 默认');
  });
  it('switches native audio/subtitle tracks and releases all listeners', () => {
    const video = document.createElement('video');
    const audio = Object.assign(new EventTarget(), { 0: { enabled: true, label: '日语', language: 'jpn' }, 1: { enabled: false, label: '英语', language: 'eng' }, length: 2 });
    const text = Object.assign(new EventTarget(), { 0: { mode: 'disabled', label: '中文', language: 'chi', kind: 'subtitles' }, length: 1 });
    Object.defineProperty(video, 'audioTracks', { value: audio }); Object.defineProperty(video, 'textTracks', { value: text });
    const update = vi.fn(); const release = bindNativeTracks(video, update); const engine = update.mock.lastCall![0];
    engine.selectAudio('1'); expect(audio[0].enabled).toBe(false); expect(audio[1].enabled).toBe(true);
    engine.selectSubtitle('0'); expect(text[0].mode).toBe('showing');
    engine.selectSubtitle('-1'); expect(text[0].mode).toBe('disabled');
    const count = update.mock.calls.length; release(); audio.dispatchEvent(new Event('change')); text.dispatchEvent(new Event('change')); video.dispatchEvent(new Event('loadedmetadata'));
    expect(update).toHaveBeenCalledTimes(count);
  });
  it('keeps user selection separate for replaced media sources', () => {
    rememberTrack('movie:old-source', 'audio', 'server:7'); rememberTrack('movie:old-source', 'subtitle', '-1');
    expect(trackPreference('movie:old-source')).toEqual({ audio: 'server:7', subtitle: '-1' });
    expect(trackPreference('movie:new-source')).toBeUndefined();
  });
});
