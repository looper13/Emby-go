import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen, waitFor } from '@testing-library/react'
import { useReactScheduledStore } from '../src/react/scheduled-store'
import { useReactScrapePreviewStore } from '../src/react/scrape-preview-store'
import { scrapeApi, type Candidate, type Inspect, type Preview } from '../src/api/scrape'

const candidate: Candidate = {
  provider: 'metatube', id: 'id-1', number: 'ABP-123', title: 'Example', score: 0.98, thumb: '', exact: true,
}

const preview: Preview = {
  movie_id: 7, query: 'ABP-123', expected_number: 'ABP-123', recommended: 0, candidates: [candidate],
}

const inspect: Inspect = {
  provider: 'metatube', id: 'id-1', title: 'Example', number: 'ABP-123', summary: '', poster: '', score: 0.98,
  release_date: '', runtime: 0, overwrite: true, fields: [], images: [],
}

afterEach(() => {
  useReactScrapePreviewStore.getState().close()
  useReactScheduledStore.getState().reset()
  vi.restoreAllMocks()
})

describe('React transient stores', () => {
  it('notifies subscribers when scheduled task edit state changes and resets it', () => {
    function EditingState() {
      const editingID = useReactScheduledStore((state) => state.editingId)
      return <output data-testid="editing-id">{editingID ?? 'none'}</output>
    }
    render(<EditingState />)

    act(() => useReactScheduledStore.getState().setEditingId(42))
    expect(screen.getByTestId('editing-id')).toHaveTextContent('42')
    act(() => useReactScheduledStore.getState().reset())
    expect(screen.getByTestId('editing-id')).toHaveTextContent('none')
  })

  it('selects the recommended candidate, confirms with the current overwrite choice and closes', async () => {
    const previewRequest = vi.spyOn(scrapeApi, 'preview').mockResolvedValue(preview)
    const inspectRequest = vi.spyOn(scrapeApi, 'inspect').mockResolvedValue(inspect)
    const confirmRequest = vi.spyOn(scrapeApi, 'confirm').mockResolvedValue({
      title: 'Example', images: 3, trailer_downloaded: true, warnings: [],
    })
    const confirmed = vi.fn()
    await useReactScrapePreviewStore.getState().open(7, confirmed)
    await waitFor(() => expect(useReactScrapePreviewStore.getState().inspect?.id).toBe('id-1'))
    await useReactScrapePreviewStore.getState().confirm()

    expect(previewRequest).toHaveBeenCalledWith(7, expect.any(AbortSignal))
    expect(inspectRequest).toHaveBeenCalledWith(7, 'metatube', 'id-1', expect.any(AbortSignal))
    expect(confirmRequest).toHaveBeenCalledWith(7, 'metatube', 'id-1', true)
    expect(confirmed).toHaveBeenCalledOnce()
    expect(useReactScrapePreviewStore.getState().current).toBeNull()
    expect(useReactScrapePreviewStore.getState().writing).toBe(false)
  })

  it('aborts an inspection and ignores its late response after close', async () => {
    let finish!: (value: Inspect) => void
    const inspectRequest = vi.spyOn(scrapeApi, 'inspect').mockImplementation(() => new Promise((resolve) => { finish = resolve }))
    vi.spyOn(scrapeApi, 'preview').mockResolvedValue(preview)
    const opening = useReactScrapePreviewStore.getState().open(7, () => {})
    await waitFor(() => expect(inspectRequest).toHaveBeenCalledOnce())
    const signal = inspectRequest.mock.calls[0]?.[3]
    useReactScrapePreviewStore.getState().close()
    expect(signal?.aborted).toBe(true)
    finish(inspect)
    await opening
    expect(useReactScrapePreviewStore.getState().inspect).toBeNull()
    expect(useReactScrapePreviewStore.getState().current).toBeNull()
  })
})
