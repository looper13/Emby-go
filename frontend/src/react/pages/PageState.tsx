import { AlertCircle, LoaderCircle, RefreshCw } from 'lucide-react'
import type { ReactNode } from 'react'

export function PageState({ loading, error, empty, retry, label = '数据' }: {
  loading: boolean
  error: string
  empty: boolean
  retry: () => void
  label?: string
}) {
  if (loading) return <p className="react-page-state" role="status"><LoaderCircle size={17} aria-hidden="true" />正在读取{label}…</p>
  if (error) return <div className="react-page-state react-page-error" role="alert"><AlertCircle size={17} aria-hidden="true" /><span>{error}</span><button className="react-icon-button" type="button" title="重试" aria-label="重试" onClick={retry}><RefreshCw size={16} aria-hidden="true" /></button></div>
  if (empty) return <p className="react-page-state" role="status">暂无{label}</p>
  return null
}

export function PageHeading({ title, description, action }: {
  title: string
  description?: string
  action?: ReactNode
}) {
  return <header className="react-page-heading"><div><h2>{title}</h2>{description && <p>{description}</p>}</div>{action}</header>
}
