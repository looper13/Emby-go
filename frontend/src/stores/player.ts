import { nextTick, onScopeDispose, shallowRef } from 'vue';
import { defineStore } from 'pinia';
import type { MovieDto } from '../api/contracts';
import { playbackType, type PlayerOptions } from '../lib/player';
import type { TrackSelection } from '../lib/player-tracks';
export const usePlayerStore = defineStore('player', () => {
  const current = shallowRef<(PlayerOptions & { id: number }) | null>(null); let id = 0; let focus: HTMLElement | null = null;
  function open(options: PlayerOptions) { focus = document.activeElement instanceof HTMLElement ? document.activeElement : null; current.value = { ...options, id: ++id }; }
  function media(movie: MovieDto, selection?: TrackSelection, source?: { id: number; container: string }) {
    const sourceId = source?.id ?? movie.id;
    open({ title: movie.Title || movie.source_path.split(/[\\/]/u).pop() || '播放', url: `/Videos/${sourceId}/stream`, proxyURL: `/Videos/${sourceId}/proxy`, type: playbackType(source?.container || movie.source_container), itemId: String(movie.id), sourceItemId: String(sourceId), selection });
  }
  function trailer(movie: MovieDto, url: string) { open({ title: '预告片 · ' + movie.Title, url: `/Videos/${encodeURIComponent(String(movie.id))}/trailer`, type: playbackType(url), itemId: String(movie.id) }); }
  function close() { const previous = focus; current.value = null; focus = null; void nextTick(() => { if (!current.value && previous?.isConnected) previous.focus({ preventScroll: true }); }); }
  onScopeDispose(close); return { current, media, trailer, close, open };
});
