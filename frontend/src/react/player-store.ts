import { create } from 'zustand'
import type { MovieDto } from '../api/contracts'
import type { R18LocalSource } from '../api/r18'
import { playbackType, type PlayerOptions } from '../lib/player'
import type { TrackSelection } from '../lib/player-tracks'

interface PlayerState {
  current: (PlayerOptions & { id: number }) | null
  media: (movie: MovieDto, selection?: TrackSelection, source?: { id: number; container: string }) => void
  local: (source: R18LocalSource) => void
  trailer: (movie: MovieDto, url: string) => void
  open: (options: PlayerOptions) => void
  close: () => void
}

let nextID = 0
let restoreFocus: HTMLElement | null = null

export const useReactPlayerStore = create<PlayerState>()((set, get) => ({
  current: null,
  media(movie, selection, source) {
    const sourceID = source?.id ?? movie.id
    get().open({
      title: movie.Title || movie.source_path.split(/[\\/]/u).pop() || '播放',
      url: `/Videos/${sourceID}/stream`,
      proxyURL: `/Videos/${sourceID}/proxy`,
      type: playbackType(source?.container || movie.source_container),
      itemId: String(movie.id),
      sourceItemId: String(sourceID),
      selection,
    })
  },
  local(source) {
    const sourceID = encodeURIComponent(source.item_id)
    get().open({
      title: source.name || '本地 STRM',
      url: `/Videos/${sourceID}/stream`,
      proxyURL: `/Videos/${sourceID}/proxy`,
      type: playbackType(source.container),
      itemId: source.item_id,
      sourceItemId: source.item_id,
    })
  },
  trailer(movie, url) {
    get().open({
      title: `预告片 · ${movie.Title}`,
      url: `/Videos/${encodeURIComponent(String(movie.id))}/trailer`,
      type: playbackType(url),
      itemId: String(movie.id),
    })
  },
  open(options) {
    if (!get().current) restoreFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    set({ current: { ...options, id: ++nextID } })
  },
  close() {
    const previous = restoreFocus
    set({ current: null })
    restoreFocus = null
    queueMicrotask(() => {
      if (!get().current && previous?.isConnected) previous.focus({ preventScroll: true })
    })
  },
}))

export function activatePlayerRuntime(): () => void {
  return () => useReactPlayerStore.getState().close()
}
