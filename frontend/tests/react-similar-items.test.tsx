import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { SimilarDto } from '../src/api/contracts'
import { SimilarItems } from '../src/react/media/SimilarItems'

const items: SimilarDto[] = Array.from({ length: 12 }, (_, index) => ({
  Id: String(index + 1), Name: `Movie ${index + 1}`, ProductionYear: 2024,
  RunTimeTicks: 5_400_000_000, CommunityRating: 7.5, ImageTags: { Primary: `tag-${index + 1}` },
}))

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function gallery(clientWidth = 400, scrollWidth = 1738) {
  let onResize: (() => void) | undefined
  const disconnect = vi.fn()
  vi.stubGlobal('ResizeObserver', class {
    constructor(callback: () => void) { onResize = callback }
    observe = vi.fn()
    disconnect = disconnect
  })
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false })))
  const onOpen = vi.fn()
  const view = render(<SimilarItems items={items} hideImages={false} onOpen={onOpen} />)
  const row = view.container.querySelector('.react-detail-similar-row') as HTMLDivElement
  Object.defineProperties(row, {
    clientWidth: { configurable: true, get: () => clientWidth },
    scrollWidth: { configurable: true, get: () => scrollWidth },
  })
  row.scrollBy = vi.fn()
  const resize = () => act(() => onResize?.())
  resize()
  return { view, row, onOpen, resize, disconnect, setWidths: (client: number, total: number) => { clientWidth = client; scrollWidth = total; resize() } }
}

describe('React similar items', () => {
  it('shows navigation only when needed and disables the arrow at each end', () => {
    const { row } = gallery()
    expect(screen.getByRole('button', { name: '上一组相似影片' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '下一组相似影片' })).toBeEnabled()

    row.scrollLeft = row.scrollWidth - row.clientWidth
    fireEvent.scroll(row)
    expect(screen.getByRole('button', { name: '上一组相似影片' })).toBeEnabled()
    expect(screen.getByRole('button', { name: '下一组相似影片' })).toBeDisabled()

    Object.defineProperties(row, { clientWidth: { configurable: true, value: 2000 }, scrollWidth: { configurable: true, value: 2000 } })
    act(() => fireEvent.scroll(row))
    expect(screen.queryByRole('button', { name: '上一组相似影片' })).not.toBeInTheDocument()
  })

  it('scrolls by one visible row in either direction without activating a recommendation', async () => {
    const user = userEvent.setup()
    const { row, onOpen } = gallery()

    await user.click(screen.getByRole('button', { name: '下一组相似影片' }))
    expect(row.scrollBy).toHaveBeenLastCalledWith({ left: 400, behavior: 'smooth' })
    row.scrollLeft = 400
    fireEvent.scroll(row)
    await user.click(screen.getByRole('button', { name: '上一组相似影片' }))
    expect(row.scrollBy).toHaveBeenLastCalledWith({ left: -400, behavior: 'smooth' })
    expect(onOpen).not.toHaveBeenCalled()

    await user.click(screen.getByText('Movie 1', { exact: true }))
    expect(onOpen).toHaveBeenCalledWith('1')
  })

  it('rechecks overflow after resize and asynchronous items, then disconnects the observer', () => {
    const { view, setWidths, disconnect } = gallery()
    expect(screen.getByRole('button', { name: '下一组相似影片' })).toBeInTheDocument()
    setWidths(2000, 1738)
    expect(screen.queryByRole('button', { name: '下一组相似影片' })).not.toBeInTheDocument()
    setWidths(390, 1738)
    expect(screen.getByRole('button', { name: '下一组相似影片' })).toBeInTheDocument()

    view.rerender(<SimilarItems items={[]} hideImages={false} onOpen={vi.fn()} />)
    expect(view.container.querySelector('.react-detail-similar-row')).toBeNull()
    view.rerender(<SimilarItems items={items} hideImages={false} onOpen={vi.fn()} />)
    setWidths(390, 1738)
    expect(screen.getAllByRole('button', { name: /Movie/ })).toHaveLength(items.length)
    view.unmount()
    expect(disconnect).toHaveBeenCalled()
  })

  it('uses immediate scrolling for reduced motion and never loads images when hidden', async () => {
    const user = userEvent.setup()
    const { view, row } = gallery()
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true })))
    await user.click(screen.getByRole('button', { name: '下一组相似影片' }))
    expect(row.scrollBy).toHaveBeenLastCalledWith({ left: 400, behavior: 'instant' })

    view.rerender(<SimilarItems items={items} hideImages onOpen={vi.fn()} />)
    expect(view.container.querySelectorAll('img')).toHaveLength(0)
    expect(view.container.textContent).toContain('Movie 1')
  })
})
