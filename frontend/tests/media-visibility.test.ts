import { beforeEach, describe, expect, it } from 'vitest';
import { useReactPreferencesStore } from '../src/react/preferences-store';

describe('media visibility system', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('data-hide-media');
    document.documentElement.className = '';
    useReactPreferencesStore.setState({ hideImages: false });
  });

  it('initializes with images visible when no preference is selected', () => {
    useReactPreferencesStore.getState().initialize();
    expect(useReactPreferencesStore.getState().hideImages).toBe(false);
    expect(document.documentElement.getAttribute('data-hide-media')).toBeNull();
    expect(document.documentElement.classList.contains('hide-media-images')).toBe(false);
  });

  it('toggles image visibility and syncs the document and storage', () => {
    useReactPreferencesStore.getState().initialize();
    useReactPreferencesStore.getState().toggleHideImages();
    expect(useReactPreferencesStore.getState().hideImages).toBe(true);
    expect(document.documentElement.getAttribute('data-hide-media')).toBe('true');
    expect(document.documentElement.classList.contains('hide-media-images')).toBe(true);
    expect(localStorage.getItem('emby_hide_media_images')).toBe('true');

    useReactPreferencesStore.getState().toggleHideImages();
    expect(useReactPreferencesStore.getState().hideImages).toBe(false);
    expect(document.documentElement.getAttribute('data-hide-media')).toBeNull();
    expect(document.documentElement.classList.contains('hide-media-images')).toBe(false);
    expect(localStorage.getItem('emby_hide_media_images')).toBe('false');
  });

  it('restores a hidden-image preference during React application initialization', () => {
    useReactPreferencesStore.setState({ hideImages: true });
    useReactPreferencesStore.getState().initialize();
    expect(document.documentElement.getAttribute('data-hide-media')).toBe('true');
    expect(document.documentElement.classList.contains('hide-media-images')).toBe(true);
  });
});
