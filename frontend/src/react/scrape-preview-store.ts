import { create } from 'zustand'
import { scrapeApi, type Candidate, type Inspect, type Preview } from '../api/scrape'
import { useAppRuntimeStore } from './app-store'

interface ScrapePreviewOwner {
  movieId: number
  confirmed: () => void | Promise<unknown>
}

interface ScrapePreviewState {
  current: ScrapePreviewOwner | null
  preview: Preview | null
  inspect: Inspect | null
  loading: boolean
  writing: boolean
  error: string
  overwrite: boolean
  open: (movieId: number, confirmed: ScrapePreviewOwner['confirmed']) => Promise<void>
  select: (candidate: Candidate) => Promise<void>
  setOverwrite: (value: boolean) => void
  confirm: () => Promise<void>
  close: () => void
}

let generation = 0
let controller: AbortController | undefined
let restoreFocus: HTMLElement | null = null

export const useReactScrapePreviewStore = create<ScrapePreviewState>()((set, get) => ({
  current: null,
  preview: null,
  inspect: null,
  loading: false,
  writing: false,
  error: '',
  overwrite: false,
  close() {
    generation += 1
    controller?.abort()
    controller = undefined
    set({ current: null, preview: null, inspect: null, loading: false, error: '', overwrite: false })
    const previous = restoreFocus
    restoreFocus = null
    queueMicrotask(() => {
      if (!get().current && previous?.isConnected) previous.focus({ preventScroll: true })
    })
  },
  async select(candidate) {
    const owner = get().current
    if (!owner || get().writing) return
    const epoch = ++generation
    controller?.abort()
    controller = new AbortController()
    const request = controller
    set({ loading: true, error: '', inspect: null })
    try {
      const value = await scrapeApi.inspect(owner.movieId, candidate.provider, candidate.id, request.signal)
      if (epoch === generation && get().current === owner) {
        set({ inspect: value, overwrite: value.overwrite })
      }
    } catch (cause) {
      if (epoch === generation) set({ error: cause instanceof Error ? cause.message : '读取失败' })
    } finally {
      if (epoch === generation) set({ loading: false })
    }
  },
  async open(movieId, confirmed) {
    if (get().writing) return
    get().close()
    restoreFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const owner = { movieId, confirmed }
    const epoch = ++generation
    controller = new AbortController()
    const request = controller
    set({ current: owner, loading: true, error: '' })
    try {
      const value = await scrapeApi.preview(movieId, request.signal)
      if (epoch !== generation || get().current !== owner) return
      set({ preview: value, loading: false })
      const candidate = value.candidates[value.recommended < 0 ? 0 : value.recommended]
      if (candidate) await get().select(candidate)
    } catch (cause) {
      if (epoch === generation) set({ error: cause instanceof Error ? cause.message : '搜索失败', loading: false })
    }
  },
  setOverwrite(value) {
    if (!get().writing) set({ overwrite: value })
  },
  async confirm() {
    const owner = get().current
    const inspect = get().inspect
    if (!owner || !inspect || get().writing || get().loading) return
    const force = get().overwrite
    set({ writing: true })
    try {
      const result = await scrapeApi.confirm(owner.movieId, inspect.provider, inspect.id, force)
      const showToast = useAppRuntimeStore.getState().showToast
      showToast(`已写入「${result.title}」（图片 ${result.images} 张${result.trailer_downloaded ? '，预告片已下载' : ''}）`)
      if (result.warnings?.length) showToast(`部分资源未下载：${result.warnings.join('；')}`, 'error')
      if (get().current === owner) get().close()
      await owner.confirmed()
    } catch (cause) {
      useAppRuntimeStore.getState().showToast(cause instanceof Error ? cause.message : '写入失败', 'error')
    } finally {
      set({ writing: false })
    }
  },
}))

export function activateScrapePreviewRuntime(): () => void {
  return () => useReactScrapePreviewStore.getState().close()
}
