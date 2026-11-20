import { create } from 'zustand'

export type CardSize = 'compact' | 'normal' | 'large'

const CARD_SIZE_WIDTH: Record<CardSize, string> = {
  compact: '144px',
  normal: '176px',
  large: '224px',
}

interface PreferencesState {
  cardSize: CardSize
  sidebarCollapsed: boolean
  hideImages: boolean
  initialize: () => void
  setCardSize: (size: CardSize) => void
  toggleSidebar: () => void
  toggleHideImages: () => void
}

function readStorage(key: string): string | null {
  try { return localStorage.getItem(key) } catch { return null }
}

function persist(key: string, value: string) {
  try { localStorage.setItem(key, value) } catch { /* Keep preferences for this page session. */ }
}

function applyHideImages(hide: boolean) {
  document.documentElement.toggleAttribute('data-hide-media', hide)
  if (hide) document.documentElement.dataset.hideMedia = 'true'
  else delete document.documentElement.dataset.hideMedia
  document.documentElement.classList.toggle('hide-media-images', hide)
}

function applyCardSize(size: CardSize) {
  document.documentElement.style.setProperty('--wall-card-min-width', CARD_SIZE_WIDTH[size])
}

function savedCardSize(): CardSize {
  const size = readStorage('emby_wall_card_size')
  return size === 'compact' || size === 'large' ? size : 'normal'
}

export const useReactPreferencesStore = create<PreferencesState>()((set, get) => ({
  cardSize: savedCardSize(),
  sidebarCollapsed: readStorage('emby_sidebar_collapsed') === 'true',
  hideImages: readStorage('emby_hide_media_images') === 'true',
  initialize() {
    applyCardSize(get().cardSize)
    applyHideImages(get().hideImages)
  },
  setCardSize(cardSize) {
    applyCardSize(cardSize)
    persist('emby_wall_card_size', cardSize)
    set({ cardSize })
  },
  toggleSidebar() {
    const sidebarCollapsed = !get().sidebarCollapsed
    persist('emby_sidebar_collapsed', String(sidebarCollapsed))
    set({ sidebarCollapsed })
  },
  toggleHideImages() {
    const hideImages = !get().hideImages
    applyHideImages(hideImages)
    persist('emby_hide_media_images', String(hideImages))
    set({ hideImages })
  },
}))
