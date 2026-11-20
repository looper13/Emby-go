import { beforeEach, describe, expect, it } from 'vitest';
import { useReactPreferencesStore } from '../src/react/preferences-store';

describe('responsive layout preferences', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.style.removeProperty('--wall-card-min-width');
    useReactPreferencesStore.setState({ cardSize: 'normal', sidebarCollapsed: false, hideImages: false });
  });

  it('toggles and persists the sidebar preference', () => {
    useReactPreferencesStore.getState().toggleSidebar();
    expect(useReactPreferencesStore.getState().sidebarCollapsed).toBe(true);
    expect(localStorage.getItem('emby_sidebar_collapsed')).toBe('true');

    useReactPreferencesStore.getState().toggleSidebar();
    expect(useReactPreferencesStore.getState().sidebarCollapsed).toBe(false);
    expect(localStorage.getItem('emby_sidebar_collapsed')).toBe('false');
  });

  it('sets and persists card size while updating the responsive grid width', () => {
    const store = useReactPreferencesStore.getState();
    store.setCardSize('compact');
    expect(useReactPreferencesStore.getState().cardSize).toBe('compact');
    expect(localStorage.getItem('emby_wall_card_size')).toBe('compact');
    expect(document.documentElement.style.getPropertyValue('--wall-card-min-width')).toBe('144px');

    useReactPreferencesStore.getState().setCardSize('large');
    expect(useReactPreferencesStore.getState().cardSize).toBe('large');
    expect(localStorage.getItem('emby_wall_card_size')).toBe('large');
    expect(document.documentElement.style.getPropertyValue('--wall-card-min-width')).toBe('224px');
  });
});
