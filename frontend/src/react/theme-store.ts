import { create } from 'zustand'

export type ThemeMode = 'dark' | 'light'

const STORAGE_KEY = 'emby_theme'

function preferredTheme(): ThemeMode {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved === 'dark' || saved === 'light') return saved
    if (window.matchMedia?.('(prefers-color-scheme: light)').matches) return 'light'
  } catch {
    // Keep the dark theme when browser preferences cannot be read.
  }
  return 'dark'
}

function applyTheme(theme: ThemeMode) {
  document.documentElement.dataset.theme = theme
  document.documentElement.style.colorScheme = theme
  document.documentElement.classList.toggle('theme-light', theme === 'light')
  document.documentElement.classList.toggle('theme-dark', theme === 'dark')
  try {
    localStorage.setItem(STORAGE_KEY, theme)
  } catch {
    // Theme changes remain active for the current page when storage is unavailable.
  }
}

interface ThemeState {
  theme: ThemeMode
  initialize: () => void
  toggle: () => void
}

export const useThemeStore = create<ThemeState>()((set, get) => ({
  theme: preferredTheme(),
  initialize() {
    applyTheme(get().theme)
  },
  toggle() {
    const theme = get().theme === 'dark' ? 'light' : 'dark'
    applyTheme(theme)
    set({ theme })
  },
}))
