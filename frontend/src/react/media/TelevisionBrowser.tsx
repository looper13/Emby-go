import { ArrowLeft, RefreshCw } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { televisionApi, type TelevisionItem } from '../../api/television'
import { imageURL } from '../../lib/image-url'
import { usePageRequest } from '../hooks/use-page-request'
import { useReactPreferencesStore } from '../preferences-store'
import { FavoriteToggleButton } from './FavoriteToggleButton'
import './television-browser.css'

interface Props {
  library: number
  search: string
  revision: number
  series?: string
  season?: string
  onOpen: (id: number) => void
  onBrowse: (series: string, season: string) => void
}

export function TelevisionBrowser({ library, search, revision, series = '', season: requestedSeason = '', onOpen, onBrowse }: Props) {
  const scope = usePageRequest()
  const hideImages = useReactPreferencesStore((state) => state.hideImages)
  const [items, setItems] = useState<TelevisionItem[]>([])
  const [seasons, setSeasons] = useState<TelevisionItem[]>([])
  const [selected, setSelected] = useState<TelevisionItem>()
  const [season, setSeason] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [total, setTotal] = useState(0)
  const [retryMore, setRetryMore] = useState(false)
  const itemsRef = useRef<TelevisionItem[]>([])
  const title = selected?.Name || '电视节目'

  const load = useCallback(async (more = false, selectedItem: TelevisionItem | null = null, selectedSeason = '') => {
    const current = scope.begin()
    setLoading(true)
    setError('')
    setRetryMore(more)
    const offset = more ? itemsRef.current.length : 0
    if (!more) { itemsRef.current = []; setItems([]) }
    try {
      const result = selectedItem
        ? await televisionApi.episodes(selectedItem.Id, selectedSeason, offset, current.signal)
        : await televisionApi.list(library, search, offset, current.signal)
      if (current.isCurrent()) {
        const next = more ? [...itemsRef.current, ...result.items] : result.items
        itemsRef.current = next
        setItems(next)
        setTotal(result.total)
      }
    } catch (cause) {
      if (current.isCurrent()) setError(cause instanceof Error ? cause.message : '加载失败')
    } finally {
      if (current.isCurrent()) setLoading(false)
    }
  }, [library, scope, search])

  const loadSeasons = useCallback(async () => {
    if (!series) return
    const current = scope.begin()
    setLoading(true)
    setError('')
    try {
      if (!series.startsWith(`tv-series:${library}:`)) throw new Error('剧集不属于当前媒体库')
      const item = await televisionApi.series(series, current.signal)
      if (!current.isCurrent()) return
      const result = await televisionApi.seasons(item.Id, current.signal)
      if (!current.isCurrent()) return
      setSelected(item)
      setSeasons(result.items)
      const chosen = result.items.find((entry) => entry.Id === requestedSeason)?.Id
        ?? result.items.find((entry) => entry.IndexNumber > 0)?.Id
        ?? result.items[0]?.Id
        ?? ''
      setSeason(chosen)
      if (requestedSeason && chosen !== requestedSeason) {
        onBrowse(item.Id, chosen)
        setLoading(false)
        return
      }
      if (chosen) await load(false, item, chosen)
      else {
        itemsRef.current = []
        setItems([])
        setTotal(0)
        setLoading(false)
      }
    } catch (cause) {
      if (current.isCurrent()) {
        setError(cause instanceof Error ? cause.message : '加载失败')
        setLoading(false)
      }
    }
  }, [library, load, onBrowse, requestedSeason, scope, series])

  const enter = useCallback(() => {
    setSelected(undefined)
    setSeasons([])
    setSeason('')
    setTotal(0)
    setItems([])
    setRetryMore(false)
    itemsRef.current = []
    if (series) void loadSeasons()
    else void load(false, null, '')
  }, [load, loadSeasons, series])

  useEffect(() => {
    enter()
    return () => scope.cancel()
  }, [enter, scope, revision])

  function select(item: TelevisionItem) {
    if (item.Type === 'Episode') onOpen(Number(item.Id))
    else onBrowse(item.Id, '')
  }

  function retry() {
    if (series && !seasons.length) void loadSeasons()
    else void load(retryMore, selected ?? null, season)
  }

  function back() { onBrowse('', '') }

  return (
    <section className="react-television-browser" aria-label="电视节目浏览">
      <div className="react-tv-heading">
        {(selected || series) && <button className="react-icon-button" type="button" title="返回电视节目" aria-label="返回电视节目" onClick={back}><ArrowLeft size={16} /></button>}
        <h3>{title}</h3>
        {selected && <FavoriteToggleButton itemId={selected.Id} favorite={selected.Favorite} label={selected.Name} />}
        <span className="react-tv-count">{total} {selected ? '集' : '部'}</span>
      </div>

      {selected && seasons.length > 0 && <div className="react-tv-seasons" role="tablist" aria-label="季">
        {seasons.map((entry) => <button key={entry.Id} type="button" role="tab" aria-selected={season === entry.Id}
          className={season === entry.Id ? 'is-active' : ''} onClick={() => onBrowse(selected.Id, entry.Id)}>{entry.Name}</button>)}
      </div>}

      {selected && season && <div className="react-tv-season-favorite">
        <strong>{seasons.find((entry) => entry.Id === season)?.Name}</strong>
        <FavoriteToggleButton itemId={season} favorite={seasons.find((entry) => entry.Id === season)?.Favorite}
          label={seasons.find((entry) => entry.Id === season)?.Name} />
      </div>}

      {error && <p className="react-tv-error" role="alert">{error}<button type="button" className="react-wall-more" onClick={retry}><RefreshCw size={13} />重试</button></p>}

      <div className={`react-tv-grid${selected ? ' is-episodes' : ''}`} aria-busy={loading}>
        {loading && !items.length && Array.from({ length: 8 }, (_, index) => <div className="react-tv-skeleton" key={index} />)}
        {items.map((item) => {
          const imageTag = item.ImageTags.Primary || (item.Type === 'Episode' ? item.ImageTags.Thumb : '')
          const imageType = item.ImageTags.Primary ? 'Primary' : 'Thumb'
          return <article key={item.Id} className="react-tv-entry">
            <button type="button" className="react-tv-card" data-tv-id={item.Id} onClick={() => select(item)}>
              <span className="react-tv-cover">
                {!hideImages && imageTag
                  ? <img src={imageURL(item.Id, imageType, 320, imageTag)} loading="lazy" alt="" />
                  : <span>{item.Name}</span>}
              </span>
              <strong>{selected ? `${item.IndexNumber}. ${item.Name}` : item.Name}</strong>
              <small>{selected ? item.SeriesName : [item.ProductionYear || '', `${item.ChildCount} 季`, `${item.RecursiveItemCount} 集`].filter(Boolean).join(' · ')}</small>
            </button>
            <FavoriteToggleButton className="television-favorite-button" itemId={item.Id} favorite={item.Favorite} label={item.Name}
              onChanged={(favorite) => {
                const next = itemsRef.current.map((entry) => entry.Id === item.Id ? { ...entry, Favorite: favorite } : entry)
                itemsRef.current = next
                setItems(next)
              }} />
          </article>
        })}
      </div>

      {loading && items.length > 0 && <p className="react-tv-state" role="status">加载中…</p>}
      {!loading && !error && !items.length && <p className="react-tv-state">{selected ? '暂无单集' : '暂无电视节目'}</p>}
      {items.length < total && !error && <button className="react-tv-load-more" type="button" disabled={loading} onClick={() => void load(true)}>加载更多</button>}
    </section>
  )
}
