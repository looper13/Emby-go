import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { adminApi } from '../src/api/admin'
import { catalogApi } from '../src/api/catalog'
import type { DetailDto, LibraryDto, MovieDto } from '../src/api/contracts'
import { mediaApi } from '../src/api/media'
import { javdbCommunityApi } from '../src/api/javdb-community'
import { r18Api } from '../src/api/r18'
import { AppProviders } from '../src/react/AppProviders'
import { useReactAuthStore } from '../src/react/auth-store'
import { useReactMediaWallStore } from '../src/react/media-wall-store'
import { useReactPlayerStore } from '../src/react/player-store'
import { ItemDetailPage } from '../src/react/media/ItemDetailPage'
import { MediaWallPage } from '../src/react/media/MediaWallPage'
import { ViewHistoryProvider } from '../src/react/view-history'
import { LibrariesPage } from '../src/react/pages/library-settings-pages'
import authFixture from './fixtures/json/auth-login.json'
import detailFixture from './fixtures/json/item-detail.json'

const detail = detailFixture as unknown as DetailDto
const movie = detail.movie as MovieDto
const library: LibraryDto = {
  Id: 7, Name: 'Films', Path: '/media', Paths: ['/media'], Type: 'mixed',
  SourceType: 'nfo_directory', ProxyEnabled: false, ResolveRedirects: false, Revision: 3, ProxyHeaders: {},
}

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{`${location.pathname}${location.search}`}</output>
}

function Harness({ initialEntry }: { initialEntry: string }) {
  return <AppProviders><MemoryRouter initialEntries={[initialEntry]}><ViewHistoryProvider>
    <LocationProbe />
    <Routes>
      <Route path="/items" element={<MediaWallPage />} />
      <Route path="/libraries" element={<LibrariesPage />} />
      <Route path="/item/:id" element={<ItemDetailPage />} />
    </Routes>
  </ViewHistoryProvider></MemoryRouter></AppProviders>
}

function signIn() {
  useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
}

function mockDetailServices() {
  signIn()
  const titles: Record<number, string> = { 101: '第一部影片', 102: '第二部影片', 103: '第三部影片' }
  vi.spyOn(catalogApi, 'detail').mockImplementation(async id => ({
    ...detail,
    movie: { ...movie, id: Number(id), Title: titles[Number(id)] || `影片 ${id}` },
  }))
  vi.spyOn(mediaApi, 'similar').mockResolvedValue([])
  vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [], total: 0 })
  vi.spyOn(javdbCommunityApi, 'match').mockResolvedValue({
    status: 'matched', number: 'ABF-005', library_id: 2, library_name: 'AVdb 资料库',
    video: { id: 'abf-005', number: 'ABF-005', title: 'AVdb 匹配影片' },
  })
  vi.spyOn(javdbCommunityApi, 'lists').mockResolvedValue({
    items: [], page: 1, page_size: 15, has_more: false, total: 0,
    number: 'ABF-005', video_id: 'abf-005', library_id: 2,
  })
}

function reset() {
  useReactMediaWallStore.getState().clear()
  useReactPlayerStore.setState({ current: null })
  useReactAuthStore.setState({ token: null, user: null, initialized: null, phase: 'anonymous', error: '', submitting: false, expiration: 0 })
}

afterEach(() => {
  reset()
  vi.restoreAllMocks()
})

describe('React random playback and detail navigation', () => {
  it('plays a random item from the active media wall filters', async () => {
    const user = userEvent.setup()
    signIn()
    vi.spyOn(catalogApi, 'libraries').mockResolvedValue({ items: [library], total: 1 })
    const items = vi.spyOn(catalogApi, 'items').mockImplementation(async query => ({
      items: [query.get('sort') === 'random' ? { ...movie, id: 999, Title: '随机选中影片' } : movie],
      total: query.get('sort') === 'random' ? 20 : 10,
      image_tags: {},
    }))
    render(<Harness initialEntry="/items?library_id=7&search=ABF-005" />)

    expect(await screen.findByRole('region', { name: '影视库' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '随机播放' }))

    await waitFor(() => expect(useReactPlayerStore.getState().current).toMatchObject({
      itemId: '999', title: '随机选中影片', url: '/Videos/999/stream',
    }))
    const query = items.mock.calls.filter(([value]) => value.get('sort') === 'random').at(-1)?.[0]
    expect(query?.get('library_id')).toBe('7')
    expect(query?.get('search')).toBe('ABF-005')
    expect(query?.get('seed')).toMatch(/^\d+$/)
    expect(query?.get('limit')).toBe('1')
    expect(query?.get('offset')).toBe('0')
  })

  it('plays a random item from the selected library and constrains mixed libraries to movies', async () => {
    const user = userEvent.setup()
    signIn()
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [library], total: 1 })
    const items = vi.spyOn(catalogApi, 'items').mockResolvedValue({
      items: [{ ...movie, id: 888, Title: '媒体库随机影片' }], total: 30, image_tags: {},
    })
    render(<Harness initialEntry="/libraries" />)

    expect(await screen.findByRole('heading', { name: '媒体库' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '随机播放' }))

    await waitFor(() => expect(useReactPlayerStore.getState().current).toMatchObject({ itemId: '888', title: '媒体库随机影片' }))
    const query = items.mock.calls[0]![0]
    expect(query.get('library_id')).toBe('7')
    expect(query.get('sort')).toBe('random')
    expect(query.get('media_type')).toBe('Movie')
    expect(query.get('seed')).toMatch(/^\d+$/)
    expect(query.get('limit')).toBe('1')
    expect(query.get('offset')).toBe('0')
  })

  it('navigates through loaded wall items by buttons and keyboard while retaining the query', async () => {
    const user = userEvent.setup()
    mockDetailServices()
    const wallItems = [101, 102, 103].map((id, index) => ({
      ...movie, id, Title: ['第一部影片', '第二部影片', '第三部影片'][index]!,
    }))
    useReactMediaWallStore.setState(state => ({ ...state, items: wallItems, done: true, queryKey: 'library_id=7&search=ABF-005' }))
    render(<Harness initialEntry="/item/101?library_id=7&search=ABF-005" />)

    expect(await screen.findByRole('heading', { name: '第一部影片' })).toBeInTheDocument()
    const previous = screen.getByRole('button', { name: '前一部' })
    const next = screen.getByRole('button', { name: '后一部' })
    expect(previous).toBeDisabled()
    expect(next).toHaveAttribute('title', '后一部：第二部影片')

    await user.click(next)
    expect(await screen.findByRole('heading', { name: '第二部影片' })).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/item/102?library_id=7&search=ABF-005')
    expect(screen.getByRole('button', { name: '前一部' })).toHaveAttribute('title', '前一部：第一部影片')
    expect(screen.getByRole('button', { name: '后一部' })).toHaveAttribute('title', '后一部：第三部影片')

    await user.keyboard('{ArrowRight}')
    expect(await screen.findByRole('heading', { name: '第三部影片' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '后一部' })).toBeDisabled()
    await user.keyboard('{ArrowLeft}')
    expect(await screen.findByRole('heading', { name: '第二部影片' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '前一部' }))
    expect(await screen.findByRole('heading', { name: '第一部影片' })).toBeInTheDocument()
  })
})
