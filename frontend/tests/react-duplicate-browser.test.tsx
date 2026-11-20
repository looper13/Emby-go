import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { ApiError } from '../src/api/client'
import {
  duplicatesApi,
  type DuplicateDeletePreview,
  type DuplicateGroups,
  type DuplicateMembers,
  type DuplicateMovie,
  type DuplicateSelection,
  type DuplicateSelectionItem,
  type DuplicateStrmContents,
} from '../src/api/duplicates'
import type { LibraryDto } from '../src/api/contracts'
import { DuplicateBrowser } from '../src/react/pages/DuplicateBrowser'

const library: LibraryDto = {
  Id: 7, Name: 'Films', Path: '/media', Paths: ['/media'], Type: 'movies',
  SourceType: 'nfo_directory', ProxyEnabled: false, ResolveRedirects: false, Revision: 3, ProxyHeaders: {},
}
const keeper: DuplicateSelectionItem['keep'] = {
  id: 500, library_id: 7, library_name: 'Films', number: 'ALDN-001', title: 'Keep',
  source_path: '/films/keep.strm', status: 'success', additional_parts: [],
  created_at: '2020-01-01T00:00:00Z', path_length: 16,
}
const preview: DuplicateDeletePreview = {
  movie_id: 1, alist_url: 'http://alist.fixture', preview_token: 'fresh-preview', expires_at: 9999999999,
  files: [{ local_path: '/films/copy-1.strm', remote_path: '/115/movie.mp4', removed: false, remote: { size: 100 } }],
}
const emptyStrm: DuplicateStrmContents = { movie_id: 1, files: [] }

function item(id: number): DuplicateSelectionItem {
  const movie: DuplicateSelectionItem['movie'] = {
    ...keeper, id, title: `Copy ${id}`, source_path: `/films/copy-${id}.strm`,
    created_at: '2024-01-01T00:00:00Z', path_length: 20 + id,
  }
  return { key: 'ALDN001', movie, keep: keeper, selection_token: `signed-${id}` }
}

function selection(count = 2): DuplicateSelection {
  return { rule: 'oldest', library_id: 0, items: Array.from({ length: count }, (_, index) => item(index + 1)), total: count, groups: 1 }
}

function groupReport(): DuplicateGroups {
  return { items: [{ key: 'ALDN001', number: 'ALDN-001', count: 4 }], total: 1, total_movies: 4, extra_copies: 3 }
}

function membersReport(total = 4): DuplicateMembers {
  const copies = Array.from({ length: total - 1 }, (_, index): DuplicateMovie => item(index + 1).movie)
  return { items: [...copies.slice(0, 20), { ...keeper, additional_parts: [] }], total }
}

function renderBrowser() {
  return render(<MemoryRouter><DuplicateBrowser libraries={[library]} /></MemoryRouter>)
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

function bulkStatus(dialog: HTMLElement) {
  return within(dialog).getByRole('status')
}

function statusContaining(text: string) {
  const status = screen.getAllByRole('status').find(element => element.textContent?.includes(text))
  if (!status) throw new Error(`Status not found: ${text}`)
  return status
}

afterEach(() => vi.restoreAllMocks())

describe('React duplicate browser', () => {
  it('loads paged STRM data and keeps member pagination and full-selection exclusions', async () => {
    const user = userEvent.setup()
    vi.spyOn(duplicatesApi, 'groups').mockResolvedValue(groupReport())
    const members = vi.spyOn(duplicatesApi, 'members').mockImplementation(async (_libraryId, _key, _limit, offset) => ({
      items: offset ? [item(21).movie, { ...keeper, additional_parts: [] }] : membersReport(22).items,
      total: 22,
    }))
    const select = vi.spyOn(duplicatesApi, 'selection').mockResolvedValue(selection(21))
    const strm = vi.spyOn(duplicatesApi, 'strm').mockResolvedValue(emptyStrm)
    renderBrowser()

    await user.click(screen.getByRole('button', { name: '开始查重' }))
    await user.click(await screen.findByRole('button', { name: '全量选择多余副本' }))
    await waitFor(() => expect(statusContaining('已全量选中 21 份')).toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: '查看位置' }))
    const checkboxes = await screen.findAllByLabelText('批量待删副本')
    expect(checkboxes[0]).toBeChecked()
    await user.click(checkboxes[0]!)
    expect(screen.getByRole('button', { name: '批量删除 20 份' })).toBeEnabled()
    expect(strm).toHaveBeenCalledWith(1, 0, 'ALDN001', expect.any(AbortSignal))

    await user.click(screen.getByRole('button', { name: '下一页影片' }))
    await waitFor(() => expect(members.mock.calls.at(-1)?.[3]).toBe(20))
    expect(await screen.findByLabelText('批量待删副本')).toBeChecked()
    expect(await screen.findByText('按规则保留此副本')).toBeInTheDocument()
    await user.selectOptions(screen.getByLabelText('每组保留规则'), 'longest')
    expect(select).toHaveBeenCalledOnce()
    expect(screen.queryByText(/已全量选中/)).not.toBeInTheDocument()
  })

  it('shows STRM read failures, retries them, and renders file contents as inert text', async () => {
    const user = userEvent.setup()
    vi.spyOn(duplicatesApi, 'groups').mockResolvedValue({
      ...groupReport(), items: [{ key: 'ALDN001', number: 'ALDN-001', count: 2 }], total: 1, total_movies: 2, extra_copies: 1,
    })
    vi.spyOn(duplicatesApi, 'members').mockResolvedValue({ items: [item(1).movie, { ...keeper, additional_parts: [] }], total: 2 })
    const content = 'https://media.fixture/private.mp4?token=fixture\n<script>window.fixture = true</script>'
    const strm = vi.spyOn(duplicatesApi, 'strm')
      .mockRejectedValueOnce(new DOMException('request timeout', 'TimeoutError'))
      .mockResolvedValue({ movie_id: 1, files: [{ local_path: '/films/copy-1.strm', content, error: '' }] })
    renderBrowser()

    await user.click(screen.getByRole('button', { name: '开始查重' }))
    await user.click(await screen.findByRole('button', { name: '查看位置' }))
    await waitFor(() => expect(strm).toHaveBeenCalledTimes(2))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('读取 STRM 内容超时')
    const copyViewer = alert.closest('.react-duplicate-strm') as HTMLElement
    await user.click(within(copyViewer).getByRole('button', { name: '重试读取' }))
    await waitFor(() => expect(copyViewer.querySelector('pre')?.textContent).toBe(content))
    expect(copyViewer.querySelector('pre script')).toBeNull()
    expect(copyViewer.querySelector('.react-duplicate-strm a')).toBeNull()
    expect(strm).toHaveBeenCalledTimes(3)
  })

  it('aborts STRM reads when a duplicate group collapses and ignores the late response', async () => {
    const user = userEvent.setup()
    vi.spyOn(duplicatesApi, 'groups').mockResolvedValue({
      ...groupReport(), items: [{ key: 'ALDN001', number: 'ALDN-001', count: 2 }], total: 1, total_movies: 2, extra_copies: 1,
    })
    vi.spyOn(duplicatesApi, 'members').mockResolvedValue({ items: [item(1).movie, { ...keeper, additional_parts: [] }], total: 2 })
    const pending = deferred<DuplicateStrmContents>()
    const strm = vi.spyOn(duplicatesApi, 'strm').mockReturnValueOnce(pending.promise).mockResolvedValue(emptyStrm)
    renderBrowser()

    await user.click(screen.getByRole('button', { name: '开始查重' }))
    await user.click(await screen.findByRole('button', { name: '查看位置' }))
    await waitFor(() => expect(strm).toHaveBeenCalledTimes(2))
    await user.click(screen.getByRole('button', { name: '收起' }))
    expect(strm.mock.calls[0]![3].aborted).toBe(true)

    pending.resolve({ movie_id: 1, files: [{ local_path: '/films/copy-1.strm', content: 'stale contents', error: '' }] })
    expect(screen.queryByText('stale contents')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '查看位置' }))
    await waitFor(() => expect(strm).toHaveBeenCalledTimes(4))
    expect(screen.queryByText('stale contents')).not.toBeInTheDocument()
  })

  it('previews each item before serial deletion, records partial failures and retries only failed items', async () => {
    const user = userEvent.setup()
    const groups = vi.spyOn(duplicatesApi, 'groups').mockResolvedValue(groupReport())
    vi.spyOn(duplicatesApi, 'selection').mockResolvedValue(selection(2))
    vi.spyOn(duplicatesApi, 'strm').mockResolvedValue(emptyStrm)
    const previewDelete = vi.spyOn(duplicatesApi, 'previewDelete')
      .mockRejectedValueOnce(new ApiError('remote denied', 502, { removed_remote_paths: ['/115/CD1.mp4'] }))
      .mockResolvedValue(preview)
    const deleteCopy = vi.spyOn(duplicatesApi, 'deleteCopy').mockResolvedValue(null)
    renderBrowser()

    await user.click(screen.getByRole('button', { name: '开始查重' }))
    await user.click(await screen.findByRole('button', { name: '全量选择多余副本' }))
    await user.click(await screen.findByRole('button', { name: '批量删除 2 份' }))
    const dialog = await screen.findByRole('dialog', { name: '批量删除多余副本' })
    expect(dialog).toBeInTheDocument()
    expect(previewDelete).not.toHaveBeenCalled()
    expect(deleteCopy).not.toHaveBeenCalled()

    await user.click(screen.getByLabelText(/我已核对待删清单/))
    await user.click(screen.getByRole('button', { name: '确认批量删除' }))
    await waitFor(() => expect(bulkStatus(dialog)).toHaveTextContent('成功 1，失败 1'))
    expect(previewDelete.mock.calls.map(call => call[0])).toEqual([1, 2])
    expect(previewDelete.mock.calls[1]![4]).toEqual({ keep_id: 500, selection_token: 'signed-2' })
    expect(deleteCopy).toHaveBeenCalledOnce()
    expect(deleteCopy).toHaveBeenCalledWith(2, 0, 'ALDN001', preview, { keep_id: 500, selection_token: 'signed-2' })

    const failedRow = screen.getByText(/remote denied/).closest('[data-duplicate-result]') as HTMLElement
    expect(within(failedRow).getByText(/remote denied/)).toBeInTheDocument()
    expect(within(failedRow).getByText(/\/115\/CD1\.mp4/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '只看失败 1 份' }))
    expect(within(screen.getByLabelText('全量待删及保留副本清单')).getAllByRole('article')).toHaveLength(1)
    await user.click(screen.getByLabelText(/我已核对失败清单/))
    await user.click(screen.getByRole('button', { name: '重新核验并重试失败 1 份' }))
    await waitFor(() => expect(bulkStatus(dialog)).toHaveTextContent('成功 2，失败 0'))
    expect(previewDelete.mock.calls.map(call => call[0])).toEqual([1, 2, 1])
    expect(deleteCopy).toHaveBeenCalledTimes(2)

    await user.click(screen.getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(groups).toHaveBeenCalledTimes(2))
    expect(await screen.findByText('批量删除已完成 2 份，查重统计已刷新。')).toBeInTheDocument()
  })

  it('cancels an in-flight preview on stop and does not start later items', async () => {
    const user = userEvent.setup()
    vi.spyOn(duplicatesApi, 'groups').mockResolvedValue(groupReport())
    vi.spyOn(duplicatesApi, 'selection').mockResolvedValue(selection(3))
    vi.spyOn(duplicatesApi, 'strm').mockResolvedValue(emptyStrm)
    const pending = deferred<DuplicateDeletePreview>()
    const previewDelete = vi.spyOn(duplicatesApi, 'previewDelete').mockReturnValue(pending.promise)
    const deleteCopy = vi.spyOn(duplicatesApi, 'deleteCopy')
    renderBrowser()

    await user.click(screen.getByRole('button', { name: '开始查重' }))
    await user.click(await screen.findByRole('button', { name: '全量选择多余副本' }))
    await user.click(await screen.findByRole('button', { name: '批量删除 3 份' }))
    const dialog = await screen.findByRole('dialog', { name: '批量删除多余副本' })
    await user.click(await within(dialog).findByLabelText(/我已核对待删清单/))
    await user.click(screen.getByRole('button', { name: '确认批量删除' }))
    await waitFor(() => expect(previewDelete).toHaveBeenCalledOnce())
    await user.click(screen.getByRole('button', { name: '停止队列' }))
    expect(previewDelete.mock.calls[0]![3].aborted).toBe(true)
    pending.resolve(preview)
    await waitFor(() => expect(bulkStatus(dialog)).toHaveTextContent('已处理 0 / 3 份'))
    expect(bulkStatus(dialog)).toHaveTextContent('未处理 3 份')
    expect(deleteCopy).not.toHaveBeenCalled()
    expect(previewDelete).toHaveBeenCalledOnce()
  })

  it('finishes the active delete after stop and leaves later copies for an explicit continuation', async () => {
    const user = userEvent.setup()
    vi.spyOn(duplicatesApi, 'groups').mockResolvedValue(groupReport())
    vi.spyOn(duplicatesApi, 'selection').mockResolvedValue(selection(3))
    vi.spyOn(duplicatesApi, 'strm').mockResolvedValue(emptyStrm)
    vi.spyOn(duplicatesApi, 'previewDelete').mockResolvedValue(preview)
    const pending = deferred<unknown>()
    const deleteCopy = vi.spyOn(duplicatesApi, 'deleteCopy').mockReturnValue(pending.promise)
    const previewDelete = vi.mocked(duplicatesApi.previewDelete)
    renderBrowser()

    await user.click(screen.getByRole('button', { name: '开始查重' }))
    await user.click(await screen.findByRole('button', { name: '全量选择多余副本' }))
    await user.click(await screen.findByRole('button', { name: '批量删除 3 份' }))
    const dialog = await screen.findByRole('dialog', { name: '批量删除多余副本' })
    await user.click(await within(dialog).findByLabelText(/我已核对待删清单/))
    await user.click(screen.getByRole('button', { name: '确认批量删除' }))
    await waitFor(() => expect(deleteCopy).toHaveBeenCalledOnce())
    await user.click(screen.getByRole('button', { name: '停止队列' }))
    pending.resolve(undefined)
    await waitFor(() => expect(bulkStatus(dialog)).toHaveTextContent('成功 1，失败 0'))
    expect(bulkStatus(dialog)).toHaveTextContent('未处理 2 份')
    expect(previewDelete).toHaveBeenCalledOnce()
    expect(deleteCopy).toHaveBeenCalledOnce()
    await user.click(await within(dialog).findByLabelText(/我已核对待删清单/))
    await user.click(screen.getByRole('button', { name: '继续处理未完成 2 份' }))
    await waitFor(() => expect(deleteCopy).toHaveBeenCalledTimes(3))
  })
})
