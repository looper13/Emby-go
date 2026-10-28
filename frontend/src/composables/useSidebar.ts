import { ref } from 'vue';

const STORAGE_KEY = 'emby_sidebar_collapsed';

function readInitial(): boolean {
  try {
    return typeof localStorage !== 'undefined' && localStorage.getItem(STORAGE_KEY) === 'true';
  } catch {
    return false;
  }
}

const isCollapsed = ref<boolean>(readInitial());

export function useSidebar() {
  function toggle() {
    isCollapsed.value = !isCollapsed.value;
    try {
      if (typeof localStorage !== 'undefined') {
        localStorage.setItem(STORAGE_KEY, String(isCollapsed.value));
      }
    } catch {
      // Ignore storage errors in restricted environments.
    }
  }

  function setCollapsed(value: boolean) {
    isCollapsed.value = value;
    try {
      if (typeof localStorage !== 'undefined') {
        localStorage.setItem(STORAGE_KEY, String(value));
      }
    } catch {
      // Ignore storage errors.
    }
  }

  return {
    isCollapsed,
    toggle,
    setCollapsed,
  };
}
