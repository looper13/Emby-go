import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { backgroundApi, type BackgroundProgress } from '../src/api/background'
import { taskApi, type ScanProgress, type ScanResult } from '../src/api/tasks'
import { useReactAuthStore } from '../src/react/auth-store'
import { useReactMediaWallStore } from '../src/react/media-wall-store'
import { activateTasksRuntime, useReactTasksStore } from '../src/react/tasks-store'
import authFixture from './fixtures/json/auth-login.json'
import runningScanFixture from './fixtures/json/scan-progress-running.json'
import endedScanFixture from './fixtures/json/scan-progress.json'
import scanResultFixture from './fixtures/json/scan-result.json'

const runningScan = runningScanFixture as ScanProgress
const endedScan = endedScanFixture as ScanProgress
const scanResult = scanResultFixture as ScanResult

const idleBackground: BackgroundProgress = {
  running: false, total: 0, done: 0, success: 0, skipped: 0, failed: 0,
  current: '', cancelled: false, aborted: false, kind: '', started_at: '', finished_at: '', error: '', failures: [],
}
const runningBackground: BackgroundProgress = {
  ...idleBackground, running: true, total: 2, started_at: 'started',
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

let releaseRuntime: (() => void) | undefined

beforeEach(() => {
  vi.useFakeTimers()
  useReactTasksStore.getState().reset()
  useReactTasksStore.setState({ revision: 0 })
  useReactMediaWallStore.setState({ dirty: false })
  useReactAuthStore.setState({ token: null, user: null, phase: 'anonymous' })
})

afterEach(() => {
  releaseRuntime?.()
  releaseRuntime = undefined
  useReactTasksStore.getState().reset()
  useReactTasksStore.setState({ revision: 0 })
  useReactMediaWallStore.setState({ dirty: false })
  useReactAuthStore.setState({ token: null, user: null, phase: 'anonymous' })
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('React background task store', () => {
  it('serializes scans and refreshes the media wall once after the accepted job completes', async () => {
    const progress = vi.spyOn(taskApi, 'progress').mockResolvedValueOnce(runningScan).mockResolvedValue(endedScan)
    const run = vi.spyOn(taskApi, 'run').mockResolvedValue(scanResult)
    const markDirty = vi.spyOn(useReactMediaWallStore.getState(), 'markDirty')
    const tasks = useReactTasksStore.getState()

    const accepted = await tasks.run('scan', 3)
    expect(accepted).toBe(true)
    expect(run).toHaveBeenCalledTimes(1)
    expect(run).toHaveBeenCalledWith('scan', 3, expect.any(AbortSignal))
    expect(useReactTasksStore.getState().busy).toBe(true)
    expect(await tasks.run('reindex')).toBe(false)
    expect(run).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(600)
    expect(progress).toHaveBeenCalledTimes(2)
    expect(useReactTasksStore.getState().busy).toBe(false)
    expect(useReactTasksStore.getState().revision).toBe(1)
    expect(markDirty).toHaveBeenCalledOnce()
    expect(useReactMediaWallStore.getState().dirty).toBe(true)
  })

  it('keeps running progress through a failed poll, backs off, and ignores overlapping reads', async () => {
    const pending = deferred<ScanProgress>()
    const progress = vi.spyOn(taskApi, 'progress')
      .mockResolvedValueOnce(runningScan)
      .mockRejectedValueOnce(new TypeError('offline'))
      .mockReturnValueOnce(pending.promise)
      .mockResolvedValue(endedScan)
    const tasks = useReactTasksStore.getState()

    await tasks.restore()
    await vi.advanceTimersByTimeAsync(600)
    expect(useReactTasksStore.getState().unavailable).toBe(true)
    expect(useReactTasksStore.getState().progress).toEqual(runningScan)
    expect(useReactTasksStore.getState().busy).toBe(true)

    await vi.advanceTimersByTimeAsync(1200)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(progress).toHaveBeenCalledTimes(3)

    pending.resolve(endedScan)
    await Promise.resolve()
    await Promise.resolve()
    expect(useReactTasksStore.getState().busy).toBe(false)
    expect(useReactTasksStore.getState().revision).toBe(1)
    expect(useReactTasksStore.getState().unavailable).toBe(false)
  })

  it('blocks other mutations while probing and treats cancel as a request, not completion', async () => {
    const startWait = deferred<{ total: number; message: string }>()
    const cancelWait = deferred<unknown>()
    const start = vi.spyOn(backgroundApi, 'start').mockReturnValue(startWait.promise)
    vi.spyOn(backgroundApi, 'progress').mockResolvedValue(runningBackground)
    const cancel = vi.spyOn(backgroundApi, 'cancel').mockReturnValue(cancelWait.promise)
    const tasks = useReactTasksStore.getState()

    const accepted = tasks.probe.run({ only_missing: true })
    await Promise.resolve()
    expect(useReactTasksStore.getState().mutationBusy).toBe(true)
    expect(start).toHaveBeenCalledWith('probe', false, { only_missing: true }, expect.any(AbortSignal))
    expect(await tasks.scrape.run({})).toBe(false)
    expect(await tasks.run()).toBe(false)

    startWait.resolve({ total: 2, message: '' })
    expect(await accepted).toBe(true)
    const cancelling = useReactTasksStore.getState().probe.cancel()
    await Promise.resolve()
    await useReactTasksStore.getState().probe.cancel()
    expect(cancel).toHaveBeenCalledOnce()
    expect(useReactTasksStore.getState().probe.cancelling).toBe(true)

    cancelWait.resolve({ status: 'cancelling' })
    await cancelling
    expect(useReactTasksStore.getState().probe.busy).toBe(true)
    expect(useReactTasksStore.getState().revision).toBe(0)
  })

  it('restores administrator tasks and aborts their reads when the account changes', async () => {
    const scan = deferred<ScanProgress>()
    const backgrounds: Array<{ kind: string; signal: AbortSignal; request: ReturnType<typeof deferred<BackgroundProgress>> }> = []
    const scanProgress = vi.spyOn(taskApi, 'progress').mockReturnValue(scan.promise)
    const backgroundProgress = vi.spyOn(backgroundApi, 'progress').mockImplementation((kind, signal) => {
      const request = deferred<BackgroundProgress>()
      backgrounds.push({ kind, signal, request })
      return request.promise
    })
    useReactAuthStore.setState({ token: 'admin-token', user: authFixture.User })

    releaseRuntime = activateTasksRuntime()
    await Promise.resolve()
    expect(scanProgress).toHaveBeenCalledOnce()
    expect(backgroundProgress).toHaveBeenCalledTimes(2)
    const scanSignal = scanProgress.mock.calls[0]?.[0]
    expect(scanSignal?.aborted).toBe(false)

    useReactAuthStore.setState({ token: 'viewer-token', user: null, phase: 'idle' })
    expect(scanSignal?.aborted).toBe(true)
    expect(backgrounds.every(({ signal }) => signal.aborted)).toBe(true)

    scan.resolve(runningScan)
    backgrounds.forEach(({ request }) => request.resolve(runningBackground))
    await Promise.resolve()
    await Promise.resolve()
    expect(useReactTasksStore.getState().progress).toBeNull()
    expect(useReactTasksStore.getState().probe.progress).toBeNull()
    expect(vi.getTimerCount()).toBe(0)
  })
})
