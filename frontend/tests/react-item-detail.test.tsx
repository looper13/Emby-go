import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { catalogApi } from '../src/api/catalog'
import type { DetailDto } from '../src/api/contracts'
import { mediaApi } from '../src/api/media'
import { apiClient } from '../src/api/client'
import { scrapeApi, type Candidate, type Inspect, type Preview } from '../src/api/scrape'
import { javdbCommunityApi, type JavdbListMovies, type JavdbPage, type JavdbRelatedList, type JavdbReview } from '../src/api/javdb-community'
import { r18Api } from '../src/api/r18'
import { AppProviders } from '../src/react/AppProviders'
import { useReactAuthStore } from '../src/react/auth-store'
import { useReactPlayerStore } from '../src/react/player-store'
import { ScrapePreviewDialog } from '../src/react/ScrapePreviewDialog'
import { ItemDetailPage } from '../src/react/media/ItemDetailPage'
import { useReactMediaWallStore } from '../src/react/media-wall-store'
import { useReactScrapePreviewStore } from '../src/react/scrape-preview-store'
import { ViewHistoryProvider } from '../src/react/view-history'
import authFixture from './fixtures/json/auth-login.json'
import detailFixture from './fixtures/json/item-detail.json'

const detail = detailFixture as unknown as DetailDto
const relatedList: JavdbRelatedList = { id: 'related-01', name: '精选清单', movie_count: 3, favorite: false }
const relatedLists: JavdbPage<JavdbRelatedList> = {
  items: [relatedList], page: 1, page_size: 15, has_more: false, total: 1,
  number: 'ABF-005', video_id: 'abf-005', library_id: 2,
}

function reviewPage(page: number, content: string): JavdbPage<JavdbReview> {
  return {
    items: [{ id: `review-${page}`, username: `评论作者 ${page}`, content, score: '4.5', status: '看过', created_at: '2026-11-20' }],
    page, page_size: 15, has_more: page === 1, total: 2, number: 'ABF-005', video_id: 'abf-005', library_id: 2,
  }
}

function listMovies(page: number): JavdbListMovies {
  const items = page === 1 ? [
    { id: 'film-local', number: 'ABF-007', title: '本地已有标题', cover_url: '/api/r18/libraries/2/videos/film-local/images/poster', has_local: true,
      local_movies: [{ id: 7, number: 'ABF-007', title: '本库影片标题' }] },
    { id: 'film-remote', number: 'ABF-008', title: '尚未入库的标题', cover_url: '/api/r18/libraries/2/videos/film-remote/images/poster', has_local: false, local_movies: [] },
  ] : [
    { id: 'film-last', number: 'ABF-009', title: '下一页标题', cover_url: '/api/r18/libraries/2/videos/film-last/images/poster', has_local: false, local_movies: [] },
  ]
  return { items, number: 'ABF-005', video_id: 'abf-005', library_id: 2, list_id: relatedList.id,
    local_library_id: 1, next_page: page + 1, has_more: page === 1 }
}

function Harness({ initialEntry }: { initialEntry: string }) {
  return <AppProviders><ScrapePreviewDialog /><MemoryRouter initialEntries={[initialEntry]}><ViewHistoryProvider>
    <Routes>
      <Route path="/item/:id" element={<ItemDetailPage />} />
      <Route path="/items" element={<main>影视库回退页</main>} />
    </Routes>
  </ViewHistoryProvider></MemoryRouter></AppProviders>
}

function reset() {
  useReactScrapePreviewStore.getState().close()
  useReactMediaWallStore.getState().clear()
  useReactAuthStore.setState({ token: null, user: null, initialized: null, phase: 'anonymous', error: '', submitting: false, expiration: 0 })
  useReactPlayerStore.setState({ current: null })
}

afterEach(() => {
  reset()
  vi.restoreAllMocks()
})

function mockDetailAndAvdb() {
  useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, phase: 'authenticated' })
  vi.spyOn(catalogApi, 'detail').mockResolvedValue(detail)
  vi.spyOn(mediaApi, 'similar').mockResolvedValue([])
  vi.spyOn(r18Api, 'libraries').mockResolvedValue({
    items: [{ Id: 2, Name: 'AVdb 资料库', Path: '', SourceType: 'avdb_javdb' }], total: 1,
  })
  vi.spyOn(javdbCommunityApi, 'match').mockResolvedValue({
    status: 'matched', number: 'ABF-005', library_id: 2, library_name: 'AVdb 资料库',
    video: { id: 'abf-005', number: 'ABF-005', title: 'AVdb 匹配影片' },
  })
  vi.spyOn(javdbCommunityApi, 'lists').mockResolvedValue(relatedLists)
}

describe('React item detail', () => {
  it('renders the detail hero and returns to the wall with its filters', async () => {
    mockDetailAndAvdb()
    const user = userEvent.setup()
    render(<Harness initialEntry="/item/5?library_id=1&search=ABF-005" />)

    expect(await screen.findByRole('heading', { name: detail.movie.Title })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '收藏 · ABF-005 深夜的测试影片' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '影片简介' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '元数据' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '返回媒体墙' }))
    expect(await screen.findByText('影视库回退页')).toBeInTheDocument()
  })

  it('applies selected audio and subtitle options only when starting normal playback', async () => {
    mockDetailAndAvdb()
    const user = userEvent.setup()
    const source = {
      Id: 'media-source', TrackRevision: 'tracks-1', Container: 'mkv', SupportsAudioSelection: true,
      DefaultAudioStreamIndex: 1, DefaultSubtitleStreamIndex: 3, ExternalStreamUrl: 'https://media.example/film.mkv?sig=abc%2Bdef',
      MediaStreams: [
        { Type: 'Audio', Index: 1, DisplayTitle: '日语', IsDefault: true },
        { Type: 'Audio', Index: 2, DisplayTitle: '英语 FLAC', IsExternal: true },
        { Type: 'Subtitle', Index: 3, DisplayTitle: '中文字幕', Codec: 'srt', IsExternal: true,
          DeliveryUrl: '/Videos/5/mediasource_5/Subtitles/3/Stream.srt' },
      ],
    }
    const request = vi.spyOn(apiClient, 'request').mockResolvedValue({ MediaSources: [source] })
    render(<Harness initialEntry="/item/5?library_id=1" />)

    await user.selectOptions(await screen.findByRole('combobox', { name: '选择音轨' }), '2')
    await user.selectOptions(screen.getByRole('combobox', { name: '选择字幕' }), '-1')
    expect(useReactPlayerStore.getState().current).toBeNull()
    await user.click(screen.getByRole('button', { name: '播放' }))

    expect(useReactPlayerStore.getState().current).toMatchObject({
      url: '/Videos/5/stream', itemId: '5', sourceItemId: '5',
      selection: { sourceId: 'media-source', revision: 'tracks-1', audioIndex: 2, subtitleIndex: -1 },
    })
    expect(request).toHaveBeenCalledWith('/Items/5/PlaybackInfo', expect.objectContaining({ auth: 'required', method: 'POST', readOnly: true }))
    expect(request.mock.calls.some(([path, options]) => String(path).endsWith('/Playback') && options.method === 'POST')).toBe(false)
  })

  it('opens and confirms the inspected candidate from the single-item detail action', async () => {
    mockDetailAndAvdb()
    const user = userEvent.setup()
    const candidate: Candidate = {
      provider: 'metatube', id: 'candidate-5', number: 'ABF-005', title: '确认候选影片',
      score: 0.98, thumb: '', exact: true,
    }
    const scrapePreview: Preview = {
      movie_id: detail.movie.id, query: 'ABF-005', expected_number: 'ABF-005', recommended: 0, candidates: [candidate],
    }
    const candidateInspect: Inspect = {
      provider: candidate.provider, id: candidate.id, number: candidate.number, title: candidate.title,
      summary: '候选简介', poster: '', score: candidate.score, release_date: '2024-01-01', runtime: 90,
      overwrite: false, fields: [{ key: 'title', label: '标题', old: detail.movie.Title, next: candidate.title, change: true }],
      images: [],
    }
    const previewRead = vi.spyOn(scrapeApi, 'preview').mockResolvedValue(scrapePreview)
    vi.spyOn(scrapeApi, 'inspect').mockResolvedValue(candidateInspect)
    const confirmWrite = vi.spyOn(scrapeApi, 'confirm').mockResolvedValue({ title: candidate.title, images: 0, trailer_downloaded: false, warnings: [] })
    render(<Harness initialEntry="/item/5?library_id=1" />)

    await user.click(await screen.findByRole('button', { name: '刮削' }))
    expect(await screen.findByRole('dialog', { name: '刮削预览与确认' })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: candidate.title })).toBeInTheDocument()
    expect(previewRead).toHaveBeenCalledWith(detail.movie.id, expect.any(AbortSignal))

    await user.click(screen.getByRole('button', { name: '确认写入' }))
    await waitFor(() => expect(confirmWrite).toHaveBeenCalledWith(detail.movie.id, candidate.provider, candidate.id, false))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '刮削预览与确认' })).not.toBeInTheDocument())
  })

  it('reopens a saved AVdb list and shows every loaded item with its title, cover, and local STRM status', async () => {
    mockDetailAndAvdb()
    const user = userEvent.setup()
    const movies = vi.spyOn(javdbCommunityApi, 'listMovies').mockImplementation(async (_item, _library, _listId, page) => listMovies(page))
    render(<Harness initialEntry="/item/5?library_id=1&javdb_library_id=2&javdb_list_id=related-01&javdb_related_page=1" />)

    expect(await screen.findByRole('heading', { name: '精选清单' })).toBeInTheDocument()
    expect(await screen.findByText('本地已有标题')).toBeInTheDocument()
    expect(screen.getByText('尚未入库的标题')).toBeInTheDocument()
    expect(screen.getByText('本地可播放')).toBeInTheDocument()
    expect(screen.getByText('无本地 STRM')).toBeInTheDocument()
    expect(screen.getByRole('img', { name: '本地已有标题 封面' })).toHaveAttribute('src', '/api/r18/libraries/2/videos/film-local/images/poster')
    expect(screen.getByRole('link', { name: '查看 尚未入库的标题 的 AVdb 详情' })).toHaveAttribute('href', '/r18/2/item/film-remote')
    expect(movies).toHaveBeenCalledWith(5, 2, 'related-01', 1, 1, expect.any(AbortSignal), false)

    await user.click(screen.getByRole('button', { name: '加载更多影片' }))
    expect(await screen.findByText('下一页标题')).toBeInTheDocument()
    expect(movies).toHaveBeenLastCalledWith(5, 2, 'related-01', 2, 1, expect.any(AbortSignal), false)
    expect(screen.getAllByRole('link', { name: /查看 .* 的 AVdb 详情/ })).toHaveLength(3)
  })

  it('saves and removes only an AVdb list reference without fetching its movies', async () => {
    mockDetailAndAvdb()
    const user = userEvent.setup()
    const save = vi.spyOn(javdbCommunityApi, 'setListFavorite').mockResolvedValue()
    const remove = vi.spyOn(javdbCommunityApi, 'removeListFavorite').mockResolvedValue()
    const movies = vi.spyOn(javdbCommunityApi, 'listMovies').mockResolvedValue(listMovies(1))
    render(<Harness initialEntry="/item/5?library_id=1" />)

    await user.click(await screen.findByRole('tab', { name: /相关清单/ }))
    const favorite = await screen.findByRole('button', { name: '收藏清单 · 精选清单' })
    await user.click(favorite)
    await waitFor(() => expect(save).toHaveBeenCalledWith(5, 2, 'related-01', 1))
    expect(movies).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: '取消收藏清单 · 精选清单' }))
    await waitFor(() => expect(remove).toHaveBeenCalledWith(2, 'related-01'))
    expect(movies).not.toHaveBeenCalled()
  })

  it('keeps review text inert, retains the current page through failure, and retries only comments', async () => {
    mockDetailAndAvdb()
    vi.spyOn(apiClient, 'request').mockResolvedValue({ MediaSources: [] })
    const user = userEvent.setup()
    const longText = '<img src=x onerror=bad()>' + '长评论内容'.repeat(140)
    const nextText = '第二页评论内容'
    const reviews = vi.spyOn(javdbCommunityApi, 'reviews')
      .mockResolvedValueOnce(reviewPage(1, longText))
      .mockRejectedValueOnce(new Error('评论第 2 页读取失败'))
      .mockResolvedValueOnce(reviewPage(2, nextText))
    const lists = vi.spyOn(javdbCommunityApi, 'lists').mockResolvedValue(relatedLists)
    const movies = vi.spyOn(javdbCommunityApi, 'listMovies')
    render(<Harness initialEntry="/item/5?library_id=1" />)

    const firstPage = await screen.findByText(`${longText.slice(0, 600)}…`)
    expect(document.querySelector('.react-avdb-review img')).toBeNull()
    expect(document.querySelector('.react-avdb-review')?.textContent).toContain('<img src=x onerror=bad()>')
    await user.click(screen.getByRole('button', { name: '展开完整评论' }))
    expect(await screen.findByText(longText, { exact: true })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '下一页评论' }))
    expect(await screen.findByText('评论第 2 页读取失败')).toBeInTheDocument()
    expect(firstPage).toBeInTheDocument()
    await user.click(screen.getByRole('tab', { name: /相关清单/ }))
    await screen.findByText('精选清单')
    await user.click(screen.getByRole('tab', { name: /评论/ }))
    await user.click(screen.getByRole('button', { name: '重试评论' }))

    expect(await screen.findByText(nextText)).toBeInTheDocument()
    expect(reviews.mock.calls.map(call => call.slice(0, 3))).toEqual([[5, 2, 1], [5, 2, 2], [5, 2, 2]])
    expect(reviews).toHaveBeenLastCalledWith(5, 2, 2, expect.any(AbortSignal), true)
    expect(lists).toHaveBeenCalledOnce()
    expect(movies).not.toHaveBeenCalled()
  })

  it('does not place a review under a different upstream movie identity', async () => {
    mockDetailAndAvdb()
    vi.spyOn(apiClient, 'request').mockResolvedValue({ MediaSources: [] })
    vi.mocked(javdbCommunityApi.match).mockResolvedValue({
      status: 'matched', number: 'ABF-005', library_id: 2, library_name: 'AVdb 资料库',
      video: { id: 'different-movie', number: 'ABF-005', title: '错误关联' },
    })
    vi.spyOn(javdbCommunityApi, 'reviews').mockResolvedValue(reviewPage(1, '错误关联评论'))
    render(<Harness initialEntry="/item/5?library_id=1" />)

    expect(await screen.findByText('影片番号或 AVdb 资料来源已更新，请重新匹配后查看评论。')).toBeInTheDocument()
    expect(screen.queryByText('错误关联评论')).not.toBeInTheDocument()
  })
})
