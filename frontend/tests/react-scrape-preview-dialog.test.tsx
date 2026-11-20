import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { catalogApi } from '../src/api/catalog'
import { scrapeApi, type Candidate, type Inspect, type Preview } from '../src/api/scrape'
import { useAppRuntimeStore } from '../src/react/app-store'
import App from '../src/react/App'
import { useReactAuthStore } from '../src/react/auth-store'
import { ScrapePreviewDialog } from '../src/react/ScrapePreviewDialog'
import { useReactMediaWallStore } from '../src/react/media-wall-store'
import { useReactScrapePreviewStore } from '../src/react/scrape-preview-store'
import { useReactTasksStore } from '../src/react/tasks-store'
import authFixture from './fixtures/json/auth-login.json'

const candidates: Candidate[] = [
  { provider: 'metatube', id: 'a', number: 'ABP-123', title: 'Candidate A', score: 0.98, thumb: '', exact: true },
  { provider: 'metatube', id: 'b', number: 'ABP-123', title: 'Candidate B', score: 0.94, thumb: '', exact: false },
]

const preview: Preview = {
  movie_id: 7, query: 'ABP-123', expected_number: 'ABP-123', recommended: 0, candidates,
}

const inspect: Inspect = {
  provider: 'metatube', id: 'a', number: 'ABP-123', title: 'Candidate A', summary: 'Preview plot', poster: '',
  score: 0.98, release_date: '2024-01-01', runtime: 90, overwrite: true,
  fields: [{ key: 'title', label: '标题', old: 'Existing title', next: 'Candidate A', change: true }],
  images: [{ name: 'poster.webp', preview: '', exists: true }],
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((finish) => { resolve = finish })
  return { promise, resolve }
}

beforeEach(() => useReactTasksStore.setState({ mutationBusy: false }))

afterEach(() => {
  cleanup()
  useReactScrapePreviewStore.getState().close()
  useReactMediaWallStore.getState().clear()
  useReactAuthStore.setState({ token: null, user: null, initialized: null, phase: 'anonymous', error: '', submitting: false, expiration: 0 })
  useAppRuntimeStore.getState().clearToasts()
  vi.restoreAllMocks()
  document.body.style.overflow = ''
})

describe('React scrape preview dialog', () => {
  it('shows the recommended inspect, switches candidates, and keeps a failed write retryable', async () => {
    const user = userEvent.setup()
    const inspectRequest = vi.spyOn(scrapeApi, 'inspect').mockImplementation(async (_id, _provider, candidateId) => ({
      ...inspect, id: candidateId, title: candidateId === 'b' ? 'Candidate B' : 'Candidate A',
    }))
    const confirmRequest = vi.spyOn(scrapeApi, 'confirm')
      .mockRejectedValueOnce(new Error('写入失败'))
      .mockResolvedValueOnce({ title: 'Saved title', images: 1, trailer_downloaded: false, warnings: [] })
    const confirmed = vi.fn()
    vi.spyOn(scrapeApi, 'preview').mockResolvedValue(preview)

    await useReactScrapePreviewStore.getState().open(7, confirmed)
    render(<ScrapePreviewDialog />)

    expect(await screen.findByRole('dialog', { name: '刮削预览与确认' })).toBeInTheDocument()
    expect(screen.getByText('Existing title')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Candidate B/ }))
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Candidate B' })).toBeInTheDocument())
    expect(inspectRequest).toHaveBeenCalledTimes(2)
    expect(screen.getByRole('button', { name: /Candidate B/ })).toHaveAttribute('aria-pressed', 'true')

    await user.click(screen.getByRole('checkbox', { name: /强制覆盖/ }))
    const confirmButton = screen.getByRole('button', { name: '确认写入' })
    await user.click(confirmButton)
    await waitFor(() => expect(useAppRuntimeStore.getState().toasts.some((toast) => toast.message === '写入失败')).toBe(true))
    expect(screen.getByRole('dialog', { name: '刮削预览与确认' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Candidate B' })).toBeInTheDocument()
    expect(confirmButton).toBeEnabled()

    await user.click(confirmButton)
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '刮削预览与确认' })).not.toBeInTheDocument())
    expect(confirmRequest).toHaveBeenNthCalledWith(1, 7, 'metatube', 'b', false)
    expect(confirmRequest).toHaveBeenNthCalledWith(2, 7, 'metatube', 'b', false)
    expect(confirmed).toHaveBeenCalledOnce()
  })

  it('traps focus, locks page scrolling, and closes on Escape without a write', async () => {
    const trigger = document.createElement('button')
    document.body.append(trigger)
    trigger.focus()
    document.body.style.overflow = 'auto'
    vi.spyOn(scrapeApi, 'preview').mockResolvedValue({ ...preview, candidates: [] })
    const confirmRequest = vi.spyOn(scrapeApi, 'confirm')

    await useReactScrapePreviewStore.getState().open(7, vi.fn())
    render(<ScrapePreviewDialog />)

    expect(document.body.style.overflow).toBe('hidden')
    expect(screen.getByRole('button', { name: '取消 (Esc)' })).toHaveFocus()
    fireEvent.keyDown(window, { key: 'Tab', shiftKey: true, cancelable: true })
    expect(screen.getByRole('button', { name: '取消' })).toHaveFocus()
    fireEvent.keyDown(window, { key: 'Escape', cancelable: true })

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '刮削预览与确认' })).not.toBeInTheDocument())
    await waitFor(() => expect(trigger).toHaveFocus())
    expect(document.body.style.overflow).toBe('auto')
    expect(confirmRequest).not.toHaveBeenCalled()
    trigger.remove()
  })

  it('cancels an inspected candidate without writing and restores focus', async () => {
    const user = userEvent.setup()
    const trigger = document.createElement('button')
    document.body.append(trigger)
    trigger.focus()
    vi.spyOn(scrapeApi, 'preview').mockResolvedValue(preview)
    vi.spyOn(scrapeApi, 'inspect').mockResolvedValue(inspect)
    const confirmRequest = vi.spyOn(scrapeApi, 'confirm')

    await useReactScrapePreviewStore.getState().open(7, vi.fn())
    render(<ScrapePreviewDialog />)

    expect(await screen.findByRole('heading', { name: 'Candidate A' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '取消' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '刮削预览与确认' })).not.toBeInTheDocument())
    await waitFor(() => expect(trigger).toHaveFocus())
    expect(confirmRequest).not.toHaveBeenCalled()
    trigger.remove()
  })

  it('uses the selected candidate and blocks a second confirmation while the write is pending', async () => {
    const user = userEvent.setup()
    const pending = deferred<{ title: string; images: number; trailer_downloaded: boolean; warnings: string[] }>()
    const confirmRequest = vi.spyOn(scrapeApi, 'confirm').mockReturnValue(pending.promise)
    vi.spyOn(scrapeApi, 'preview').mockResolvedValue(preview)
    vi.spyOn(scrapeApi, 'inspect').mockImplementation(async (_id, _provider, candidateId) => ({
      ...inspect, id: candidateId, title: candidateId === 'b' ? 'Candidate B' : 'Candidate A',
    }))
    const confirmed = vi.fn()

    await useReactScrapePreviewStore.getState().open(7, confirmed)
    render(<ScrapePreviewDialog />)

    await user.click(screen.getByRole('button', { name: /Candidate B/ }))
    expect(await screen.findByRole('heading', { name: 'Candidate B' })).toBeInTheDocument()
    const confirmButton = screen.getByRole('button', { name: '确认写入' })
    fireEvent.click(confirmButton)
    fireEvent.click(confirmButton)

    expect(confirmButton).toBeDisabled()
    expect(confirmRequest).toHaveBeenCalledOnce()
    expect(confirmRequest).toHaveBeenCalledWith(7, 'metatube', 'b', true)

    pending.resolve({ title: 'Saved title', images: 1, trailer_downloaded: false, warnings: [] })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '刮削预览与确认' })).not.toBeInTheDocument())
    expect(confirmed).toHaveBeenCalledOnce()
  })

  it('keeps a failed candidate search visible with an explicit retry', async () => {
    const user = userEvent.setup()
    vi.spyOn(scrapeApi, 'preview').mockRejectedValueOnce(new Error('搜索暂不可用')).mockResolvedValueOnce({ ...preview, candidates: [] })
    const confirmRequest = vi.spyOn(scrapeApi, 'confirm')

    await useReactScrapePreviewStore.getState().open(7, vi.fn())
    render(<ScrapePreviewDialog />)

    expect(await screen.findByRole('alert')).toHaveTextContent('搜索暂不可用')
    await user.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByText(/没有搜到候选/)).toBeInTheDocument()
    expect(confirmRequest).not.toHaveBeenCalled()
  })

  it('mounts the dialog from the authenticated React application runtime', async () => {
    window.location.hash = '#/items'
    useReactAuthStore.setState({ token: 'fixture-token', user: authFixture.User, initialized: true, phase: 'authenticated' })
    vi.spyOn(catalogApi, 'libraries').mockResolvedValue({ items: [], total: 0 })
    vi.spyOn(catalogApi, 'items').mockResolvedValue({ items: [], total: 0, image_tags: {} })
    vi.spyOn(scrapeApi, 'preview').mockResolvedValue(preview)
    vi.spyOn(scrapeApi, 'inspect').mockResolvedValue(inspect)

    render(<App />)
    expect(await screen.findByRole('heading', { level: 1, name: '影视库' })).toBeInTheDocument()
    await act(async () => {
      await useReactScrapePreviewStore.getState().open(7, vi.fn())
    })

    expect(await screen.findByRole('dialog', { name: '刮削预览与确认' })).toBeInTheDocument()
  })
})
