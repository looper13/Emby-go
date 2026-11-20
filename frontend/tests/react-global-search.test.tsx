import { afterEach, describe, expect, it, vi } from 'vitest'
import { useState } from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { actorsApi } from '../src/api/actors'
import { adminApi } from '../src/api/admin'
import { catalogApi } from '../src/api/catalog'
import type { LibraryDto, MovieDto } from '../src/api/contracts'
import { ConsoleLayout } from '../src/react/ConsoleLayout'
import { GlobalSearchDialog } from '../src/react/GlobalSearchDialog'
import detailFixture from './fixtures/json/item-detail.json'

const library: LibraryDto = { Id: 7, Name: '星际电影库', Path: '/media/星际', Type: 'movies' }
const movie: MovieDto = { ...detailFixture.movie as MovieDto, id: 101, Title: '星际穿越' }

function SearchHarness({ initiallyOpen = false }: { initiallyOpen?: boolean }) {
  const [open, setOpen] = useState(initiallyOpen)
  const location = useLocation()
  return <>
    <button type="button" onClick={() => setOpen(true)}>打开搜索</button>
    <output data-testid="search-location">{`${location.pathname}${location.search}`}</output>
    <GlobalSearchDialog open={open} onClose={() => setOpen(false)} />
  </>
}

function LayoutHarness() {
  return <MemoryRouter initialEntries={['/items']}>
    <Routes><Route element={<ConsoleLayout />}><Route path="/items" element={<div>媒体墙</div>} /></Route></Routes>
  </MemoryRouter>
}

afterEach(() => {
  cleanup()
  document.body.style.overflow = ''
  vi.restoreAllMocks()
})

describe('React global search', () => {
  it('loads library shortcuts, returns focus on close, and navigates to a selected library', async () => {
    const user = userEvent.setup()
    const loadLibraries = vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [library], total: 1 })
    render(<MemoryRouter initialEntries={['/items']}><SearchHarness /></MemoryRouter>)

    const trigger = screen.getByRole('button', { name: '打开搜索' })
    await user.click(trigger)
    expect(await screen.findByRole('dialog', { name: '全局搜索' })).toBeInTheDocument()
    expect(document.body.style.overflow).toBe('hidden')
    expect(await screen.findByRole('option', { name: /星际电影库/ })).toBeInTheDocument()
    expect(loadLibraries).toHaveBeenCalledOnce()

    await user.click(screen.getByRole('option', { name: /星际电影库/ }))
    await waitFor(() => expect(screen.getByTestId('search-location')).toHaveTextContent('/items?library_id=7'))
    expect(screen.queryByRole('dialog', { name: '全局搜索' })).not.toBeInTheDocument()
    await waitFor(() => expect(trigger).toHaveFocus())
    expect(document.body.style.overflow).toBe('')
  })

  it('searches movies and actors, matches library paths, and confirms the highlighted result with Enter', async () => {
    const user = userEvent.setup()
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [library], total: 1 })
    const searchMovies = vi.spyOn(catalogApi, 'items').mockResolvedValue({ items: [movie], total: 1, image_tags: {} })
    const searchActors = vi.spyOn(actorsApi, 'list').mockResolvedValue({
      items: [{ name: '星际演员', movie_count: 3, has_image: false, image_tag: '' }],
      total: 1, stats: { total: 1, ready: 0, missing: 1 },
    })
    render(<MemoryRouter initialEntries={['/items']}><SearchHarness initiallyOpen /></MemoryRouter>)

    const input = await screen.findByRole('textbox', { name: '全局搜索' })
    await user.type(input, '星际')
    expect(await screen.findByRole('option', { name: /星际穿越/ })).toBeInTheDocument()
    expect(await screen.findByRole('option', { name: /星际演员/ })).toBeInTheDocument()
    expect(await screen.findByRole('option', { name: /星际电影库/ })).toBeInTheDocument()
    await waitFor(() => expect(searchMovies).toHaveBeenCalledOnce())
    expect(searchMovies.mock.calls[0]?.[0]).toEqual(new URLSearchParams({ search: '星际', limit: '6' }))
    expect(searchActors).toHaveBeenCalledWith({ library_id: 0, search: '星际', status: 'all', offset: 0, limit: 4 }, expect.any(AbortSignal))

    fireEvent.keyDown(input, { key: 'ArrowDown', cancelable: true })
    expect(screen.getByRole('option', { name: /星际演员/ })).toHaveAttribute('aria-selected', 'true')
    expect(input).toHaveAttribute('aria-activedescendant', 'global-search-result-1')
    fireEvent.keyDown(input, { key: 'Enter', cancelable: true })
    await waitFor(() => expect(screen.getByTestId('search-location')).toHaveTextContent('/items?person=%E6%98%9F%E9%99%85%E6%BC%94%E5%91%98'))
    expect(screen.queryByRole('dialog', { name: '全局搜索' })).not.toBeInTheDocument()
  })

  it('opens from the Ctrl/Command+K shortcut and from the top-bar button', async () => {
    const user = userEvent.setup()
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [], total: 0 })
    render(<LayoutHarness />)

    fireEvent.keyDown(window, { key: 'k', ctrlKey: true, cancelable: true })
    expect(await screen.findByRole('dialog', { name: '全局搜索' })).toBeInTheDocument()
    fireEvent.keyDown(screen.getByRole('textbox', { name: '全局搜索' }), { key: 'Escape', cancelable: true })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '全局搜索' })).not.toBeInTheDocument())

    await user.click(screen.getByRole('button', { name: '全局搜索 (Ctrl + K)' }))
    expect(await screen.findByRole('dialog', { name: '全局搜索' })).toBeInTheDocument()
  })

  it('keeps Tab focus inside the dialog and restores focus after Escape', async () => {
    const user = userEvent.setup()
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [], total: 0 })
    render(<MemoryRouter initialEntries={['/items']}><SearchHarness /></MemoryRouter>)

    const trigger = screen.getByRole('button', { name: '打开搜索' })
    await user.click(trigger)
    const input = await screen.findByRole('textbox', { name: '全局搜索' })
    const close = screen.getByRole('button', { name: '关闭搜索' })
    expect(input).toHaveFocus()

    await user.tab()
    expect(close).toHaveFocus()
    await user.tab()
    expect(input).toHaveFocus()
    await user.keyboard('{Shift>}{Tab}{/Shift}')
    expect(close).toHaveFocus()

    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog', { name: '全局搜索' })).not.toBeInTheDocument()
    await waitFor(() => expect(trigger).toHaveFocus())
  })
})
