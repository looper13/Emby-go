import { nextTick, onScopeDispose, ref, shallowRef } from 'vue';
import { defineStore } from 'pinia';
import { scrapeApi, type Candidate, type Inspect, type Preview } from '../api/scrape';
import { useToastStore } from './toasts';
export const useScrapePreviewStore = defineStore('scrape-preview', () => {
  const current = shallowRef<{ movieId: number; confirmed: () => void | Promise<unknown> } | null>(null);
  const preview = shallowRef<Preview | null>(null), inspect = shallowRef<Inspect | null>(null);
  const loading = ref(false), writing = ref(false), error = ref(''), overwrite = ref(false);
  const toasts = useToastStore(); let controller: AbortController | undefined, generation = 0, focus: HTMLElement | null = null;
  function close() { generation++; controller?.abort(); current.value = null; preview.value = inspect.value = null; loading.value = false; error.value = ''; const previous = focus; focus = null; void nextTick(() => { if (!current.value && previous?.isConnected) previous.focus({ preventScroll: true }); }); }
  async function select(candidate: Candidate) {
    if (!current.value || writing.value) return;
    const owner = current.value, epoch = ++generation; controller?.abort(); controller = new AbortController(); loading.value = true; error.value = ''; inspect.value = null;
    try { const value = await scrapeApi.inspect(owner.movieId, candidate.provider, candidate.id, controller.signal); if (epoch === generation && current.value === owner) { inspect.value = value; overwrite.value = value.overwrite; } }
    catch (cause) { if (epoch === generation) error.value = cause instanceof Error ? cause.message : '读取失败'; }
    finally { if (epoch === generation) loading.value = false; }
  }
  async function open(movieId: number, confirmed: () => void | Promise<unknown>) {
    if (writing.value) return;
    close(); focus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const owner = { movieId, confirmed }; current.value = owner; const epoch = ++generation; controller = new AbortController(); loading.value = true;
    try {
      const value = await scrapeApi.preview(movieId, controller.signal);
      if (epoch !== generation || current.value !== owner) return;
      preview.value = value; loading.value = false;
      const candidate = value.candidates[value.recommended < 0 ? 0 : value.recommended]; if (candidate) await select(candidate);
    } catch (cause) { if (epoch === generation) { error.value = cause instanceof Error ? cause.message : '搜索失败'; loading.value = false; } }
  }
  async function confirm() {
    if (!current.value || !inspect.value || writing.value || loading.value) return;
    const owner = current.value, picked = inspect.value, force = overwrite.value; writing.value = true;
    try {
      const result = await scrapeApi.confirm(owner.movieId, picked.provider, picked.id, force);
      toasts.show(`已写入「${result.title}」（图片 ${result.images} 张）`);
      if (current.value === owner) close();
      await owner.confirmed();
    } catch (cause) { toasts.show(cause instanceof Error ? cause.message : '写入失败', 'error'); }
    finally { writing.value = false; }
  }
  onScopeDispose(close); return { current, preview, inspect, loading, writing, error, overwrite, open, select, confirm, close };
});
