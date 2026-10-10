import { nextTick, onScopeDispose, shallowRef } from 'vue';
import { defineStore } from 'pinia';
import type { MovieDto } from '../api/contracts';
import type { PlayerOptions } from '../lib/player';
export const usePlayerStore = defineStore('player', () => {
  const current = shallowRef<(PlayerOptions & { id: number }) | null>(null); let id = 0; let focus: HTMLElement | null = null;
  function open(options: PlayerOptions) { focus = document.activeElement instanceof HTMLElement ? document.activeElement : null; current.value = { ...options, id: ++id }; }
  function media(movie: MovieDto) { open({ title: movie.Title || movie.source_path.split(/[\\/]/u).pop() || '播放', url: `/Videos/${movie.id}/stream`, proxyURL: `/Videos/${movie.id}/proxy`, type: movie.source_container?.toLowerCase() === 'webm' ? 'webm' : 'mp4' }); }
  function trailer(movie: MovieDto, url: string) { open({ title: '预告片 · ' + movie.Title, url, type: url.split('?')[0]?.split('.').pop()?.toLowerCase() === 'webm' ? 'webm' : 'mp4' }); }
  function close() { const previous = focus; current.value = null; focus = null; void nextTick(() => { if (!current.value && previous?.isConnected) previous.focus({ preventScroll: true }); }); }
  onScopeDispose(close); return { current, media, trailer, close };
});
