import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { televisionApi, type TelevisionItem } from '../src/api/television'
import { TelevisionBrowser } from '../src/react/media/TelevisionBrowser'
import { useReactPreferencesStore } from '../src/react/preferences-store'

const series: TelevisionItem = {
  Id: 'tv-series:2:L3Nob3c', Name: 'Drama', Type: 'Series', ImageTags: { Primary: 'series-cover' },
  ChildCount: 2, RecursiveItemCount: 3, IndexNumber: 0, SeriesName: '', ProductionYear: 2026, Overview: '',
}
const season: TelevisionItem = {
  ...series, Id: 'tv-season:2:1:L3Nob3c', Name: '第 1 季', Type: 'Season', IndexNumber: 1,
}
const episode: TelevisionItem = {
  ...series, Id: '5', Name: 'Pilot', Type: 'Episode', ImageTags: {}, SeriesName: 'Drama', IndexNumber: 1,
}

function renderBrowser(overrides: Partial<React.ComponentProps<typeof TelevisionBrowser>> = {}) {
  const props: React.ComponentProps<typeof TelevisionBrowser> = {
    library: 2, search: '', revision: 0, onOpen: vi.fn(), onBrowse: vi.fn(), ...overrides,
  }
  return { ...render(<TelevisionBrowser {...props} />), props }
}

afterEach(() => {
  cleanup()
  useReactPreferencesStore.setState({ hideImages: false })
  vi.restoreAllMocks()
})

describe('React television browser', () => {
  it('opens a series, loads its season and episodes, and returns to the series list', async () => {
    vi.spyOn(televisionApi, 'list').mockResolvedValue({ items: [series], total: 1 })
    vi.spyOn(televisionApi, 'series').mockResolvedValue(series)
    vi.spyOn(televisionApi, 'seasons').mockResolvedValue({ items: [season], total: 1 })
    const episodes = vi.spyOn(televisionApi, 'episodes').mockResolvedValue({ items: [episode], total: 1 })
    const view = renderBrowser()

    await screen.findByText('Drama')
    fireEvent.click(view.container.querySelector(`[data-tv-id="${series.Id}"]`)!)
    expect(view.props.onBrowse).toHaveBeenCalledWith(series.Id, '')

    view.rerender(<TelevisionBrowser {...view.props} series={series.Id} season="" />)
    expect(await screen.findByRole('tab', { name: '第 1 季' })).toHaveAttribute('aria-selected', 'true')
    expect(episodes).toHaveBeenCalledWith(series.Id, season.Id, 0, expect.any(AbortSignal))

    fireEvent.click(view.container.querySelector('[data-tv-id="5"]')!)
    expect(view.props.onOpen).toHaveBeenCalledWith(5)
    fireEvent.click(screen.getByRole('button', { name: '返回电视节目' }))
    expect(view.props.onBrowse).toHaveBeenLastCalledWith('', '')
  })

  it('restores the requested season and chooses a main season when the query is missing or invalid', async () => {
    const special = { ...season, Id: 'tv-season:2:0:L3Nob3c', Name: '特别篇', IndexNumber: 0 }
    const second = { ...season, Id: 'tv-season:2:2:L3Nob3c', Name: '第 2 季', IndexNumber: 2 }
    vi.spyOn(televisionApi, 'series').mockResolvedValue(series)
    vi.spyOn(televisionApi, 'seasons').mockResolvedValue({ items: [special, season, second], total: 3 })
    const episodes = vi.spyOn(televisionApi, 'episodes').mockResolvedValue({ items: [episode], total: 1 })
    const view = renderBrowser({ series: series.Id, season: second.Id })

    expect(await screen.findByRole('tab', { name: '第 2 季' })).toHaveAttribute('aria-selected', 'true')
    expect(episodes).toHaveBeenLastCalledWith(series.Id, second.Id, 0, expect.any(AbortSignal))

    view.rerender(<TelevisionBrowser {...view.props} series={series.Id} season="" />)
    await waitFor(() => expect(episodes).toHaveBeenLastCalledWith(series.Id, season.Id, 0, expect.any(AbortSignal)))
    expect(screen.getByRole('tab', { name: '第 1 季' })).toHaveAttribute('aria-selected', 'true')

    view.rerender(<TelevisionBrowser {...view.props} series={series.Id} season="invalid-season" />)
    await waitFor(() => expect(view.props.onBrowse).toHaveBeenLastCalledWith(series.Id, season.Id))
  })

  it('uses episode Thumb only as a fallback, keeps Primary priority, and hides all images on request', async () => {
    const episodes: TelevisionItem[] = [
      { ...episode, ImageTags: { Thumb: 'episode-thumb' } },
      { ...episode, Id: '6', Name: 'Second', ImageTags: { Primary: 'episode-poster', Thumb: 'other-thumb' } },
      { ...episode, Id: '7', Name: 'Third', ImageTags: {} },
    ]
    vi.spyOn(televisionApi, 'series').mockResolvedValue(series)
    vi.spyOn(televisionApi, 'seasons').mockResolvedValue({ items: [season], total: 1 })
    vi.spyOn(televisionApi, 'episodes').mockResolvedValue({ items: episodes, total: 3 })
    const view = renderBrowser({ series: series.Id })

    await screen.findByText('1. Pilot')
    expect(view.container.querySelector('[data-tv-id="5"] img')?.getAttribute('src'))
      .toBe('/Items/5/Images/Thumb?tag=episode-thumb&maxWidth=320')
    expect(view.container.querySelector('[data-tv-id="6"] img')?.getAttribute('src'))
      .toBe('/Items/6/Images/Primary?tag=episode-poster&maxWidth=320')
    expect(view.container.querySelector('[data-tv-id="7"] img')).toBeNull()

    act(() => useReactPreferencesStore.setState({ hideImages: true }))
    expect(view.container.querySelectorAll('.react-tv-cover img')).toHaveLength(0)
    expect(screen.getByText('Third')).toBeInTheDocument()
  })

  it('keeps loaded rows when a later page fails and retries from the same offset', async () => {
    const second = { ...series, Id: 'tv-series:2:b3RoZXI', Name: 'Second' }
    const list = vi.spyOn(televisionApi, 'list')
      .mockResolvedValueOnce({ items: [series], total: 2 })
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ items: [second], total: 2 })
    const view = renderBrowser()

    await screen.findByText('Drama')
    fireEvent.click(screen.getByRole('button', { name: '加载更多' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('offline')
    expect(view.container.querySelectorAll('[data-tv-id]')).toHaveLength(1)

    fireEvent.click(screen.getByRole('button', { name: /重试/ }))
    await screen.findByText('Second')
    expect(list.mock.calls.map(([library, , offset]) => [library, offset])).toEqual([[2, 0], [2, 1], [2, 1]])
    expect(view.container.querySelectorAll('[data-tv-id]')).toHaveLength(2)
  })

  it('discards a stale library response and rejects a series from another library', async () => {
    let resolveOld!: (result: { items: TelevisionItem[]; total: number }) => void
    let oldSignal: AbortSignal | undefined
    const other = { ...series, Id: 'tv-series:3:b3RoZXI', Name: 'Other library' }
    const list = vi.spyOn(televisionApi, 'list').mockImplementation((library, _search, _offset, signal) => {
      if (library === 2) {
        oldSignal = signal
        return new Promise((resolve) => { resolveOld = resolve })
      }
      return Promise.resolve({ items: [other], total: 1 })
    })
    const view = renderBrowser()
    await waitFor(() => expect(resolveOld).toBeTypeOf('function'))
    view.rerender(<TelevisionBrowser {...view.props} library={3} />)
    await screen.findByText('Other library')
    expect(oldSignal?.aborted).toBe(true)

    await act(async () => { resolveOld({ items: [series], total: 1 }) })
    expect(screen.queryByText('Drama')).not.toBeInTheDocument()
    expect(list).toHaveBeenCalledTimes(2)

    const readSeries = vi.spyOn(televisionApi, 'series')
    view.rerender(<TelevisionBrowser {...view.props} library={3} series={series.Id} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('剧集不属于当前媒体库')
    expect(readSeries).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '返回电视节目' }))
    expect(view.props.onBrowse).toHaveBeenCalledWith('', '')
  })
})
