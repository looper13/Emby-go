import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Moon, RefreshCw, Sun } from 'lucide-react'
import { useLocation, useNavigate } from 'react-router-dom'
import { resolveLoginTarget } from '../router/legacy-url'
import { useReactAuthStore } from './auth-store'
import { useThemeStore } from './theme-store'

export function LoginPage() {
  const location = useLocation()
  const navigate = useNavigate()
  const token = useReactAuthStore((state) => state.token)
  const phase = useReactAuthStore((state) => state.phase)
  const initialized = useReactAuthStore((state) => state.initialized)
  const error = useReactAuthStore((state) => state.error)
  const submitting = useReactAuthStore((state) => state.submitting)
  const checkSession = useReactAuthStore((state) => state.checkSession)
  const loadStatus = useReactAuthStore((state) => state.loadStatus)
  const initialize = useReactAuthStore((state) => state.initialize)
  const login = useReactAuthStore((state) => state.login)
  const theme = useThemeStore((state) => state.theme)
  const toggleTheme = useThemeStore((state) => state.toggle)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [message, setMessage] = useState('')
  const [checking, setChecking] = useState(true)
  const [needsRetry, setNeedsRetry] = useState(false)
  const [retry, setRetry] = useState(0)
  const usernameInput = useRef<HTMLInputElement>(null)
  const returnTo = new URLSearchParams(location.search).get('returnTo')
  const initializing = initialized === false
  const busy = checking || submitting

  useEffect(() => {
    let active = true
    async function boot() {
      setChecking(true)
      setNeedsRetry(false)
      setMessage('')
      try {
        if (token) {
          if (retry === 0 && phase === 'unavailable') {
            setNeedsRetry(true)
            setMessage(error)
            return
          }
          if (await checkSession()) {
            if (active) navigate(resolveLoginTarget(returnTo), { replace: true })
            return
          }
        }
        await loadStatus()
        const currentError = useReactAuthStore.getState().error
        if (active && currentError) setMessage(currentError)
      } catch {
        if (active) {
          setMessage('无法连接服务，请重试')
          setNeedsRetry(true)
        }
      } finally {
        if (active) setChecking(false)
      }
    }
    void boot()
    return () => { active = false }
  }, [token, phase, error, retry, checkSession, loadStatus, navigate, returnTo])

  useEffect(() => {
    if (!checking) usernameInput.current?.focus()
  }, [checking])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy || needsRetry) return
    setMessage('')
    if (Array.from(username.trim()).length < 3 || Array.from(password).length < 9) {
      setMessage('账号至少 3 个字符，密码至少 9 个字符')
      return
    }
    if (initializing && password !== confirmation) {
      setMessage('两次输入的密码不一致')
      return
    }
    const credentials = { Username: username, Pw: password }
    try {
      if (initializing) {
        await initialize(credentials)
        setMessage('初始化完成，请使用新账户登录')
      } else if (await login(credentials)) {
        navigate(resolveLoginTarget(returnTo), { replace: true })
      }
    } catch (cause) {
      setMessage(cause instanceof Error ? cause.message : '网络错误')
    }
  }

  return (
    <main id="auth-root" className="auth-root" aria-live="polite">
      <div className="auth-theme-switch">
        <button
          className="icon-btn theme-toggle"
          type="button"
          title={theme === 'dark' ? '切换为浅色模式' : '切换为深色模式'}
          aria-label={theme === 'dark' ? '切换为浅色模式' : '切换为深色模式'}
          onClick={toggleTheme}
        >
          {theme === 'dark' ? <Sun size={18} aria-hidden="true" /> : <Moon size={18} aria-hidden="true" />}
        </button>
      </div>
      <section className="auth-card">
        <p className="eyebrow">Emby-go · Media Console</p>
        <h1>{initializing ? '建立管理员账户' : '欢迎回来'}</h1>
        <form id="auth-form" className="auth-form" onSubmit={submit}>
          <div className="field">
            <label htmlFor="auth-user">{initializing ? '管理员账号' : '用户名'}</label>
            <input
              id="auth-user"
              ref={usernameInput}
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              name="Username"
              autoComplete="username"
              minLength={3}
              required
              disabled={busy}
              autoFocus
              placeholder="至少 3 个字符"
            />
          </div>
          <div className="field">
            <label htmlFor="auth-pw">密码</label>
            <input
              id="auth-pw"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              name="Pw"
              type="password"
              autoComplete={initializing ? 'new-password' : 'current-password'}
              minLength={9}
              required
              disabled={busy}
              placeholder="至少 9 个字符"
            />
          </div>
          {initializing && (
            <div className="field">
              <label htmlFor="auth-confirm">确认密码</label>
              <input
                id="auth-confirm"
                value={confirmation}
                onChange={(event) => setConfirmation(event.target.value)}
                name="confirm"
                type="password"
                autoComplete="new-password"
                required
                disabled={busy}
                placeholder="再次输入密码"
              />
            </div>
          )}
          <p className="auth-error" role="alert">{message}</p>
          <button className="btn btn-accent" type="submit" disabled={busy || needsRetry}>
            {busy ? '请稍候…' : initializing ? '完成初始化' : '登录'}
          </button>
          {needsRetry && (
            <button className="btn" type="button" disabled={busy} onClick={() => setRetry((count) => count + 1)}>
              <RefreshCw size={15} aria-hidden="true" />
              <span>重试</span>
            </button>
          )}
        </form>
      </section>
    </main>
  )
}
