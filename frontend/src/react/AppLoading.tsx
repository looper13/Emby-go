import { LoaderCircle } from 'lucide-react'

export function AppLoading() {
  return (
    <div className="react-page-loading" role="status" aria-live="polite">
      <LoaderCircle size={18} aria-hidden="true" />
      <span>正在加载…</span>
    </div>
  )
}
