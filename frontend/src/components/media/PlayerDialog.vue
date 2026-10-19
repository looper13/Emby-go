<script setup lang="ts">
import { computed, onMounted, onScopeDispose, ref, shallowRef } from 'vue';
import {
  loadPlayerFactory,
  managePlayer,
  type PlayerOptions,
} from '../../lib/player';
import { useToastStore } from '../../stores/toasts';
import { apiClient } from '../../api/client';
import { offsetVtt, parsePlaybackSource, rememberTrack, trackPreference, subtitlePath, trackLabel, type PlaybackSource, type TrackEngine } from '../../lib/player-tracks';
import { playbackType } from '../../lib/player';
const props = defineProps<{ options: PlayerOptions }>();
const emit = defineEmits<{ close: [] }>();
const shell = ref<HTMLElement>();
const stage = ref<HTMLElement>();
const closeButton = ref<HTMLButtonElement>();
const toasts = useToastStore();
const source = shallowRef<PlaybackSource>();
const engine = shallowRef<TrackEngine>();
const trackHost = shallowRef<HTMLElement>();
const selectedAudio = ref('');
const selectedSubtitle = ref('-1');
const changingAudio = ref(false);
const trackError = ref('');
const offset = ref(0), position = ref(0);
const remuxSession = ref('');
const controller = new AbortController();
let subtitleRequest: AbortController | undefined;
let subtitleObjectURL: string | undefined;
let subtitleElement: HTMLTrackElement | undefined;
let generation = 0;
let preferenceKey = '';
let initialAudioChoice: string | undefined;
let initialHlsChoice: string | undefined;
let initialHlsSubtitle: string | undefined;
let baseOptions = { ...props.options };
const duration = computed(() => (source.value?.RunTimeTicks ?? 0) / 1e7);
function clock(seconds: number) { const value = Math.max(0, Math.floor(seconds)); return `${Math.floor(value / 60)}:${String(value % 60).padStart(2, '0')}`; }
const audioOptions = computed(() => {
  const native = !remuxSession.value ? engine.value?.audio() ?? [] : [];
  return [...native.map(track => ({ value: 'hls:'+track.id, label: track.label, disabled: false })),
  ...(source.value?.MediaStreams ?? []).filter(track => track.Type === 'Audio' && (!native.length || track.IsExternal)).map(track => ({
    value: 'server:'+track.Index, label: trackLabel(track)+(track.ContainerIndexKnown === false ? ' · 需重新探测' : ''), disabled: (!source.value?.SupportsAudioSelection || track.ContainerIndexKnown === false) && track.Index !== source.value?.DefaultAudioStreamIndex,
  }))];
});
const subtitleOptions = computed(() => [
  ...(engine.value?.subtitles() ?? []).map(track => ({ value: 'hls:'+track.id, label: track.label, disabled: false })),
  ...(source.value?.MediaStreams ?? []).filter(track => track.Type === 'Subtitle').map(track => {
    const nativeOnly = track.IsTextSubtitleStream === false || (track.IsExternal && !['srt', 'subrip', 'vtt', 'webvtt', 'ass', 'ssa'].includes(track.Codec?.toLowerCase() ?? ''));
    return { value: 'server:'+track.Index, label: trackLabel(track)+(nativeOnly ? ' · 需原生客户端' : !track.DeliveryUrl ? ' · 需启用 FFmpeg' : ''), disabled: !track.DeliveryUrl || nativeOnly };
  }),
]);
let active = true;
let session: ReturnType<typeof managePlayer> | undefined;
let progressTimer: ReturnType<typeof setInterval> | undefined;
let began = false,
  resumed = false;
function reportProgress(stopped = false) {
  if (!stopped && remuxSession.value && props.options.itemId) void apiClient.request(`/Videos/${encodeURIComponent(props.options.itemId)}/Playback/${encodeURIComponent(remuxSession.value)}`, { auth: 'required', method: 'PATCH', readOnly: true }).catch(() => {});
  const video = session?.player.video;
  if (
    !began ||
    !props.options.itemId ||
    !video ||
    !Number.isFinite(video.currentTime) ||
    video.currentTime <= 0
  )
    return;
  void apiClient
    .request(`/Sessions/Playing/${stopped ? 'Stopped' : 'Progress'}`, {
      auth: 'required',
      method: 'POST',
      json: {
        ItemId: props.options.itemId,
        PositionTicks: Math.round((offset.value + video.currentTime) * 10000000),
      },
    })
    .catch(() => {});
}
function keyboard(event: KeyboardEvent) {
  if (document.getElementById('scrape-overlay')) return;
  if (event.key === 'Escape') {
    event.preventDefault();
    event.stopImmediatePropagation();
    emit('close');
  }
  if (event.key !== 'Tab' || !shell.value) return;
  const candidates = Array.from(
    shell.value.querySelectorAll<HTMLElement>(
      'button, a[href], input, select, [tabindex]:not([tabindex="-1"])',
    ),
  ).filter(
    (el) => !el.hasAttribute('disabled') && el.getClientRects().length > 0,
  );
  const first = candidates[0] ?? closeButton.value;
  const last = candidates.at(-1) ?? first;
  if (
    event.shiftKey &&
    (document.activeElement === first ||
      !shell.value.contains(document.activeElement))
  ) {
    event.preventDefault();
    last?.focus();
  } else if (
    !event.shiftKey &&
    (document.activeElement === last ||
      !shell.value.contains(document.activeElement))
  ) {
    event.preventDefault();
    first?.focus();
  }
}
function clearSubtitle() {
  subtitleElement?.remove(); subtitleElement = undefined;
  if (subtitleObjectURL) URL.revokeObjectURL(subtitleObjectURL);
  subtitleObjectURL = undefined;
}
async function chooseSubtitle(value: string) {
  subtitleRequest?.abort(); subtitleRequest = new AbortController();
  const request = subtitleRequest;
  trackError.value = '';
  try {
    if (value === '-1' || value.startsWith('hls:')) {
      selectedSubtitle.value = value;
      if (preferenceKey) rememberTrack(preferenceKey, 'subtitle', value);
      engine.value?.selectSubtitle(value === '-1' ? '-1' : value.slice(4));
      clearSubtitle(); return;
    }
    const track = source.value?.MediaStreams.find(track => 'server:'+track.Index === value && track.Type === 'Subtitle');
    if (!track?.DeliveryUrl || !subtitleOptions.value.some(option => option.value === value && !option.disabled)) throw new Error('该字幕暂不支持网页加载');
    let text = await apiClient.request(subtitlePath(track.DeliveryUrl), { auth: 'required', responseType: 'text', signal: request.signal });
    if (!active || request.signal.aborted || typeof text !== 'string') return;
    if (offset.value > 0) text = offsetVtt(text, offset.value);
    const objectURL = URL.createObjectURL(new Blob([text as string], { type: 'text/vtt' }));
    const element = document.createElement('track');
    element.dataset.embySubtitle = 'true';
    element.addEventListener('load', () => {
      // Native captions do not know about Artplayer's overlaid controls.
      const line = Math.max(60, Math.min(80, 100 - 9000 / (session?.player.video?.clientHeight || 360)));
      for (const cue of Array.from(element.track.cues ?? [])) { (cue as VTTCue).snapToLines = false; (cue as VTTCue).line = line; }
    });
    element.kind = 'subtitles'; element.label = trackLabel(track); element.src = objectURL;
    engine.value?.selectSubtitle('-1'); clearSubtitle();
    subtitleObjectURL = objectURL; subtitleElement = element;
    session?.player.video?.appendChild(element); element.track.mode = 'showing';
    selectedSubtitle.value = value;
    if (preferenceKey) rememberTrack(preferenceKey, 'subtitle', value);
  } catch (error) {
    if (!request.signal.aborted && active) trackError.value = error instanceof Error ? error.message : '字幕加载失败';
  }
}
function playbackState() {
  const video = session?.player.video;
  return video ? { time: video.currentTime, paused: video.paused, muted: video.muted, volume: video.volume, rate: video.playbackRate } : undefined;
}
async function mountPlayback(options: PlayerOptions, state?: ReturnType<typeof playbackState>) {
  const serial = ++generation;
  const factory = await loadPlayerFactory(options.type);
  if (!active || serial !== generation || !stage.value) return;
  subtitleRequest?.abort(); clearSubtitle(); trackHost.value = undefined;
  session?.destroy(); engine.value = undefined;
  session = managePlayer(stage.value, { ...options, onTracks(next) {
    if (!active || serial !== generation) return;
    engine.value = { ...next };
    if (!remuxSession.value && next.audio().length) selectedAudio.value = 'hls:'+next.audioId();
    if (initialHlsChoice && next.audio().some(track => 'hls:'+track.id === initialHlsChoice)) {
      const choice = initialHlsChoice; initialHlsChoice = undefined; next.selectAudio(choice.slice(4)); selectedAudio.value = choice;
    }
    if (initialHlsSubtitle && next.subtitles().some(track => 'hls:'+track.id === initialHlsSubtitle)) {
      const choice = initialHlsSubtitle; initialHlsSubtitle = undefined; selectedSubtitle.value = choice; next.selectSubtitle(choice.slice(4));
    }
    if (!selectedSubtitle.value.startsWith('hls:') && next.subtitleId() !== '-1') next.selectSubtitle('-1');
  } }, factory, message => toasts.show(message, 'error'));
  trackHost.value = stage.value.querySelector<HTMLElement>('.art-video-player') ?? stage.value;
  let loaded = false;
  session.player.on('video:loadedmetadata', () => {
    if (!active || serial !== generation || loaded || !session?.player.video) return;
    loaded = true;
    const video = session.player.video;
    if (state) {
      video.currentTime = state.time; video.muted = state.muted; video.volume = state.volume; video.playbackRate = state.rate;
      if (state.paused) video.pause(); else void session.player.play().catch(() => {});
    } else if (!resumed && options.resumeTicks) { video.currentTime = options.resumeTicks / 1e7; resumed = true; }
    if (selectedSubtitle.value !== '-1') void chooseSubtitle(selectedSubtitle.value);
    if (initialAudioChoice) { const choice = initialAudioChoice; initialAudioChoice = undefined; void chooseAudio(choice); }
  });
  session.player.on('video:timeupdate', () => { if (serial === generation) position.value = offset.value + (session?.player.video?.currentTime ?? 0); });
  session.player.on('video:playing', () => { began = true; });
  session.player.on('video:ended', () => {
    if (props.options.itemId && serial === generation) void apiClient.request(`/Users/1/PlayedItems/${encodeURIComponent(props.options.itemId)}`, { auth: 'required', method: 'POST' }).catch(() => {});
  });
}
async function chooseAudio(value: string, seek?: number) {
  if (changingAudio.value) return;
  trackError.value = '';
  if (value.startsWith('hls:') && seek === undefined) {
    engine.value?.selectAudio(value.slice(4)); selectedAudio.value = value;
    if (preferenceKey) rememberTrack(preferenceKey, 'audio', value); return;
  }
  if (!props.options.itemId || !value.startsWith('server:')) return;
  changingAudio.value = true;
  const state = playbackState();
  const start = seek ?? offset.value + (state?.time ?? 0);
  try {
    const result = await apiClient.request(`/Videos/${encodeURIComponent(props.options.itemId)}/Playback`, {
      auth: 'required', method: 'POST', readOnly: true, signal: controller.signal,
      json: { AudioStreamIndex: Number(value.slice(7)), StartTimeTicks: Math.round(start * 1e7), PreviousSession: remuxSession.value },
    }) as { SessionId: string; Url: string; StartTimeTicks: number };
    if (!active) return;
    remuxSession.value = result.SessionId; offset.value = result.StartTimeTicks / 1e7;
    await mountPlayback({ ...baseOptions, type: 'm3u8', url: result.Url, proxyURL: undefined, resumeTicks: undefined, hlsStartPosition: 0 }, state ? { ...state, time: 0 } : undefined);
    selectedAudio.value = value;
    if (preferenceKey) rememberTrack(preferenceKey, 'audio', value);
  } catch (error) {
    if (active && !controller.signal.aborted) {
      trackError.value = error instanceof Error ? error.message : '音轨切换失败';
      if (remuxSession.value) { remuxSession.value = ''; offset.value = 0; selectedAudio.value = 'server:'+source.value?.DefaultAudioStreamIndex; await mountPlayback(baseOptions, state ? { ...state, time: start } : undefined); }
    }
  } finally { changingAudio.value = false; }
}
onMounted(async () => {
  document.body.classList.add('player-open');
  closeButton.value?.focus();
  window.addEventListener('keydown', keyboard, true);
  try {
    if (props.options.itemId) {
      try {
        source.value = parsePlaybackSource(await apiClient.request(`/Items/${encodeURIComponent(props.options.itemId)}/PlaybackInfo`, { auth: 'required', method: 'POST', readOnly: true, signal: controller.signal }));
        baseOptions = { ...baseOptions, type: playbackType(source.value.Container) };
        if (baseOptions.resumeTicks == null && source.value.ResumePositionTicks) baseOptions.resumeTicks = source.value.ResumePositionTicks;
        selectedAudio.value = 'server:'+source.value.DefaultAudioStreamIndex;
        const initial = 'server:'+source.value.DefaultSubtitleStreamIndex;
        if (subtitleOptions.value.some(track => track.value === initial && !track.disabled)) selectedSubtitle.value = initial;
        preferenceKey = props.options.itemId+':'+(source.value.TrackRevision ?? '');
        const preference = trackPreference(preferenceKey);
        if (preference?.subtitle?.startsWith('hls:')) initialHlsSubtitle = preference.subtitle;
        if (preference?.subtitle === '-1' || subtitleOptions.value.some(track => track.value === preference?.subtitle && !track.disabled)) selectedSubtitle.value = preference!.subtitle!;
        if (preference?.audio?.startsWith('hls:')) initialHlsChoice = preference.audio;
        else if (preference?.audio && audioOptions.value.some(track => track.value === preference.audio && !track.disabled) && preference.audio !== selectedAudio.value) initialAudioChoice = preference.audio;
      } catch (error) { if (active) trackError.value = error instanceof Error ? error.message : '选轨信息加载失败'; }
    }
    await mountPlayback(baseOptions);
    if (active && props.options.itemId) progressTimer = setInterval(() => reportProgress(), 10000);
  } catch {
    if (active) {
      toasts.show('播放器加载失败，请刷新后重试', 'error');
      emit('close');
    }
  }
});
onScopeDispose(() => {
  active = false;
  generation++; controller.abort(); subtitleRequest?.abort(); clearSubtitle();
  if (remuxSession.value && props.options.itemId) void apiClient.request(`/Videos/${encodeURIComponent(props.options.itemId)}/Playback/${encodeURIComponent(remuxSession.value)}`, { auth: 'required', method: 'DELETE', readOnly: true }).catch(() => {});
  clearInterval(progressTimer);
  reportProgress(true);
  session?.destroy();
  window.removeEventListener('keydown', keyboard, true);
  document.body.classList.remove('player-open');
});
</script>
<template>
  <div id="player-overlay" class="player-overlay" @click.self="emit('close')">
    <div
      ref="shell"
      class="player-shell"
      role="dialog"
      aria-modal="true"
      aria-labelledby="player-title"
    >
      <div class="player-head">
        <strong id="player-title">{{ options.title }}</strong
        ><button
          id="player-close"
          ref="closeButton"
          class="icon-btn"
          title="关闭 (Esc)"
          @click="emit('close')"
        >
          ✕
        </button>
      </div>
      <div id="player-stage" ref="stage" class="player-stage"></div>
      <Teleport v-if="trackHost && options.itemId" :to="trackHost">
        <div class="player-track-toolbar" @keydown.stop @click.stop @dblclick.stop>
          <label>音轨 <select aria-label="选择音轨" :value="selectedAudio" :disabled="changingAudio || !audioOptions.length" @change="chooseAudio(($event.target as HTMLSelectElement).value)">
            <option v-if="!audioOptions.length" value="">默认音轨</option>
            <option v-for="track in audioOptions" :key="track.value" :value="track.value" :disabled="track.disabled">{{ track.label }}{{ track.disabled ? ' · 需启用 FFmpeg' : '' }}</option>
          </select></label>
          <label>字幕 <select aria-label="选择字幕" :value="selectedSubtitle" @change="chooseSubtitle(($event.target as HTMLSelectElement).value)">
            <option value="-1">关闭字幕</option>
            <option v-for="track in subtitleOptions" :key="track.value" :value="track.value" :disabled="track.disabled">{{ track.label }}</option>
          </select></label>
          <span v-if="changingAudio" role="status">正在切换音轨…</span>
          <span v-if="trackError" class="player-track-error" role="alert">{{ trackError }}</span>
          <label v-if="remuxSession && duration > 0" class="player-track-seek">播放位置
            <input aria-label="重封装播放位置" type="range" min="0" :max="Math.max(0, duration - 1)" step="1" :value="position" :disabled="changingAudio" @change="chooseAudio(selectedAudio, Number(($event.target as HTMLInputElement).value))">
            <span>{{ clock(position) }} / {{ clock(duration) }}</span>
          </label>
        </div>
      </Teleport>
    </div>
  </div>
</template>
<style scoped>
.player-track-toolbar { position: absolute; z-index: 30; top: 8px; left: 8px; right: 8px; display: flex; flex-wrap: wrap; gap: 8px; padding: 8px; background: rgb(0 0 0 / 75%); border-radius: 6px; color: #fff; font-size: 12px; }
.player-track-toolbar label { display: flex; align-items: center; gap: 5px; min-width: 0; flex: 1 1 120px; white-space: nowrap; }
.player-track-toolbar select { min-width: 0; max-width: 100%; width: 100%; padding: 4px; background: #242424; color: #fff; border: 1px solid #777; border-radius: 4px; }
.player-track-error { flex-basis: 100%; color: #ffb6ac; }
.player-track-toolbar .player-track-seek { flex-basis: 100%; }
.player-track-seek input { flex: 1; min-width: 30px; }
.player-track-seek span { white-space: nowrap; }
.player-stage :deep(video::cue) { font-size: clamp(12px, 2vw, 22px); }
</style>
