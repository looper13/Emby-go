import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { adminApi } from '../src/api/admin'
import { catalogApi } from '../src/api/catalog'
import type { MovieDto } from '../src/api/contracts'
import { useReactAuthStore } from '../src/react/auth-store'
import { useReactTasksStore } from '../src/react/tasks-store'
import { OverviewPage } from '../src/react/pages/basic-pages'
import authFixture from './fixtures/json/auth-login.json'
import detailFixture from './fixtures/json/item-detail.json'

function signIn() {
  useReactAuthStore.setState({ token: 'fixture-token', user: authFixture.User, phase: 'authenticated' })
}

function mockOverview({ latestMovies = [], status = { success: 0, manual: 0, pending: 0, incompatible: 0 } }: {
  latestMovies?: MovieDto[]
  status?: { success: number; manual: number; pending: number; incompatible: number }
} = {}) {
  vi.spyOn(adminApi, 'libraries').mockResolvedValue({
    items: [{ Id: 7, Name: 'Films', Path: '/media/films', Type: 'movies' }], total: 1,
  })
  vi.spyOn(catalogApi, 'successTotal').mockResolvedValue(status.success)
  const statusRead = vi.spyOn(catalogApi, 'status').mockResolvedValue(status)
  const resumeRead = vi.spyOn(catalogApi, 'resume').mockResolvedValue([])
  vi.spyOn(catalogApi, 'latest').mockResolvedValue({
    items: latestMovies, total: latestMovies.length, image_tags: {}, userdata: {},
  })
  return { statusRead, resumeRead }
}

function renderOverview() {
  return render(<MemoryRouter initialEntries={['/overview']}><OverviewPage /></MemoryRouter>)
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useReactAuthStore.setState({ token: null, user: null, initialized: null, phase: 'anonymous', error: '', submitting: false, expiration: 0 })
  useReactTasksStore.setState({ mutationBusy: false })
})

describe('React overview page', () => {
  it('shows the empty archive state instead of four zero-count rows', async () => {
    mockOverview()
    signIn()
    renderOverview()

    expect(await screen.findByText('档案为空')).toBeInTheDocument()
    expect(screen.getByText('添加媒体库并开始扫描。')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('keeps the latest shelf link scoped to newest-first media', async () => {
    const movie = detailFixture.movie as MovieDto
    mockOverview({ latestMovies: [movie], status: { success: 1, manual: 0, pending: 0, incompatible: 0 } })
    signIn()
    renderOverview()

    expect(await screen.findByRole('region', { name: '最新入库' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /查看全部/ })).toHaveAttribute('href', '/items?sort=datecreated&order=desc')
  })

  it('runs a full reindex through the application task store', async () => {
    const user = userEvent.setup()
    mockOverview()
    signIn()
    const run = vi.spyOn(useReactTasksStore.getState(), 'run').mockResolvedValue(true)
    renderOverview()

    await user.click(await screen.findByRole('button', { name: /全量重建索引/ }))
    expect(run).toHaveBeenCalledWith('reindex')
  })

  it('keeps the overview after a refresh error and clears the single alert on retry', async () => {
    const user = userEvent.setup()
    const status = { success: 1, manual: 0, pending: 0, incompatible: 0 }
    const { statusRead } = mockOverview({ status })
    signIn()
    renderOverview()

    expect(await screen.findByRole('table')).toBeInTheDocument()
    statusRead.mockRejectedValueOnce(new Error('刷新失败')).mockResolvedValueOnce(status)
    await user.click(screen.getByRole('button', { name: '刷新' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('刷新失败')
    expect(screen.getAllByRole('alert')).toHaveLength(1)
    expect(screen.getByRole('table')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '刷新' }))
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
    expect(screen.getByRole('table')).toBeInTheDocument()
    expect(statusRead).toHaveBeenCalledTimes(3)
  })

  it('cancels the previous user resume request when the signed-in user changes', async () => {
    const { resumeRead } = mockOverview()
    const requests: Array<{ userId: string | number | undefined; signal?: AbortSignal }> = []
    resumeRead.mockImplementation((userId, signal) => {
      requests.push({ userId, signal })
      if (String(userId) !== '1') return Promise.resolve([])
      return new Promise((_resolve, reject) => {
        const abort = () => reject(new DOMException('Aborted', 'AbortError'))
        if (signal?.aborted) abort()
        else signal?.addEventListener('abort', abort, { once: true })
      })
    })
    signIn()
    renderOverview()

    await waitFor(() => expect(requests).toHaveLength(1))
    await act(async () => {
      useReactAuthStore.setState({ user: { ...authFixture.User, Id: '7', Name: 'viewer' } })
    })
    await waitFor(() => expect(requests).toHaveLength(2))
    await waitFor(() => expect(screen.getByText('档案为空')).toBeInTheDocument())
    expect(requests[0]?.signal?.aborted).toBe(true)
    expect(requests[1]?.userId).toBe('7')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
