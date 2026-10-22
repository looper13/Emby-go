<script setup lang="ts">
import { computed, nextTick, onScopeDispose, ref, shallowRef, watch } from 'vue';
import { ChevronDown, ExternalLink, MonitorPlay, RefreshCw } from '@lucide/vue';
import { apiClient } from '../../api/client';
import { audioTrackOptions, parsePlaybackSource, resolveTrackSelection, selectionMatches, subtitleTrackOptions, supportsNativeAudio, type PlaybackSource, type TrackSelection } from '../../lib/player-tracks';
import { externalPlayerURL, externalPlayers } from '../../lib/external-player';

const props = defineProps<{ itemId: string | number; refreshKey?: unknown }>();
const emit = defineEmits<{ selection: [value: TrackSelection | undefined] }>();
const source = shallowRef<PlaybackSource>();
const selectedAudio = ref(-1), selectedSubtitle = ref(-1), loading = ref(false), error = ref('');
const root = ref<HTMLElement>(), menu = ref<HTMLElement>(), toggle = ref<HTMLButtonElement>(), expanded = ref(false);
const audio = computed(() => source.value ? audioTrackOptions(source.value, supportsNativeAudio(source.value)) : []);
const subtitles = computed(() => source.value ? subtitleTrackOptions(source.value, supportsNativeAudio(source.value)) : []);
const links = computed(() => {
  try { return source.value?.ExternalStreamUrl ? externalPlayers.map(player => ({ ...player, url: externalPlayerURL(player.id, source.value!.ExternalStreamUrl!) })) : []; }
  catch { return []; }
});
let request: AbortController | undefined;
function selection(): TrackSelection | undefined {
  return source.value ? { sourceId: source.value.Id, revision: source.value.TrackRevision, audioIndex: selectedAudio.value, subtitleIndex: selectedSubtitle.value } : undefined;
}
function change() { emit('selection', selection()); }
async function load(preserve = false) {
  const previous = preserve ? selection() : undefined;
  request?.abort();
  const current = new AbortController(); request = current;
  source.value = undefined; expanded.value = false; error.value = ''; loading.value = true;
  emit('selection', undefined);
  try {
    const result = parsePlaybackSource(await apiClient.request(`/Items/${encodeURIComponent(props.itemId)}/PlaybackInfo`, { auth: 'required', method: 'POST', readOnly: true, signal: current.signal }));
    if (current.signal.aborted) return;
    source.value = result;
    const value = resolveTrackSelection(result, selectionMatches(result, previous) ? previous : undefined, supportsNativeAudio(result));
    selectedAudio.value = value.audioIndex; selectedSubtitle.value = value.subtitleIndex;
    change();
  } catch (cause) {
    if (!current.signal.aborted) error.value = cause instanceof Error ? cause.message : '播放选项加载失败';
  } finally { if (!current.signal.aborted) loading.value = false; }
}
async function openMenu() {
  expanded.value = !expanded.value;
  if (expanded.value) { await nextTick(); menu.value?.querySelector<HTMLElement>('a')?.focus(); }
}
function closeMenu(restoreFocus = false) {
  expanded.value = false;
  if (restoreFocus) toggle.value?.focus();
}
function keydown(event: KeyboardEvent) {
  if (!expanded.value) return;
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); closeMenu(true); }
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp' || event.key === 'Home' || event.key === 'End') {
    const items = Array.from(menu.value?.querySelectorAll<HTMLAnchorElement>('a') ?? []);
    if (!items.length) return;
    event.preventDefault();
    const index = items.indexOf(document.activeElement as HTMLAnchorElement);
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : (index + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length;
    items[next]?.focus();
  }
}
function outside(event: Event) { if (event.target instanceof Node && !root.value?.contains(event.target)) closeMenu(); }
function focusout(event: FocusEvent) { if (!root.value?.contains(event.relatedTarget as Node | null)) closeMenu(); }
document.addEventListener('pointerdown', outside);
watch(() => props.itemId, () => void load(), { immediate: true });
watch(() => props.refreshKey, () => void load(true));
onScopeDispose(() => { request?.abort(); document.removeEventListener('pointerdown', outside); });
</script>
<template>
  <div ref="root" class="detail-playback" :aria-busy="loading" @keydown="keydown" @focusout="focusout">
    <div class="detail-track-fields">
      <label><span>音轨</span><select v-model="selectedAudio" aria-label="选择音轨" :disabled="loading || !audio.length" @change="change">
        <option v-if="!audio.length" :value="-1">{{ loading ? '加载中…' : '默认音轨' }}</option>
        <option v-for="track in audio" :key="track.value" :value="track.value" :disabled="track.disabled">{{ track.label }}{{ track.disabled ? ' · 需启用 FFmpeg' : '' }}</option>
      </select></label>
      <label><span>字幕</span><select v-model="selectedSubtitle" aria-label="选择字幕" :disabled="loading || !source" @change="change">
        <option :value="-1">关闭字幕</option>
        <option v-for="track in subtitles" :key="track.value" :value="track.value" :disabled="track.disabled">{{ track.label }}</option>
      </select></label>
      <div class="external-player">
        <button ref="toggle" class="btn" aria-label="外部播放器" aria-haspopup="menu" :aria-expanded="expanded" :disabled="!links.length" @click="openMenu"><MonitorPlay :size="16" aria-hidden="true" /><span>外部播放</span><ChevronDown :size="14" aria-hidden="true" /></button>
        <div v-if="expanded" ref="menu" class="external-player-menu" role="menu" aria-label="外部播放器">
          <a v-for="link in links" :key="link.id" :href="link.url" :data-player="link.id" role="menuitem" :title="`用 ${link.name} 打开原始媒体（需注册本机协议）`" rel="noreferrer" @click="closeMenu(true)"><ExternalLink :size="15" aria-hidden="true" />{{ link.name }}</a>
        </div>
      </div>
    </div>
    <div v-if="error" class="detail-playback-error" role="alert"><span>{{ error }}</span><button class="icon-btn" title="重新加载播放选项" aria-label="重新加载播放选项" @click="load()"><RefreshCw :size="16" aria-hidden="true" /></button></div>
  </div>
</template>
<style scoped>
.detail-playback { min-width: 0; margin: 16px 0; position: relative; z-index: 3; }
.detail-track-fields { display: flex; flex-wrap: wrap; align-items: flex-end; gap: 12px; }
.detail-track-fields label { display: flex; flex: 1 1 190px; flex-direction: column; gap: 6px; min-width: 0; max-width: 340px; font-size: 12px; color: var(--muted); }
.detail-track-fields select { width: 100%; min-width: 0; height: 38px; padding: 0 30px 0 10px; border: 1px solid var(--line); border-radius: 8px; background: var(--panel-2); color: var(--text); font: inherit; font-size: 13px; text-overflow: ellipsis; }
.external-player { position: relative; flex: 0 0 auto; }
.external-player > button { display: inline-flex; align-items: center; gap: 7px; height: 38px; }
.external-player-menu { position: absolute; z-index: 40; top: calc(100% + 6px); right: 0; width: 180px; max-width: calc(100vw - 48px); border: 1px solid var(--line); border-radius: 8px; background: var(--panel); padding: 4px; box-shadow: var(--shadow); }
.external-player-menu a { display: flex; align-items: center; gap: 10px; padding: 8px 10px; color: var(--text); font-size: 13px; text-decoration: none; border-radius: 6px; transition: background-color 0.15s ease, color 0.15s ease; }
.external-player-menu a:hover, .external-player-menu a:focus-visible { background: var(--accent-soft); color: var(--accent); outline: none; }
.detail-playback-error { display: flex; align-items: center; gap: 8px; color: var(--danger); font-size: 13px; margin-top: 8px; overflow-wrap: anywhere; }
@media (max-width: 600px) { .detail-track-fields label { flex-basis: 100%; max-width: none; } .external-player-menu { left: 0; right: auto; } }
</style>
