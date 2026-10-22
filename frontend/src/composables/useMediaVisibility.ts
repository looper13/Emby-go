import { ref } from 'vue';

const STORAGE_KEY = 'emby_hide_media_images';

function detectPreferredMediaVisibility(): boolean {
  if (typeof window === 'undefined') return false;
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved === 'true') return true;
    if (saved === 'false') return false;
  } catch {
    // Ignore storage quota or security errors
  }
  return false;
}

const hideImages = ref<boolean>(false);
let initialized = false;

export function applyMediaVisibility(hidden: boolean) {
  hideImages.value = hidden;
  if (typeof document !== 'undefined') {
    if (hidden) {
      document.documentElement.setAttribute('data-hide-media', 'true');
      document.documentElement.classList.add('hide-media-images');
    } else {
      document.documentElement.removeAttribute('data-hide-media');
      document.documentElement.classList.remove('hide-media-images');
    }
  }
  if (typeof localStorage !== 'undefined') {
    try {
      localStorage.setItem(STORAGE_KEY, String(hidden));
    } catch {
      // Ignore
    }
  }
}

export function initMediaVisibility() {
  if (initialized) return;
  initialized = true;
  const initial = detectPreferredMediaVisibility();
  applyMediaVisibility(initial);
}

export function useMediaVisibility() {
  if (!initialized) {
    initMediaVisibility();
  }

  function toggleHideImages() {
    applyMediaVisibility(!hideImages.value);
  }

  function setHideImages(hidden: boolean) {
    applyMediaVisibility(hidden);
  }

  return {
    hideImages,
    toggleHideImages,
    setHideImages,
    isHidden: () => hideImages.value,
  };
}
