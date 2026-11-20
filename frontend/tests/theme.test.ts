import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useThemeStore } from '../src/react/theme-store';

describe('theme system', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('data-theme');
    document.documentElement.className = '';
    document.documentElement.style.colorScheme = '';
    useThemeStore.setState({ theme: 'dark' });
  });

  it('applies the restored theme from the React store during application initialization', () => {
    useThemeStore.setState({ theme: 'light' });
    useThemeStore.getState().initialize();

    expect(useThemeStore.getState().theme).toBe('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
    expect(document.documentElement.classList.contains('theme-light')).toBe(true);
    expect(document.documentElement.style.colorScheme).toBe('light');
  });

  it('toggles theme and syncs the document and storage', () => {
    useThemeStore.getState().initialize();
    useThemeStore.getState().toggle();

    expect(useThemeStore.getState().theme).toBe('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
    expect(document.documentElement.classList.contains('theme-light')).toBe(true);
    expect(document.documentElement.classList.contains('theme-dark')).toBe(false);
    expect(localStorage.getItem('emby_theme')).toBe('light');

    useThemeStore.getState().toggle();
    expect(useThemeStore.getState().theme).toBe('dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(document.documentElement.classList.contains('theme-dark')).toBe(true);
    expect(localStorage.getItem('emby_theme')).toBe('dark');
  });

  it('keeps the current-page preference when browser storage is unavailable', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('storage unavailable'); });
    expect(() => useThemeStore.getState().toggle()).not.toThrow();
    expect(useThemeStore.getState().theme).toBe('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
  });
});
