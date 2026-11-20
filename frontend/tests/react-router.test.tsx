import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { HashRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { ViewHistoryProvider, readViewPosition, saveViewPosition, useViewHistory } from '../src/react/view-history'

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="current-route">{`${location.pathname}${location.search}`}</output>
}

function TargetProbe() {
  const { target } = useViewHistory()
  return <output data-testid="view-target">{target ? `${target.scrollY}:${target.wallCount}:${target.focusID}` : 'none'}</output>
}

function WallPage() {
  const navigate = useNavigate()
  return (
    <article className="wall-card" data-play="123" data-testid="wall-card">
      <button type="button" onClick={() => navigate('/item/123?library_id=3')}>打开详情</button>
    </article>
  )
}

function ItemPage() {
  const { backFromItem } = useViewHistory()
  return <button type="button" onClick={() => backFromItem('/items')}>返回媒体墙</button>
}

function Harness({ wallCount = () => 300 }: { wallCount?: () => number }) {
  return (
    <HashRouter>
      <ViewHistoryProvider wallCount={wallCount}>
        <LocationProbe />
        <TargetProbe />
        <Routes>
          <Route path="/items" element={<WallPage />} />
          <Route path="/item/:id" element={<ItemPage />} />
        </Routes>
      </ViewHistoryProvider>
    </HashRouter>
  )
}

describe('React router history', () => {
  it('validates saved view state and preserves unrelated history state', () => {
    window.history.replaceState({ custom: 'kept' }, '', '/admin#/items')
    const position = { scrollY: 480, wallCount: 300, focusID: '123' }

    saveViewPosition(position)

    expect(readViewPosition(window.history.state.__embyView)).toEqual(position)
    expect(window.history.state.custom).toBe('kept')
    expect(readViewPosition({ scrollY: -1, wallCount: 0, focusID: '' })).toBeNull()
    expect(readViewPosition({ scrollY: 0, wallCount: 1.5, focusID: '' })).toBeNull()
  })

  it('returns to the visited wall entry and restores its count and focused card', async () => {
    window.history.replaceState({}, '', '/admin#/items?search=sample')
    const user = userEvent.setup()
    const scroll = vi.spyOn(window, 'scrollTo')
    let restoredState: unknown
    const capturePop = (event: PopStateEvent) => { restoredState = event.state?.__embyView }
    window.addEventListener('popstate', capturePop)
    render(<Harness />)

    await user.click(screen.getByRole('button', { name: '打开详情' }))
    expect(screen.getByTestId('current-route')).toHaveTextContent('/item/123?library_id=3')
    await user.click(screen.getByRole('button', { name: '返回媒体墙' }))

    await waitFor(() => expect(screen.getByTestId('current-route')).toHaveTextContent('/items?search=sample'))
    expect(restoredState).toEqual({ scrollY: 0, wallCount: 300, focusID: '123' })
    await waitFor(() => expect(screen.getByTestId('view-target')).toHaveTextContent('0:300:123'))
    await waitFor(() => expect(scroll).toHaveBeenCalledWith(0, 0))
    expect(scroll).toHaveBeenCalledWith(0, 0)
    await waitFor(() => expect(document.activeElement).toBe(screen.getByTestId('wall-card')))
    window.removeEventListener('popstate', capturePop)
  })

  it('uses a replacement fallback when a detail page has no visited wall entry', async () => {
    window.history.replaceState({}, '', '/admin#/item/123?library_id=7')
    const user = userEvent.setup()
    render(<Harness wallCount={() => 0} />)

    await user.click(screen.getByRole('button', { name: '返回媒体墙' }))

    await waitFor(() => expect(screen.getByTestId('current-route')).toHaveTextContent('/items'))
    expect(window.location.hash).toBe('#/items')
  })
})
