import {
  Clock3, CornerDownLeft, Film, Folder, Heart, Search, Settings, Tv, Users, X,
  type LucideIcon,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent, type MouseEvent } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router-dom'
import { actorsApi, type ActorEntry } from '../api/actors'
import { adminApi } from '../api/admin'
import { catalogApi } from '../api/catalog'
import type { LibraryDto, MovieDto } from '../api/contracts'
import { entityIdOf } from '../lib/media'
import { imageURL } from '../lib/image-url'
import { useReactAuthStore } from './auth-store'
import { useReactPreferencesStore } from './preferences-store'
import './global-search.css'

interface SearchResultItem {
  key: string
  category: 'movie' | 'actor' | 'library' | 'nav'
  title: string
  subtitle?: string
  movie?: MovieDto
  actor?: ActorEntry
  library?: LibraryDto
  icon?: LucideIcon
  target: string
}

const quickNavItems: Array<SearchResultItem & { adminOnly?: boolean }> = [
  { key: 'nav:items', category: 'nav', title: '媒体墙', subtitle: '全部已入库影片与单集', icon: Film, target: '/items' },
  { key: 'nav:favorites', category: 'nav', title: '我的收藏', subtitle: '标有爱心的影片与演员', icon: Heart, target: '/favorites' },
  { key: 'nav:actors', category: 'nav', title: '演职人员', subtitle: '演员列表与头像管理', icon: Users, target: '/actors', adminOnly: true },
  { key: 'nav:tv', category: 'nav', title: '电视节目', subtitle: '剧集与季集浏览', icon: Tv, target: '/items?view=' },
  { key: 'nav:settings', category: 'nav', title: '系统设置', subtitle: '转码、监控与运行状态', icon: Settings, target: '/settings', adminOnly: true },
  { key: 'nav:scheduled', category: 'nav', title: '计划任务', subtitle: '定时扫描与刮削排程', icon: Clock3, target: '/scheduled', adminOnly: true },
]

function movieSubtitle(movie: MovieDto) {
  const parts = movie.television
    ? [`${movie.television.title} · S${movie.television.season}E${movie.television.episode}`]
    : movie.Year ? [String(movie.Year)] : []
  if (movie.Rating) parts.push(`★ ${movie.Rating}`)
  return parts.join(' · ')
}

export function GlobalSearchDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate()
  const administrator = useReactAuthStore((state) => state.user?.Policy.IsAdministrator === true)
  const hideImages = useReactPreferencesStore((state) => state.hideImages)
  const input = useRef<HTMLInputElement>(null)
  const shell = useRef<HTMLDivElement>(null)
  const activeRequest = useRef<AbortController | undefined>(undefined)
  const libraryRequest = useRef<AbortController | undefined>(undefined)
  const searchTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const libraryCache = useRef<LibraryDto[]>([])
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(false)
  const [movies, setMovies] = useState<MovieDto[]>([])
  const [imageTags, setImageTags] = useState<Record<string, Record<string, string>>>({})
  const [actors, setActors] = useState<ActorEntry[]>([])
  const [libraries, setLibraries] = useState<LibraryDto[]>([])
  const [selectedIndex, setSelectedIndex] = useState(0)
  const queryRef = useRef(query)
  queryRef.current = query
  const openRef = useRef(open)
  openRef.current = open
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose

  const loadLibraries = useCallback((signal: AbortSignal) => {
    if (libraryCache.current.length > 0) return
    libraryRequest.current?.abort()
    const controller = new AbortController()
    libraryRequest.current = controller
    signal.addEventListener('abort', () => controller.abort(), { once: true })
    void adminApi.libraries(controller.signal).then((result) => {
      if (controller.signal.aborted) return
      const next = result.items ?? []
      libraryCache.current = next
      setLibraries(next)
    }).catch(() => {
      if (!controller.signal.aborted) setLibraries([])
    })
  }, [])

  const term = query.trim()
  const matchedLibraries = useMemo(() => {
    const lowered = term.toLocaleLowerCase()
    if (!lowered) return libraries
    return libraries.filter((library) => library.Name.toLocaleLowerCase().includes(lowered)
      || library.Path.toLocaleLowerCase().includes(lowered))
  }, [libraries, term])

  const results = useMemo<SearchResultItem[]>(() => {
    if (!term) {
      const libraryResults = libraries.slice(0, 4).map((library): SearchResultItem => ({
        key: `lib:${library.Id}`, category: 'library', title: library.Name,
        subtitle: library.Path || (library.SourceType === 'avdb_javdb' ? 'AVdb 在线资料库' : ''),
        library, target: `/items?library_id=${library.Id}`,
      }))
      return [...libraryResults, ...quickNavItems.filter((item) => !item.adminOnly || administrator)]
    }
    return [
      ...movies.map((movie): SearchResultItem => ({
        key: `movie:${movie.id}`, category: 'movie', title: movie.Title || `#${movie.id}`,
        subtitle: movieSubtitle(movie), movie, target: `/item/${movie.id}`,
      })),
      ...actors.map((actor): SearchResultItem => ({
        key: `actor:${actor.name}`, category: 'actor', title: actor.name,
        subtitle: actor.movie_count ? `${actor.movie_count} 部作品` : undefined,
        actor, target: `/items?person=${encodeURIComponent(actor.name)}`,
      })),
      ...matchedLibraries.map((library): SearchResultItem => ({
        key: `lib:${library.Id}`, category: 'library', title: library.Name,
        subtitle: library.Path || (library.SourceType === 'avdb_javdb' ? 'AVdb 在线资料库' : ''),
        library, target: `/items?library_id=${library.Id}`,
      })),
    ]
  }, [actors, administrator, libraries, matchedLibraries, movies, term])

  const movieResults = results.filter((item) => item.category === 'movie')
  const actorResults = results.filter((item) => item.category === 'actor')
  const libraryResults = results.filter((item) => item.category === 'library')
  const navigationResults = results.filter((item) => item.category === 'nav')

  useEffect(() => {
    if (!open) return
    setQuery('')
    setMovies([])
    setActors([])
    setSelectedIndex(0)
    const previousOverflow = document.body.style.overflow
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const controller = new AbortController()
    document.body.style.overflow = 'hidden'
    input.current?.focus()
    loadLibraries(controller.signal)

    function trapTab(event: globalThis.KeyboardEvent) {
      if (event.key !== 'Tab' || !shell.current) return
      const focusable = Array.from(shell.current.querySelectorAll<HTMLElement>(
        'button, input, select, a[href], [tabindex]:not([tabindex="-1"])',
      )).filter((element) => !element.hasAttribute('disabled') && element.tabIndex >= 0)
      const first = focusable[0]
      const last = focusable.at(-1)
      if (event.shiftKey && (document.activeElement === first || !shell.current.contains(document.activeElement))) {
        event.preventDefault()
        last?.focus()
      } else if (!event.shiftKey && (document.activeElement === last || !shell.current.contains(document.activeElement))) {
        event.preventDefault()
        first?.focus()
      }
    }

    window.addEventListener('keydown', trapTab, true)
    return () => {
      controller.abort()
      libraryRequest.current?.abort()
      activeRequest.current?.abort()
      if (searchTimer.current) clearTimeout(searchTimer.current)
      window.removeEventListener('keydown', trapTab, true)
      document.body.style.overflow = previousOverflow
      queueMicrotask(() => {
        if (!openRef.current && previousFocus?.isConnected) previousFocus.focus({ preventScroll: true })
      })
    }
  }, [loadLibraries, open])

  useEffect(() => {
    if (!open) return
    if (searchTimer.current) clearTimeout(searchTimer.current)
    const searchTerm = query.trim()
    setSelectedIndex(0)
    if (!searchTerm) {
      activeRequest.current?.abort()
      setMovies([])
      setActors([])
      setLoading(false)
      return
    }
    const timer = setTimeout(() => {
      activeRequest.current?.abort()
      const controller = new AbortController()
      activeRequest.current = controller
      setLoading(true)
      void Promise.allSettled([
        catalogApi.items(new URLSearchParams({ search: searchTerm, limit: '6' }), controller.signal),
        actorsApi.list({ library_id: 0, search: searchTerm, status: 'all', offset: 0, limit: 4 }, controller.signal),
      ]).then(([wallResult, actorResult]) => {
        if (controller.signal.aborted || queryRef.current.trim() !== searchTerm) return
        if (wallResult.status === 'fulfilled') {
          setMovies(wallResult.value.items ?? [])
          setImageTags(wallResult.value.image_tags ?? {})
        } else setMovies([])
        if (actorResult.status === 'fulfilled') setActors(actorResult.value.items ?? [])
        else setActors([])
      }).finally(() => {
        if (!controller.signal.aborted && queryRef.current.trim() === searchTerm) setLoading(false)
      })
    }, 160)
    searchTimer.current = timer
    return () => clearTimeout(timer)
  }, [open, query])

  useEffect(() => {
    if (!open || !results[selectedIndex]) return
    shell.current?.querySelector<HTMLElement>(`#global-search-result-${selectedIndex}`)?.scrollIntoView?.({ block: 'nearest' })
  }, [open, results, selectedIndex])

  function selectItem(item: SearchResultItem) {
    navigate(item.target)
    onCloseRef.current()
  }

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === 'Escape') {
      event.preventDefault()
      onCloseRef.current()
    } else if (event.key === 'ArrowDown') {
      event.preventDefault()
      if (results.length) setSelectedIndex((index) => (index + 1) % results.length)
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      if (results.length) setSelectedIndex((index) => (index - 1 + results.length) % results.length)
    } else if (event.key === 'Enter' && event.target === input.current) {
      event.preventDefault()
      const selected = results[selectedIndex]
      if (selected) selectItem(selected)
    }
  }

  function closeFromBackdrop(event: MouseEvent<HTMLDivElement>) {
    if (event.target === event.currentTarget) onCloseRef.current()
  }

  function renderItem(item: SearchResultItem) {
    const index = results.findIndex((result) => result.key === item.key)
    const selected = index === selectedIndex
    const Icon = item.icon ?? (item.category === 'library' ? Folder : Film)
    const imageType = item.movie?.PosterPath ? 'Primary' : 'Thumb'
    const imageTag = item.movie ? imageTags[item.movie.id]?.[imageType] : undefined
    const imageSource = item.movie
      ? item.movie.PosterPath || item.movie.LandscapePath ? imageURL(item.movie.id, imageType, 120, imageTag) : item.movie.cover_url
      : item.actor?.has_image ? imageURL(entityIdOf('person', item.actor.name), 'Primary', 120, item.actor.image_tag) : ''
    return <button key={item.key} id={`global-search-result-${index}`} type="button" role="option" tabIndex={-1}
      className={`search-dialog-item${selected ? ' is-selected' : ''}`} aria-selected={selected}
      onMouseEnter={() => setSelectedIndex(index)} onClick={() => selectItem(item)}>
      {item.category === 'movie' && <span className="search-item-thumb">
        {!hideImages && imageSource ? <img src={imageSource} alt="" loading="lazy" decoding="async" /> : <Film className="thumb-icon-fallback" size={18} aria-hidden="true" />}
      </span>}
      {item.category === 'actor' && <span className="search-item-avatar">
        {!hideImages && imageSource ? <img src={imageSource} alt="" loading="lazy" decoding="async" /> : <span className="avatar-letter">{(item.actor?.name || '?').trim().slice(0, 1)}</span>}
      </span>}
      {(item.category === 'library' || item.category === 'nav') && <span className="search-item-icon-box"><Icon size={18} aria-hidden="true" /></span>}
      <span className="search-item-info"><span className="search-item-title">{item.title}</span>{item.subtitle && <span className="search-item-sub">{item.subtitle}</span>}</span>
      <CornerDownLeft className="item-enter-icon" size={14} aria-hidden="true" />
    </button>
  }

  if (!open) return null

  return createPortal(<div className="search-backdrop react-theme-scope" onClick={closeFromBackdrop}>
    <div ref={shell} id="global-search-dialog" className="search-modal" role="dialog" aria-modal="true" aria-label="全局搜索" onKeyDown={handleKeyDown}>
      <div className="search-header">
        <Search className="search-header-icon" size={20} aria-hidden="true" />
        <input ref={input} className="search-input" type="text" value={query} placeholder="搜索影片、剧集、演员、媒体库..."
          aria-label="全局搜索" aria-controls="global-search-results"
          aria-activedescendant={results[selectedIndex] ? `global-search-result-${selectedIndex}` : undefined}
          autoComplete="off" spellCheck={false} onChange={(event) => setQuery(event.currentTarget.value)} />
        {query && <button className="icon-btn search-clear-btn" type="button" title="清空搜索" aria-label="清空搜索" onClick={() => { setQuery(''); input.current?.focus() }}>
          <X size={16} aria-hidden="true" />
        </button>}
        <button className="search-close-btn" type="button" title="关闭搜索" aria-label="关闭搜索" onClick={() => onCloseRef.current()}><kbd>ESC</kbd><X size={15} aria-hidden="true" /></button>
      </div>
      <div className={`search-loading-bar${loading ? ' is-loading' : ''}`} role={loading ? 'progressbar' : undefined} aria-label={loading ? '正在搜索' : undefined} />
      <div className="search-body" id="global-search-results" role="listbox" aria-label="搜索结果">
        {results.length === 0 && !loading ? <div className="search-empty">
          <p>未找到与「<strong>{query}</strong>」相关的结果</p><small>请尝试使用其它关键词、原名或番号</small>
        </div> : <>
          {term && movieResults.length > 0 && <section className="search-group" role="group" aria-label="影片与单集">
            <div className="search-group-title"><Film size={13} aria-hidden="true" /><span>影片与单集</span></div>{movieResults.map(renderItem)}
          </section>}
          {term && actorResults.length > 0 && <section className="search-group" role="group" aria-label="演职人员">
            <div className="search-group-title"><Users size={13} aria-hidden="true" /><span>演职人员</span></div>{actorResults.map(renderItem)}
          </section>}
          {libraryResults.length > 0 && <section className="search-group" role="group" aria-label="媒体库">
            <div className="search-group-title"><Folder size={13} aria-hidden="true" /><span>媒体库</span></div>{libraryResults.map(renderItem)}
          </section>}
          {!term && navigationResults.length > 0 && <section className="search-group" role="group" aria-label="常用导航入口">
            <div className="search-group-title"><span>常用导航入口</span></div>{navigationResults.map(renderItem)}
          </section>}
        </>}
      </div>
      <div className="search-footer">
        <span className="search-shortcut"><kbd>↑</kbd><kbd>↓</kbd> 切换选项</span>
        <span className="search-shortcut"><kbd>↵</kbd> 跳转</span>
        <span className="search-shortcut"><kbd>ESC</kbd> 关闭</span>
      </div>
    </div>
  </div>, document.body)
}
