import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import App from '../src/react/App'
import { apiClient } from '../src/api/client'
import { useReactAuthStore, activateAuthRuntime, TOKEN_KEY } from '../src/react/auth-store'
import { RequireAuth } from '../src/react/RequireAuth'
import authFixture from './fixtures/json/auth-login.json'
import detailFixture from './fixtures/json/item-detail.json'
import librariesFixture from './fixtures/json/libraries.json'

const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), {
  status, headers: { 'Content-Type': 'application/json' },
})

type Handler = (path: string, init?: RequestInit) => Response | Promise<Response> | undefined

function installServer(handler?: Handler) {
  const fetcher = vi.fn<typeof fetch>(async (input, init) => {
    const path = String(input)
    const custom = handler?.(path, init)
    if (custom !== undefined) return custom
    switch (path) {
      case '/api/auth/status': return json({ initialized: true })
      case '/api/auth/initialize': return new Response(null, { status: 204 })
      case '/Users/AuthenticateByName': return json(authFixture)
      case '/Users/Me': return json(authFixture.User)
      case '/Sessions/Logout': return new Response(null, { status: 204 })
      case '/api/admin/status': return json({ success: 1 })
      case '/api/browse/libraries': return json(librariesFixture)
      default:
        if (path.startsWith('/api/admin/items?')) return json({ items: [detailFixture.movie], total: 1, image_tags: {} })
        throw new Error(`Unexpected test request: ${path}`)
    }
  })
  vi.stubGlobal('fetch', fetcher)
  return fetcher
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

const views: Array<{ unmount: () => void }> = []
const releases: Array<() => void> = []

function renderApp(path = '/login') {
  window.history.replaceState({}, '', `/admin#${path}`)
  releases.push(activateAuthRuntime())
  const view = render(<App />)
  views.push(view)
  return view
}

function resetAuthState() {
  useReactAuthStore.setState({
    token: null,
    user: null,
    initialized: null,
    phase: 'anonymous',
    error: '',
    submitting: false,
    expiration: 0,
  })
}

beforeEach(() => {
  localStorage.clear()
  resetAuthState()
  vi.stubGlobal('fetch', vi.fn())
})

afterEach(() => {
  views.splice(0).forEach((view) => view.unmount())
  releases.splice(0).forEach((release) => release())
  resetAuthState()
  localStorage.clear()
  window.history.replaceState({}, '', '/admin#/login')
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('React authentication flow', () => {
  it('initializes an account, keeps the draft and then returns to an allowed route after login', async () => {
    const fetcher = installServer((path) => path === '/api/auth/status' ? json({ initialized: false }) : undefined)
    const user = userEvent.setup()
    renderApp('/login?returnTo=%2Fitems%3Fsearch%3Dsample')

    await screen.findByRole('heading', { name: '建立管理员账户' })
    await user.type(screen.getByLabelText('管理员账号'), 'admin')
    await user.type(screen.getByLabelText('密码'), 'test-password')
    await user.type(screen.getByLabelText('确认密码'), 'other-password')
    await user.click(screen.getByRole('button', { name: '完成初始化' }))
    expect(screen.getByRole('alert')).toHaveTextContent('两次输入的密码不一致')
    expect(screen.getByLabelText('密码')).toHaveValue('test-password')
    expect(fetcher.mock.calls.filter(([path]) => path === '/api/auth/initialize')).toHaveLength(0)

    await user.clear(screen.getByLabelText('确认密码'))
    await user.type(screen.getByLabelText('确认密码'), 'test-password')
    await user.click(screen.getByRole('button', { name: '完成初始化' }))
    expect(await screen.findByRole('heading', { name: '欢迎回来' })).toBeInTheDocument()
    expect(screen.getByLabelText('用户名')).toHaveValue('admin')
    expect(screen.getByLabelText('密码')).toHaveValue('test-password')
    expect(screen.getByRole('alert')).toHaveTextContent('初始化完成，请使用新账户登录')
    expect(fetcher.mock.calls.some(([path]) => path === '/Users/AuthenticateByName')).toBe(false)

    await user.click(screen.getByRole('button', { name: '登录' }))
    expect(await screen.findByRole('region', { name: '影视库' }, { timeout: 10000 })).toBeInTheDocument()
    expect(document.querySelector('.react-wall-card .react-wall-meta strong')).toHaveTextContent(detailFixture.movie.Title)
    expect(window.location.hash).toBe('#/items?search=sample')
    const loginCall = fetcher.mock.calls.find(([path]) => path === '/Users/AuthenticateByName')
    expect(loginCall?.[1]?.body).toBe(JSON.stringify({ Username: 'admin', Pw: 'test-password' }))
    expect(new Headers(loginCall?.[1]?.headers).has('X-Emby-Token')).toBe(false)
  })

  it('renders failed credentials as text and allows another login attempt', async () => {
    let fail = true
    installServer((path) => path === '/Users/AuthenticateByName' && fail
      ? json({ error: '<b>账号或密码错误</b>' }, 401) : undefined)
    const user = userEvent.setup()
    renderApp()
    await screen.findByRole('heading', { name: '欢迎回来' })
    await user.type(screen.getByLabelText('用户名'), 'admin')
    await user.type(screen.getByLabelText('密码'), 'test-password')
    await user.click(screen.getByRole('button', { name: '登录' }))

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('<b>账号或密码错误</b>'))
    expect(document.querySelector('.auth-error b')).toBeNull()
    expect(screen.getByLabelText('用户名')).toHaveValue('admin')
    expect(screen.getByLabelText('密码')).toHaveValue('test-password')
    expect(useReactAuthStore.getState().expiration).toBe(0)

    fail = false
    await user.click(screen.getByRole('button', { name: '登录' }))
    await screen.findByRole('heading', { name: '档案状态' })
    expect(window.location.hash).toBe('#/overview')
  })

  it('keeps a protected route through an explicit retry after a network failure', async () => {
    localStorage.setItem(TOKEN_KEY, 'stored-token')
    let offline = true
    const fetcher = installServer((path) => path === '/Users/Me' && offline
      ? Promise.reject(new TypeError('Failed to fetch')) : undefined)
    const user = userEvent.setup()
    renderApp('/items?status=success')

    const retry = await screen.findByRole('button', { name: '重试' })
    expect(localStorage.getItem(TOKEN_KEY)).toBe('stored-token')
    expect(fetcher.mock.calls.filter(([path]) => path === '/Users/Me')).toHaveLength(1)
    offline = false
    await user.click(retry)
    await screen.findByRole('region', { name: '影视库' })
    expect(window.location.hash).toBe('#/items?status=success')
    expect(fetcher.mock.calls.filter(([path]) => path === '/Users/Me')).toHaveLength(2)
  })

  it('clears an expired token and presents the session message', async () => {
    localStorage.setItem(TOKEN_KEY, 'expired-token')
    installServer((path) => path === '/Users/Me' ? json({ error: 'unauthorized' }, 401) : undefined)
    renderApp('/overview?library_id=3')

    expect(await screen.findByRole('heading', { name: '欢迎回来' })).toBeInTheDocument()
    await waitFor(() => expect(localStorage.getItem(TOKEN_KEY)).toBeNull())
    expect(useReactAuthStore.getState().expiration).toBe(1)
    expect(document.querySelector('.auth-error')).toHaveTextContent('登录已失效')
    expect(await screen.findByText('登录已失效，请重新登录')).toBeInTheDocument()
    expect(window.location.hash).toBe('#/login?returnTo=%2Foverview%3Flibrary_id%3D3')
  })

  it('does not let an old protected 401 remove a token from a newer login', async () => {
    localStorage.setItem(TOKEN_KEY, 'old-token')
    useReactAuthStore.setState({ token: 'old-token', phase: 'idle' })
    const response = deferred<Response>()
    installServer((path) => path === '/api/admin/status' ? response.promise : undefined)
    releases.push(activateAuthRuntime())

    const oldRequest = apiClient.request('/api/admin/status', { auth: 'required' })
    await useReactAuthStore.getState().login({ Username: 'admin', Pw: 'test-password' })
    response.resolve(json({ error: 'unauthorized' }, 401))
    await expect(oldRequest).rejects.toMatchObject({ status: 401 })

    expect(useReactAuthStore.getState().token).toBe(authFixture.AccessToken)
    expect(useReactAuthStore.getState().user?.Id).toBe(authFixture.User.Id)
    expect(useReactAuthStore.getState().expiration).toBe(0)
    expect(localStorage.getItem(TOKEN_KEY)).toBe(authFixture.AccessToken)
  })

  it('reacts to a token change from another tab and validates the new session', async () => {
    localStorage.setItem(TOKEN_KEY, 'first-token')
    useReactAuthStore.setState({ token: 'first-token', phase: 'idle' })
    const fetcher = installServer()
    releases.push(activateAuthRuntime())

    localStorage.setItem(TOKEN_KEY, 'second-token')
    window.dispatchEvent(new StorageEvent('storage', {
      key: TOKEN_KEY, oldValue: 'first-token', newValue: 'second-token',
    }))

    await waitFor(() => expect(useReactAuthStore.getState().user?.Id).toBe(authFixture.User.Id))
    expect(useReactAuthStore.getState().token).toBe('second-token')
    expect(fetcher.mock.calls.filter(([path]) => path === '/Users/Me')).toHaveLength(1)
  })

  it('keeps administrators on maintenance routes and sends viewers to the media wall', async () => {
    const viewer = {
      ...authFixture.User,
      Id: '7',
      Name: 'viewer',
      Policy: { ...authFixture.User.Policy, IsAdministrator: false },
    }
    const fetcher = installServer((path) => path === '/Users/Me' ? json(viewer) : undefined)
    localStorage.setItem(TOKEN_KEY, 'viewer-token')
    const release = activateAuthRuntime()
    releases.push(release)

    function RouteProbe() {
      const location = useLocation()
      return <output data-testid="route-probe">{location.pathname}</output>
    }
    const viewerView = render(
      <MemoryRouter initialEntries={['/settings']}>
        <RequireAuth adminOnly><div data-testid="admin-content">settings</div></RequireAuth>
        <RouteProbe />
      </MemoryRouter>,
    )
    views.push(viewerView)

    await waitFor(() => expect(screen.getByTestId('route-probe')).toHaveTextContent('/items'))
    expect(screen.queryByTestId('admin-content')).not.toBeInTheDocument()
    expect(fetcher.mock.calls.filter(([path]) => path === '/Users/Me')).toHaveLength(1)

    viewerView.unmount()
    views.pop()
    release()
    localStorage.setItem(TOKEN_KEY, 'admin-token')
    useReactAuthStore.setState({ token: 'admin-token', user: null, phase: 'idle' })
    installServer()
    const adminRelease = activateAuthRuntime()
    releases.push(adminRelease)
    const adminView = render(
      <MemoryRouter initialEntries={['/settings']}>
        <RequireAuth adminOnly><div data-testid="admin-content">settings</div></RequireAuth>
      </MemoryRouter>,
    )
    views.push(adminView)
    expect(await screen.findByTestId('admin-content')).toHaveTextContent('settings')
  })
})
