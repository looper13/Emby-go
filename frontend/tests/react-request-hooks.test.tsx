import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { useAdminRead } from '../src/react/hooks/use-admin-read'
import { createPageRequestScope } from '../src/react/hooks/use-page-request'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

describe('React page request hooks', () => {
  it('cancels the prior page read and ignores its late response', async () => {
    const first = deferred<string>()
    const second = deferred<string>()
    const signals: AbortSignal[] = []
    const read = vi.fn((signal: AbortSignal) => {
      signals.push(signal)
      return signals.length === 1 ? first.promise : second.promise
    })
    const { result, unmount } = renderHook(() => useAdminRead(read))

    await waitFor(() => expect(read).toHaveBeenCalledOnce())
    expect(result.current.loading).toBe(true)
    let reload!: Promise<void>
    act(() => { reload = result.current.load() })
    expect(signals[0]?.aborted).toBe(true)
    expect(signals[1]?.aborted).toBe(false)

    first.resolve('stale')
    await first.promise
    expect(result.current.data).toBeNull()

    second.resolve('current')
    await act(async () => { await reload })
    expect(result.current.data).toBe('current')
    expect(result.current.error).toBe('')
    expect(result.current.loading).toBe(false)
    expect(result.current.active()).toBe(true)

    unmount()
    expect(signals[1]?.aborted).toBe(true)
  })

  it('keeps existing data when a refresh fails and clears errors on retry', async () => {
    const read = vi.fn()
      .mockResolvedValueOnce('loaded')
      .mockRejectedValueOnce(new Error('network failed'))
      .mockResolvedValueOnce('recovered')
    const { result } = renderHook(() => useAdminRead(read))

    await waitFor(() => expect(result.current.data).toBe('loaded'))
    let refresh!: Promise<void>
    act(() => { refresh = result.current.load() })
    await act(async () => { await refresh })
    expect(result.current.data).toBe('loaded')
    expect(result.current.error).toBe('network failed')

    act(() => { refresh = result.current.load() })
    expect(result.current.error).toBe('')
    await act(async () => { await refresh })
    expect(result.current.data).toBe('recovered')
    expect(result.current.loading).toBe(false)
  })

  it('invalidates a request when its scope cancels or starts a newer read', () => {
    const scope = createPageRequestScope()
    const first = scope.begin()
    const second = scope.begin()

    expect(first.signal.aborted).toBe(true)
    expect(first.isCurrent()).toBe(false)
    expect(second.isCurrent()).toBe(true)
    scope.cancel()
    expect(second.signal.aborted).toBe(true)
    expect(second.isCurrent()).toBe(false)
  })
})
