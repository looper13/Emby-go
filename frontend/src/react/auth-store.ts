import { create } from 'zustand'
import { ApiError, apiClient } from '../api/client'
import { authApi } from '../api/auth'
import type { Credentials, UserDto } from '../api/contracts'

export const TOKEN_KEY = 'emby_token'

export type AuthPhase = 'idle' | 'checking' | 'authenticated' | 'anonymous' | 'unavailable'

interface AuthState {
  token: string | null
  user: UserDto | null
  initialized: boolean | null
  phase: AuthPhase
  error: string
  submitting: boolean
  expiration: number
  checkSession: () => Promise<boolean>
  loadStatus: () => Promise<boolean>
  initialize: (credentials: Credentials) => Promise<void>
  login: (credentials: Credentials) => Promise<boolean>
  logout: () => Promise<void>
  clearSession: (expectedToken?: string | null) => void
  storageChanged: (event: StorageEvent) => void
}

interface SessionCheck {
  token: string
  promise: Promise<boolean>
}

let generation = 0
let sessionCheck: SessionCheck | undefined
let statusRequest: Promise<boolean> | undefined
let authRuntimeRefs = 0
let detachAuthHandlers: (() => void) | undefined
let stopStorageListener: (() => void) | undefined

function storedToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY)
  } catch {
    return null
  }
}

function initialState(): Pick<AuthState, 'token' | 'user' | 'initialized' | 'phase' | 'error' | 'submitting' | 'expiration'> {
  const token = storedToken()
  return {
    token,
    user: null,
    initialized: null,
    phase: token ? 'idle' : 'anonymous',
    error: '',
    submitting: false,
    expiration: 0,
  }
}

export const useReactAuthStore = create<AuthState>()((set, get) => {
  function invalidate(sentToken: string) {
    if (get().token !== sentToken) return
    generation += 1
    const latestToken = storedToken()
    if (latestToken && latestToken !== sentToken) {
      set({ token: latestToken, user: null, phase: 'idle', error: '' })
      return
    }
    try {
      localStorage.removeItem(TOKEN_KEY)
    } catch {
      // The in-memory token is still cleared when browser storage is unavailable.
    }
    set((state) => ({
      token: null,
      user: null,
      phase: 'anonymous',
      error: '登录已失效',
      expiration: state.expiration + 1,
    }))
  }

  return {
    ...initialState(),
    checkSession() {
      if (get().token && get().user && get().phase === 'authenticated') return Promise.resolve(true)
      const sentToken = get().token
      if (!sentToken) {
        set({ phase: 'anonymous' })
        return Promise.resolve(false)
      }
      if (sessionCheck?.token === sentToken) return sessionCheck.promise

      const startedGeneration = generation
      set({ phase: 'checking', error: '' })
      const promise = authApi.me().then((currentUser) => {
        if (startedGeneration !== generation || sentToken !== get().token) return Boolean(get().token && get().user)
        set({ user: currentUser, phase: 'authenticated' })
        return true
      }).catch((cause: unknown) => {
        if (startedGeneration !== generation || sentToken !== get().token) return Boolean(get().token && get().user)
        if (cause instanceof ApiError && cause.status === 401) {
          invalidate(sentToken)
          return false
        }
        set({ phase: 'unavailable', error: '无法连接服务，请重试' })
        throw cause
      }).finally(() => {
        if (sessionCheck?.promise === promise) sessionCheck = undefined
      })
      sessionCheck = { token: sentToken, promise }
      return promise
    },
    loadStatus() {
      if (get().initialized !== null) return Promise.resolve(get().initialized === true)
      if (statusRequest) return statusRequest
      statusRequest = authApi.status().then((data) => {
        set({ initialized: data.initialized })
        return data.initialized
      }).finally(() => { statusRequest = undefined })
      return statusRequest
    },
    async initialize(credentials) {
      if (get().submitting) throw new Error('请求正在进行中')
      set({ submitting: true })
      try {
        await authApi.initialize(credentials)
        set({ initialized: true })
      } finally {
        set({ submitting: false })
      }
    },
    async login(credentials) {
      if (get().submitting) throw new Error('请求正在进行中')
      set({ submitting: true })
      try {
        const data = await authApi.login(credentials)
        try {
          localStorage.setItem(TOKEN_KEY, data.AccessToken)
        } catch {
          throw new Error('浏览器无法保存登录状态')
        }
        generation += 1
        set({
          token: data.AccessToken,
          user: data.User,
          initialized: true,
          phase: 'authenticated',
          error: '',
        })
        return true
      } finally {
        set({ submitting: false })
      }
    },
    async logout() {
      if (get().submitting) return
      const sentToken = get().token
      set({ submitting: true })
      try {
        await authApi.logout()
        get().clearSession(sentToken)
      } catch (cause) {
        if (!(cause instanceof ApiError && cause.status === 401)) throw cause
        get().clearSession(sentToken)
      } finally {
        set({ submitting: false })
      }
    },
    clearSession(expectedToken = get().token) {
      if (get().token !== expectedToken) return
      generation += 1
      if (storedToken() === expectedToken) {
        try {
          localStorage.removeItem(TOKEN_KEY)
        } catch {
          // Keep the in-memory state usable even if local storage is blocked.
        }
      }
      const token = storedToken()
      set({ token, user: null, phase: token ? 'idle' : 'anonymous', error: '' })
    },
    storageChanged(event) {
      if (event.key !== TOKEN_KEY) return
      generation += 1
      set((state) => ({
        token: event.newValue,
        user: null,
        phase: event.newValue ? 'idle' : 'anonymous',
        error: '',
        expiration: event.newValue ? state.expiration : state.expiration + 1,
      }))
      if (event.newValue) void get().checkSession().catch(() => {})
    },
  }
})

export function activateAuthRuntime(): () => void {
  authRuntimeRefs += 1
  if (authRuntimeRefs === 1) {
    useReactAuthStore.setState(initialState())
    detachAuthHandlers = apiClient.setAuthHandlers({
      getToken: () => useReactAuthStore.getState().token,
      onUnauthorized: (token) => invalidateAuthToken(token),
    })
    const onStorage = (event: StorageEvent) => useReactAuthStore.getState().storageChanged(event)
    window.addEventListener('storage', onStorage)
    stopStorageListener = () => window.removeEventListener('storage', onStorage)
  }

  let released = false
  return () => {
    if (released) return
    released = true
    authRuntimeRefs -= 1
    if (authRuntimeRefs !== 0) return
    detachAuthHandlers?.()
    detachAuthHandlers = undefined
    stopStorageListener?.()
    stopStorageListener = undefined
    generation += 1
    sessionCheck = undefined
    statusRequest = undefined
    useReactAuthStore.setState(initialState())
  }
}

function invalidateAuthToken(token: string) {
  const state = useReactAuthStore.getState()
  if (state.token !== token) return
  state.clearSession(token)
  const latest = storedToken()
  if (!latest) {
    useReactAuthStore.setState((current) => ({ ...current, error: '登录已失效', expiration: current.expiration + 1 }))
  }
}
