import { Suspense, useEffect, useState, type ReactNode } from 'react'
import { RefreshCw } from 'lucide-react'
import { AppErrorBoundary } from './AppErrorBoundary'
import { AppLoading } from './AppLoading'
import { useAppRuntimeStore } from './app-store'

interface Props {
  children: ReactNode
}

export function AppProviders({ children }: Props) {
  const toasts = useAppRuntimeStore((state) => state.toasts)
  const [updateAvailable, setUpdateAvailable] = useState(false)

  useEffect(() => {
    const onPreloadError = (event: Event) => {
      event.preventDefault()
      setUpdateAvailable(true)
    }
    window.addEventListener('vite:preloadError', onPreloadError)
    return () => {
      window.removeEventListener('vite:preloadError', onPreloadError)
      useAppRuntimeStore.getState().clearToasts()
    }
  }, [])

  return (
    <>
      <AppErrorBoundary>
        <Suspense fallback={<AppLoading />}>{children}</Suspense>
      </AppErrorBoundary>
      <div id="toasts" className="toasts" aria-live="assertive">
        {toasts.map((toast) => (
          <div
            key={toast.id}
            className={`toast ${toast.kind}${toast.out ? ' out' : ''}`}
            role={toast.kind === 'error' ? 'alert' : 'status'}
          >
            {toast.message}
          </div>
        ))}
        {updateAvailable && (
          <div className="toast error" role="alert">
            <p>页面资源已更新。刷新会丢失未保存的输入。</p>
            <button className="btn" type="button" onClick={() => window.location.reload()}>
              <RefreshCw size={15} aria-hidden="true" />
              <span>刷新页面</span>
            </button>
          </div>
        )}
      </div>
    </>
  )
}
