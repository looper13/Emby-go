import { useEffect, useRef, useState } from 'react'
import { Heart } from 'lucide-react'
import { favoritesApi } from '../../api/favorites'
import { useAppRuntimeStore } from '../app-store'
import './favorite-toggle.css'

interface Props {
  itemId: string | number
  favorite?: boolean
  label?: string
  onChanged?: (favorite: boolean) => void
  className?: string
}

export function FavoriteToggleButton({ itemId, favorite = false, label = '', onChanged, className = '' }: Props) {
  const [selected, setSelected] = useState(favorite)
  const [busy, setBusy] = useState(false)
  const generation = useRef(0)
  const showToast = useAppRuntimeStore((state) => state.showToast)

  useEffect(() => {
    generation.current += 1
    setSelected(favorite)
    setBusy(false)
  }, [itemId, favorite])

  useEffect(() => () => { generation.current += 1 }, [])

  async function toggle() {
    if (busy) return
    const current = ++generation.current
    setBusy(true)
    try {
      const next = await favoritesApi.set(String(itemId), !selected)
      if (generation.current !== current) return
      setSelected(next)
      onChanged?.(next)
    } catch (cause) {
      if (generation.current === current) showToast(cause instanceof Error ? cause.message : '收藏操作失败', 'error')
    } finally {
      if (generation.current === current) setBusy(false)
    }
  }

  const action = selected ? '取消收藏' : '收藏'
  const accessibleLabel = label ? `${action} · ${label}` : action
  return (
    <button type="button" className={`favorite-button${selected ? ' selected' : ''}${className ? ` ${className}` : ''}`}
      disabled={busy} aria-busy={busy} aria-pressed={selected} title={accessibleLabel} aria-label={accessibleLabel}
      onClick={(event) => { event.stopPropagation(); event.preventDefault(); void toggle() }}>
      <Heart size={18} fill={selected ? 'currentColor' : 'none'} aria-hidden="true" />
    </button>
  )
}
