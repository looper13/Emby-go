import { onMounted, onScopeDispose, ref, shallowRef } from 'vue';
import { usePageRequest } from './usePageRequest';
export function useAdminRead<T>(read: (signal: AbortSignal) => Promise<T>) {
  const scope = usePageRequest(); const data = shallowRef<T | null>(null);
  const error = ref(''); const loading = ref(false); let active = true;
  onScopeDispose(() => { active = false; });
  async function load() {
    if (!active) return;
    const current = scope.begin(); loading.value = true; error.value = '';
    try { const result = await read(current.signal); if (current.isCurrent()) data.value = result; }
    catch (cause) { if (current.isCurrent()) error.value = cause instanceof Error ? cause.message : '加载失败'; }
    finally { if (current.isCurrent()) loading.value = false; }
  }
  onMounted(() => { void load(); });
  return { data, error, loading, load, active: () => active };
}
