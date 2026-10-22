import { beforeEach, describe, expect, it } from 'vitest';
import { applyTheme, initTheme, useTheme } from '../src/composables/useTheme';

describe('theme system', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('data-theme');
    document.documentElement.className = '';
  });

  it('initializes with default dark theme when no preference stored', () => {
    initTheme();
    const { theme, isDark, isLight } = useTheme();
    expect(theme.value).toBe('dark');
    expect(isDark()).toBe(true);
    expect(isLight()).toBe(false);
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(document.documentElement.classList.contains('theme-dark')).toBe(true);
  });

  it('toggles theme between dark and light and syncs document and storage', () => {
    applyTheme('dark');
    const { theme, toggleTheme, isDark, isLight } = useTheme();

    toggleTheme();
    expect(theme.value).toBe('light');
    expect(isLight()).toBe(true);
    expect(isDark()).toBe(false);
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
    expect(document.documentElement.classList.contains('theme-light')).toBe(true);
    expect(localStorage.getItem('emby_theme')).toBe('light');

    toggleTheme();
    expect(theme.value).toBe('dark');
    expect(isDark()).toBe(true);
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(document.documentElement.classList.contains('theme-dark')).toBe(true);
    expect(localStorage.getItem('emby_theme')).toBe('dark');
  });

  it('explicitly sets theme', () => {
    const { theme, setTheme } = useTheme();
    setTheme('light');
    expect(theme.value).toBe('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
    expect(localStorage.getItem('emby_theme')).toBe('light');

    setTheme('dark');
    expect(theme.value).toBe('dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(localStorage.getItem('emby_theme')).toBe('dark');
  });
});
