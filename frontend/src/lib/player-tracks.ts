export interface PlaybackTrack { id: string; label: string; language?: string }
export interface TrackEngine {
  audio: () => PlaybackTrack[];
  subtitles: () => PlaybackTrack[];
  selectAudio: (id: string) => void;
  selectSubtitle: (id: string) => void;
  audioId: () => string;
  subtitleId: () => string;
}

export function bindNativeTracks(video: HTMLVideoElement, update: (engine: TrackEngine) => void): () => void {
  type Audio = { enabled: boolean; label: string; language: string };
  const audio = (video as HTMLVideoElement & { audioTracks?: EventTarget & ArrayLike<Audio> }).audioTracks;
  const audioList = () => audio ? Array.from(audio) : [];
  const textList = () => Array.from(video.textTracks).filter(track =>
    (track.kind === 'subtitles' || track.kind === 'captions') && !Array.from(video.querySelectorAll<HTMLTrackElement>('track[data-emby-subtitle]')).some(element => element.track === track));
  const engine: TrackEngine = {
    audio: () => audioList().map((track, index) => ({ id: String(index), label: track.label || track.language || `音轨 ${index + 1}`, language: track.language })),
    subtitles: () => textList().map((track, index) => ({ id: String(index), label: track.label || track.language || `字幕 ${index + 1}`, language: track.language })),
    selectAudio: id => audioList().forEach((track, index) => { track.enabled = index === Number(id); }),
    selectSubtitle: id => textList().forEach((track, index) => { track.mode = index === Number(id) ? 'showing' : 'disabled'; }),
    audioId: () => String(audioList().findIndex(track => track.enabled)),
    subtitleId: () => String(textList().findIndex(track => track.mode === 'showing')),
  };
  const emit = () => update(engine);
  const events: [EventTarget, string][] = [[video, 'loadedmetadata'], [video.textTracks, 'addtrack'], [video.textTracks, 'removetrack'], [video.textTracks, 'change']];
  if (audio) events.push([audio, 'addtrack'], [audio, 'removetrack'], [audio, 'change']);
  for (const [target, name] of events) target.addEventListener(name, emit);
  emit();
  return () => { for (const [target, name] of events) target.removeEventListener(name, emit); };
}
export interface ServerTrack {
  Index: number; Type: string; DisplayTitle?: string; Language?: string; Codec?: string;
  Channels?: number; IsDefault?: boolean; IsForced?: boolean; IsExternal?: boolean;
  IsTextSubtitleStream?: boolean; DeliveryUrl?: string; DeliveryFormat?: string;
  ContainerIndexKnown?: boolean; ContainerIndex?: number;
}
export interface PlaybackSource {
  Id: string; Container?: string; MediaStreams: ServerTrack[];
  DefaultAudioStreamIndex?: number; DefaultSubtitleStreamIndex?: number;
  SupportsAudioSelection?: boolean; RunTimeTicks?: number;
  TrackRevision?: string; ResumePositionTicks?: number;
}

const preferences = new Map<string, { audio?: string; subtitle?: string }>();
export function trackPreference(key: string) { return preferences.get(key); }
export function rememberTrack(key: string, kind: 'audio' | 'subtitle', value: string) {
  preferences.set(key, { ...preferences.get(key), [kind]: value });
  if (preferences.size > 128) preferences.delete(preferences.keys().next().value!);
}
export function parsePlaybackSource(raw: unknown): PlaybackSource {
  const response = raw as { MediaSources?: PlaybackSource[] };
  const source = response?.MediaSources?.[0];
  if (!source || typeof source.Id !== 'string' || !Array.isArray(source.MediaStreams)) throw new Error('没有可播放资源');
  return { ...source, MediaStreams: source.MediaStreams.filter(track => Number.isSafeInteger(track.Index) && typeof track.Type === 'string') };
}
export function trackLabel(track: ServerTrack) {
  const name = (track.DisplayTitle !== 'und' ? track.DisplayTitle : '') || [track.Language && track.Language !== 'und' ? track.Language : '语言未标明', track.Codec?.toUpperCase(), track.Channels ? `${track.Channels} 声道` : ''].filter(Boolean).join(' · ');
  return name + (track.IsExternal ? ' · 外挂' : '') + (track.IsForced ? ' · 强制' : '') + (track.IsDefault && !/(默认|default)/iu.test(name) ? ' · 默认' : '');
}
// Subtitle credentials may only be sent to this service's own delivery route.
export function subtitlePath(raw: string): string {
  const url = new URL(raw, location.origin);
  if (url.origin !== location.origin || !/^\/(?:emby\/)?videos\//iu.test(url.pathname)) throw new Error('字幕地址无效');
  url.pathname = url.pathname.replace(/Stream\.[^/]+$/iu, 'Stream.vtt');
  return url.pathname + url.search;
}

export function offsetVtt(text: string, offset: number): string {
  const seconds = (raw: string) => raw.split(':').reduce((total, value) => total * 60 + Number(value), 0);
  const timestamp = (value: number) => {
    const ms = Math.max(0, Math.round(value * 1000));
    return `${String(Math.floor(ms / 3600000)).padStart(2, '0')}:${String(Math.floor(ms / 60000) % 60).padStart(2, '0')}:${String(Math.floor(ms / 1000) % 60).padStart(2, '0')}.${String(ms % 1000).padStart(3, '0')}`;
  };
  return text.split(/\r?\n\r?\n/u).map(block => {
    const match = /((?:\d+:)?\d{2}:\d{2}\.\d{3})\s+-->\s+((?:\d+:)?\d{2}:\d{2}\.\d{3})/u.exec(block);
    if (!match) return block;
    if (seconds(match[2]!) <= offset) return '';
    return block.replace(match[0], `${timestamp(seconds(match[1]!) - offset)} --> ${timestamp(seconds(match[2]!) - offset)}`);
  }).filter(Boolean).join('\n\n');
}
