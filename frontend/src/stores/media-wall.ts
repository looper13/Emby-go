import { onScopeDispose, ref, shallowRef } from 'vue';
import { defineStore } from 'pinia';
import { catalogApi } from '../api/catalog';
import { apiClient } from '../api/client';
import type { MovieDto, WallDto } from '../api/contracts';

export const WALL_PAGE_SIZE = 100;
export const useMediaWallStore = defineStore('media-wall', () => {
  const items = shallowRef<MovieDto[]>([]);
  const imageTags = shallowRef<WallDto['image_tags']>({});
  const userdata = shallowRef<NonNullable<WallDto['userdata']>>({});
  const total = ref(0);
  const loading = ref(false);
  const done = ref(false);
  const error = ref('');
  const dirty = ref(false);
  const loadedAt = ref(0);
  const queryKey = ref('');
  onScopeDispose(apiClient.onWriteSuccess(() => { dirty.value = true; }));
  let generation = 0;
  let controller: AbortController | undefined;

  function cancel() {
    generation += 1;
    controller?.abort();
    controller = undefined;
    loading.value = false;
  }
  function clear() {
    cancel();
    items.value = []; imageTags.value = {}; userdata.value = {}; total.value = 0;
    done.value = false; error.value = ''; loadedAt.value = 0; dirty.value = false; queryKey.value = '';
  }
  function prepare(query: URLSearchParams, restore: boolean): boolean {
    cancel();
    const key = query.toString();
    if (restore && !dirty.value && !error.value && key === queryKey.value
      && loadedAt.value > 0 && Date.now() - loadedAt.value < 60_000) return true;
    clear();
    queryKey.value = key;
    return false;
  }
  async function loadMore(signal: AbortSignal, retry = false): Promise<boolean> {
    if (signal.aborted || loading.value || done.value || (error.value && !retry)) return false;
    error.value = '';
    const currentGeneration = generation;
    const current = new AbortController();
    controller = current;
    const abort = () => current.abort();
    signal.addEventListener('abort', abort, { once: true });
    loading.value = true;
    const valid = () => generation === currentGeneration && !signal.aborted && !current.signal.aborted;
    try {
      const query = new URLSearchParams(queryKey.value);
      const sort = query.get('sort') || 'datecreated';
      query.set('sort', sort);
      if (!query.has('order')) query.set('order', sort === 'title' ? 'asc' : 'desc');
      query.set('limit', String(WALL_PAGE_SIZE)); query.set('offset', String(items.value.length));
      const data = await catalogApi.items(query, current.signal);
      if (!valid()) return false;
      items.value = [...items.value, ...data.items];
      imageTags.value = { ...imageTags.value, ...data.image_tags };
      userdata.value = { ...userdata.value, ...data.userdata };
      total.value = data.total;
      loadedAt.value = Date.now();
      done.value = data.items.length === 0 || items.value.length >= data.total;
      return true;
    } catch (cause) {
      if (valid()) error.value = cause instanceof Error ? cause.message : '加载失败';
      return false;
    } finally {
      signal.removeEventListener('abort', abort);
      if (generation === currentGeneration && controller === current) loading.value = false;
    }
  }
  onScopeDispose(cancel);
  return { items, imageTags, userdata, total, loading, done, error, dirty, loadedAt, queryKey, cancel, clear, prepare, loadMore };
});
