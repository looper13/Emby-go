import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { lazy } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from '../src/react/App'
import { AppErrorBoundary } from '../src/react/AppErrorBoundary'
import { AppProviders } from '../src/react/AppProviders'
import { mountReactApplication } from '../src/react/mount'
import { useAppRuntimeStore } from '../src/react/app-store'
import { authApi } from '../src/api/auth'
import { backgroundApi } from '../src/api/background'
import { taskApi } from '../src/api/tasks'
import { TOKEN_KEY } from '../src/react/auth-store'
import { useReactTasksStore } from '../src/react/tasks-store'
import authFixture from './fixtures/json/auth-login.json'
import scanProgress from './fixtures/json/scan-progress.json'

afterEach(() => vi.useRealTimers())

describe('React migration foundation', () => {
  it('mounts a hash-routed React application root', () => {
    localStorage.removeItem(TOKEN_KEY)
    window.history.replaceState({}, '', '/admin#/login')
    vi.spyOn(authApi, 'status').mockResolvedValue({ initialized: true })
    render(<App />)
    expect(screen.getByLabelText('用户名')).toBeInTheDocument()
  })

  it('renders notifications and clears their timers when the provider unmounts', () => {
    vi.useFakeTimers()
    const view = render(<AppProviders><div>content</div></AppProviders>)

    act(() => useAppRuntimeStore.getState().showToast('保存成功'))
    expect(screen.getByRole('status')).toHaveTextContent('保存成功')

    view.unmount()
    expect(useAppRuntimeStore.getState().toasts).toEqual([])
    act(() => vi.runOnlyPendingTimers())
    expect(vi.getTimerCount()).toBe(0)
  })

  it('shows the shared loading state while a lazy page is pending', () => {
    const PendingPage = lazy(() => new Promise<{ default: () => null }>(() => {}))
    render(<AppProviders><PendingPage /></AppProviders>)

    expect(screen.getByText('正在加载…')).toBeInTheDocument()
  })

  it('catches a page error and allows the subtree to render again after retry', () => {
    const failure = vi.spyOn(console, 'error').mockImplementation(() => {})
    let shouldThrow = true
    function UnstablePage() {
      if (shouldThrow) throw new Error('render failure')
      return <p>页面已恢复</p>
    }

    render(<AppErrorBoundary><UnstablePage /></AppErrorBoundary>)
    expect(screen.getByRole('alert')).toHaveTextContent('页面暂时无法显示')
    shouldThrow = false
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(screen.getByText('页面已恢复')).toBeInTheDocument()
    failure.mockRestore()
  })

  it('uses one React root per container and releases it on unmount', async () => {
    const container = document.createElement('div')
    const warning = vi.spyOn(console, 'error').mockImplementation(() => {})
    localStorage.removeItem(TOKEN_KEY)
    window.history.replaceState({}, '', '/admin#/login')
    vi.spyOn(authApi, 'status').mockResolvedValue({ initialized: true })
    let dispose = () => {}

    act(() => {
      dispose = mountReactApplication(container)
      expect(mountReactApplication(container)).toBe(dispose)
    })
    await waitFor(() => expect(container.querySelector('#auth-user')).not.toBeNull())

    act(dispose)
    expect(container).toBeEmptyDOMElement()
    expect(warning).not.toHaveBeenCalled()
    warning.mockRestore()
  })

  it('restores administrator task progress from the mounted root and releases it on unmount', async () => {
    localStorage.setItem(TOKEN_KEY, 'admin-token')
    window.history.replaceState({}, '', '/admin#/items')
    vi.spyOn(authApi, 'me').mockResolvedValue(authFixture.User)
    const scanProgressRequest = vi.spyOn(taskApi, 'progress').mockResolvedValue(scanProgress)
    const backgroundProgressRequest = vi.spyOn(backgroundApi, 'progress').mockResolvedValue({
      running: false, total: 0, done: 0, success: 0, skipped: 0, failed: 0,
      current: '', cancelled: false, aborted: false, kind: '', started_at: '', finished_at: '', error: '', failures: [],
    })
    const container = document.createElement('div')
    const dispose = mountReactApplication(container)

    await waitFor(() => expect(scanProgressRequest).toHaveBeenCalledOnce())
    expect(backgroundProgressRequest).toHaveBeenCalledTimes(2)
    expect(useReactTasksStore.getState().busy).toBe(false)

    act(dispose)
    expect(useReactTasksStore.getState().progress).toBeNull()
    expect(useReactTasksStore.getState().probe.progress).toBeNull()
    expect(useReactTasksStore.getState().scrape.progress).toBeNull()
  })
})
