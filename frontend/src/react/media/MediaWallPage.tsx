import { Film, Grid2X2, Layers3, Shuffle, Tv } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { catalogApi } from '../../api/catalog'
import type { LibraryDto, MovieDto } from '../../api/contracts'
import { mediaApi } from '../../api/media'
import { entityParams, probeSummary, statusText } from '../../lib/media'
import { usePageRequest } from '../hooks/use-page-request'
import { useAppRuntimeStore } from '../app-store'
import { useReactAuthStore } from '../auth-store'
import { useReactMediaWallStore, WALL_PAGE_SIZE } from '../media-wall-store'
import { useReactPlayerStore } from '../player-store'
import { useReactPreferencesStore } from '../preferences-store'
import { useReactTasksStore } from '../tasks-store'
import { saveViewPosition, useViewHistory } from '../view-history'
import { favoriteEntityId, favoritesApi, type FavoriteItem } from '../../api/favorites'
import { MediaCard } from './MediaCard'
import { FavoriteToggleButton } from './FavoriteToggleButton'
import { TelevisionBrowser } from './TelevisionBrowser'
import { WallFilters } from './WallFilters'
import './media-wall.css'

function apiQuery(locationSearch: string) {
  const query = new URLSearchParams(locationSearch)
  query.delete('limit')
  query.delete('offset')
  return query
}

export function MediaWallPage() {
  const location = useLocation()
  const navigate = useNavigate()
  const history = useViewHistory()
  const scope = usePageRequest()
  const query = useMemo(() => new URLSearchParams(location.search), [location.search])
  const queryKey = useMemo(() => apiQuery(location.search).toString(), [location.search])
  const [libraries, setLibraries] = useState<LibraryDto[]>([])
  const [entityFavorite, setEntityFavorite] = useState<FavoriteItem | null>(null)
  const [pageReady, setPageReady] = useState(false)
  const [pageError, setPageError] = useState('')
  const [search, setSearch] = useState(query.get('search') || '')
  const [randomPlaying, setRandomPlaying] = useState(false)
  const [busy, setBusy] = useState<Set<string>>(() => new Set())
  const requestRef = useRef<ReturnType<typeof scope.begin> | null>(null)
  const searchTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const randomController = useRef<AbortController | null>(null)
  const moreRef = useRef<HTMLDivElement>(null)
  const activeRef = useRef(true)
  const locationRef = useRef(`${location.pathname}${location.search}`)
  locationRef.current = `${location.pathname}${location.search}`

  const items = useReactMediaWallStore((state) => state.items)
  const imageTags = useReactMediaWallStore((state) => state.imageTags)
  const userdata = useReactMediaWallStore((state) => state.userdata)
  const total = useReactMediaWallStore((state) => state.total)
  const loading = useReactMediaWallStore((state) => state.loading)
  const refreshing = useReactMediaWallStore((state) => state.refreshing)
  const refreshError = useReactMediaWallStore((state) => state.refreshError)
  const done = useReactMediaWallStore((state) => state.done)
  const wallError = useReactMediaWallStore((state) => state.error)
  const wallDirty = useReactMediaWallStore((state) => state.dirty)
  const wallLoadedAt = useReactMediaWallStore((state) => state.loadedAt)
  const wallQueryKey = useReactMediaWallStore((state) => state.queryKey)
  const wallPrepare = useReactMediaWallStore((state) => state.prepare)
  const wallLoadMore = useReactMediaWallStore((state) => state.loadMore)
  const wallRefresh = useReactMediaWallStore((state) => state.refresh)
  const wallCancel = useReactMediaWallStore((state) => state.cancel)
  const markDirty = useReactMediaWallStore((state) => state.markDirty)

  const user = useReactAuthStore((state) => state.user)
  const administrator = user?.Policy.IsAdministrator === true
  const playerMedia = useReactPlayerStore((state) => state.media)
  const playerOpen = useReactPlayerStore((state) => state.current !== null)
  const showToast = useAppRuntimeStore((state) => state.showToast)
  const revision = useReactTasksStore((state) => state.revision)
  const tasks = useReactTasksStore((state) => state)
  const cardSize = useReactPreferencesStore((state) => state.cardSize)
  const setCardSize = useReactPreferencesStore((state) => state.setCardSize)

  const libraryID = query.get('library_id') || ''
  const library = libraries.find((item) => String(item.Id) === libraryID)
  const activeEntity = entityParams.map(([key, label]) => ({ key, label, value: query.get(key) || '' })).find((item) => item.value)
  const televisionLibrary = library?.Type === 'tvshows' || library?.Type === 'mixed'
  const browseTelevision = televisionLibrary && query.get('view') !== 'index' && !query.get('status') && !activeEntity && !query.get('scrape') && query.get('favorite') !== 'true'
  const showWall = !browseTelevision || library?.Type !== 'tvshows'
  const scopeLabel = [library?.Name, activeEntity ? `${activeEntity.label}：${activeEntity.value}` : '', statusText[query.get('status') || ''] || '']
    .filter(Boolean).join(' · ')
  const emptySubject = activeEntity ? `「${activeEntity.value}」的影片` : library ? `「${library.Name}」的影片` : '影片'
  const emptyTitle = query.get('status')
    ? `没有 ${statusText[query.get('status') || ''] || query.get('status')} 的${emptySubject}`
    : `没有符合条件的${emptySubject}`

  useEffect(() => {
    setSearch(query.get('search') || '')
  }, [location.search])

  useEffect(() => {
    let active = true
    setEntityFavorite(null)
    if (!activeEntity) return () => { active = false }
    const controller = new AbortController()
    void favoritesApi.item(favoriteEntityId(activeEntity.key, activeEntity.value), controller.signal)
      .then((favorite) => { if (active) setEntityFavorite(favorite) })
      .catch(() => {})
    return () => { active = false; controller.abort() }
  }, [activeEntity?.key, activeEntity?.value])

  useEffect(() => {
    activeRef.current = true
    return () => { activeRef.current = false }
  }, [])

  useEffect(() => () => {
    clearTimeout(searchTimer.current)
    randomController.current?.abort()
  }, [])

  useEffect(() => {
    const current = scope.begin()
    requestRef.current = current
    const viewTarget = history.target
    const state = useReactMediaWallStore.getState()
    const cached = state.loadedAt > 0 && state.queryKey === queryKey
    const preservePosition = Boolean(viewTarget) || cached
    setPageReady(false)
    setPageError('')
    if (!preservePosition) window.scrollTo(0, 0)

    async function enter() {
      try {
        const result = await catalogApi.libraries(current.signal)
        if (!current.isCurrent()) return
        const visibleLibraries = (result.items || []).filter((item) => item.Enabled !== false)
        setLibraries((prev) => {
          if (prev.length === visibleLibraries.length && prev.every((p, i) => p.Id === visibleLibraries[i]?.Id && p.Name === visibleLibraries[i]?.Name && p.Type === visibleLibraries[i]?.Type)) {
            return prev
          }
          return visibleLibraries
        })

        const selected = visibleLibraries.find((item) => String(item.Id) === libraryID)
        if (selected?.SourceType === 'avdb_javdb') {
          const destination = new URLSearchParams({ library_id: libraryID })
          navigate(`/r18?${destination}`, { replace: true })
          return
        }
        if (libraryID && !selected) {
          const next = new URLSearchParams(location.search)
          next.delete('library_id')
          navigate(`/items${next.size ? `?${next}` : ''}`, { replace: true })
          return
        }

        const isTv = selected?.Type === 'tvshows' || selected?.Type === 'mixed'
        const browseTv = isTv && query.get('view') !== 'index' && !query.get('status') && !activeEntity && !query.get('scrape') && query.get('favorite') !== 'true'
        const shouldShowWall = !browseTv || selected?.Type !== 'tvshows'

        const params = new URLSearchParams(queryKey)
        if (browseTv && selected?.Type === 'mixed') params.set('media_type', 'Movie')
        const restored = wallPrepare(params, preservePosition)
        if (shouldShowWall && !restored) {
          const wanted = Math.max(WALL_PAGE_SIZE, viewTarget?.wallCount || WALL_PAGE_SIZE)
          while (current.isCurrent() && useReactMediaWallStore.getState().items.length < wanted
            && !useReactMediaWallStore.getState().done && !useReactMediaWallStore.getState().error) {
            const loaded = await wallLoadMore(current.signal)
            if (!loaded) break
          }
        }
        if (!current.isCurrent()) return
        setPageReady(true)

        if (viewTarget) {
          requestAnimationFrame(() => {
            const card = Array.from(document.querySelectorAll<HTMLElement>('.wall-card'))
              .find((item) => item.dataset.play === viewTarget.focusID)
            card?.focus({ preventScroll: true })
            window.scrollTo(0, viewTarget.scrollY)
          })
        }
        if (restored && useReactMediaWallStore.getState().needsRefresh()) void revalidate(current)
      } catch (cause) {
        if (current.isCurrent()) setPageError(cause instanceof Error ? cause.message : '加载失败')
      }
    }

    async function revalidate(request: ReturnType<typeof scope.begin>) {
      if (!request.isCurrent()) return
      const refreshed = await wallRefresh(request.signal)
      if (!request.isCurrent()) return
      if (refreshed && useReactMediaWallStore.getState().dirty) { void revalidate(request); return }
      maybeLoadRef.current()
    }

    void enter()
    return () => {
      scope.cancel()
      wallCancel()
    }
  }, [history.target, libraryID, location.search, navigate, queryKey, revision, scope, wallCancel, wallLoadMore, wallPrepare, wallRefresh])

  const loadMore = useCallback(async (retry = false) => {
    const request = requestRef.current
    const state = useReactMediaWallStore.getState()
    if (!pageReady || !request?.isCurrent() || state.loading || state.done || state.refreshError) return false
    return wallLoadMore(request.signal, retry)
  }, [pageReady, wallLoadMore])

  const maybeLoadRef = useRef<() => void>(() => {})
  const maybeLoad = useCallback(() => {
    const request = requestRef.current
    const state = useReactMediaWallStore.getState()
    if (!pageReady || !request?.isCurrent() || state.loading || state.done || state.error || state.refreshError) return
    if (moreRef.current && moreRef.current.getBoundingClientRect().top <= window.innerHeight + 600) {
      void loadMore().then((loaded) => { if (loaded) requestAnimationFrame(() => maybeLoadRef.current()) })
    }
  }, [loadMore, pageReady])
  maybeLoadRef.current = maybeLoad

  useEffect(() => {
    if (!pageReady) return
    window.addEventListener('scroll', maybeLoad, { passive: true })
    maybeLoad()
    return () => window.removeEventListener('scroll', maybeLoad)
  }, [maybeLoad, pageReady, items.length, loading, done])

  function goFilter(key: string, nextValue: string, toggle = false) {
    clearTimeout(searchTimer.current)
    if (key === 'library_id' && libraries.some((item) => String(item.Id) === nextValue && item.SourceType === 'avdb_javdb')) {
      navigate(`/r18?library_id=${encodeURIComponent(nextValue)}`)
      return
    }
    const next = new URLSearchParams(location.search)
    if (nextValue && !(toggle && query.get(key) === nextValue)) next.set(key, nextValue)
    else next.delete(key)
    next.delete('limit')
    next.delete('offset')
    if (key === 'library_id' || key === 'view') { next.delete('tv_series'); next.delete('tv_season') }
    if (key === 'status') next.delete('search')
    if (key === 'sort') next.delete('order')
    navigate(`/items${next.size ? `?${next}` : ''}`)
  }

  function updateSearch(nextSearch: string) {
    setSearch(nextSearch)
    clearTimeout(searchTimer.current)
    searchTimer.current = setTimeout(() => {
      const next = new URLSearchParams(location.search)
      next.delete('tv_series')
      next.delete('tv_season')
      if (nextSearch.trim()) next.set('search', nextSearch.trim())
      else next.delete('search')
      next.delete('limit')
      next.delete('offset')
      navigate(`/items${next.size ? `?${next}` : ''}`, { replace: true })
    }, 400)
  }

  function clearEntity() {
    clearTimeout(searchTimer.current)
    const next = new URLSearchParams(location.search)
    entityParams.forEach(([key]) => next.delete(key))
    navigate(`/items${next.size ? `?${next}` : ''}`)
  }

  function clearFilters() {
    clearTimeout(searchTimer.current)
    const next = new URLSearchParams(location.search)
    for (const key of ['library_id', 'status', 'scrape', 'favorite', 'genre', 'tag', 'studio', 'person', 'collection', 'source_protocol']) next.delete(key)
    navigate(`/items${next.size ? `?${next}` : ''}`)
  }

  function browseSeries(series: string, season: string) {
    const next = new URLSearchParams(location.search)
    if (series) next.set('tv_series', series)
    else next.delete('tv_series')
    if (season) next.set('tv_season', season)
    else next.delete('tv_season')
    navigate(`/items${next.size ? `?${next}` : ''}`, { replace: true })
  }

  function shuffle() {
    const next = new URLSearchParams(location.search)
    next.set('sort', 'random')
    next.delete('order')
    next.delete('offset')
    navigate(`/items?${next}`)
  }

  function openMovie(id: number) {
    saveViewPosition({ scrollY: Math.max(0, window.scrollY), wallCount: items.length, focusID: String(id) })
    document.querySelector<HTMLElement>(`.wall-card[data-play="${id}"]`)?.focus({ preventScroll: true })
    navigate(`/item/${id}${location.search}`)
  }

  async function quickPlay(movie: MovieDto) {
    playerMedia(movie)
  }

  async function playRandom() {
    if (randomPlaying || total === 0) return
    randomController.current?.abort()
    const controller = new AbortController()
    randomController.current = controller
    setRandomPlaying(true)
    try {
      const randomQuery = apiQuery(location.search)
      randomQuery.set('sort', 'random')
      randomQuery.set('seed', String((crypto.getRandomValues(new Uint32Array(1))[0]! & 0x7fffffff) || 1))
      randomQuery.set('limit', '1')
      randomQuery.set('offset', '0')
      const result = await catalogApi.items(randomQuery, controller.signal)
      if (controller.signal.aborted) return
      if (result.items[0]) playerMedia(result.items[0])
      else if (items[0]) playerMedia(items[Math.floor(Math.random() * items.length)]!)
      else showToast('未找到可播放的影片', 'error')
    } catch (cause) {
      if (!controller.signal.aborted) showToast(cause instanceof Error ? cause.message : '随机播放失败', 'error')
    } finally {
      if (randomController.current === controller && activeRef.current) setRandomPlaying(false)
    }
  }

  async function runCardAction(kind: 'probe' | 'reread' | 'remove', id: number) {
    const key = `${kind}:${id}`
    if (busy.has(key)) return
    if (kind === 'remove' && !window.confirm('确定删除该影片？将删除本地文件（STRM/视频、NFO、图片等）并清理关联网盘源文件。')) return
    const owner = `${location.pathname}${location.search}`
    setBusy((current) => new Set(current).add(key))
    try {
      if (kind === 'probe') showToast('正在探测…')
      if (kind === 'probe') showToast(probeSummary(await mediaApi.probe(id)))
      else if (kind === 'reread') {
        const status = await mediaApi.reread(id)
        showToast(`状态已更新：${statusText[status] || status}`)
      } else {
        await mediaApi.remove(id)
        showToast('已删除影片及文件')
      }
      if (activeRef.current && owner === locationRef.current) {
        markDirty()
        const request = requestRef.current
        if (request?.isCurrent()) {
          await wallRefresh(request.signal)
          maybeLoadRef.current()
        }
      }
    } catch (cause) {
      showToast(cause instanceof Error ? cause.message : '操作失败', 'error')
    } finally {
      setBusy((current) => { const next = new Set(current); next.delete(key); return next })
    }
  }

  const mediaType = query.get('media_type') || ''
  const activeStatus = query.get('status') || ''
  const canPlayRandom = total > 0 && !randomPlaying

  return (
    <section className="react-media-wall" aria-label="影视库">
      <div className="react-wall-heading">
        <h2 className="react-wall-visually-hidden">影视库</h2>
        <div>
          <p className="react-wall-count">共 {total.toLocaleString()} 条{scopeLabel ? ` · ${scopeLabel}` : ''} · 已加载 {items.length.toLocaleString()}</p>
          <button className="react-primary-button" id="wall-random-play" type="button" disabled={!canPlayRandom}
            title="随机播放当前范围内的影片" onClick={() => void playRandom()}>
            <Shuffle size={14} aria-hidden="true" /><span>{randomPlaying ? '选取中…' : '随机播放'}</span>
          </button>
          {administrator && <button className="react-primary-button" id="probe-media" type="button" disabled={tasks.mutationBusy}
            title="探测缺少媒体信息的影片" onClick={() => void tasks.probe.run({ only_missing: true })}>
            <Film size={14} aria-hidden="true" /><span>{tasks.probe.busy ? '探测中…' : '探测信息'}</span>
          </button>}
        </div>
      </div>

      {pageError && <div className="react-wall-error" role="alert"><p>{pageError}</p><button className="react-wall-more" type="button" onClick={() => navigate(`/items${location.search}`, { replace: true })}>重试</button></div>}

      {administrator && tasks.probe.visible && tasks.probe.progress && <div className="react-wall-task" role="status">
        <span>{tasks.probe.progress.running ? `探测中 · ${tasks.probe.progress.done}/${tasks.probe.progress.total} · ${tasks.probe.progress.current}`
          : tasks.probe.progress.error || `探测完成 · 成功 ${tasks.probe.progress.success} · 失败 ${tasks.probe.progress.failed}`}</span>
        {tasks.probe.busy && <button type="button" disabled={tasks.probe.cancelling} onClick={() => void tasks.probe.cancel()}>{tasks.probe.cancelling ? '正在中止…' : '中止'}</button>}
      </div>}

      <div className="react-wall-controls">
        <div className="react-wall-type-tabs" role="tablist" aria-label="媒体类型">
          <button type="button" role="tab" aria-selected={!mediaType} aria-pressed={!mediaType} onClick={() => goFilter('media_type', '')}>
            <Layers3 size={14} aria-hidden="true" /><span>全部</span>{!mediaType && <small>{total.toLocaleString()}</small>}
          </button>
          <button type="button" role="tab" aria-selected={mediaType === 'Movie'} aria-pressed={mediaType === 'Movie'} onClick={() => goFilter('media_type', 'Movie')}>
            <Film size={14} aria-hidden="true" /><span>电影</span>{mediaType === 'Movie' && <small>{total.toLocaleString()}</small>}
          </button>
          <button type="button" role="tab" aria-selected={mediaType === 'Episode'} aria-pressed={mediaType === 'Episode'} onClick={() => goFilter('media_type', 'Episode')}>
            <Tv size={14} aria-hidden="true" /><span>剧集</span>{mediaType === 'Episode' && <small>{total.toLocaleString()}</small>}
          </button>
        </div>

        <WallFilters query={query} libraries={libraries} search={search} canManage={administrator} cardSize={cardSize}
          onFilter={goFilter} onSearch={updateSearch} onCardSize={setCardSize} onClearEntity={clearEntity}
          onClearAll={clearFilters} onShuffle={shuffle}
          entityAction={entityFavorite ? <FavoriteToggleButton className="wall-entity-favorite" itemId={entityFavorite.Id}
            favorite={entityFavorite.UserData.IsFavorite} label={entityFavorite.Name} onChanged={(favorite) => setEntityFavorite((current) => current
              ? { ...current, UserData: { ...current.UserData, IsFavorite: favorite } } : current)} /> : null} />
      </div>

      {televisionLibrary && !query.get('status') && !activeEntity && <div className="react-tv-view-tabs" role="tablist" aria-label="媒体视图">
        <button type="button" role="tab" aria-selected={browseTelevision} aria-pressed={browseTelevision} onClick={() => goFilter('view', '')}>
          {library?.Type === 'mixed' ? '影片与电视节目' : '电视节目'}
        </button>
        <button type="button" role="tab" aria-selected={!browseTelevision} aria-pressed={!browseTelevision} onClick={() => goFilter('view', 'index')}>全部媒体索引</button>
      </div>}

      {browseTelevision && library && <TelevisionBrowser key={library.Id} library={library.Id} search={query.get('search') || ''}
        revision={revision} series={query.get('tv_series') || ''} season={query.get('tv_season') || ''}
        onBrowse={browseSeries} onOpen={(id) => openMovie(id)} />}
      {browseTelevision && library?.Type === 'mixed' && <h3 className="react-tv-movie-label">影片</h3>}

      {showWall && items.length > 0 && <div className="react-wall-grid" aria-label="影片列表">
        {items.map((movie) => <MediaCard key={movie.id} movie={movie} tags={imageTags[String(movie.id)]} userdata={userdata[String(movie.id)]}
          busy={busy} canManage={administrator} onOpen={openMovie} onQuickPlay={(id) => {
            const selected = items.find((item) => item.id === id)
            if (selected) void quickPlay(selected)
          }} onAction={(kind, id) => void runCardAction(kind, id)} />)}
      </div>}

      {showWall && loading && items.length === 0 && <div className="react-wall-grid" aria-label="正在加载影片" aria-busy="true">
        {Array.from({ length: 12 }, (_, index) => <div className="react-wall-skeleton" key={index} />)}
      </div>}

      {showWall && pageReady && !pageError && !wallError && total === 0 && <div className="react-wall-empty">
        <Film size={25} aria-hidden="true" /><strong>{emptyTitle}</strong>
        {query.get('search') && <p>试试其它关键词。</p>}
        {administrator && !query.get('search') && <p>开始扫描或手动补录后再来看看。</p>}
      </div>}

      {showWall && wallError && <div className="react-wall-error" role="alert"><p>{wallError}</p><button type="button" onClick={() => void loadMore(true)}>重试加载</button></div>}
      {showWall && refreshError && <div className="react-wall-error" role="alert"><p>列表刷新失败：{refreshError}</p><button type="button" onClick={() => {
        const request = requestRef.current
        if (request?.isCurrent()) void wallRefresh(request.signal)
      }}>重试刷新</button></div>}
      {showWall && refreshing && items.length > 0 && <div className="react-wall-more" aria-live="polite">正在更新列表…</div>}
      {showWall && <div id="wall-more" ref={moreRef} className="react-wall-more" aria-live="polite">
        {loading && items.length > 0 && '正在加载…'}
        {!loading && !done && !wallError && !refreshError && pageReady && <button type="button" onClick={() => void loadMore()}>加载更多</button>}
        {done && total > 0 && items.length > 0 && <span>已显示全部 {items.length.toLocaleString()} 条</span>}
      </div>}
      <span className="react-wall-hidden-state" aria-hidden="true">{activeStatus}</span>
    </section>
  )
}
