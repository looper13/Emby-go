import { useCallback, useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react'
import { usePageRequest } from './use-page-request'

export interface AdminReadState<T> {
  data: T | null
  setData: Dispatch<SetStateAction<T | null>>
  error: string
  loading: boolean
  load: () => Promise<void>
  loadData: () => Promise<T | null>
  active: () => boolean
}

export function useAdminRead<T>(read: (signal: AbortSignal) => Promise<T>): AdminReadState<T> {
  const scope = usePageRequest()
  const readRef = useRef(read)
  const activeRef = useRef(true)
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  readRef.current = read

  useEffect(() => {
    activeRef.current = true
    return () => { activeRef.current = false }
  }, [])

  const loadData = useCallback(async () => {
    if (!activeRef.current) return null
    const current = scope.begin()
    setLoading(true)
    setError('')
    try {
      const result = await readRef.current(current.signal)
      if (!current.isCurrent()) return null
      setData(result)
      return result
    } catch (cause) {
      if (current.isCurrent()) setError(cause instanceof Error ? cause.message : '加载失败')
      return null
    } finally {
      if (current.isCurrent()) setLoading(false)
    }
  }, [scope])
  const load = useCallback(async () => { await loadData() }, [loadData])

  useEffect(() => { void load() }, [load])
  return { data, setData, error, loading, load, loadData, active: () => activeRef.current }
}
