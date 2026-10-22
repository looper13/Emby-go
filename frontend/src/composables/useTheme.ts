import { ref } from 'vue';

export type ThemeMode = 'dark' | 'light';

const STORAGE_KEY = 'emby_theme';

function detectPreferredTheme(): ThemeMode {
  if (typeof window === 'undefined') return 'dark';
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved === 'light' || saved === 'dark') return saved;
    if (window.matchMedia?.('(prefers-color-scheme: light)')?.matches) {
      return 'light';
    }
  } catch {
    // Ignore storage quota or security errors
  }
  return 'dark';
}

const currentTheme = ref<ThemeMode>('dark');
let initialized = false;

export function applyTheme(theme: ThemeMode) {
  currentTheme.value = theme;
  if (typeof document !== 'undefined') {
    document.documentElement.setAttribute('data-theme', theme);
    document.documentElement.style.colorScheme = theme;
    if (theme === 'light') {
      document.documentElement.classList.add('theme-light');
      document.documentElement.classList.remove('theme-dark');
    } else {
      document.documentElement.classList.add('theme-dark');
      document.documentElement.classList.remove('theme-light');
    }
  }
  if (typeof localStorage !== 'undefined') {
    try {
      localStorage.setItem(STORAGE_KEY, theme);
    } catch {
      // Ignore
    }
  }
}

export function initTheme() {
  if (initialized) return;
  initialized = true;
  const initial = detectPreferredTheme();
  applyTheme(initial);
}

export function useTheme() {
  if (!initialized) {
    initTheme();
  }

  function toggleTheme() {
    applyTheme(currentTheme.value === 'dark' ? 'light' : 'dark');
  }

  function setTheme(theme: ThemeMode) {
    applyTheme(theme);
  }

  return {
    theme: currentTheme,
    toggleTheme,
    setTheme,
    isDark: () => currentTheme.value === 'dark',
    isLight: () => currentTheme.value === 'light',
  };
}
