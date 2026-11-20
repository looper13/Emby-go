import { useCallback, useEffect, useRef } from 'react'
import { useBlocker } from 'react-router-dom'

export function useConfirmLeave(when: boolean, message: string, busy = false) {
  const allowNextRef = useRef(false)
  const blocker = useBlocker(({ currentLocation, nextLocation }) =>
    !allowNextRef.current && (when || busy) && currentLocation.pathname !== nextLocation.pathname)

  useEffect(() => {
    if (blocker.state !== 'blocked') return
    if (busy) {
      blocker.reset()
      return
    }
    if (window.confirm(message)) blocker.proceed()
    else blocker.reset()
  }, [blocker, busy, message])

  return useCallback((allow: boolean) => { allowNextRef.current = allow }, [])
}
