import { ref } from 'vue';

export type CardSize = 'compact' | 'normal' | 'large';

const STORAGE_KEY = 'emby_wall_card_size';

const SIZES: Record<CardSize, string> = {
  compact: '144px',
  normal: '176px',
  large: '224px',
};

function readInitialSize(): CardSize {
  try {
    if (typeof localStorage !== 'undefined') {
      const saved = localStorage.getItem(STORAGE_KEY);
      if (saved === 'compact' || saved === 'normal' || saved === 'large') {
        return saved;
      }
    }
  } catch {
    // Ignore storage errors.
  }
  return 'normal';
}

const currentSize = ref<CardSize>(readInitialSize());

function applySizeVariable(size: CardSize) {
  if (typeof document !== 'undefined' && document.documentElement) {
    document.documentElement.style.setProperty('--wall-card-min-width', SIZES[size]);
  }
}

// Apply immediately on module evaluation
applySizeVariable(currentSize.value);

export function useCardSize() {
  function setSize(size: CardSize) {
    currentSize.value = size;
    try {
      if (typeof localStorage !== 'undefined') {
        localStorage.setItem(STORAGE_KEY, size);
      }
    } catch {
      // Ignore storage errors.
    }
    applySizeVariable(size);
  }

  return {
    cardSize: currentSize,
    setSize,
    sizes: SIZES,
  };
}
