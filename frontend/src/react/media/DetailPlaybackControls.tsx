import { ChevronDown, ExternalLink, MonitorPlay, RefreshCw } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState, type FocusEvent, type KeyboardEvent } from 'react'
import { apiClient } from '../../api/client'
import {
  audioTrackOptions, parsePlaybackSource, resolveTrackSelection, selectionMatches,
  subtitleTrackOptions, supportsNativeAudio, type PlaybackSource, type TrackSelection,
} from '../../lib/player-tracks'
import { externalPlayerURL, externalPlayers } from '../../lib/external-player'
import './detail-playback.css'

interface Props {
  itemId: number
  refreshKey: unknown
  onSelection: (selection: TrackSelection | undefined) => void
}

export function DetailPlaybackControls({ itemId, refreshKey, onSelection }: Props) {
  const [source, setSource] = useState<PlaybackSource>()
  const [selectedAudio, setSelectedAudio] = useState(-1)
  const [selectedSubtitle, setSelectedSubtitle] = useState(-1)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [expanded, setExpanded] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const menu = useRef<HTMLDivElement>(null)
  const toggle = useRef<HTMLButtonElement>(null)
  const request = useRef<AbortController | undefined>(undefined)
  const audio = useMemo(() => source ? audioTrackOptions(source, supportsNativeAudio(source)) : [], [source])
  const subtitles = useMemo(() => source ? subtitleTrackOptions(source, supportsNativeAudio(source)) : [], [source])
  const links = useMemo(() => {
    try {
      return source?.ExternalStreamUrl
        ? externalPlayers.map((player) => ({ ...player, url: externalPlayerURL(player.id, source.ExternalStreamUrl!) }))
        : []
    } catch { return [] }
  }, [source])

  const selection = useCallback((): TrackSelection | undefined => source ? {
    sourceId: source.Id, revision: source.TrackRevision, audioIndex: selectedAudio, subtitleIndex: selectedSubtitle,
  } : undefined, [selectedAudio, selectedSubtitle, source])
  const selectionRef = useRef(selection)
  selectionRef.current = selection

  const load = useCallback(async (preserve = false) => {
    const previous = preserve ? selectionRef.current() : undefined
    request.current?.abort()
    const current = new AbortController()
    request.current = current
    setSource(undefined)
    setExpanded(false)
    setError('')
    setLoading(true)
    onSelection(undefined)
    try {
      const result = parsePlaybackSource(await apiClient.request(`/Items/${encodeURIComponent(itemId)}/PlaybackInfo`, {
        auth: 'required', method: 'POST', readOnly: true, signal: current.signal,
      }))
      if (current.signal.aborted) return
      setSource(result)
      const selected = resolveTrackSelection(result, selectionMatches(result, previous) ? previous : undefined, supportsNativeAudio(result))
      setSelectedAudio(selected.audioIndex)
      setSelectedSubtitle(selected.subtitleIndex)
      onSelection({ sourceId: result.Id, revision: result.TrackRevision, ...selected })
    } catch (cause) {
      if (!current.signal.aborted) setError(cause instanceof Error ? cause.message : '播放选项加载失败')
    } finally {
      if (!current.signal.aborted) setLoading(false)
    }
  }, [itemId, onSelection])

  useEffect(() => {
    void load()
    return () => { request.current?.abort(); onSelection(undefined) }
  }, [itemId, refreshKey, load, onSelection])

  function change() { onSelection(selection()) }
  function closeMenu(restoreFocus = false) {
    setExpanded(false)
    if (restoreFocus) toggle.current?.focus()
  }
  function outside(event: PointerEvent) {
    if (event.target instanceof Node && !root.current?.contains(event.target)) closeMenu()
  }
  function focusout(event: FocusEvent<HTMLDivElement>) {
    if (!root.current?.contains(event.relatedTarget as Node | null)) closeMenu()
  }
  function keydown(event: KeyboardEvent<HTMLDivElement>) {
    if (!expanded) return
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); closeMenu(true); return }
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
    const items = Array.from(menu.current?.querySelectorAll<HTMLAnchorElement>('a') ?? [])
    if (!items.length) return
    event.preventDefault()
    const index = items.indexOf(document.activeElement as HTMLAnchorElement)
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1
      : (index + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length
    items[next]?.focus()
  }

  useEffect(() => {
    document.addEventListener('pointerdown', outside)
    return () => document.removeEventListener('pointerdown', outside)
  })

  return (
    <div ref={root} className="react-detail-playback" aria-busy={loading} onKeyDown={keydown} onBlur={focusout}>
      <div className="react-detail-track-fields">
        <label><span>音轨</span><select aria-label="选择音轨" value={selectedAudio} disabled={loading || !audio.length}
          onChange={(event) => {
            const audioIndex = Number(event.currentTarget.value)
            setSelectedAudio(audioIndex)
            onSelection(source ? { sourceId: source.Id, revision: source.TrackRevision, audioIndex, subtitleIndex: selectedSubtitle } : undefined)
          }}>
          {!audio.length && <option value={-1}>{loading ? '加载中…' : '默认音轨'}</option>}
          {audio.map((track) => <option key={track.value} value={track.value} disabled={track.disabled}>{track.label}{track.disabled ? ' · 需启用 FFmpeg' : ''}</option>)}
        </select></label>
        <label><span>字幕</span><select aria-label="选择字幕" value={selectedSubtitle} disabled={loading || !source}
          onChange={(event) => {
            const subtitleIndex = Number(event.currentTarget.value)
            setSelectedSubtitle(subtitleIndex)
            onSelection(source ? { sourceId: source.Id, revision: source.TrackRevision, audioIndex: selectedAudio, subtitleIndex } : undefined)
          }}>
          <option value={-1}>关闭字幕</option>
          {subtitles.map((track) => <option key={track.value} value={track.value} disabled={track.disabled}>{track.label}</option>)}
        </select></label>
        <div className="react-external-player">
          <button ref={toggle} type="button" className="react-detail-action" aria-label="外部播放器" aria-haspopup="menu"
            aria-expanded={expanded} disabled={!links.length} onClick={async () => {
              const next = !expanded
              setExpanded(next)
              if (next) requestAnimationFrame(() => menu.current?.querySelector<HTMLElement>('a')?.focus())
            }}>
            <MonitorPlay size={15} aria-hidden="true" /><span>外部播放</span><ChevronDown size={13} aria-hidden="true" />
          </button>
          {expanded && <div ref={menu} className="react-external-player-menu" role="menu" aria-label="外部播放器">
            {links.map((link) => <a key={link.id} href={link.url} data-player={link.id} role="menuitem" rel="noreferrer"
              title={`用 ${link.name} 打开原始媒体（需注册本机协议）`} onClick={() => closeMenu(true)}>
              <ExternalLink size={14} aria-hidden="true" />{link.name}
            </a>)}
          </div>}
        </div>
      </div>
      {error && <div className="react-detail-playback-error" role="alert"><span>{error}</span>
        <button className="react-icon-button" type="button" title="重新加载播放选项" aria-label="重新加载播放选项" onClick={() => void load()}><RefreshCw size={16} /></button>
      </div>}
    </div>
  )
}
