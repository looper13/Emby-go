import { onScopeDispose, ref } from 'vue';
import { defineStore } from 'pinia';
export const useToastStore = defineStore('toasts', () => {
  const items = ref<{ id: number; message: string; kind: string; out: boolean }[]>([]);
  const timers = new Set<ReturnType<typeof setTimeout>>(); let next = 0;
  function later(callback: () => void, delay: number) {
    const timer = setTimeout(() => { timers.delete(timer); callback(); }, delay); timers.add(timer);
  }
  function show(message: string, kind = 'ok') {
    const toast = { id: ++next, message, kind, out: false }; items.value.push(toast);
    later(() => {
      const current = items.value.find(t => t.id === toast.id); if (current) current.out = true;
      later(() => { items.value = items.value.filter(t => t.id !== toast.id); }, 260);
    }, 3200);
  }
  function clear() { for (const timer of timers) clearTimeout(timer); timers.clear(); items.value = []; }
  onScopeDispose(clear);
  return { items, show, clear };
});
