import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '../src/api/client'
import { catalogApi } from '../src/api/catalog'
import type { MovieDto } from '../src/api/contracts'
import { useReactAuthStore } from '../src/react/auth-store'
import { activateMediaWallRuntime, useReactMediaWallStore, WALL_PAGE_SIZE } from '../src/react/media-wall-store'
import detailFixture from './fixtures/json/item-detail.json'

function batch(offset: number, count = WALL_PAGE_SIZE, total = 350) {
  return {
    total,
    image_tags: {},
    items: Array.from({ length: count }, (_, index) => ({ ...detailFixture.movie, id: offset + index + 1 } as MovieDto)),
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

let releaseWall: (() => void) | undefined
let detachAuth: (() => void) | undefined

beforeEach(() => {
  releaseWall = activateMediaWallRuntime()
  detachAuth = apiClient.setAuthHandlers({ getToken: () => 'test-token', onUnauthorized: () => {} })
  useReactAuthStore.setState({ token: 'test-token', phase: 'authenticated' })
})

afterEach(() => {
  releaseWall?.()
  detachAuth?.()
  releaseWall = undefined
  detachAuth = undefined
  useReactMediaWallStore.getState().clear()
  useReactAuthStore.setState({ token: null, user: null, phase: 'anonymous', error: '', expiration: 0 })
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('React media wall store', () => {
  it('restores loaded pages and refreshes the same snapshot length without collapsing it', async () => {
    const api = vi.spyOn(catalogApi, 'items').mockImplementation(async (query) => batch(Number(query.get('offset'))))
    const wall = useReactMediaWallStore.getState()
    const signal = new AbortController().signal
    const query = new URLSearchParams('status=success&sort=title')
    expect(wall.prepare(query, false)).toBe(false)
    for (let page = 0; page < 3; page += 1) await useReactMediaWallStore.getState().loadMore(signal)

    expect(useReactMediaWallStore.getState().items).toHaveLength(300)
    expect(useReactMediaWallStore.getState().prepare(query, true)).toBe(true)
    useReactMediaWallStore.getState().markDirty()
    expect(useReactMediaWallStore.getState().needsRefresh()).toBe(true)
    expect(await useReactMediaWallStore.getState().refresh(signal)).toBe(true)
    expect(api.mock.calls.map(([page]) => page.get('offset'))).toEqual(['0', '100', '200', '0', '100', '200'])
    expect(useReactMediaWallStore.getState().items).toHaveLength(300)
    expect(useReactMediaWallStore.getState().dirty).toBe(false)
  })

  it('keeps the previous snapshot on refresh failure and retries only on explicit refresh', async () => {
    const api = vi.spyOn(catalogApi, 'items').mockImplementation(async (query) => batch(Number(query.get('offset'))))
    const wall = useReactMediaWallStore.getState()
    const signal = new AbortController().signal
    wall.prepare(new URLSearchParams(), false)
    await wall.loadMore(signal)
    await wall.loadMore(signal)
    const previous = useReactMediaWallStore.getState().items
    api.mockResolvedValueOnce(batch(1000)).mockRejectedValueOnce(new Error('offline'))

    expect(await useReactMediaWallStore.getState().refresh(signal)).toBe(false)
    expect(useReactMediaWallStore.getState().items).toBe(previous)
    expect(useReactMediaWallStore.getState().refreshError).toBe('offline')
    expect(await useReactMediaWallStore.getState().loadMore(signal)).toBe(false)
    api.mockResolvedValueOnce(batch(0, 2, 2))
    expect(await useReactMediaWallStore.getState().refresh(signal)).toBe(true)
    expect(api.mock.lastCall?.[0].get('offset')).toBe('0')
    expect(useReactMediaWallStore.getState().items).toHaveLength(2)
    expect(useReactMediaWallStore.getState().done).toBe(true)
  })

  it('cancels stale requests, prevents duplicate loads and requires explicit retry after a page error', async () => {
    let reject!: (cause: Error) => void
    const api = vi.spyOn(catalogApi, 'items')
      .mockImplementationOnce(() => new Promise((_resolve, rejectRequest) => { reject = rejectRequest }))
      .mockResolvedValueOnce(batch(0))
    const signal = new AbortController().signal
    const wall = useReactMediaWallStore.getState()
    wall.prepare(new URLSearchParams('search=old'), false)
    const first = wall.loadMore(signal)
    expect(await useReactMediaWallStore.getState().loadMore(signal)).toBe(false)
    expect(api).toHaveBeenCalledTimes(1)
    reject(new Error('offline'))
    await first
    expect(await useReactMediaWallStore.getState().loadMore(signal)).toBe(false)
    expect(api).toHaveBeenCalledTimes(1)
    expect(await useReactMediaWallStore.getState().loadMore(signal, true)).toBe(true)
    expect(useReactMediaWallStore.getState().error).toBe('')
  })

  it('keeps random pagination and restored refreshes on the same seed', async () => {
    vi.spyOn(crypto, 'getRandomValues')
      .mockReturnValueOnce(new Uint32Array([12345]) as never)
      .mockReturnValueOnce(new Uint32Array([98765]) as never)
    const api = vi.spyOn(catalogApi, 'items').mockImplementation(async (query) => batch(Number(query.get('offset'))))
    const wall = useReactMediaWallStore.getState()
    const query = new URLSearchParams('sort=random&tag=keep')
    const signal = new AbortController().signal
    wall.prepare(query, false)
    await useReactMediaWallStore.getState().loadMore(signal)
    await useReactMediaWallStore.getState().loadMore(signal)
    expect(api.mock.calls.slice(0, 2).map(([page]) => page.get('seed'))).toEqual(['12345', '12345'])
    expect(api.mock.calls.slice(0, 2).map(([page]) => page.get('offset'))).toEqual(['0', '100'])
    expect(useReactMediaWallStore.getState().prepare(query, true)).toBe(true)
    useReactMediaWallStore.setState({ loadedAt: Date.now() - 60_000 })
    expect(await useReactMediaWallStore.getState().refresh(signal)).toBe(true)
    expect(api.mock.lastCall?.[0].get('seed')).toBe('12345')
    useReactMediaWallStore.getState().clear()
    useReactMediaWallStore.getState().prepare(query, true)
    await useReactMediaWallStore.getState().loadMore(signal)
    expect(api.mock.lastCall?.[0].get('seed')).toBe('98765')
  })

  it('patches playback and favorite writes in place and invalidates filtered or unknown mutations', async () => {
    vi.spyOn(catalogApi, 'items').mockResolvedValue({
      ...batch(0),
      userdata: { '1': { position_ticks: 0, played: false, favorite: false } },
    })
    const wall = useReactMediaWallStore.getState()
    const signal = new AbortController().signal
    wall.prepare(new URLSearchParams(), false)
    await useReactMediaWallStore.getState().loadMore(signal)
    const items = useReactMediaWallStore.getState().items
    vi.stubGlobal('fetch', vi.fn(async () => new Response(null, { status: 204 })))
    await apiClient.request('/Sessions/Playing/Progress', {
      auth: 'required', method: 'POST', json: { ItemId: '1', PositionTicks: 12300000 },
    })
    await apiClient.request('/Users/1/PlayedItems/1', { auth: 'required', method: 'POST' })
    await apiClient.request('/Users/1/FavoriteItems/1', { auth: 'required', method: 'POST' })
    expect(useReactMediaWallStore.getState().items).toBe(items)
    expect(useReactMediaWallStore.getState().userdata['1']).toEqual({
      position_ticks: 12300000, played: true, favorite: true,
    })
    expect(useReactMediaWallStore.getState().dirty).toBe(false)

    await apiClient.request('/Users/1/FavoriteItems/1', { auth: 'required', method: 'DELETE' })
    await apiClient.request('/Users/1/FavoriteItems/person%3Aexample', { auth: 'required', method: 'POST' })
    expect(useReactMediaWallStore.getState().dirty).toBe(true)
    useReactMediaWallStore.getState().prepare(new URLSearchParams('favorite=true'), false)
    await useReactMediaWallStore.getState().loadMore(signal)
    await apiClient.request('/Users/1/FavoriteItems/1', { auth: 'required', method: 'DELETE' })
    expect(useReactMediaWallStore.getState().userdata['1']?.favorite).toBe(false)
    expect(useReactMediaWallStore.getState().dirty).toBe(true)
  })

  it('merges writes made during replacement refresh and ignores late results after account changes', async () => {
    const api = vi.spyOn(catalogApi, 'items').mockImplementation(async (query) => batch(Number(query.get('offset'))))
    vi.stubGlobal('fetch', vi.fn(async () => new Response(null, { status: 204 })))
    const wall = useReactMediaWallStore.getState()
    const signal = new AbortController().signal
    wall.prepare(new URLSearchParams(), false)
    await wall.loadMore(signal)
    const pending = deferred<ReturnType<typeof batch>>()
    api.mockImplementationOnce(() => pending.promise)
    const refresh = useReactMediaWallStore.getState().refresh(signal)
    await vi.waitFor(() => expect(api).toHaveBeenCalledTimes(2))
    await apiClient.request('/Sessions/Playing/Stopped', {
      auth: 'required', method: 'POST', json: { ItemId: '1', PositionTicks: 999 },
    })
    useReactAuthStore.setState({ token: 'other-account' })
    pending.resolve(batch(1000))
    await refresh
    expect(useReactMediaWallStore.getState().items).toEqual([])
    expect(useReactMediaWallStore.getState().userdata).toEqual({})
    expect(useReactMediaWallStore.getState().loading).toBe(false)
  })
})
