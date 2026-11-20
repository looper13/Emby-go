import { create } from 'zustand'

interface ScheduledState {
  editingId: number | null
  setEditingId: (id: number | null) => void
  reset: () => void
}

export const useReactScheduledStore = create<ScheduledState>()((set) => ({
  editingId: null,
  setEditingId: (editingId) => set({ editingId }),
  reset: () => set({ editingId: null }),
}))
