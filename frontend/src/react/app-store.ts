import { create } from 'zustand'

export type ToastKind = 'ok' | 'error' | 'info'

export interface AppToast {
  id: number
  message: string
  kind: ToastKind
  out: boolean
}

interface AppRuntimeState {
  toasts: AppToast[]
  showToast: (message: string, kind?: ToastKind) => void
  dismissToast: (id: number) => void
  clearToasts: () => void
}

const timers = new Map<number, Set<ReturnType<typeof setTimeout>>>()
let nextToastID = 0

function schedule(id: number, callback: () => void, delay: number) {
  const timer = setTimeout(() => {
    const current = timers.get(id)
    current?.delete(timer)
    if (current?.size === 0) timers.delete(id)
    callback()
  }, delay)
  const current = timers.get(id) ?? new Set<ReturnType<typeof setTimeout>>()
  current.add(timer)
  timers.set(id, current)
}

function clearTimers(id: number) {
  const pending = timers.get(id)
  if (!pending) return
  pending.forEach(clearTimeout)
  timers.delete(id)
}

export const useAppRuntimeStore = create<AppRuntimeState>()((set, get) => ({
  toasts: [],
  showToast: (message, kind = 'ok') => {
    const toast = { id: ++nextToastID, message, kind, out: false }
    set((state) => ({ toasts: [...state.toasts, toast] }))
    schedule(toast.id, () => {
      if (!get().toasts.some((item) => item.id === toast.id)) return
      set((state) => ({
        toasts: state.toasts.map((item) => item.id === toast.id ? { ...item, out: true } : item),
      }))
      schedule(toast.id, () => {
        set((state) => ({ toasts: state.toasts.filter((item) => item.id !== toast.id) }))
      }, 260)
    }, 3200)
  },
  dismissToast: (id) => {
    clearTimers(id)
    set((state) => ({ toasts: state.toasts.filter((item) => item.id !== id) }))
  },
  clearToasts: () => {
    timers.forEach((pending) => pending.forEach(clearTimeout))
    timers.clear()
    set({ toasts: [] })
  },
}))
