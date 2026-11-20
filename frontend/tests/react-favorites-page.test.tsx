import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { favoritesApi, type FavoriteItem } from '../src/api/favorites'
import { useReactAuthStore } from '../src/react/auth-store'
import { FavoritesPage } from '../src/react/pages/basic-pages'
import authFixture from './fixtures/json/auth-login.json'

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="favorites-location">{`${location.pathname}${location.search}`}</output>
}

function Harness({ entry }: { entry: string }) {
  return <MemoryRouter initialEntries={[entry]}>
    <LocationProbe />
    <Routes><Route path="/favorites" element={<FavoritesPage />} /></Routes>
  </MemoryRouter>
}

function favorite(id: string, name: string): FavoriteItem {
  return { Id: id, Name: name, Type: 'Movie', ImageTags: {}, UserData: { IsFavorite: true }, ParentId: '', SeriesId: '' }
}

function signIn(token: string, id = '2') {
  useReactAuthStore.setState({
    token,
    user: { ...authFixture.User, Id: id },
    phase: 'authenticated',
  })
}

afterEach(() => {
  useReactAuthStore.setState({ token: null, user: null, initialized: null, phase: 'anonymous', error: '', submitting: false, expiration: 0 })
  vi.restoreAllMocks()
})

describe('React favorites page', () => {
  it('switches favorite types and applies the search through the URL', async () => {
    signIn('fixture-token')
    const list = vi.spyOn(favoritesApi, 'list').mockResolvedValue({ items: [], total: 0 })
    const user = userEvent.setup()

    render(<Harness entry="/favorites" />)
    await screen.findByText('暂无收藏。')

    await user.click(screen.getByRole('tab', { name: '演员' }))
    await waitFor(() => expect(screen.getByTestId('favorites-location')).toHaveTextContent('/favorites?type=Person'))
    await waitFor(() => expect(list).toHaveBeenLastCalledWith('Person', '', 0, expect.any(AbortSignal)))

    await user.type(screen.getByRole('searchbox', { name: '搜索收藏' }), 'actor')
    await user.click(screen.getByRole('button', { name: '搜索' }))
    await waitFor(() => expect(screen.getByTestId('favorites-location')).toHaveTextContent('/favorites?type=Person&search=actor'))
    await waitFor(() => expect(list).toHaveBeenLastCalledWith('Person', 'actor', 0, expect.any(AbortSignal)))
  })

  it('returns to the previous valid page after removing the last item on a later page', async () => {
    signIn('fixture-token')
    const removed = favorite('5', '最后一条收藏')
    const remaining = favorite('4', '仍保留的收藏')
    const list = vi.spyOn(favoritesApi, 'list')
      .mockResolvedValueOnce({ items: [removed], total: 49 })
      .mockResolvedValueOnce({ items: [removed], total: 49 })
      .mockResolvedValueOnce({ items: [], total: 48 })
      .mockResolvedValue({ items: [remaining], total: 48 })
    const remove = vi.spyOn(favoritesApi, 'set').mockResolvedValue(false)
    const user = userEvent.setup()

    render(<Harness entry="/favorites" />)
    await screen.findByText('最后一条收藏')
    await user.click(screen.getByRole('button', { name: '下一页' }))
    await waitFor(() => expect(screen.getByTestId('favorites-location')).toHaveTextContent('/favorites?offset=48'))
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2))

    await user.click(screen.getByRole('button', { name: '取消收藏 · 最后一条收藏' }))
    await screen.findByText('仍保留的收藏')
    await waitFor(() => expect(screen.getByTestId('favorites-location')).toHaveTextContent('/favorites'))
    expect(remove).toHaveBeenCalledExactlyOnceWith('5', false)
    expect(list.mock.calls.map(([type, search, offset]) => [type, search, offset])).toEqual([
      ['Movie,Episode', '', 0],
      ['Movie,Episode', '', 48],
      ['Movie,Episode', '', 48],
      ['Movie,Episode', '', 0],
    ])
  })

  it('clears the previous account list and ignores its delayed refresh after a token change', async () => {
    signIn('first-token')
    let finishOldRequest!: (value: { items: FavoriteItem[]; total: number }) => void
    let oldSignal: AbortSignal | undefined
    const list = vi.spyOn(favoritesApi, 'list')
      .mockResolvedValueOnce({ items: [favorite('1', '旧账号收藏')], total: 1 })
      .mockImplementationOnce((_type, _search, _offset, signal) => {
        oldSignal = signal
        return new Promise(resolve => { finishOldRequest = resolve })
      })
      .mockResolvedValueOnce({ items: [favorite('2', '新账号收藏')], total: 1 })
    const user = userEvent.setup()

    render(<Harness entry="/favorites" />)
    await screen.findByText('旧账号收藏')
    await user.click(screen.getByRole('button', { name: '刷新收藏' }))
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2))

    act(() => signIn('second-token', '7'))
    await screen.findByText('新账号收藏')
    expect(oldSignal?.aborted).toBe(true)
    expect(screen.queryByText('旧账号收藏')).not.toBeInTheDocument()

    await act(async () => finishOldRequest({ items: [favorite('1', '旧账号收藏')], total: 1 }))
    expect(screen.getByText('新账号收藏')).toBeInTheDocument()
    expect(screen.queryByText('旧账号收藏')).not.toBeInTheDocument()
  })
})
