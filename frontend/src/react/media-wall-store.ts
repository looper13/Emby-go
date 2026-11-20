import { create } from 'zustand'
import { catalogApi } from '../api/catalog'
import { apiClient, type ApiWriteEvent } from '../api/client'
import { isRecord, type MovieDto, type WallDto, type WallUserData } from '../api/contracts'
import { useReactAuthStore } from './auth-store'

export const WALL_PAGE_SIZE = 100

interface MediaWallState {
  items: MovieDto[]
  imageTags: WallDto['image_tags']
  userdata: NonNullable<WallDto['userdata']>
  total: number
  loading: boolean
  refreshing: boolean
  refreshError: string
  done: boolean
  error: string
  dirty: boolean
  loadedAt: number
  queryKey: string
  cancel: () => void
  clear: () => void
  markDirty: () => void
  prepare: (query: URLSearchParams, restore: boolean) => boolean
  needsRefresh: () => boolean
  loadMore: (signal: AbortSignal, retry?: boolean) => Promise<boolean>
  refresh: (signal: AbortSignal) => Promise<boolean>
  recordWrite: (event: ApiWriteEvent) => void
}

let generation = 0
let randomSeed = 0
let controller: AbortController | undefined
let updatesDuringLoad: Record<string, Partial<WallUserData>> | undefined
let runtimeRefs = 0
let detachWriteListener: (() => void) | undefined
let detachAuthListener: (() => void) | undefined

function emptyUserData(): WallUserData {
  return { position_ticks: 0, played: false, favorite: false }
}

export const useReactMediaWallStore = create<MediaWallState>()((set, get) => {
  function pageQuery(offset: number): URLSearchParams {
    const query = new URLSearchParams(get().queryKey)
    const sort = query.get('sort') || 'datecreated'
    query.set('sort', sort)
    if (sort === 'random') query.set('seed', String(randomSeed))
    else query.delete('seed')
    if (!query.has('order')) query.set('order', sort === 'title' ? 'asc' : 'desc')
    query.set('limit', String(WALL_PAGE_SIZE))
    query.set('offset', String(offset))
    return query
  }

  async function fetchItems(signal: AbortSignal, replace: boolean, retry = false): Promise<boolean> {
    const state = get()
    if (signal.aborted || state.loading || (!replace && (state.done || state.refreshError || (state.error && !retry)))) return false
    set({ error: '' })
    if (replace) set({ refreshError: '', dirty: false })

    const currentGeneration = generation
    const current = new AbortController()
    controller = current
    const abort = () => current.abort()
    signal.addEventListener('abort', abort, { once: true })
    if (signal.aborted) abort()
    set({ loading: true, refreshing: replace })
    const patches: Record<string, Partial<WallUserData>> = {}
    updatesDuringLoad = patches
    const valid = () => generation === currentGeneration && !signal.aborted && !current.signal.aborted
    let completed = false

    try {
      const count = Math.max(WALL_PAGE_SIZE, get().items.length)
      let nextItems = replace ? [] : get().items
      let nextTags = replace ? {} : get().imageTags
      let nextUserdata = replace ? {} : get().userdata
      let nextTotal = get().total
      let nextDone = false
      do {
        const data = await catalogApi.items(pageQuery(nextItems.length), current.signal)
        if (!valid()) return false
        nextItems = [...nextItems, ...data.items]
        nextTags = { ...nextTags, ...data.image_tags }
        nextUserdata = { ...nextUserdata, ...data.userdata }
        nextTotal = data.total
        nextDone = data.items.length === 0 || nextItems.length >= data.total
      } while (replace && nextItems.length < count && !nextDone)

      for (const item of nextItems) {
        const id = String(item.id)
        if (patches[id]) nextUserdata[id] = { ...(nextUserdata[id] ?? emptyUserData()), ...patches[id] }
      }
      set({
        items: nextItems,
        imageTags: nextTags,
        userdata: nextUserdata,
        total: nextTotal,
        loadedAt: Date.now(),
        done: nextDone,
      })
      completed = true
      return true
    } catch (cause) {
      if (valid()) set(replace
        ? { refreshError: cause instanceof Error ? cause.message : '加载失败' }
        : { error: cause instanceof Error ? cause.message : '加载失败' })
      return false
    } finally {
      signal.removeEventListener('abort', abort)
      if (generation === currentGeneration && controller === current) {
        if (replace && !completed) set({ dirty: true })
        set({ loading: false, refreshing: false })
        updatesDuringLoad = undefined
        controller = undefined
      }
    }
  }

  return {
    items: [],
    imageTags: {},
    userdata: {},
    total: 0,
    loading: false,
    refreshing: false,
    refreshError: '',
    done: false,
    error: '',
    dirty: false,
    loadedAt: 0,
    queryKey: '',
    cancel() {
      if (get().refreshing) set({ dirty: true })
      generation += 1
      controller?.abort()
      controller = undefined
      updatesDuringLoad = undefined
      set({ loading: false, refreshing: false })
    },
    clear() {
      get().cancel()
      set({
        items: [], imageTags: {}, userdata: {}, total: 0, done: false, error: '',
        loadedAt: 0, dirty: false, queryKey: '', refreshError: '',
      })
      randomSeed = 0
    },
    markDirty() { set({ dirty: true }) },
    prepare(query, restore) {
      get().cancel()
      const key = query.toString()
      const state = get()
      if (restore && key === state.queryKey && state.loadedAt > 0) return true
      const previousSeed = restore && key === state.queryKey ? randomSeed : 0
      get().clear()
      set({ queryKey: key })
      if (query.get('sort') === 'random') {
        randomSeed = previousSeed || (crypto.getRandomValues(new Uint32Array(1))[0]! & 0x7fffffff) || 1
      }
      return false
    },
    needsRefresh() {
      const state = get()
      return state.dirty || Boolean(state.error || state.refreshError) || Date.now() - state.loadedAt >= 60_000
    },
    loadMore(signal, retry = false) { return fetchItems(signal, false, retry) },
    refresh(signal) { return fetchItems(signal, true) },
    recordWrite({ path, method, json }) {
      const mutation = /^\/Users\/[^/]+\/(FavoriteItems|PlayedItems)\/([^/?]+)$/u.exec(path)
      let id: string | undefined
      let patch: Partial<WallUserData> | undefined
      if (mutation && (method === 'POST' || method === 'DELETE')) {
        id = mutation[2]
        patch = mutation[1] === 'FavoriteItems' ? { favorite: method === 'POST' } : { played: method === 'POST' }
        if (/^[1-9]\d*$/u.test(id!) && mutation[1] === 'FavoriteItems'
          && new URLSearchParams(get().queryKey).has('favorite')) set({ dirty: true })
      } else if (/^\/Sessions\/Playing(?:\/(Progress|Stopped))?$/u.test(path) && method === 'POST' && isRecord(json)
        && typeof json.ItemId === 'string' && Number.isSafeInteger(json.PositionTicks) && Number(json.PositionTicks) >= 0) {
        id = json.ItemId
        patch = { position_ticks: Number(json.PositionTicks) }
      } else {
        set({ dirty: true })
        return
      }

      if (!id || !/^[1-9]\d*$/u.test(id)) {
        set({ dirty: true })
        return
      }
      if (updatesDuringLoad) updatesDuringLoad[id] = { ...updatesDuringLoad[id], ...patch }
      if (get().items.some((item) => String(item.id) === id)) {
        set((state) => ({
          userdata: {
            ...state.userdata,
            [id]: { ...(state.userdata[id] ?? emptyUserData()), ...patch },
          },
        }))
      }
    },
  }
})

export function activateMediaWallRuntime(): () => void {
  runtimeRefs += 1
  if (runtimeRefs === 1) {
    detachWriteListener = apiClient.onWriteSuccess((event) => useReactMediaWallStore.getState().recordWrite(event))
    detachAuthListener = useReactAuthStore.subscribe((state, previous) => {
      if (state.token !== previous.token) useReactMediaWallStore.getState().clear()
    })
  }

  let released = false
  return () => {
    if (released) return
    released = true
    runtimeRefs -= 1
    if (runtimeRefs !== 0) return
    detachWriteListener?.()
    detachWriteListener = undefined
    detachAuthListener?.()
    detachAuthListener = undefined
    useReactMediaWallStore.getState().clear()
  }
}
