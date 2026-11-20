import { create } from 'zustand'
import { ApiError } from '../api/client'
import { backgroundApi, type BackgroundKind, type BackgroundProgress } from '../api/background'
import { taskApi, type ScanProgress } from '../api/tasks'
import { useAppRuntimeStore } from './app-store'
import { useReactAuthStore } from './auth-store'
import { useReactMediaWallStore } from './media-wall-store'

type ScanKind = 'scan' | 'reindex'
type AvatarMode = boolean | 'import'

interface BackgroundTaskState {
  progress: BackgroundProgress | null
  visible: boolean
  unavailable: boolean
  submitting: boolean
  cancelling: boolean
  busy: boolean
  restore: () => Promise<void>
  run: (payload: Record<string, unknown>, avatar?: AvatarMode) => Promise<boolean>
  cancel: () => Promise<void>
  reset: () => void
}

interface TasksState {
  progress: ScanProgress | null
  submitting: boolean
  kind: ScanKind
  visible: boolean
  unavailable: boolean
  revision: number
  busy: boolean
  mutationBusy: boolean
  probe: BackgroundTaskState
  scrape: BackgroundTaskState
  restore: () => Promise<void>
  restoreAll: () => Promise<void>
  run: (kind?: ScanKind, libraryId?: number) => Promise<boolean>
  reset: () => void
}

interface BackgroundRuntime {
  generation: number
  failures: number
  tracking: boolean
  timer?: ReturnType<typeof setTimeout>
  hide?: ReturnType<typeof setTimeout>
  reading?: Promise<void>
  readController?: AbortController
  writeController?: AbortController
  cancelController?: AbortController
}

const backgroundRuntime: Record<BackgroundKind, BackgroundRuntime> = {
  probe: { generation: 0, failures: 0, tracking: false },
  scrape: { generation: 0, failures: 0, tracking: false },
}
let scanGeneration = 0
let scanFailures = 0
let scanTimer: ReturnType<typeof setTimeout> | undefined
let scanHide: ReturnType<typeof setTimeout> | undefined
let scanReadController: AbortController | undefined
let scanWriteController: AbortController | undefined
let scanRead: Promise<boolean> | undefined
let scanTick: Promise<void> | undefined
let scanCompletionAnnounced = false
let scanObservedRunning = false
let runtimeRefs = 0
let detachAuthListener: (() => void) | undefined

function backgroundKindName(kind: BackgroundKind) {
  return kind === 'probe' ? '探测' : '刮削'
}

export const useReactTasksStore = create<TasksState>()((set, get) => {
  function scanBusy(state = get()) {
    return state.submitting || Boolean(state.progress?.running)
  }

  function updateScan(patch: Partial<Pick<TasksState, 'progress' | 'submitting' | 'kind' | 'visible' | 'unavailable'>>) {
    set((state) => {
      const next = { ...state, ...patch }
      const busy = scanBusy(next)
      return { ...patch, busy, mutationBusy: busy || next.probe.busy || next.scrape.busy }
    })
  }

  function updateBackground(kind: BackgroundKind, patch: Partial<Omit<BackgroundTaskState, 'restore' | 'run' | 'cancel' | 'reset'>>) {
    set((state) => {
      const current = state[kind]
      const next = { ...current, ...patch }
      next.busy = next.submitting || Boolean(next.progress?.running)
      const probe = kind === 'probe' ? next : state.probe
      const scrape = kind === 'scrape' ? next : state.scrape
      return {
        [kind]: next,
        mutationBusy: scanBusy(state) || probe.busy || scrape.busy,
      } as Pick<TasksState, BackgroundKind | 'mutationBusy'>
    })
  }

  function markCompleted() {
    set((state) => ({ revision: state.revision + 1 }))
    useReactMediaWallStore.getState().markDirty()
  }

  function stopBackground(kind: BackgroundKind) {
    const runtime = backgroundRuntime[kind]
    runtime.generation += 1
    clearTimeout(runtime.timer)
    clearTimeout(runtime.hide)
    runtime.timer = undefined
    runtime.hide = undefined
    runtime.readController?.abort()
    runtime.writeController?.abort()
    runtime.cancelController?.abort()
    runtime.reading = undefined
    runtime.failures = 0
    runtime.tracking = false
    updateBackground(kind, {
      progress: null, visible: false, unavailable: false, submitting: false, cancelling: false, busy: false,
    })
  }

  function reset() {
    scanGeneration += 1
    clearTimeout(scanTimer)
    clearTimeout(scanHide)
    scanTimer = undefined
    scanHide = undefined
    scanReadController?.abort()
    scanWriteController?.abort()
    scanRead = undefined
    scanTick = undefined
    scanFailures = 0
    scanCompletionAnnounced = false
    scanObservedRunning = false
    updateScan({ progress: null, submitting: false, visible: false, unavailable: false })
    stopBackground('probe')
    stopBackground('scrape')
  }

  async function readScanProgress(): Promise<boolean> {
    if (scanRead) return scanRead
    const epoch = scanGeneration
    const controller = new AbortController()
    scanReadController = controller
    const promise = taskApi.progress(controller.signal).then((progress) => {
      if (epoch !== scanGeneration || controller.signal.aborted) return false
      scanObservedRunning ||= progress.running
      scanFailures = 0
      updateScan({
        progress,
        unavailable: false,
        visible: get().submitting || progress.running || (get().visible && Boolean(progress.finished_at)),
      })
      if (scanObservedRunning && !progress.running && !get().submitting && !scanCompletionAnnounced) {
        markCompleted()
        scanCompletionAnnounced = true
      }
      return true
    }).catch(() => {
      if (epoch !== scanGeneration || controller.signal.aborted) return false
      scanFailures = Math.min(3, scanFailures + 1)
      updateScan({ unavailable: true })
      return false
    }).finally(() => {
      if (scanRead === promise) scanRead = undefined
    })
    scanRead = promise
    return promise
  }

  function tickScan(): Promise<void> {
    if (scanTick) return scanTick
    clearTimeout(scanTimer)
    scanTimer = undefined
    const epoch = scanGeneration
    const pending = (async () => {
      const ok = await readScanProgress()
      if (epoch !== scanGeneration) return
      if (!ok || get().busy) {
        scanTimer = setTimeout(() => { scanTimer = undefined; void tickScan() }, Math.min(4800, 600 * 2 ** scanFailures))
      } else {
        clearTimeout(scanHide)
        if (get().visible) scanHide = setTimeout(() => { updateScan({ visible: false }); scanHide = undefined }, 4000)
      }
    })().finally(() => {
      if (scanTick === pending) scanTick = undefined
    })
    scanTick = pending
    return pending
  }

  async function pollBackground(kind: BackgroundKind): Promise<void> {
    const runtime = backgroundRuntime[kind]
    if (runtime.reading) return runtime.reading
    clearTimeout(runtime.timer)
    runtime.timer = undefined
    const epoch = runtime.generation
    const controller = new AbortController()
    runtime.readController = controller
    const interval = kind === 'probe' ? 1000 : 1500
    const promise = backgroundApi.progress(kind, controller.signal).then((progress) => {
      if (epoch !== runtime.generation || controller.signal.aborted) return
      updateBackground(kind, { progress, unavailable: false })
      runtime.failures = 0
      if (progress.running) {
        runtime.tracking = true
        updateBackground(kind, { visible: true })
        clearTimeout(runtime.hide)
        runtime.hide = undefined
      } else if (runtime.tracking && !get()[kind].submitting) {
        runtime.tracking = false
        updateBackground(kind, { visible: true })
        if (!progress.finished_at) {
          updateBackground(kind, {
            progress: { ...progress, error: '任务状态已重置，服务可能已重启；请检查已有结果后重新启动。' },
          })
          useAppRuntimeStore.getState().showToast(get()[kind].progress!.error, 'error')
          markCompleted()
        } else {
          const label = kind === 'probe' ? '探测' : progress.kind === 'actor_import' ? '旧头像导入' : progress.kind === 'scrape_avatars' ? '头像任务' : '刮削'
          useAppRuntimeStore.getState().showToast(
            `${label}${progress.cancelled ? '已中止' : progress.aborted ? '已因连续失败中止' : '完成'}：成功 ${progress.success} / 跳过 ${progress.skipped} / 失败 ${progress.failed}${progress.error ? ' · ' + progress.error : ''}`,
            progress.failed || (progress.error && !progress.cancelled) ? 'error' : 'ok',
          )
          if (progress.failures[0]) useAppRuntimeStore.getState().showToast(`失败/待确认示例：${progress.failures[0]}`, 'error')
          markCompleted()
          if (kind === 'probe' && !progress.failed && !progress.error) {
            runtime.hide = setTimeout(() => { updateBackground(kind, { visible: false }); runtime.hide = undefined }, 8000)
          }
        }
      }
    }).catch(() => {
      if (epoch !== runtime.generation || controller.signal.aborted) return
      runtime.failures = Math.min(runtime.failures + 1, 3)
      updateBackground(kind, { unavailable: true })
    }).finally(() => {
      if (runtime.reading === promise) runtime.reading = undefined
      if (epoch !== runtime.generation) return
      if (get()[kind].busy || get()[kind].unavailable) {
        runtime.timer = setTimeout(() => { runtime.timer = undefined; void pollBackground(kind) }, Math.min(interval * 2 ** runtime.failures, 8000))
      }
    })
    runtime.reading = promise
    return promise
  }

  async function runBackground(kind: BackgroundKind, payload: Record<string, unknown>, avatar: AvatarMode = false) {
    if (get().mutationBusy) return false
    const runtime = backgroundRuntime[kind]
    runtime.generation += 1
    clearTimeout(runtime.timer)
    clearTimeout(runtime.hide)
    runtime.timer = undefined
    runtime.hide = undefined
    runtime.readController?.abort()
    runtime.reading = undefined
    const epoch = runtime.generation
    const controller = new AbortController()
    runtime.writeController = controller
    runtime.tracking = false
    updateBackground(kind, { submitting: true, visible: true, unavailable: false, progress: null })
    try {
      const result = await backgroundApi.start(kind, avatar, payload, controller.signal)
      if (epoch !== runtime.generation) return false
      if (!result.total) {
        updateBackground(kind, { visible: false })
        useAppRuntimeStore.getState().showToast(result.message || '没有符合条件的条目')
      } else {
        runtime.tracking = true
        const label = kind === 'probe' ? '探测' : avatar === 'import' ? '旧头像导入' : avatar ? '头像任务' : '刮削'
        useAppRuntimeStore.getState().showToast(`${label}已启动（${result.total} ${avatar ? '个演员' : '条'}）`)
      }
      return true
    } catch (cause) {
      if (epoch === runtime.generation) {
        updateBackground(kind, { visible: false })
        useAppRuntimeStore.getState().showToast(cause instanceof Error ? cause.message : '启动失败', 'error')
      }
      return false
    } finally {
      if (epoch === runtime.generation) {
        updateBackground(kind, { submitting: false })
        runtime.writeController = undefined
        await pollBackground(kind)
      }
    }
  }

  async function cancelBackground(kind: BackgroundKind) {
    const task = get()[kind]
    if (!task.busy || task.submitting || task.cancelling) return
    const runtime = backgroundRuntime[kind]
    const epoch = runtime.generation
    const controller = new AbortController()
    runtime.cancelController = controller
    updateBackground(kind, { cancelling: true })
    try {
      await backgroundApi.cancel(kind, controller.signal)
      if (epoch === runtime.generation) useAppRuntimeStore.getState().showToast('已请求中止')
    } catch (cause) {
      if (epoch === runtime.generation) {
        useAppRuntimeStore.getState().showToast(cause instanceof Error ? cause.message : '中止失败', 'error')
        if (cause instanceof ApiError && cause.status === 409) await pollBackground(kind)
      }
    } finally {
      if (epoch === runtime.generation) {
        updateBackground(kind, { cancelling: false })
        runtime.cancelController = undefined
        await pollBackground(kind)
      }
    }
  }

  function makeBackgroundTask(kind: BackgroundKind): BackgroundTaskState {
    return {
      progress: null,
      visible: false,
      unavailable: false,
      submitting: false,
      cancelling: false,
      busy: false,
      restore: () => pollBackground(kind),
      run: (payload, avatar = false) => runBackground(kind, payload, avatar),
      cancel: () => cancelBackground(kind),
      reset: () => stopBackground(kind),
    }
  }

  return {
    progress: null,
    submitting: false,
    kind: 'scan',
    visible: false,
    unavailable: false,
    revision: 0,
    busy: false,
    mutationBusy: false,
    probe: makeBackgroundTask('probe'),
    scrape: makeBackgroundTask('scrape'),
    restore: tickScan,
    restoreAll: () => Promise.allSettled([tickScan(), pollBackground('probe'), pollBackground('scrape')]).then(() => {}),
    async run(kind = 'scan', libraryId) {
      if (get().mutationBusy) return false
      scanGeneration += 1
      clearTimeout(scanTimer)
      clearTimeout(scanHide)
      scanTimer = undefined
      scanHide = undefined
      scanReadController?.abort()
      scanRead = undefined
      scanTick = undefined
      const epoch = scanGeneration
      const controller = new AbortController()
      scanWriteController = controller
      scanCompletionAnnounced = false
      scanObservedRunning = false
      scanFailures = 0
      updateScan({ submitting: true, visible: true, unavailable: false, progress: null, kind })
      void tickScan()
      let succeeded = false
      try {
        const result = await taskApi.run(kind, libraryId, controller.signal)
        if (epoch !== scanGeneration) return false
        succeeded = true
        useAppRuntimeStore.getState().showToast(kind === 'reindex'
          ? `重建完成：${result.success} 成功 / ${result.pending} 待补录 / ${result.incompatible} 不兼容`
          : `增量扫描完成：新增 ${result.added} / 更新 ${result.updated} / 跳过 ${result.skipped} / 删除 ${result.deleted} / 失败 ${result.failed}`)
        return true
      } catch (cause) {
        if (epoch === scanGeneration) useAppRuntimeStore.getState().showToast(cause instanceof Error ? cause.message : '扫描失败', 'error')
        return false
      } finally {
        if (epoch === scanGeneration) {
          updateScan({ submitting: false })
          scanWriteController = undefined
          if (succeeded) {
            scanCompletionAnnounced = true
            markCompleted()
          }
          await tickScan()
        }
      }
    },
    reset,
  }
})

export function activateTasksRuntime(): () => void {
  runtimeRefs += 1
  if (runtimeRefs === 1) {
    let previousToken: string | null | undefined
    let previousAdmin: boolean | undefined
    detachAuthListener = useReactAuthStore.subscribe((state) => {
      const isAdmin = state.user?.Policy.IsAdministrator === true
      if (state.token === previousToken && isAdmin === previousAdmin) return
      previousToken = state.token
      previousAdmin = isAdmin
      useReactTasksStore.getState().reset()
      if (state.token && isAdmin) void useReactTasksStore.getState().restoreAll()
    })
    const auth = useReactAuthStore.getState()
    if (auth.token && auth.user?.Policy.IsAdministrator) void useReactTasksStore.getState().restoreAll()
  }

  let released = false
  return () => {
    if (released) return
    released = true
    runtimeRefs -= 1
    if (runtimeRefs !== 0) return
    detachAuthListener?.()
    detachAuthListener = undefined
    useReactTasksStore.getState().reset()
  }
}
