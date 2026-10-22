import { beforeEach, describe, expect, it } from 'vitest';
import { applyMediaVisibility, initMediaVisibility, useMediaVisibility } from '../src/composables/useMediaVisibility';

describe('media visibility system', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('data-hide-media');
    document.documentElement.className = '';
  });

  it('initializes with images visible (false) when no preference stored', () => {
    applyMediaVisibility(false);
    initMediaVisibility();
    const { hideImages, isHidden } = useMediaVisibility();
    expect(hideImages.value).toBe(false);
    expect(isHidden()).toBe(false);
    expect(document.documentElement.getAttribute('data-hide-media')).toBeNull();
    expect(document.documentElement.classList.contains('hide-media-images')).toBe(false);
  });

  it('toggles media visibility between hidden and visible and syncs document and storage', () => {
    applyMediaVisibility(false);
    const { hideImages, toggleHideImages, isHidden } = useMediaVisibility();

    toggleHideImages();
    expect(hideImages.value).toBe(true);
    expect(isHidden()).toBe(true);
    expect(document.documentElement.getAttribute('data-hide-media')).toBe('true');
    expect(document.documentElement.classList.contains('hide-media-images')).toBe(true);
    expect(localStorage.getItem('emby_hide_media_images')).toBe('true');

    toggleHideImages();
    expect(hideImages.value).toBe(false);
    expect(isHidden()).toBe(false);
    expect(document.documentElement.getAttribute('data-hide-media')).toBeNull();
    expect(document.documentElement.classList.contains('hide-media-images')).toBe(false);
    expect(localStorage.getItem('emby_hide_media_images')).toBe('false');
  });

  it('explicitly sets media visibility', () => {
    const { hideImages, setHideImages } = useMediaVisibility();
    setHideImages(true);
    expect(hideImages.value).toBe(true);
    expect(document.documentElement.getAttribute('data-hide-media')).toBe('true');
    expect(localStorage.getItem('emby_hide_media_images')).toBe('true');

    setHideImages(false);
    expect(hideImages.value).toBe(false);
    expect(document.documentElement.getAttribute('data-hide-media')).toBeNull();
    expect(localStorage.getItem('emby_hide_media_images')).toBe('false');
  });
});
