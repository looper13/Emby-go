import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { LibraryDto } from '../src/api/contracts'
import { WallFilters } from '../src/react/media/WallFilters'

const libraries: LibraryDto[] = [
  { Id: 2, Name: '电影库', Path: '/media/movies' },
]

function renderFilters(query = new URLSearchParams(), canManage = true) {
  const handlers = {
    onFilter: vi.fn(),
    onSearch: vi.fn(),
    onCardSize: vi.fn(),
    onClearEntity: vi.fn(),
    onClearAll: vi.fn(),
    onShuffle: vi.fn(),
  }
  render(<WallFilters query={query} libraries={libraries} search="" canManage={canManage} cardSize="normal" {...handlers} />)
  return handlers
}

describe('React media wall filters', () => {
  it('starts expanded, exposes selected states and clears individual conditions', async () => {
    const user = userEvent.setup()
    const handlers = renderFilters(new URLSearchParams('library_id=2&status=pending&scrape=failed&favorite=true&genre=剧情'))

    expect(screen.getByRole('button', { name: '待补录' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: '电影库' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: '清除媒体库：电影库' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '清除刮削：刮削失败' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '清除该筛选' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '清除所有筛选' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '清除媒体库：电影库' }))
    await user.click(screen.getByRole('button', { name: '清除所有筛选' }))
    expect(handlers.onFilter).toHaveBeenCalledWith('library_id', '')
    expect(handlers.onClearAll).toHaveBeenCalledOnce()
  })

  it('keeps search, sort direction, random shuffle and card density controls interactive', async () => {
    const user = userEvent.setup()
    const handlers = renderFilters(new URLSearchParams('sort=title&order=asc'))

    fireEvent.change(screen.getByRole('textbox', { name: '搜索标题 / 番号 / 原名' }), { target: { value: 'ABF-005' } })
    expect(handlers.onSearch).toHaveBeenCalledWith('ABF-005')
    await user.selectOptions(screen.getByRole('combobox', { name: '排序方向' }), 'desc')
    expect(handlers.onFilter).toHaveBeenCalledWith('order', 'desc')
    await user.click(screen.getByRole('button', { name: '大图尺寸' }))
    expect(handlers.onCardSize).toHaveBeenCalledWith('large')
  })

  it('offers the shuffle action for random ordering', async () => {
    const user = userEvent.setup()
    const handlers = renderFilters(new URLSearchParams('sort=random'))

    await user.click(screen.getByRole('button', { name: '换一批' }))
    expect(handlers.onShuffle).toHaveBeenCalledOnce()
  })

  it('does not expose administrative filters to a regular account', () => {
    renderFilters(new URLSearchParams(), false)

    expect(screen.queryByRole('button', { name: '入库状态' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '待补录' })).not.toBeInTheDocument()
    expect(screen.getByRole('group', { name: '卡片尺寸' })).toBeInTheDocument()
  })
})
