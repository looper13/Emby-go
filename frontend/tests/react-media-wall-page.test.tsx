import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useEffect } from 'react'
import { HashRouter, MemoryRouter, Route, Routes, useLocation, useNavigationType } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { catalogApi } from '../src/api/catalog'
import type { MovieDto } from '../src/api/contracts'
import { AppProviders } from '../src/react/AppProviders'
import { useReactAuthStore } from '../src/react/auth-store'
import { MediaWallPage } from '../src/react/media/MediaWallPage'
import { useReactMediaWallStore } from '../src/react/media-wall-store'
import { useViewHistory, ViewHistoryProvider } from '../src/react/view-history'
import authFixture from './fixtures/json/auth-login.json'
import detailFixture from './fixtures/json/item-detail.json'

function NavigationProbe() {
  const location = useLocation()
  const type = useNavigationType()
  return <output data-testid="wall-location">{`${location.pathname}${location.search}|${type}`}</output>
}

function DetailReturnPage() {
  const history = useViewHistory()
  return <button type="button" onClick={() => history.backFromItem('/items')}>返回媒体墙</button>
}

function Harness({ initialEntry }: { initialEntry: string }) {
  const wallCount = () => useReactMediaWallStore.getState().items.length
  return <AppProviders>
    <MemoryRouter initialEntries={[initialEntry]}>
      <ViewHistoryProvider wallCount={wallCount}>
        <NavigationProbe />
        <Routes>
          <Route path="/items" element={<MediaWallPage />} />
          <Route path="/item/:id" element={<DetailReturnPage />} />
        </Routes>
      </ViewHistoryProvider>
    </MemoryRouter>
  </AppProviders>
}

function BrowserHarness() {
  const wallCount = () => useReactMediaWallStore.getState().items.length
  return <AppProviders><HashRouter><ViewHistoryProvider wallCount={wallCount}>
    <NavigationProbe />
    <Routes>
      <Route path="/items" element={<MediaWallPage />} />
      <Route path="/item/:id" element={<DetailReturnPage />} />
    </Routes>
  </ViewHistoryProvider></HashRouter></AppProviders>
}

const movie = detailFixture.movie as MovieDto
const initialScrollYDescriptor = Object.getOwnPropertyDescriptor(window, 'scrollY')

function reset() {
  useReactMediaWallStore.getState().clear()
  useReactAuthStore.setState({ token: null, user: null, initialized: null, phase: 'anonymous', error: '', submitting: false, expiration: 0 })
}

afterEach(() => {
  reset()
  if (initialScrollYDescriptor) Object.defineProperty(window, 'scrollY', initialScrollYDescriptor)
  else Reflect.deleteProperty(window, 'scrollY')
  vi.restoreAllMocks()
})

describe('React media wall page', () => {
  it('loads the library and carries filter pushes plus debounced search replacement into the URL', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(catalogApi, 'libraries').mockResolvedValue({ items: [{ Id: 1, Name: '影视库', Path: '/media', Type: 'mixed' }], total: 1 })
    const items = vi.spyOn(catalogApi, 'items').mockResolvedValue({ items: [movie], total: 1, image_tags: {} })
    render(<Harness initialEntry="/items?status=success" />)

    expect(await screen.findByRole('region', { name: '影视库' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /电影/ })).toHaveAttribute('aria-selected', 'false')
    await user.click(screen.getByRole('tab', { name: /电影/ }))

    await waitFor(() => expect(screen.getByTestId('wall-location')).toHaveTextContent('/items?status=success&media_type=Movie|PUSH'))
    await user.type(screen.getByRole('textbox', { name: '搜索标题 / 番号 / 原名' }), 'ABF-005')
    await waitFor(() => expect(screen.getByTestId('wall-location')).toHaveTextContent('/items?status=success&media_type=Movie&search=ABF-005|REPLACE'), { timeout: 2000 })
    await waitFor(() => expect(items.mock.calls.at(-1)?.[0].get('search')).toBe('ABF-005'))
    expect(items.mock.calls.at(-1)?.[0].get('media_type')).toBe('Movie')
    expect(screen.getByRole('button', { name: '清除入库：已入库' })).toBeInTheDocument()
  })

  it('preserves query context while changing sort order and resets the wall only when query changes', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(catalogApi, 'libraries').mockResolvedValue({ items: [], total: 0 })
    const items = vi.spyOn(catalogApi, 'items').mockResolvedValue({ items: [movie], total: 1, image_tags: {} })
    render(<Harness initialEntry="/items?tag=深夜系列&sort=title&order=asc" />)

    await screen.findByRole('region', { name: '影视库' })
    await user.selectOptions(screen.getByRole('combobox', { name: '排序方向' }), 'desc')
    await waitFor(() => expect(screen.getByTestId('wall-location')).toHaveTextContent('/items?tag=%E6%B7%B1%E5%A4%9C%E7%B3%BB%E5%88%97&sort=title&order=desc|PUSH'))
    expect(items.mock.calls.at(-1)?.[0].get('tag')).toBe('深夜系列')
    expect(items.mock.calls.at(-1)?.[0].get('order')).toBe('desc')
  })

  it('returns from detail with 300 loaded cards, the original focus and scroll, without refetching pages', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(catalogApi, 'libraries').mockResolvedValue({ items: [], total: 0 })
    const items = vi.spyOn(catalogApi, 'items').mockImplementation(async (query) => {
      const offset = Number(query.get('offset'))
      const count = offset === 300 ? 50 : 100
      return { items: Array.from({ length: count }, (_, index) => ({ ...movie, id: offset + index + 1 })), total: 350, image_tags: {} }
    })
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => ({
      x: 0, y: 10000, top: 10000, right: 0, bottom: 10000, left: 0, width: 0, height: 0, toJSON: () => ({}),
    }))
    Object.defineProperty(window, 'scrollY', { configurable: true, value: 1800 })
    const scroll = vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
    window.history.replaceState({}, '', '/admin#/items?status=success')
    render(<BrowserHarness />)

    await screen.findByRole('region', { name: '影视库' })
    await waitFor(() => expect(items).toHaveBeenCalledTimes(1))
    await user.click(await screen.findByRole('button', { name: '加载更多' }))
    await waitFor(() => expect(items).toHaveBeenCalledTimes(2))
    await user.click(await screen.findByRole('button', { name: '加载更多' }))
    await waitFor(() => expect(items).toHaveBeenCalledTimes(3))
    expect(document.querySelectorAll('.wall-card')).toHaveLength(300)

    const visitedCard = document.querySelector<HTMLElement>('.wall-card[data-play="250"]')
    expect(visitedCard).not.toBeNull()
    await user.click(visitedCard!)
    expect(screen.getByRole('button', { name: '返回媒体墙' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '返回媒体墙' }))

    await waitFor(() => expect(document.querySelectorAll('.wall-card')).toHaveLength(300))
    const originalCard = document.querySelector<HTMLElement>('.wall-card[data-play="250"]')
    await waitFor(() => expect(document.activeElement).toBe(originalCard))
    await waitFor(() => expect(scroll).toHaveBeenCalledWith(0, 1800))
    expect(items).toHaveBeenCalledTimes(3)
    expect(screen.getByTestId('wall-location')).toHaveTextContent('/items?status=success|POP')
  }, 15000)
})
