import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useLocation, useNavigate, useNavigationType } from 'react-router-dom'

export interface ViewPosition {
  scrollY: number
  wallCount: number
  focusID: string
}

export interface ViewHistory {
  target: ViewPosition | null
  backFromItem: (fallback: string) => void
  backTo: (fallback: string) => void
}

interface Props {
  children: ReactNode
  wallCount?: () => number
}

interface RouteSnapshot {
  key: string
  path: string
}

const ViewHistoryContext = createContext<ViewHistory | null>(null)

function requestFrame(callback: FrameRequestCallback): number {
  if (typeof window.requestAnimationFrame === 'function') return window.requestAnimationFrame(callback)
  return window.setTimeout(() => callback(Date.now()), 0)
}

function cancelFrame(frame: number) {
  if (typeof window.cancelAnimationFrame === 'function') window.cancelAnimationFrame(frame)
  else window.clearTimeout(frame)
}

function isCurrentHistoryEntry(routeKey: string): boolean {
  const historyKey = window.history.state?.key
  return typeof historyKey !== 'string' || historyKey === routeKey
}

export function readViewPosition(value: unknown): ViewPosition | null {
  if (typeof value !== 'object' || value === null) return null
  const position = value as Partial<ViewPosition>
  return typeof position.scrollY === 'number' && Number.isFinite(position.scrollY) && position.scrollY >= 0
    && typeof position.wallCount === 'number' && Number.isSafeInteger(position.wallCount) && position.wallCount >= 0
    && typeof position.focusID === 'string' ? position as ViewPosition : null
}

export function saveViewPosition(position: ViewPosition) {
  window.history.replaceState({ ...window.history.state, __embyView: position }, '')
}

export function ViewHistoryProvider({ children, wallCount = () => 0 }: Props) {
  const location = useLocation()
  const navigationType = useNavigationType()
  const navigate = useNavigate()
  const [target, setTarget] = useState<ViewPosition | null>(null)
  const currentPosition = useRef(0)
  const positionByKey = useRef(new Map<string, number>())
  const pathByPosition = useRef(new Map<number, string>())
  const visited = useRef(new Set<number>())
  const initialized = useRef(false)
  const popTarget = useRef<ViewPosition | null>(null)
  const route = useRef<RouteSnapshot>({ key: location.key, path: `${location.pathname}${location.search}` })
  const wallCountRef = useRef(wallCount)
  wallCountRef.current = wallCount
  route.current = { key: location.key, path: `${location.pathname}${location.search}` }

  useLayoutEffect(() => {
    if (!initialized.current) {
      initialized.current = true
      currentPosition.current = 0
      positionByKey.current.set(location.key, 0)
    } else if (navigationType === 'PUSH') {
      currentPosition.current += 1
      positionByKey.current.set(location.key, currentPosition.current)
    } else if (navigationType === 'REPLACE') {
      positionByKey.current.set(location.key, currentPosition.current)
    } else {
      const previousPosition = positionByKey.current.get(location.key)
      if (previousPosition !== undefined) currentPosition.current = previousPosition
    }

    const position = currentPosition.current
    pathByPosition.current.set(position, `${location.pathname}${location.search}`)
    visited.current.add(position)
    setTarget(navigationType === 'POP'
      ? popTarget.current ?? readViewPosition(window.history.state?.__embyView)
      : null)
    popTarget.current = null
  }, [location.key, location.pathname, location.search, navigationType])

  const saveCurrent = useCallback(() => {
    if (!isCurrentHistoryEntry(route.current.key)) return
    const activeCard = document.activeElement?.closest<HTMLElement>('.wall-card, .r18-card, .react-r18-card')
    const focusID = activeCard?.dataset.play ?? activeCard?.dataset.r18Id ?? ''
    saveViewPosition({
      scrollY: Math.max(0, window.scrollY),
      wallCount: route.current.path.split('?')[0] === '/items' ? wallCountRef.current() : 0,
      focusID,
    })
  }, [])

  useEffect(() => {
    let frame = 0
    const scheduleSave = () => {
      const routeKey = route.current.key
      if (frame) cancelFrame(frame)
      frame = requestFrame(() => {
        frame = 0
        if (route.current.key === routeKey && isCurrentHistoryEntry(routeKey)) saveCurrent()
      })
    }
    const saveBeforeAction = () => saveCurrent()
    const readPopState = (event: PopStateEvent) => { popTarget.current = readViewPosition(event.state?.__embyView) }
    window.addEventListener('scroll', scheduleSave, { passive: true })
    window.addEventListener('popstate', readPopState)
    window.addEventListener('beforeunload', saveBeforeAction)
    document.addEventListener('focusin', scheduleSave)
    document.addEventListener('click', saveBeforeAction, true)
    document.addEventListener('submit', saveBeforeAction, true)
    return () => {
      if (frame) cancelFrame(frame)
      window.removeEventListener('scroll', scheduleSave)
      window.removeEventListener('popstate', readPopState)
      window.removeEventListener('beforeunload', saveBeforeAction)
      document.removeEventListener('focusin', scheduleSave)
      document.removeEventListener('click', saveBeforeAction, true)
      document.removeEventListener('submit', saveBeforeAction, true)
    }
  }, [saveCurrent])

  useEffect(() => {
    if (!target) return
    const frame = requestFrame(() => {
      window.scrollTo(0, target.scrollY)
      if (!target.focusID) return
      const card = Array.from(document.querySelectorAll<HTMLElement>('.wall-card, .r18-card, .react-r18-card'))
        .find((element) => element.dataset.play === target.focusID || element.dataset.r18Id === target.focusID)
      if (!card) return
      if (!card.hasAttribute('tabindex')) card.tabIndex = -1
      card.focus({ preventScroll: true })
    })
    return () => cancelFrame(frame)
  }, [location.key, target])

  const backFromItem = useCallback((fallback: string) => {
    if (currentPosition.current > 0 && visited.current.has(currentPosition.current - 1)) navigate(-1)
    else navigate(fallback, { replace: true })
  }, [navigate])

  const backTo = useCallback((fallback: string) => {
    const previousPosition = currentPosition.current - 1
    if (previousPosition >= 0 && visited.current.has(previousPosition)
      && pathByPosition.current.get(previousPosition) === fallback) {
      navigate(-1)
    } else {
      navigate(fallback, { replace: true })
    }
  }, [navigate])

  const value = useMemo(() => ({ target, backFromItem, backTo }), [target, backFromItem, backTo])
  return <ViewHistoryContext.Provider value={value}>{children}</ViewHistoryContext.Provider>
}

export function useViewHistory(): ViewHistory {
  const context = useContext(ViewHistoryContext)
  if (!context) throw new Error('useViewHistory must be used within ViewHistoryProvider')
  return context
}
