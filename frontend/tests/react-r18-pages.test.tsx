import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, Outlet, RouterProvider, useLocation, useNavigate } from 'react-router-dom'
import { AppProviders } from '../src/react/AppProviders'
import { useReactAuthStore } from '../src/react/auth-store'
import { useReactPlayerStore } from '../src/react/player-store'
import { useReactPreferencesStore } from '../src/react/preferences-store'
import { ViewHistoryProvider } from '../src/react/view-history'
import { R18CatalogPage, R18DetailPage } from '../src/react/pages/r18-pages'
import { r18Api, type R18Video } from '../src/api/r18'
import { ApiError } from '../src/api/client'
import { cloud115Api, type OfflineState, type OfflineTask } from '../src/api/cloud115'
import { guangyaApi } from '../src/api/guangya'
import authFixture from './fixtures/json/auth-login.json'

const video: R18Video = {
  id: 'r18-abc', content_id: 'abc', service_code: 'avdb', number: 'ABP-123',
  title: 'Sample English Title', original_title: '', release_date: '2026-01-02',
  cover_url: '/api/r18/libraries/8/videos/r18-abc/images/cover', images: [],
  duration: 120, score: 8.2, magnets_count: 3, has_local: true, tags: [],
  has_preview: false, preview_url: '', rank: 0,
}
const localSource = { id: 'strm-5', item_id: '5', name: 'ABP-123.strm', library_name: '本地资料库', container: 'mp4' }
const resource = {
  id: 'resource-1', title: 'Sample magnet', number: 'ABP-123', date: '2026-01-02',
  magnet: `magnet:?xt=urn:btih:${'a'.repeat(40)}`, website: 'AVdb', detail_url: '',
  size_bytes: 0, size_mb: 0, tags: [], origins: ['online'] as ('online' | 'library')[],
}

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="route-location">{`${location.pathname}${location.search}`}</output>
}

function RouteControl() {
  const navigate = useNavigate()
  return <>
    <button type="button" onClick={() => navigate('/r18/8/item/r18-next')}>Open next fixture</button>
    <button type="button" onClick={() => navigate('/r18?library_id=9&filter=magnets')}>Switch library 9 in URL</button>
    <button type="button" onClick={() => navigate('/r18?library_id=9&mode=categories')}>Switch invalid library in categories</button>
  </>
}

function TestLayout() {
  return <ViewHistoryProvider><LocationProbe /><RouteControl /><Outlet /></ViewHistoryProvider>
}

function TestApp({ initialEntry = '/r18?library_id=8&filter=magnets' }: { initialEntry?: string }) {
  const router = createMemoryRouter([{
    element: <TestLayout />,
    children: [
      { path: '/r18', element: <R18CatalogPage /> },
      { path: '/r18/:libraryId/item/:id', element: <R18DetailPage /> },
    ],
  }], { initialEntries: [initialEntry] })
  return <AppProviders><RouterProvider router={router} /></AppProviders>
}

function reset() {
  useReactAuthStore.setState({ token: null, user: null, initialized: null, phase: 'anonymous', error: '', submitting: false, expiration: 0 })
}

afterEach(() => {
  reset()
  useReactPlayerStore.setState({ current: null })
  useReactPreferencesStore.setState({ hideImages: false })
  vi.restoreAllMocks()
  window.history.replaceState({}, '', '/admin#/login')
})

describe('React AVdb catalog and detail routes', () => {
  it('switches to TOP250 with a clean query and resets the page while keeping page size', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    const videos = vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [], page: 1, page_size: 40, has_more: false, has_previous: false, total: 0 })

    render(<TestApp initialEntry="/r18?library_id=8&page=7&page_size=40&sort=score&year=2024" />)
    await screen.findByRole('heading', { name: 'AVdb 资料库' })
    expect(screen.getByRole('navigation', { name: 'AVdb 浏览模式' })).toHaveTextContent('最新影片分类查询TOP250排行榜热播热门')
    await user.click(screen.getByRole('button', { name: 'TOP250' }))

    await waitFor(() => expect(videos).toHaveBeenCalledTimes(2))
    const query = videos.mock.calls.at(-1)![1]
    expect(query.get('mode')).toBe('top250')
    expect(query.get('page')).toBe('1')
    expect(query.get('page_size')).toBe('40')
    expect(query.get('type')).toBe('all')
    expect(query.get('filter')).toBe('all')
    expect(query.has('sort')).toBe(false)
    expect(query.has('year')).toBe(false)
    expect(screen.getByRole('button', { name: 'TOP250' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('keeps TOP250 type and year mutually exclusive and reports the page-size ceiling', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    const videos = vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [video], page: 7, page_size: 40, has_more: false, has_previous: true, total: 250 })

    render(<TestApp initialEntry="/r18?library_id=8&mode=top250&type=0&page=7&page_size=40" />)
    await screen.findByRole('heading', { name: 'AVdb 资料库' })
    const pageInput = screen.getByRole('spinbutton', { name: '跳转页码' })
    expect(pageInput).toHaveAttribute('max', '7')
    await user.selectOptions(screen.getByRole('combobox', { name: '每页' }), '50')
    await waitFor(() => expect(videos).toHaveBeenCalledTimes(2))
    expect(screen.getByRole('spinbutton', { name: '跳转页码' })).toHaveAttribute('max', '5')
    expect(videos.mock.calls.at(-1)?.[1].get('page')).toBe('1')

    await user.clear(screen.getByRole('spinbutton', { name: '跳转页码' }))
    await user.type(screen.getByRole('spinbutton', { name: '跳转页码' }), '6')
    await user.click(screen.getByRole('button', { name: '跳转' }))
    expect(screen.getByRole('alert')).toHaveTextContent('1 到 5')
    expect(videos).toHaveBeenCalledTimes(2)
  })

  it('keeps TOP250 year and type exclusive in either direction and sends watched preference', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    const videos = vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [], page: 1, page_size: 24, has_more: false, has_previous: false, total: 0 })

    render(<TestApp initialEntry="/r18?library_id=8&mode=top250&type=0" />)
    await screen.findByRole('heading', { name: 'AVdb 资料库' })
    const year = screen.getByRole('spinbutton', { name: '发行年份' })
    await user.type(year, '2024')
    await user.tab()
    await waitFor(() => expect(videos).toHaveBeenCalledTimes(2))
    expect(videos.mock.calls.at(-1)?.[1].get('type')).toBe('all')
    expect(videos.mock.calls.at(-1)?.[1].get('year')).toBe('2024')

    await user.selectOptions(screen.getByRole('combobox', { name: '影片类型' }), '3')
    await waitFor(() => expect(videos).toHaveBeenCalledTimes(3))
    expect(videos.mock.calls.at(-1)?.[1].get('type')).toBe('3')
    expect(videos.mock.calls.at(-1)?.[1].has('year')).toBe(false)
    await user.click(screen.getByRole('checkbox', { name: '排除 AVdb 账号已看' }))
    await waitFor(() => expect(videos).toHaveBeenCalledTimes(4))
    expect(videos.mock.calls.at(-1)?.[1].get('ignore_watched')).toBe('true')
  })

  it('replaces latest update sort with release sort when a year is selected', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    const videos = vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [], page: 1, page_size: 24, has_more: false, has_previous: false, total: 0 })

    render(<TestApp initialEntry="/r18?library_id=8&sort=update" />)
    await screen.findByRole('heading', { name: 'AVdb 资料库' })
    await user.type(screen.getByRole('spinbutton', { name: '发行年份' }), '2024')
    await user.tab()
    await waitFor(() => expect(videos).toHaveBeenCalledTimes(2))
    expect(videos.mock.calls.at(-1)?.[1].get('sort')).toBe('release')
    expect(videos.mock.calls.at(-1)?.[1].get('year')).toBe('2024')
    expect(screen.getByRole('combobox', { name: '排序方式' }).querySelector('option[value="update"]')).toBeDisabled()
  })

  it('changes chart period and page while preserving the upstream result order', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    const firstBySource = { ...video, id: 'rank-two', title: 'Second by AVdb', score: 2, rank: 2 }
    const secondBySource = { ...video, id: 'rank-one', number: 'ABP-124', title: 'First by AVdb', score: 9, rank: 1 }
    const videos = vi.spyOn(r18Api, 'videos')
      .mockResolvedValueOnce({ items: [], page: 4, page_size: 40, has_more: true, has_previous: true, total: null })
      .mockResolvedValueOnce({ items: [firstBySource, secondBySource], page: 1, page_size: 40, has_more: true, has_previous: false, total: null })
      .mockResolvedValueOnce({ items: [{ ...video, id: 'rank-next', title: 'Next AVdb page' }], page: 2, page_size: 40, has_more: false, has_previous: true, total: null })

    render(<TestApp initialEntry="/r18?library_id=8&mode=ranking&page=4&page_size=40" />)
    await screen.findByRole('heading', { name: 'AVdb 资料库' })
    await user.selectOptions(screen.getByRole('combobox', { name: '榜单周期' }), 'weekly')
    await waitFor(() => expect(videos).toHaveBeenCalledTimes(2))
    expect(videos.mock.calls[1]?.[1].get('mode')).toBe('ranking')
    expect(videos.mock.calls[1]?.[1].get('period')).toBe('weekly')
    expect(videos.mock.calls[1]?.[1].get('page')).toBe('1')
    const order = Array.from(document.querySelectorAll('.react-r18-card a')).map(card => card.getAttribute('aria-label'))
    expect(order).toEqual(['打开 ABP-123 Second by AVdb', '打开 ABP-124 First by AVdb'])

    await user.click(screen.getByRole('button', { name: '下一页' }))
    await waitFor(() => expect(videos).toHaveBeenCalledTimes(3))
    expect(videos.mock.calls[2]?.[1].get('mode')).toBe('ranking')
    expect(videos.mock.calls[2]?.[1].get('period')).toBe('weekly')
    expect(videos.mock.calls[2]?.[1].get('page')).toBe('2')
    expect(await screen.findByRole('link', { name: /Next AVdb page/ })).toBeInTheDocument()
  })

  it('turns a chart search into latest relevance search and does not read playback state', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    const videos = vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [], page: 1, page_size: 24, has_more: false, has_previous: false, total: 0 })
    const detail = vi.spyOn(r18Api, 'detail')
    const resources = vi.spyOn(r18Api, 'resources')
    const guangya = vi.spyOn(guangyaApi, 'state')
    const cloud115 = vi.spyOn(cloud115Api, 'state')

    render(<TestApp initialEntry="/r18?library_id=8&mode=top250&year=2024&ignore_watched=true" />)
    await screen.findByRole('heading', { name: 'AVdb 资料库' })
    await user.type(screen.getByRole('searchbox', { name: '关键词' }), 'ABP-123')
    await user.click(screen.getByRole('button', { name: '查询' }))
    await waitFor(() => expect(videos).toHaveBeenCalledTimes(2))
    const query = videos.mock.calls[1]![1]
    expect(query.get('search')).toBe('ABP-123')
    expect(query.has('mode')).toBe(false)
    expect(query.has('year')).toBe(false)
    expect(query.has('ignore_watched')).toBe(false)
    expect(detail).not.toHaveBeenCalled()
    expect(resources).not.toHaveBeenCalled()
    expect(guangya).not.toHaveBeenCalled()
    expect(cloud115).not.toHaveBeenCalled()
  })

  it.each(['categories', 'ranking'] as const)('hides catalog covers in %s while retaining local STRM status', async mode => {
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    useReactPreferencesStore.setState({ hideImages: true })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [video], page: 1, page_size: 24, has_more: false, has_previous: false, total: 1 })
    vi.spyOn(r18Api, 'tags').mockResolvedValue({ categories: [] })

    render(<TestApp initialEntry={`/r18?library_id=8&mode=${mode}`} />)
    await screen.findByRole('heading', { name: 'AVdb 资料库' })
    expect(await screen.findByText('本地 STRM 可播放')).toBeInTheDocument()
    expect(document.querySelector('.react-r18-grid img')).toBeNull()
  })

  it('combines category tags, basic flags, year, month and duration in the catalog request', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    const videos = vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [], page: 1, page_size: 24, has_more: false, has_previous: false, total: 0 })
    const tags = vi.spyOn(r18Api, 'tags').mockResolvedValue({ categories: [
      { id: 'main', name: '基础条件', type: '0', tags: [{ id: 'p', name: '有码', type: '0' }, { id: 'm', name: '无码', type: '0' }] },
      { id: 'year', name: '年份', type: '0', tags: [{ id: '2024', name: '2024年', type: '0' }] },
      { id: 'month', name: '月份', type: '0', tags: [{ id: '6', name: '6月', type: '0' }] },
      { id: 'duration', name: '时长', type: '0', tags: [{ id: '90-120', name: '90到120分钟', type: '0' }] },
      { id: 'subject', name: '题材', type: '0', tags: [{ id: '1', name: '剧情', type: '0' }] },
    ] })

    render(<TestApp initialEntry="/r18?library_id=8&mode=categories" />)
    await screen.findByRole('heading', { name: 'AVdb 资料库' })
    await user.click(await screen.findByRole('checkbox', { name: '有码' }))
    await user.click(screen.getByRole('checkbox', { name: '无码' }))
    await user.click(screen.getByText('题材'))
    await user.click(await screen.findByRole('checkbox', { name: '剧情' }))
    await user.selectOptions(screen.getByRole('combobox', { name: '发行年份' }), '2024')
    await user.selectOptions(screen.getByRole('combobox', { name: '发行月份' }), '6')
    await user.selectOptions(screen.getByRole('combobox', { name: '影片时长' }), '90-120')

    await waitFor(() => expect(tags).toHaveBeenCalledOnce())
    const query = videos.mock.calls.at(-1)![1]
    expect(query.get('mode')).toBe('categories')
    expect(query.get('tag_ids')).toBe('0:1')
    expect(query.get('main_filter')).toBe('m,p')
    expect(query.get('year')).toBe('2024')
    expect(query.get('month')).toBe('6')
    expect(query.get('duration')).toBe('90-120')
    expect(query.get('sort')).toBe('release')
    expect(query.get('order')).toBe('desc')
  })

  it('does not show one library cards while a history or URL navigation loads another library', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [
      { Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' },
      { Id: 9, Name: '第二 AVdb 资料库', Path: '', SourceType: 'avdb_javdb' },
    ], total: 2 })
    const firstPage = { items: [{ ...video, title: 'Library 8 only' }], page: 1, page_size: 24, has_more: false, has_previous: false, total: 1 }
    const secondPage = { items: [{ ...video, id: 'r18-library-9', title: 'Library 9 only' }], page: 1, page_size: 24, has_more: false, has_previous: false, total: 1 }
    let resolveSecond!: (value: typeof secondPage) => void
    const videos = vi.spyOn(r18Api, 'videos').mockResolvedValueOnce(firstPage).mockImplementationOnce(() => new Promise(resolve => { resolveSecond = resolve }))

    render(<TestApp />)
    expect(await screen.findByRole('link', { name: /ABP-123 Library 8 only/ })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Switch library 9 in URL' }))
    await waitFor(() => expect(videos).toHaveBeenCalledTimes(2))
    expect(screen.queryByRole('link', { name: /ABP-123 Library 8 only/ })).not.toBeInTheDocument()

    resolveSecond(secondPage)
    expect(await screen.findByRole('link', { name: /ABP-123 Library 9 only/ })).toBeInTheDocument()
  })

  it('hides the previous account catalog immediately while the new token is being checked', async () => {
    useReactAuthStore.setState({ token: 'first-token', user: authFixture.User, phase: 'authenticated' })
    let resolveSecond!: (value: Awaited<ReturnType<typeof r18Api.libraries>>) => void
    const libraries = vi.spyOn(r18Api, 'libraries')
      .mockResolvedValueOnce({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
      .mockImplementationOnce(() => new Promise(resolve => { resolveSecond = resolve }))
    vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [video], page: 1, page_size: 24, has_more: false, has_previous: false, total: 1 })

    render(<TestApp />)
    expect(await screen.findByRole('link', { name: /ABP-123 Sample English Title/ })).toBeInTheDocument()

    await act(async () => {
      useReactAuthStore.setState({ token: 'second-token', user: authFixture.User, phase: 'authenticated' })
    })
    await waitFor(() => expect(libraries).toHaveBeenCalledTimes(2))
    expect(screen.queryByRole('link', { name: /ABP-123 Sample English Title/ })).not.toBeInTheDocument()

    resolveSecond({ items: [], total: 0 })
    expect(await screen.findByRole('heading', { name: '尚未配置 AVdb 在线资料库' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /ABP-123 Sample English Title/ })).not.toBeInTheDocument()
  })

  it('cancels outstanding video and category requests when the account token changes', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'first-token', user: authFixture.User, phase: 'authenticated' })
    const library = { Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' as const }
    const libraries = vi.spyOn(r18Api, 'libraries')
      .mockResolvedValueOnce({ items: [library], total: 1 })
      .mockResolvedValueOnce({ items: [], total: 0 })
    let resolveVideo!: (value: Awaited<ReturnType<typeof r18Api.videos>>) => void
    const videos = vi.spyOn(r18Api, 'videos')
      .mockResolvedValueOnce({ items: [video], page: 1, page_size: 24, has_more: true, has_previous: false, total: 1 })
      .mockImplementationOnce(() => new Promise(resolve => { resolveVideo = resolve }))
    let resolveTags!: (value: Awaited<ReturnType<typeof r18Api.tags>>) => void
    const tags = vi.spyOn(r18Api, 'tags').mockImplementation(() => new Promise(resolve => { resolveTags = resolve }))

    render(<TestApp initialEntry="/r18?library_id=8&mode=categories" />)
    await screen.findByRole('link', { name: /ABP-123 Sample English Title/ })
    await waitFor(() => expect(tags).toHaveBeenCalledOnce())
    await user.selectOptions(screen.getByRole('combobox', { name: '资源筛选' }), 'subtitle')
    await waitFor(() => expect(videos).toHaveBeenCalledTimes(2))
    const videoSignal = videos.mock.calls[1]![2]!
    const tagSignal = tags.mock.calls[0]![2]!

    await act(async () => {
      useReactAuthStore.setState({ token: 'second-token', user: authFixture.User, phase: 'authenticated' })
    })
    await waitFor(() => expect(libraries).toHaveBeenCalledTimes(2))
    expect(videoSignal.aborted).toBe(true)
    expect(tagSignal.aborted).toBe(true)
    expect(screen.queryByRole('link', { name: /ABP-123 Sample English Title/ })).not.toBeInTheDocument()

    resolveVideo({ items: [{ ...video, title: 'Old account late result' }], page: 1, page_size: 24, has_more: false, has_previous: false, total: 1 })
    resolveTags({ categories: [{ id: 'subject', name: '题材', type: '0', tags: [{ id: '1', name: '旧账号分类', type: '0' }] }] })
    await waitFor(() => expect(screen.queryByText('旧账号分类')).not.toBeInTheDocument())
    expect(screen.queryByRole('link', { name: /Old account late result/ })).not.toBeInTheDocument()
    expect(videos).toHaveBeenCalledTimes(2)
  })

  it('loads pages from the cached library list and keeps refresh flags out of the URL', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    const libraries = vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb', Revision: 1 }], total: 1 })
    const videos = vi.spyOn(r18Api, 'videos')
      .mockResolvedValueOnce({ items: [video], page: 1, page_size: 24, has_more: true, has_previous: false, total: null })
      .mockResolvedValueOnce({ items: [{ ...video, id: 'page-two' }], page: 2, page_size: 24, has_more: true, has_previous: true, total: null })
      .mockResolvedValueOnce({ items: [{ ...video, id: 'refreshed-page-two' }], page: 2, page_size: 24, has_more: true, has_previous: true, total: null })

    render(<TestApp />)
    await screen.findByRole('link', { name: /ABP-123 Sample English Title/ })
    await user.click(screen.getByRole('button', { name: '下一页' }))
    await waitFor(() => expect(videos).toHaveBeenCalledTimes(2))
    expect(videos.mock.calls[1]?.[1].get('page')).toBe('2')
    expect(libraries).toHaveBeenCalledOnce()

    await user.click(screen.getByRole('button', { name: '刷新' }))
    await waitFor(() => expect(videos).toHaveBeenCalledTimes(3))
    expect(libraries).toHaveBeenCalledTimes(2)
    expect(videos.mock.calls[2]?.[1].get('refresh')).toBe('1')
    expect(screen.getByTestId('route-location')).not.toHaveTextContent('refresh=1')
  })

  it('drops old cards when refresh changes library revision or removes access', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    const library = (revision: number) => ({ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' as const, Revision: revision })
    const libraries = vi.spyOn(r18Api, 'libraries')
      .mockResolvedValueOnce({ items: [library(1)], total: 1 })
      .mockResolvedValueOnce({ items: [library(2)], total: 1 })
      .mockResolvedValueOnce({ items: [], total: 0 })
    let resolveRefreshed!: (value: Awaited<ReturnType<typeof r18Api.videos>>) => void
    const videos = vi.spyOn(r18Api, 'videos')
      .mockResolvedValueOnce({ items: [video], page: 1, page_size: 24, has_more: false, has_previous: false, total: 1 })
      .mockImplementationOnce(() => new Promise(resolve => { resolveRefreshed = resolve }))

    render(<TestApp />)
    await screen.findByRole('link', { name: /ABP-123 Sample English Title/ })
    await user.click(screen.getByRole('button', { name: '刷新' }))
    await waitFor(() => expect(videos).toHaveBeenCalledTimes(2))
    expect(screen.queryByRole('link', { name: /ABP-123 Sample English Title/ })).not.toBeInTheDocument()
    resolveRefreshed({ items: [{ ...video, title: 'Updated source version' }], page: 1, page_size: 24, has_more: false, has_previous: false, total: 1 })
    await screen.findByRole('link', { name: /ABP-123 Updated source version/ })

    await user.click(screen.getByRole('button', { name: '刷新' }))
    await screen.findByRole('heading', { name: '尚未配置 AVdb 在线资料库' })
    expect(screen.queryByRole('link', { name: /Updated source version/ })).not.toBeInTheDocument()
    expect(libraries).toHaveBeenCalledTimes(3)
    expect(videos).toHaveBeenCalledTimes(2)
  })

  it('cancels old filter requests and keeps the latest result when an aborted response arrives late', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    let resolveOld!: (value: Awaited<ReturnType<typeof r18Api.videos>>) => void
    let resolveLatest!: (value: Awaited<ReturnType<typeof r18Api.videos>>) => void
    const videos = vi.spyOn(r18Api, 'videos')
      .mockResolvedValueOnce({ items: [video], page: 1, page_size: 24, has_more: true, has_previous: false, total: 1 })
      .mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
      .mockImplementationOnce(() => new Promise(resolve => { resolveLatest = resolve }))

    render(<TestApp />)
    const original = await screen.findByRole('link', { name: /ABP-123 Sample English Title/ })
    const originalFrom = decodeURIComponent(original.getAttribute('href') ?? '')
    await user.selectOptions(screen.getByRole('combobox', { name: '资源筛选' }), 'subtitle')
    await waitFor(() => expect(videos).toHaveBeenCalledTimes(2))
    const oldSignal = videos.mock.calls[1]![2]!
    await user.selectOptions(screen.getByRole('combobox', { name: '资源筛选' }), 'can_play')
    await waitFor(() => expect(videos).toHaveBeenCalledTimes(3))
    expect(oldSignal.aborted).toBe(true)

    resolveLatest({ items: [{ ...video, id: 'latest-filter', title: 'Latest filter result' }], page: 1, page_size: 24, has_more: false, has_previous: false, total: 1 })
    const latest = await screen.findByRole('link', { name: /Latest filter result/ })
    expect(decodeURIComponent(latest.getAttribute('href') ?? '')).toContain('filter=can_play')
    resolveOld({ items: [{ ...video, id: 'old-filter', title: 'Old filter response' }], page: 1, page_size: 24, has_more: false, has_previous: false, total: 1 })
    await waitFor(() => expect(screen.queryByRole('link', { name: /Old filter response/ })).not.toBeInTheDocument())
    expect(screen.getByRole('link', { name: /Latest filter result/ })).toBeInTheDocument()
    expect(originalFrom).toContain('filter=magnets')
  })

  it('keeps same-library results after a network error, marks them stale, and disables their pager', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    const videos = vi.spyOn(r18Api, 'videos')
      .mockResolvedValueOnce({ items: [video], page: 4, page_size: 24, has_more: true, has_previous: true, total: null })
      .mockRejectedValueOnce(new Error('临时上游错误'))

    render(<TestApp initialEntry="/r18?library_id=8&page=4&filter=magnets" />)
    const card = await screen.findByRole('link', { name: /ABP-123 Sample English Title/ })
    expect(decodeURIComponent(card.getAttribute('href') ?? '')).toContain('filter=magnets')
    await user.selectOptions(screen.getByRole('combobox', { name: '资源筛选' }), 'subtitle')

    expect(await screen.findByText('临时上游错误')).toHaveAttribute('role', 'alert')
    expect(screen.getByRole('link', { name: /ABP-123 Sample English Title/ })).toBeInTheDocument()
    expect(screen.getByText('新条件尚未成功加载，当前显示上次结果。')).toHaveAttribute('role', 'alert')
    expect(screen.getByRole('button', { name: '下一页' })).toBeDisabled()
    const next = screen.getByRole('button', { name: '下一页' })
    next.removeAttribute('disabled')
    fireEvent.click(next)
    const pageInput = screen.getByRole('spinbutton', { name: '跳转页码' })
    pageInput.removeAttribute('disabled')
    fireEvent.change(pageInput, { target: { value: '5' } })
    const jump = screen.getByRole('button', { name: '跳转' })
    jump.removeAttribute('disabled')
    fireEvent.click(jump)
    expect(videos).toHaveBeenCalledTimes(2)
  })

  it('clears previous cards when the same-library request loses permission', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    vi.spyOn(r18Api, 'videos')
      .mockResolvedValueOnce({ items: [video], page: 1, page_size: 24, has_more: true, has_previous: false, total: 1 })
      .mockRejectedValueOnce(new ApiError('无权访问', 403))

    render(<TestApp />)
    await screen.findByRole('link', { name: /ABP-123 Sample English Title/ })
    await user.selectOptions(screen.getByRole('combobox', { name: '资源筛选' }), 'subtitle')

    expect(await screen.findByRole('alert')).toHaveTextContent('无权访问')
    expect(screen.queryByRole('link', { name: /ABP-123 Sample English Title/ })).not.toBeInTheDocument()
  })

  it('only mounts the open tag group, limits it to 40 tags, and searches the full category list', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [], page: 1, page_size: 24, has_more: false, has_previous: false, total: 0 })
    vi.spyOn(r18Api, 'tags').mockResolvedValue({ categories: [{
      id: 'subject', name: '题材', type: '0', tags: Array.from({ length: 1061 }, (_, index) => ({
        id: String(index + 1), name: index === 1060 ? '唯一匹配词' : `分类夹具 ${index + 1}`, type: '0',
      })),
    }] })

    render(<TestApp initialEntry="/r18?library_id=8&mode=categories" />)
    await screen.findByRole('heading', { name: 'AVdb 资料库' })
    expect(screen.queryByRole('checkbox', { name: '分类夹具 1' })).not.toBeInTheDocument()
    await user.click(await screen.findByText('题材'))
    expect(screen.getByRole('checkbox', { name: '分类夹具 1' })).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: '分类夹具 40' })).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: '分类夹具 41' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '显示更多标签' }))
    expect(screen.getByRole('checkbox', { name: '分类夹具 80' })).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: '分类夹具 81' })).not.toBeInTheDocument()

    await user.type(screen.getByRole('searchbox', { name: '搜索题材' }), '唯一匹配词')
    expect(screen.getByRole('checkbox', { name: '唯一匹配词' })).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: '分类夹具 1' })).not.toBeInTheDocument()
  })

  it('caps selected category tags at five and allows removing a tag before selecting another', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [], page: 1, page_size: 24, has_more: false, has_previous: false, total: 0 })
    vi.spyOn(r18Api, 'tags').mockResolvedValue({ categories: [{
      id: 'subject', name: '题材', type: '0', tags: Array.from({ length: 6 }, (_, index) => ({ id: String(index + 1), name: `题材 ${index + 1}`, type: '0' })),
    }] })

    render(<TestApp initialEntry="/r18?library_id=8&mode=categories" />)
    await screen.findByRole('heading', { name: 'AVdb 资料库' })
    await user.click(await screen.findByText('题材'))
    for (let index = 1; index <= 5; index++) await user.click(screen.getByRole('checkbox', { name: `题材 ${index}` }))
    const sixth = screen.getByRole('checkbox', { name: '题材 6' })
    await user.click(sixth)

    expect(screen.getByRole('alert')).toHaveTextContent('最多选择 5 个题材标签')
    expect(sixth).not.toBeChecked()
    await user.click(screen.getByRole('button', { name: '题材 1' }))
    await user.click(sixth)
    expect(sixth).toBeChecked()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('filters special and topic groups by the selected catalog type', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [], page: 1, page_size: 24, has_more: false, has_previous: false, total: 0 })
    vi.spyOn(r18Api, 'tags').mockResolvedValue({ categories: [
      { id: 'main', name: '基础条件', type: '0', tags: [{ id: 'p', name: '有码标签', type: '0' }] },
      { id: 'main', name: '基础条件', type: '3', tags: [{ id: 'm', name: 'FC2 标签', type: '3' }] },
      { id: 'year', name: '年份', type: '0', tags: [{ id: '2024', name: '2024 年', type: '0' }] },
      { id: 'year', name: '年份', type: '3', tags: [{ id: '2025', name: '2025 年', type: '3' }] },
      { id: 'subject', name: '题材', type: '0', tags: [{ id: '1', name: '普通题材', type: '0' }] },
      { id: 'subject', name: '题材', type: '3', tags: [{ id: '2', name: 'FC2 题材', type: '3' }] },
    ] })

    render(<TestApp initialEntry="/r18?library_id=8&mode=categories&type=3" />)
    await screen.findByRole('heading', { name: 'AVdb 资料库' })
    expect(await screen.findByRole('checkbox', { name: 'FC2 标签' })).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: '有码标签' })).not.toBeInTheDocument()
    const years = screen.getByRole('combobox', { name: '发行年份' })
    expect(years.querySelector('option[value="2025"]')).toBeInTheDocument()
    expect(years.querySelector('option[value="2024"]')).not.toBeInTheDocument()
    await user.click(await screen.findByText('题材'))
    expect(screen.getByRole('checkbox', { name: 'FC2 题材' })).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: '普通题材' })).not.toBeInTheDocument()
  })

  it('cancels pending category reads and clears tag controls when the selected library is unavailable', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    const libraries = vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [], page: 1, page_size: 24, has_more: false, has_previous: false, total: 0 })
    let resolveTags!: (value: Awaited<ReturnType<typeof r18Api.tags>>) => void
    const tags = vi.spyOn(r18Api, 'tags').mockImplementation(() => new Promise(resolve => { resolveTags = resolve }))

    render(<TestApp initialEntry="/r18?library_id=8&mode=categories" />)
    await screen.findByRole('heading', { name: 'AVdb 资料库' })
    await waitFor(() => expect(tags).toHaveBeenCalledOnce())
    const signal = tags.mock.calls[0]![2]!
    await user.click(screen.getByRole('button', { name: 'Switch invalid library in categories' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('不存在或当前账号无权访问')
    expect(signal.aborted).toBe(true)
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()

    resolveTags({ categories: [{ id: 'subject', name: '题材', type: '0', tags: [{ id: '1', name: '迟到标签', type: '0' }] }] })
    await waitFor(() => expect(screen.queryByText('迟到标签')).not.toBeInTheDocument())
    expect(libraries).toHaveBeenCalledOnce()
    expect(tags).toHaveBeenCalledOnce()
  })

  it('cancels pending tag metadata when leaving category mode and supports a forced tag refresh', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [], page: 1, page_size: 24, has_more: false, has_previous: false, total: 0 })
    let resolveTags!: (value: Awaited<ReturnType<typeof r18Api.tags>>) => void
    const tags = vi.spyOn(r18Api, 'tags')
      .mockImplementationOnce(() => new Promise(resolve => { resolveTags = resolve }))
      .mockResolvedValueOnce({ categories: [{ id: 'subject', name: '题材', type: '0', tags: [{ id: '1', name: '已刷新的标签', type: '0' }] }] })
      .mockResolvedValueOnce({ categories: [{ id: 'subject', name: '题材', type: '0', tags: [{ id: '1', name: '已刷新标签二次', type: '0' }] }] })

    render(<TestApp initialEntry="/r18?library_id=8&mode=categories" />)
    await screen.findByRole('heading', { name: 'AVdb 资料库' })
    await waitFor(() => expect(tags).toHaveBeenCalledOnce())
    const signal = tags.mock.calls[0]![2]!
    await user.click(screen.getByRole('button', { name: '最新影片' }))
    expect(signal.aborted).toBe(true)
    resolveTags({ categories: [{ id: 'subject', name: '题材', type: '0', tags: [{ id: '1', name: '迟到标签', type: '0' }] }] })
    await waitFor(() => expect(screen.queryByText('迟到标签')).not.toBeInTheDocument())

    await user.click(screen.getByRole('button', { name: '分类查询' }))
    await waitFor(() => expect(tags).toHaveBeenCalledTimes(2))
    await user.click(screen.getByText('题材'))
    await screen.findByRole('checkbox', { name: '已刷新的标签' })
    await user.click(screen.getByRole('button', { name: '刷新分类' }))
    await waitFor(() => expect(tags).toHaveBeenCalledTimes(3))
    expect(tags.mock.calls[2]?.[3]).toBe(true)
    await screen.findByRole('checkbox', { name: '已刷新标签二次' })
  })

  it('shows title, cover and local STRM status, applies URL filters, and returns to the filtered list', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    const videos = vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [video], page: 1, page_size: 24, has_more: false, has_previous: false, total: 1 })
    vi.spyOn(r18Api, 'detail').mockResolvedValue({
      video, library_name: 'AVdb 资料库', playable: true,
      local_sources: [{ id: 'strm-5', item_id: '5', name: 'ABP-123.strm', library_name: '本地 STRM', container: 'mp4' }],
    })
    vi.spyOn(r18Api, 'resources').mockResolvedValue({ items: [], has_more: false, next_offset: 0 })
    vi.spyOn(guangyaApi, 'state').mockResolvedValue({ configured: false, tasks: [], sources: [] })

    render(<TestApp />)
    expect(await screen.findByRole('heading', { name: 'AVdb 资料库' }, { timeout: 10000 })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /ABP-123 Sample English Title/ })).toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'ABP-123 封面' })).toHaveAttribute('src', video.cover_url)
    expect(screen.getByText('本地 STRM 可播放')).toBeInTheDocument()

    await user.selectOptions(screen.getByRole('combobox', { name: '资源筛选' }), 'subtitle')
    await waitFor(() => expect(screen.getByTestId('route-location')).toHaveTextContent('/r18?library_id=8&type=all&filter=subtitle'))
    await waitFor(() => expect(videos).toHaveBeenCalledTimes(2))
    expect(videos.mock.calls.at(-1)?.[1].get('filter')).toBe('subtitle')

    await user.click(screen.getByRole('link', { name: /ABP-123 Sample English Title/ }))
    expect(await screen.findByRole('heading', { name: 'Sample English Title' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '播放本地 STRM' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '返回 AVdb 目录' }))
    await waitFor(() => expect(screen.getByTestId('route-location')).toHaveTextContent('/r18?library_id=8&type=all&filter=subtitle'))
  })

  it('opens the AVdb still gallery without triggering playback or cloud requests', async () => {
    const user = userEvent.setup()
    const galleryVideo = { ...video, images: [
      '/api/r18/libraries/8/videos/r18-abc/images/0', '/api/r18/libraries/8/videos/r18-abc/images/1',
    ] }
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'detail').mockResolvedValue({ video: galleryVideo, library_name: 'AVdb 资料库', playable: true, local_sources: [localSource] })
    vi.spyOn(r18Api, 'resources').mockResolvedValue({ items: [], has_more: false, next_offset: 0 })
    const guangya = vi.spyOn(guangyaApi, 'state')
    const cloud115 = vi.spyOn(cloud115Api, 'state')

    render(<TestApp initialEntry="/r18/8/item/r18-abc" />)
    expect(await screen.findByRole('heading', { name: galleryVideo.title })).toBeInTheDocument()
    const trigger = screen.getByRole('button', { name: '查看剧照 1' })
    await user.click(trigger)
    expect(screen.getByRole('dialog', { name: '剧照 1 / 2' })).toBeInTheDocument()
    expect(document.querySelector('.react-artwork-image')).toHaveAttribute('src', galleryVideo.images[0])

    fireEvent.keyDown(window, { key: 'ArrowRight' })
    expect(await screen.findByRole('dialog', { name: '剧照 2 / 2' })).toBeInTheDocument()
    expect(document.querySelector('.react-artwork-image')).toHaveAttribute('src', galleryVideo.images[1])
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
    expect(document.body.style.overflow).toBe('')
    expect(useReactPlayerStore.getState().current).toBeNull()
    expect(guangya).not.toHaveBeenCalled()
    expect(cloud115).not.toHaveBeenCalled()
  })

  it('keeps an untrusted preview URL disabled and never opens it in the player', async () => {
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'detail').mockResolvedValue({
      video: { ...video, has_preview: true, preview_url: 'https://outside.fixture/preview.m3u8?token=secret' },
      library_name: 'AVdb 资料库', playable: true, local_sources: [localSource],
    })
    vi.spyOn(r18Api, 'resources').mockResolvedValue({ items: [], has_more: false, next_offset: 0 })

    render(<TestApp initialEntry="/r18/8/item/r18-abc" />)
    expect(await screen.findByRole('heading', { name: video.title })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '播放预览' })).toBeDisabled()
    expect(useReactPlayerStore.getState().current).toBeNull()
  })

  it('plays a validated same-origin preview without attaching movie playback identity', async () => {
    const user = userEvent.setup()
    const previewURL = '/api/r18/libraries/8/videos/r18-abc/preview.m3u8'
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'detail').mockResolvedValue({
      video: { ...video, has_preview: true, preview_url: previewURL },
      library_name: 'AVdb 资料库', playable: true, local_sources: [localSource],
    })
    vi.spyOn(r18Api, 'resources').mockResolvedValue({ items: [], has_more: false, next_offset: 0 })

    render(<TestApp initialEntry="/r18/8/item/r18-abc" />)
    await screen.findByRole('heading', { name: video.title })
    await user.click(screen.getByRole('button', { name: '播放预览' }))

    expect(useReactPlayerStore.getState().current).toMatchObject({
      title: `预览视频 · ${video.title}`, url: previewURL, type: 'hls',
    })
    expect(useReactPlayerStore.getState().current).not.toHaveProperty('itemId')
    expect(useReactPlayerStore.getState().current).not.toHaveProperty('proxyURL')
  })

  it('keeps network playback idle while a local STRM is available until the user opens the magnet flow', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [video], page: 1, page_size: 24, has_more: false, has_previous: false, total: 1 })
    vi.spyOn(r18Api, 'detail').mockResolvedValue({ video, library_name: 'AVdb 资料库', playable: true, local_sources: [localSource] })
    vi.spyOn(r18Api, 'resources').mockResolvedValue({ items: [resource], has_more: false, next_offset: 0 })
    const localPlayback = vi.spyOn(r18Api, 'localPlayback').mockResolvedValue({ source: localSource })
    const offlineState: OfflineState = { configured: false, tasks: [], sources: [] }
    const guangyaState = vi.spyOn(guangyaApi, 'state').mockResolvedValue(offlineState)
    const cloud115State = vi.spyOn(cloud115Api, 'state').mockResolvedValue(offlineState)

    render(<TestApp />)
    await user.click(await screen.findByRole('link', { name: /ABP-123 Sample English Title/ }))
    expect(await screen.findByRole('heading', { name: 'Sample English Title' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '光鸭 下载任务' })).not.toBeInTheDocument()
    expect(screen.queryByText('正在读取 光鸭 本地任务…')).not.toBeInTheDocument()
    expect(guangyaState).not.toHaveBeenCalled()
    expect(cloud115State).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: '播放本地 STRM' }))
    await waitFor(() => expect(localPlayback).toHaveBeenCalledWith('8', 'r18-abc', 'strm-5'))
    expect(useReactPlayerStore.getState().current).toMatchObject({ url: '/Videos/5/stream', itemId: '5' })
    expect(guangyaState).not.toHaveBeenCalled()
    expect(cloud115State).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: '手动使用网盘磁力流程' }))
    await waitFor(() => expect(guangyaState).toHaveBeenCalledTimes(1))
    expect(screen.getByRole('combobox', { name: '播放网盘' })).toHaveValue('guangya')
    expect(screen.getByRole('heading', { name: '光鸭 下载任务' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '返回本地 STRM' }))
    expect(screen.queryByRole('heading', { name: '光鸭 下载任务' })).not.toBeInTheDocument()
    expect(cloud115State).not.toHaveBeenCalled()
  })

  it('uses only the selected cloud provider and recovers its server task after returning to the catalog', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [{ ...video, has_local: false }], page: 1, page_size: 24, has_more: false, has_previous: false, total: 1 })
    vi.spyOn(r18Api, 'detail').mockResolvedValue({ video: { ...video, has_local: false }, library_name: 'AVdb 资料库', playable: false, local_sources: [] })
    vi.spyOn(r18Api, 'resources').mockResolvedValue({ items: [resource], has_more: false, next_offset: 0 })
    const empty: OfflineState = { configured: true, tasks: [], sources: [] }
    const task: OfflineTask = {
      id: 'task-1', resource_id: resource.id, magnet: resource.magnet, info_hash: 'a'.repeat(40),
      status: 'queued', progress: 0, stage: 'queued', error: '', retryable: false, version: 1,
      files: [], file_id: '', expires_at: 0,
    }
    const queued: OfflineState = { ...empty, tasks: [task] }
    let submitted = false
    const guangyaRead = vi.spyOn(guangyaApi, 'state').mockResolvedValue({ ...empty, configured: false })
    const guangyaSubmit = vi.spyOn(guangyaApi, 'submit').mockResolvedValue(queued)
    const cloud115Read = vi.spyOn(cloud115Api, 'state').mockImplementation(async () => submitted ? queued : empty)
    const cloud115Submit = vi.spyOn(cloud115Api, 'submit').mockImplementation(async () => { submitted = true; return queued })
    const cloud115Change = vi.spyOn(cloud115Api, 'change').mockResolvedValue(queued)

    render(<TestApp />)
    await user.click(await screen.findByRole('link', { name: /ABP-123 Sample English Title/ }))
    expect(await screen.findByRole('heading', { name: 'Sample English Title' })).toBeInTheDocument()
    await waitFor(() => expect(guangyaRead).toHaveBeenCalledTimes(1))
    await user.selectOptions(screen.getByRole('combobox', { name: '播放网盘' }), '115')
    await waitFor(() => expect(cloud115Read).toHaveBeenCalledTimes(1))
    await user.click(screen.getByRole('button', { name: '播放此版本' }))
    await waitFor(() => expect(cloud115Submit).toHaveBeenCalledOnce())
    expect(cloud115Submit).toHaveBeenCalledWith('8', 'r18-abc', { resource_id: resource.id })
    await screen.findAllByText('排队中')
    expect(guangyaSubmit).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: '返回 AVdb 目录' }))
    await user.click(await screen.findByRole('link', { name: /ABP-123 Sample English Title/ }))
    expect(await screen.findByRole('heading', { name: 'Sample English Title' })).toBeInTheDocument()
    await waitFor(() => expect(guangyaRead).toHaveBeenCalledTimes(2))
    await user.selectOptions(screen.getByRole('combobox', { name: '播放网盘' }), '115')
    expect(await screen.findAllByText('排队中')).not.toHaveLength(0)
    expect(cloud115Read.mock.calls.length).toBeGreaterThan(1)
    expect(cloud115Submit).toHaveBeenCalledOnce()
    expect(cloud115Change).not.toHaveBeenCalled()
    expect(guangyaSubmit).not.toHaveBeenCalled()
  })

  it('cancels old cloud reads and waits for the next detail before selecting its playback flow', async () => {
    const user = userEvent.setup()
    useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
    vi.spyOn(r18Api, 'libraries').mockResolvedValue({ items: [{ Id: 8, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1 })
    vi.spyOn(r18Api, 'videos').mockResolvedValue({ items: [{ ...video, has_local: false }], page: 1, page_size: 24, has_more: false, has_previous: false, total: 1 })
    const nextVideo = { ...video, id: 'r18-next', number: 'ABP-124', title: 'Next Fixture', has_local: true }
    let resolveNext!: (value: Awaited<ReturnType<typeof r18Api.detail>>) => void
    const detail = vi.spyOn(r18Api, 'detail')
      .mockResolvedValueOnce({ video: { ...video, has_local: false }, library_name: 'AVdb 资料库', playable: false, local_sources: [] })
      .mockImplementationOnce(() => new Promise(resolve => { resolveNext = resolve }))
    vi.spyOn(r18Api, 'resources').mockResolvedValue({ items: [], has_more: false, next_offset: 0 })
    const stateSignals: AbortSignal[] = []
    const guangyaState = vi.spyOn(guangyaApi, 'state').mockImplementation(async (_library, _video, signal) => {
      if (signal) stateSignals.push(signal)
      return await new Promise<OfflineState>(() => {})
    })
    const cloud115State = vi.spyOn(cloud115Api, 'state').mockResolvedValue({ configured: false, tasks: [], sources: [] })

    render(<TestApp />)
    await user.click(await screen.findByRole('link', { name: /ABP-123 Sample English Title/ }))
    expect(await screen.findByRole('heading', { name: 'Sample English Title' })).toBeInTheDocument()
    await waitFor(() => expect(guangyaState).toHaveBeenCalled())
    const initialStateReads = guangyaState.mock.calls.length

    await user.click(screen.getByRole('button', { name: 'Open next fixture' }))
    await waitFor(() => expect(detail).toHaveBeenCalledTimes(2))
    expect(stateSignals.length).toBeGreaterThan(0)
    expect(stateSignals.every(signal => signal.aborted)).toBe(true)
    expect(guangyaState).toHaveBeenCalledTimes(initialStateReads)
    expect(cloud115State).not.toHaveBeenCalled()

    resolveNext({ video: nextVideo, library_name: 'AVdb 资料库', playable: true, local_sources: [localSource] })
    expect(await screen.findByRole('heading', { name: 'Next Fixture' })).toBeInTheDocument()
    expect(guangyaState).toHaveBeenCalledTimes(initialStateReads)
    expect(cloud115State).not.toHaveBeenCalled()
  })
})
