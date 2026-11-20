import { useEffect, useState, type ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { AppLoading } from './AppLoading'
import { useReactAuthStore } from './auth-store'
import { sanitizeReturnTo } from '../router/legacy-url'

interface Props {
  children: ReactNode
  adminOnly?: boolean
}

interface GateResult {
  token: string | null
  settled: boolean
  permitted: boolean
}

export function RequireAuth({ children, adminOnly = false }: Props) {
  const token = useReactAuthStore((state) => state.token)
  const administrator = useReactAuthStore((state) => state.user?.Policy.IsAdministrator === true)
  const checkSession = useReactAuthStore((state) => state.checkSession)
  const location = useLocation()
  const [result, setResult] = useState<GateResult>({ token: null, settled: false, permitted: false })

  useEffect(() => {
    let active = true
    if (!token) {
      setResult({ token: null, settled: true, permitted: false })
      return () => { active = false }
    }

    setResult({ token, settled: false, permitted: false })
    void checkSession().then((permitted) => {
      if (!active) return
      setResult({ token, settled: true, permitted })
    }).catch(() => {
      if (active) setResult({ token, settled: true, permitted: false })
    })
    return () => { active = false }
  }, [token, checkSession])

  if (result.token !== token || !result.settled) return <AppLoading />
  if (!result.permitted) {
    const returnTo = sanitizeReturnTo(`${location.pathname}${location.search}`)
    const search = returnTo ? `?returnTo=${encodeURIComponent(returnTo)}` : ''
    return <Navigate replace to={`/login${search}`} />
  }
  if (adminOnly && !administrator) return <Navigate replace to="/items" />
  return children
}
