import { useEffect, useRef } from 'react'

export interface PageRequest {
  signal: AbortSignal
  isCurrent: () => boolean
}

export interface PageRequestScope {
  begin: () => PageRequest
  cancel: () => void
}

export function createPageRequestScope(): PageRequestScope {
  let controller: AbortController | undefined

  function cancel() {
    controller?.abort()
    controller = undefined
  }

  function begin(): PageRequest {
    cancel()
    const current = new AbortController()
    controller = current
    return {
      signal: current.signal,
      isCurrent: () => controller === current && !current.signal.aborted,
    }
  }

  return { begin, cancel }
}

export function usePageRequest(): PageRequestScope {
  const scopeRef = useRef<PageRequestScope | null>(null)
  if (!scopeRef.current) scopeRef.current = createPageRequestScope()
  const scope = scopeRef.current

  useEffect(() => () => scope.cancel(), [scope])
  return scope
}
