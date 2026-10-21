import { onScopeDispose, ref, shallowRef } from 'vue';
import { defineStore } from 'pinia';
import { catalogApi } from '../api/catalog';
import { apiClient, type ApiWriteEvent } from '../api/client';
import { isRecord, type MovieDto, type WallDto, type WallUserData } from '../api/contracts';

export const WALL_PAGE_SIZE = 100;
export const useMediaWallStore = defineStore('media-wall', () => {
  const items = shallowRef<MovieDto[]>([]);
  const imageTags = shallowRef<WallDto['image_tags']>({});
  const userdata = shallowRef<NonNullable<WallDto['userdata']>>({});
  const total = ref(0);
  const loading = ref(false);
  const refreshing = ref(false);
  const refreshError = ref('');
  const done = ref(false);
  const error = ref('');
  const dirty = ref(false);
  const loadedAt = ref(0);
  const queryKey = ref('');
  let generation = 0;
  let randomSeed = 0;
  let controller: AbortController | undefined;
  let updatesDuringLoad: Record<string, Partial<WallUserData>> | undefined;

  function onWrite({ path, method, json }: ApiWriteEvent) {
    const mutation = /^\/Users\/[^/]+\/(FavoriteItems|PlayedItems)\/([^/?]+)$/u.exec(path);
    let id: string | undefined, patch: Partial<WallUserData> | undefined;
    if (mutation && (method === 'POST' || method === 'DELETE')) {
      id = mutation[2];
      patch = mutation[1] === 'FavoriteItems' ? { favorite: method === 'POST' } : { played: method === 'POST' };
      if (/^[1-9]\d*$/u.test(id!) && mutation[1] === 'FavoriteItems' && new URLSearchParams(queryKey.value).has('favorite')) dirty.value = true;
    } else if (/^\/Sessions\/Playing(?:\/(Progress|Stopped))?$/u.test(path) && method === 'POST' && isRecord(json)
      && typeof json.ItemId === 'string' && Number.isSafeInteger(json.PositionTicks) && Number(json.PositionTicks) >= 0) {
      id = json.ItemId; patch = { position_ticks: Number(json.PositionTicks) };
    } else {
      dirty.value = true;
      return;
    }
    // Non-numeric identifiers can alias a movie; retain full invalidation.
    if (!id || !/^[1-9]\d*$/u.test(id)) { dirty.value = true; return; }
    if (updatesDuringLoad) updatesDuringLoad[id] = { ...updatesDuringLoad[id], ...patch };
    if (items.value.some(item => String(item.id) === id)) {
      userdata.value = { ...userdata.value, [id]: { ...(userdata.value[id] ?? { position_ticks: 0, played: false, favorite: false }), ...patch } };
    }
  }
  onScopeDispose(apiClient.onWriteSuccess(onWrite));

  function cancel() {
    if (refreshing.value) dirty.value = true;
    generation += 1;
    controller?.abort();
    controller = undefined;
    loading.value = false;
    refreshing.value = false;
    updatesDuringLoad = undefined;
  }
  function clear() {
    cancel();
    items.value = []; imageTags.value = {}; userdata.value = {}; total.value = 0;
    done.value = false; error.value = ''; loadedAt.value = 0; dirty.value = false; queryKey.value = '';
    refreshError.value = '';
    randomSeed = 0;
  }
  function prepare(query: URLSearchParams, restore: boolean): boolean {
    cancel();
    const key = query.toString();
    if (restore && key === queryKey.value && loadedAt.value > 0) return true;
    const previousSeed = restore && key === queryKey.value ? randomSeed : 0;
    clear();
    queryKey.value = key;
    if (query.get('sort') === 'random') randomSeed = previousSeed || (crypto.getRandomValues(new Uint32Array(1))[0]! & 0x7fffffff) || 1;
    return false;
  }
  function needsRefresh() {
    return dirty.value || Boolean(error.value || refreshError.value) || Date.now() - loadedAt.value >= 60_000;
  }
  function pageQuery(offset: number) {
    const query = new URLSearchParams(queryKey.value);
    const sort = query.get('sort') || 'datecreated';
    query.set('sort', sort);
    if (sort === 'random') query.set('seed', String(randomSeed));
    else query.delete('seed');
    if (!query.has('order')) query.set('order', sort === 'title' ? 'asc' : 'desc');
    query.set('limit', String(WALL_PAGE_SIZE)); query.set('offset', String(offset));
    return query;
  }
  async function fetchItems(signal: AbortSignal, replace: boolean, retry = false): Promise<boolean> {
    if (signal.aborted || loading.value || (!replace && (done.value || refreshError.value || (error.value && !retry)))) return false;
    error.value = '';
    if (replace) { refreshError.value = ''; dirty.value = false; }
    const currentGeneration = generation;
    const current = new AbortController();
    controller = current;
    const abort = () => current.abort();
    signal.addEventListener('abort', abort, { once: true });
    loading.value = true;
    refreshing.value = replace;
    const patches: Record<string, Partial<WallUserData>> = {};
    updatesDuringLoad = patches;
    const valid = () => generation === currentGeneration && !signal.aborted && !current.signal.aborted;
    let completed = false;
    try {
      const count = Math.max(WALL_PAGE_SIZE, items.value.length);
      let nextItems = replace ? [] : items.value;
      let nextTags = replace ? {} : imageTags.value;
      let nextUserdata = replace ? {} : userdata.value;
      let nextTotal = total.value, nextDone = false;
      // Stage all replacement pages so the visible count and grid never collapse.
      do {
        const data = await catalogApi.items(pageQuery(nextItems.length), current.signal);
        if (!valid()) return false;
        nextItems = [...nextItems, ...data.items];
        nextTags = { ...nextTags, ...data.image_tags };
        nextUserdata = { ...nextUserdata, ...data.userdata };
        nextTotal = data.total;
        nextDone = data.items.length === 0 || nextItems.length >= data.total;
      } while (replace && nextItems.length < count && !nextDone);
      for (const item of nextItems) {
        const id = String(item.id);
        if (patches[id]) nextUserdata[id] = { ...(nextUserdata[id] ?? { position_ticks: 0, played: false, favorite: false }), ...patches[id] };
      }
      items.value = nextItems; imageTags.value = nextTags; userdata.value = nextUserdata; total.value = nextTotal;
      loadedAt.value = Date.now();
      done.value = nextDone;
      completed = true;
      return true;
    } catch (cause) {
      if (valid()) (replace ? refreshError : error).value = cause instanceof Error ? cause.message : '加载失败';
      return false;
    } finally {
      signal.removeEventListener('abort', abort);
      if (generation === currentGeneration && controller === current) {
        if (replace && !completed) dirty.value = true;
        loading.value = false; refreshing.value = false; updatesDuringLoad = undefined; controller = undefined;
      }
    }
  }
  function loadMore(signal: AbortSignal, retry = false) { return fetchItems(signal, false, retry); }
  function refresh(signal: AbortSignal) { return fetchItems(signal, true); }
  onScopeDispose(cancel);
  return { items, imageTags, userdata, total, loading, refreshing, refreshError, done, error, dirty, loadedAt, queryKey, cancel, clear, prepare, needsRefresh, loadMore, refresh };
});
