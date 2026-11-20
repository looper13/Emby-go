import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import type { MovieDto } from '../src/api/contracts'
import { MediaCard } from '../src/react/media/MediaCard'
import { useReactPreferencesStore } from '../src/react/preferences-store'
import detailFixture from './fixtures/json/item-detail.json'

const fixture = detailFixture.movie as MovieDto

function renderCard(movie: MovieDto = fixture) {
  const onOpen = vi.fn()
  const onQuickPlay = vi.fn()
  const view = render(<MediaCard movie={movie} onOpen={onOpen} onQuickPlay={onQuickPlay} onAction={vi.fn()} />)
  return { ...view, onOpen, onQuickPlay }
}

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  useReactPreferencesStore.setState({ hideImages: false })
  vi.restoreAllMocks()
})

describe('React media poster recovery', () => {
  it('retries the same URL once after 500ms and stops after a successful load', async () => {
    vi.useFakeTimers()
    const { container } = renderCard({ ...fixture, PosterPath: '/poster.jpg', cover_url: '' })
    const first = container.querySelector('img')!
    fireEvent.error(first)
    expect(screen.getByRole('img', { name: fixture.Title })).toHaveTextContent('正在重试…')
    expect(container.querySelector('img')).toBeNull()

    await act(async () => { await vi.advanceTimersByTimeAsync(499) })
    expect(container.querySelector('img')).toBeNull()
    await act(async () => { await vi.advanceTimersByTimeAsync(1) })
    const retry = container.querySelector('img')!
    expect(retry).not.toBe(first)
    expect(retry.src).toContain('/Items/5/Images/Primary')

    fireEvent.load(retry)
    await act(async () => { await vi.advanceTimersByTimeAsync(10000) })
    expect(container.querySelector('img')).toBe(retry)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('tries the fallback first, retries that URL once, then shows a bounded failure state', async () => {
    vi.useFakeTimers()
    const { container } = renderCard({ ...fixture, PosterPath: '/poster.jpg', cover_url: '/fallback.jpg' })
    const primary = container.querySelector('img')!
    fireEvent.error(primary)
    expect(container.querySelector('img')?.src).toBe(new URL('/fallback.jpg', window.location.href).href)

    fireEvent.error(container.querySelector('img')!)
    expect(screen.getByRole('img', { name: fixture.Title })).toHaveTextContent('正在重试…')
    await act(async () => { await vi.advanceTimersByTimeAsync(500) })
    const retry = container.querySelector('img')!
    expect(retry.src).toBe(new URL('/fallback.jpg', window.location.href).href)
    fireEvent.error(retry)
    expect(screen.getByRole('img', { name: fixture.Title })).toHaveTextContent('图片加载失败')
    await act(async () => { await vi.advanceTimersByTimeAsync(10000) })
    expect(container.querySelector('img')).toBeNull()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('cancels a pending retry when the source changes or images are hidden', async () => {
    vi.useFakeTimers()
    const { container, rerender } = renderCard({ ...fixture, PosterPath: '/poster.jpg', cover_url: '' })
    fireEvent.error(container.querySelector('img')!)
    expect(vi.getTimerCount()).toBe(1)

    rerender(<MediaCard movie={{ ...fixture, PosterPath: '/new-poster.jpg', cover_url: '' }} tags={{ Primary: 'new-tag' }}
      onOpen={vi.fn()} onQuickPlay={vi.fn()} onAction={vi.fn()} />)
    expect(vi.getTimerCount()).toBe(0)
    expect(container.querySelector('img')?.src).toContain('/Items/5/Images/Primary?tag=new-tag')

    fireEvent.error(container.querySelector('img')!)
    act(() => useReactPreferencesStore.setState({ hideImages: true }))
    expect(vi.getTimerCount()).toBe(0)
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(container.querySelector('img')).toBeNull()
    expect(screen.getByRole('img', { name: fixture.Title })).toHaveClass('is-hidden')
  })

  it('keeps card activation separate from nested playback actions', () => {
    const { container, onOpen, onQuickPlay } = renderCard({ ...fixture, Status: 'success' })
    const card = container.querySelector('[data-play]')!
    fireEvent.keyDown(card, { key: 'Enter' })
    fireEvent.keyDown(card, { key: ' ' })
    expect(onOpen).toHaveBeenCalledTimes(2)
    fireEvent.keyDown(card.querySelector('strong')!, { key: 'Enter' })
    fireEvent.click(screen.getByRole('button', { name: `播放 ${fixture.Title}` }))
    expect(onOpen).toHaveBeenCalledTimes(2)
    expect(onQuickPlay).toHaveBeenCalledOnce()
  })
})
