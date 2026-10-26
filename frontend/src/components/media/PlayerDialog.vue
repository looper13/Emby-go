<script setup lang="ts">
import { computed, onMounted, onScopeDispose, ref, shallowRef } from 'vue';
import { Film, Loader2, X } from '@lucide/vue';
import {
  loadPlayerFactory,
  managePlayer,
  type PlayerOptions,
} from '../../lib/player';
import { useToastStore } from '../../stores/toasts';
import { apiClient } from '../../api/client';
import { currentUserPath } from '../../api/user-context';
import { useAuthStore } from '../../stores/auth';
import { offsetVtt, parsePlaybackSource, resolveTrackSelection, selectionMatches, subtitleTrackOptions, supportsNativeAudio, subtitlePath, trackLabel, type PlaybackSource, type TrackEngine } from '../../lib/player-tracks';
import { playbackType } from '../../lib/player';
const props = defineProps<{ options: PlayerOptions }>();
const emit = defineEmits<{ close: [] }>();
const shell = ref<HTMLElement>();
const stage = ref<HTMLElement>();
const closeButton = ref<HTMLButtonElement>();
const toasts = useToastStore();
const auth = useAuthStore(), playbackToken = auth.token;
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
let initialNativeAudio: number | undefined;
let disableNativeSubtitles = false;
let baseOptions = { ...props.options };
const duration = computed(() => (source.value?.RunTimeTicks ?? 0) / 1e7);
function clock(seconds: number) { const value = Math.max(0, Math.floor(seconds)); return `${Math.floor(value / 60)}:${String(value % 60).padStart(2, '0')}`; }
const subtitleOptions = computed(() => source.value ? subtitleTrackOptions(source.value, supportsNativeAudio(source.value)) : []);
let active = true;
let session: ReturnType<typeof managePlayer> | undefined;
let progressTimer: ReturnType<typeof setInterval> | undefined;
let began = false,
  resumed = false;
function reportProgress(stopped = false) {
  if (auth.token !== playbackToken) return;
  if (!stopped && remuxSession.value && props.options.itemId) void apiClient.request(`/Videos/${encodeURIComponent(props.options.sourceItemId || props.options.itemId)}/Playback/${encodeURIComponent(remuxSession.value)}`, { auth: 'required', method: 'PATCH', readOnly: true }).catch(() => {});
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
async function markPlayed() {
  if (!active || auth.token !== playbackToken || !props.options.itemId) return;
  try {
    await apiClient.request(currentUserPath(`PlayedItems/${encodeURIComponent(props.options.itemId)}`), { auth: 'required', method: 'POST' });
  } catch { /* Progress reporting must not interrupt playback. */ }
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
  try {
    if (value === '-1') {
      selectedSubtitle.value = value;
      engine.value?.selectSubtitle('-1');
      clearSubtitle(); return;
    }
    const track = source.value?.MediaStreams.find(track => 'server:'+track.Index === value && track.Type === 'Subtitle');
    if (track && !track.DeliveryUrl && !track.IsExternal && supportsNativeAudio(source.value)) {
      const declared = source.value!.MediaStreams.filter(entry => entry.Type === 'Subtitle' && !entry.IsExternal);
      const available = engine.value?.subtitles() ?? [];
      const target = available.length === declared.length ? available[declared.findIndex(entry => entry.Index === track.Index)] : undefined;
      if (!target) throw new Error('浏览器未提供所选字幕轨道');
      if (engine.value?.subtitleId() !== target.id) engine.value?.selectSubtitle(target.id);
      return;
    }
    if (!track?.DeliveryUrl || !subtitleOptions.value.some(option => 'server:'+option.value === value && !option.disabled)) throw new Error('该字幕暂不支持网页加载');
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
    if (!remuxSession.value && initialNativeAudio !== undefined) {
      const tracks = source.value?.MediaStreams.filter(track => track.Type === 'Audio' && !track.IsExternal) ?? [];
      const index = tracks.findIndex(track => track.Index === initialNativeAudio);
      const target = next.audio().length === tracks.length ? next.audio()[index] : undefined;
      if (target) { initialNativeAudio = undefined; next.selectAudio(target.id); }
    }
    const nativeSubtitles = source.value?.MediaStreams.filter(track => track.Type === 'Subtitle' && !track.IsExternal) ?? [];
    const subtitleIndex = nativeSubtitles.findIndex(track => 'server:'+track.Index === selectedSubtitle.value && !track.DeliveryUrl);
    const subtitle = !remuxSession.value && next.subtitles().length === nativeSubtitles.length ? next.subtitles()[subtitleIndex] : undefined;
    if (subtitle) { if (next.subtitleId() !== subtitle.id) next.selectSubtitle(subtitle.id); }
    else if (disableNativeSubtitles && next.subtitleId() !== '-1') next.selectSubtitle('-1');
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
    if (initialNativeAudio !== undefined) trackError.value = '浏览器尚未提供所选音轨；可返回详情页启用 FFmpeg 后重试';
  });
  session.player.on('video:timeupdate', () => { if (serial === generation) position.value = offset.value + (session?.player.video?.currentTime ?? 0); });
  session.player.on('video:playing', () => { began = true; });
  session.player.on('video:ended', () => {
    if (serial === generation) void markPlayed();
  });
}
async function chooseAudio(value: string, seek?: number) {
  if (changingAudio.value) return;
  trackError.value = '';
  if (!props.options.itemId || !value.startsWith('server:')) return;
  changingAudio.value = true;
  const state = playbackState();
  const start = seek ?? offset.value + (state?.time ?? 0);
  try {
    const result = await apiClient.request(`/Videos/${encodeURIComponent(props.options.sourceItemId || props.options.itemId)}/Playback`, {
      auth: 'required', method: 'POST', readOnly: true, signal: controller.signal,
      json: { AudioStreamIndex: Number(value.slice(7)), StartTimeTicks: Math.round(start * 1e7), PreviousSession: remuxSession.value },
    }) as { SessionId: string; Url: string; StartTimeTicks: number };
    if (!active) return;
    remuxSession.value = result.SessionId; offset.value = result.StartTimeTicks / 1e7;
    await mountPlayback({ ...baseOptions, type: 'm3u8', url: result.Url, proxyURL: undefined, resumeTicks: undefined, hlsStartPosition: 0 }, state ? { ...state, time: 0 } : undefined);
    selectedAudio.value = value;
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
        source.value = parsePlaybackSource(await apiClient.request(`/Items/${encodeURIComponent(props.options.sourceItemId || props.options.itemId)}/PlaybackInfo`, { auth: 'required', method: 'POST', readOnly: true, signal: controller.signal }));
        baseOptions = { ...baseOptions, type: playbackType(source.value.Container) };
        if (baseOptions.resumeTicks == null && source.value.ResumePositionTicks) baseOptions.resumeTicks = source.value.ResumePositionTicks;
        const selected = resolveTrackSelection(source.value, props.options.selection, supportsNativeAudio(source.value));
        selectedAudio.value = 'server:'+selected.audioIndex;
        selectedSubtitle.value = selected.subtitleIndex === -1 ? '-1' : 'server:'+selected.subtitleIndex;
        disableNativeSubtitles = selected.subtitleIndex !== -1 ||
          (selectionMatches(source.value, props.options.selection) && props.options.selection?.subtitleIndex === -1);
        if (props.options.selection && !selectionMatches(source.value, props.options.selection)) trackError.value = '媒体轨道已变化，已使用默认选项';
        if (selected.audioIndex !== -1 && selected.audioIndex !== source.value.DefaultAudioStreamIndex) {
          if (source.value.SupportsAudioSelection) await chooseAudio(selectedAudio.value, (baseOptions.resumeTicks ?? 0) / 1e7);
          else initialNativeAudio = selected.audioIndex;
        }
      } catch (error) { if (active) trackError.value = error instanceof Error ? error.message : '选轨信息加载失败'; }
    }
    if (!session) await mountPlayback(baseOptions);
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
  if (auth.token === playbackToken && remuxSession.value && props.options.itemId) void apiClient.request(`/Videos/${encodeURIComponent(props.options.sourceItemId || props.options.itemId)}/Playback/${encodeURIComponent(remuxSession.value)}`, { auth: 'required', method: 'DELETE', readOnly: true }).catch(() => {});
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
        <div class="player-title-wrap">
          <Film :size="16" class="player-title-icon" aria-hidden="true" />
          <strong id="player-title" :title="options.title">{{ options.title }}</strong>
        </div>
        <button
          id="player-close"
          ref="closeButton"
          class="icon-btn player-close-btn"
          title="关闭 (Esc)"
          aria-label="关闭 (Esc)"
          @click="emit('close')"
        >
          <X :size="18" aria-hidden="true" />
        </button>
      </div>
      <div v-if="!trackHost" class="player-loading" role="status">
        <Loader2 :size="20" class="player-loading-spinner" aria-hidden="true" />
        <span>{{ changingAudio ? '正在准备所选音轨…' : '正在加载播放器…' }}</span>
      </div>
      <div id="player-stage" ref="stage" class="player-stage"></div>
      <Teleport v-if="trackHost && options.itemId && (changingAudio || trackError || remuxSession)" :to="trackHost">
        <div class="player-track-toolbar" @keydown.stop @click.stop @dblclick.stop>
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
.player-title-wrap {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  flex: 1;
}

.player-title-icon {
  color: var(--accent);
  flex-shrink: 0;
}

.player-head strong,
#player-title {
  color: #f8f9fa;
  font-size: 15px;
  font-weight: 600;
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.player-close-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  padding: 0;
  border-radius: 8px;
  color: rgba(255, 255, 255, 0.75);
  background: rgba(255, 255, 255, 0.08);
  border: 1px solid rgba(255, 255, 255, 0.12);
  cursor: pointer;
  flex-shrink: 0;
  transition: all 0.15s ease;
}

.player-close-btn:hover {
  color: #ffffff;
  background: rgba(255, 255, 255, 0.18);
  border-color: rgba(255, 255, 255, 0.28);
  transform: translateY(-1px);
}

.player-close-btn:active {
  transform: translateY(0);
}

.player-close-btn svg {
  pointer-events: none;
}

.player-loading {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 18px;
  background: rgba(18, 19, 22, 0.88);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 20px;
  color: #ffffff;
  font-size: 13.5px;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.5);
  pointer-events: none;
  z-index: 10;
}

.player-loading-spinner {
  animation: player-spin 1s linear infinite;
  color: var(--accent);
  flex-shrink: 0;
}

@keyframes player-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.player-track-toolbar {
  position: absolute;
  z-index: 30;
  top: 10px;
  left: 10px;
  right: 10px;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  padding: 10px 14px;
  background: rgba(18, 19, 22, 0.9);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  color: #f8f9fa;
  font-size: 12px;
  box-shadow: 0 4px 18px rgba(0, 0, 0, 0.45);
}

.player-track-toolbar label {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  flex: 1 1 120px;
  white-space: nowrap;
}

.player-track-error {
  flex-basis: 100%;
  color: #ff8787;
  font-weight: 500;
}

.player-track-toolbar .player-track-seek {
  flex-basis: 100%;
}

.player-track-seek input {
  flex: 1;
  min-width: 30px;
  accent-color: var(--accent);
}

.player-track-seek span {
  white-space: nowrap;
}

.player-stage :deep(video::cue) {
  font-size: clamp(12px, 2vw, 22px);
}
</style>
