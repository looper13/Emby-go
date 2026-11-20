import { useEffect, useLayoutEffect, useRef, useState } from 'react'

interface ImageState {
  current: string
  failed: boolean
  retrySource: string
  revision: number
}

export function useImageRetry(src: string, fallback = '', disabled = false) {
  const [image, setImage] = useState<ImageState>(() => ({ current: src, failed: false, retrySource: '', revision: 0 }))
  const current = useRef(image)
  const timer = useRef<number | undefined>(undefined)
  const disabledRef = useRef(disabled)
  const triedFallback = useRef(false)
  const triedRetry = useRef(false)
  current.current = image
  disabledRef.current = disabled

  function clearTimer() {
    if (timer.current !== undefined) window.clearTimeout(timer.current)
    timer.current = undefined
  }

  function commit(next: ImageState) {
    current.current = next
    setImage(next)
  }

  useLayoutEffect(() => {
    clearTimer()
    triedFallback.current = false
    triedRetry.current = false
    commit({ current: src, failed: false, retrySource: '', revision: current.current.revision + 1 })
  }, [src, fallback])

  useEffect(() => {
    if (disabled || !image.retrySource) return
    const retrySource = image.retrySource
    if (current.current.retrySource !== retrySource) return
    const id = window.setTimeout(() => {
      timer.current = undefined
      const previous = current.current
      if (disabledRef.current || previous.retrySource !== retrySource) return
      commit({ ...previous, current: retrySource, retrySource: '', failed: false, revision: previous.revision + 1 })
    }, 500)
    timer.current = id
    return () => {
      window.clearTimeout(id)
      if (timer.current === id) timer.current = undefined
    }
  }, [disabled, image.retrySource])

  useEffect(() => () => clearTimer(), [])

  function onLoad(source: string) {
    const previous = current.current
    if (disabledRef.current || previous.current !== source) return
    clearTimer()
    if (previous.failed || previous.retrySource) commit({ ...previous, failed: false, retrySource: '' })
  }

  function onError(source: string) {
    const previous = current.current
    if (disabledRef.current || !source || previous.current !== source) return
    if (!triedFallback.current && fallback && fallback !== source) {
      triedFallback.current = true
      commit({ ...previous, current: fallback, failed: false, retrySource: '' })
    } else if (!triedRetry.current) {
      triedRetry.current = true
      commit({ ...previous, current: '', failed: false, retrySource: source })
    } else {
      clearTimer()
      commit({ ...previous, current: '', failed: true, retrySource: '' })
    }
  }

  return {
    current: image.current,
    failed: image.failed,
    retrying: Boolean(image.retrySource),
    revision: image.revision,
    onLoad,
    onError,
  }
}
