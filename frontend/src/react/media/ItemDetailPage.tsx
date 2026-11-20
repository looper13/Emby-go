import {
  Activity, ArrowLeft, Check, ChevronLeft, ChevronRight, Copy, Film, Heart, Play,
  RefreshCw, Search, Star,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { catalogApi } from '../../api/catalog'
import { mediaApi } from '../../api/media'
import type { DetailDto, ImageDto, MovieDto, SimilarDto } from '../../api/contracts'
import { imageURL } from '../../lib/image-url'
import { entityIdOf, fmtDuration, numberUnlessInTitle, plotClamped, probeSummary, statusText } from '../../lib/media'
import { fmtTime } from '../../lib/format'
import { isValidItemId } from '../../router/legacy-url'
import { televisionApi, type TelevisionItem } from '../../api/television'
import { usePageRequest } from '../hooks/use-page-request'
import { useAppRuntimeStore } from '../app-store'
import { useReactAuthStore } from '../auth-store'
import { useReactMediaWallStore } from '../media-wall-store'
import { useReactPreferencesStore } from '../preferences-store'
import { useReactPlayerStore } from '../player-store'
import { useReactScrapePreviewStore } from '../scrape-preview-store'
import { useReactTasksStore } from '../tasks-store'
import { useImageRetry } from './use-image-retry'
import { saveViewPosition, useViewHistory } from '../view-history'
import { FavoriteToggleButton } from './FavoriteToggleButton'
import { DetailPlaybackControls } from './DetailPlaybackControls'
import { ArtworkGallery } from './ArtworkGallery'
import { SimilarItems } from './SimilarItems'
import { AvdbCommunitySection } from './AvdbCommunitySection'
import './item-detail.css'

function readSavedList(search: string) {
  const query = new URLSearchParams(search)
  const libraryId = query.get('javdb_library_id') || ''
  const listId = query.get('javdb_list_id') || ''
  const page = query.get('javdb_related_page') || ''
  if (!/^[1-9]\d{0,18}$/u.test(libraryId) || !Number.isSafeInteger(Number(libraryId))
    || !/^[A-Za-z0-9_-]{1,120}$/u.test(listId) || !/^[1-9]\d{0,3}$/u.test(page) || Number(page) > 9999) return undefined
  return { libraryId: Number(libraryId), listId, relatedPage: Number(page) }
}

function DetailImage({ src, fallback = '', alt = '', className = '', loading = 'lazy' }: { src: string; fallback?: string; alt?: string; className?: string; loading?: 'lazy' | 'eager' }) {
  const hideImages = useReactPreferencesStore((state) => state.hideImages)
  const image = useImageRetry(src, fallback, hideImages)
  if (hideImages || !image.current) return <span className={`react-detail-image-placeholder ${className}`} aria-label={hideImages ? '媒体图片已隐藏' : alt || (image.failed ? '图片加载失败' : '媒体图片不可用')}>
    {image.retrying ? '正在重试…' : <Film size={27} aria-hidden="true" />}
  </span>
  return <img key={image.revision} className={className} src={image.current} alt={alt} loading={loading} decoding="async"
    onLoad={() => image.onLoad(image.current)} onError={() => image.onError(image.current)} />
}

function techBadges(detail: DetailDto) {
  const files = detail.files ?? []
  if (!files.length) return detail.movie.source_container ? [detail.movie.source_container.toUpperCase()] : []
  const streams = files.flatMap((file) => file.streams)
  const video = streams.find((stream) => stream.Type?.toLowerCase() === 'video')
  const audio = streams.find((stream) => stream.Type?.toLowerCase() === 'audio')
  const subtitles = streams.filter((stream) => stream.Type?.toLowerCase() === 'subtitle')
  const result: string[] = []
  if (video?.Width && video.Height) result.push(video.Width >= 3600 || video.Height >= 2000 ? '4K UHD'
    : video.Width >= 1800 || video.Height >= 1000 ? '1080p FHD'
      : video.Width >= 1200 || video.Height >= 700 ? '720p HD' : `${video.Height}p`)
  if (video && (video.BitDepth === 10 || /10/i.test(video.Profile || ''))) result.push('10-bit HDR')
  if (video?.Codec) result.push(({ hevc: 'HEVC', h265: 'HEVC', h264: 'AVC (H.264)', avc: 'AVC (H.264)', av01: 'AV1', av1: 'AV1', vp9: 'VP9' } as Record<string, string>)[video.Codec.toLowerCase()] || video.Codec.toUpperCase())
  if (audio?.Codec) result.push(`${audio.Codec.toUpperCase()}${audio.Channels === 6 ? ' · 5.1 声道' : audio.Channels === 8 ? ' · 7.1 声道' : audio.Channels === 2 ? ' · 立体声' : ''}`)
  if (subtitles.length) result.push(`字幕 ×${subtitles.length}`)
  return result
}

function MetadataRow({ label, children }: { label: string; children?: ReactNode }) {
  if (children == null || children === '') return null
  return <div className="react-detail-meta-row"><dt>{label}</dt><dd>{children}</dd></div>
}

export function ItemDetailPage() {
  const { id = '' } = useParams()
  const location = useLocation()
  const navigate = useNavigate()
  const history = useViewHistory()
  const scope = usePageRequest()
  const movieScope = usePageRequest()
  const administrator = useReactAuthStore((state) => state.user?.Policy.IsAdministrator === true)
  const hideImages = useReactPreferencesStore((state) => state.hideImages)
  const wallItems = useReactMediaWallStore((state) => state.items)
  const wallDone = useReactMediaWallStore((state) => state.done)
  const playerMedia = useReactPlayerStore((state) => state.media)
  const playerTrailer = useReactPlayerStore((state) => state.trailer)
  const showToast = useAppRuntimeStore((state) => state.showToast)
  const taskRevision = useReactTasksStore((state) => state.revision)
  const scrapeOpen = useReactScrapePreviewStore((state) => state.open)
  const [detail, setDetail] = useState<DetailDto>()
  const [similar, setSimilar] = useState<SimilarDto[]>([])
  const [error, setError] = useState('')
  const [plotExpanded, setPlotExpanded] = useState(false)
  const [filesExpanded, setFilesExpanded] = useState(false)
  const [selectedVersionId, setSelectedVersionId] = useState<number>()
  const [playbackSelection, setPlaybackSelection] = useState<import('../../lib/player-tracks').TrackSelection>()
  const [busy, setBusy] = useState<Set<string>>(() => new Set())
  const [copiedPath, setCopiedPath] = useState('')
  const mounted = useRef(true)
  const requestRef = useRef<ReturnType<typeof scope.begin> | undefined>(undefined)
  const locationRef = useRef(`${location.pathname}${location.search}`)
  locationRef.current = `${location.pathname}${location.search}`
  const currentId = Number(id)
  const savedList = useMemo(() => readSavedList(location.search), [location.search])
  const heroImage = useMemo(() => {
    if (!detail) return ''
    const movie = detail.movie
    const firstBackdrop = detail.images?.find((image) => image.ImageType === 'Backdrop')
    if (firstBackdrop) return imageURL(movie.id, 'Backdrop', 1280, firstBackdrop.ImageTag, firstBackdrop.ImageIndex)
    if (movie.BackdropPath) return imageURL(movie.id, 'Backdrop', 1280, detail.images?.find((image) => image.ImageType === 'Backdrop')?.ImageTag, 0)
    if (movie.LandscapePath) return imageURL(movie.id, 'Thumb', 1280, detail.images?.find((image) => image.ImageType === 'Thumb')?.ImageTag)
    return movie.PosterPath ? imageURL(movie.id, 'Primary', 320, detail.images?.find((image) => image.ImageType === 'Primary')?.ImageTag) : movie.cover_url
  }, [detail])
  const poster = detail?.movie.PosterPath
    ? imageURL(detail.movie.id, 'Primary', 420, detail.images?.find((image) => image.ImageType === 'Primary')?.ImageTag)
    : detail?.movie.cover_url || ''
  const backdropImages = useMemo(() => detail?.images?.filter((image) => image.ImageType === 'Backdrop') ?? [], [detail?.images])
  const backdropSources = useMemo(() => detail ? backdropImages.map((image) => imageURL(detail.movie.id, 'Backdrop', 480, image.ImageTag, image.ImageIndex)) : [], [backdropImages, detail?.movie.id])
  const selectedVersion = detail?.versions?.find((entry) => entry.id === selectedVersionId)
  const wallIndex = wallItems.findIndex((item) => item.id === currentId)
  const [tvEpisodes, setTvEpisodes] = useState<TelevisionItem[]>([])
  const tvIndex = tvEpisodes.findIndex((item) => item.Id === id)
  const hasPrev = tvIndex >= 0 ? tvIndex > 0 : wallIndex > 0
  const hasNext = tvIndex >= 0 ? tvIndex < tvEpisodes.length - 1 : wallIndex >= 0 && (wallIndex < wallItems.length - 1 || !wallDone)
  const prevTitle = tvIndex > 0 ? tvEpisodes[tvIndex - 1]?.Name : wallItems[wallIndex - 1]?.Title
  const nextTitle = tvIndex >= 0 && tvIndex < tvEpisodes.length - 1
    ? tvEpisodes[tvIndex + 1]?.Name : wallItems[wallIndex + 1]?.Title

  const back = useCallback(() => history.backFromItem(`/items${location.search}`), [history, location.search])
  const saveAndOpen = useCallback((nextId: number, nextLibraryId?: number) => {
    saveViewPosition({ scrollY: Math.max(0, window.scrollY), wallCount: wallItems.length, focusID: String(currentId) })
    const query = new URLSearchParams(location.search)
    query.delete('javdb_library_id')
    query.delete('javdb_list_id')
    query.delete('javdb_related_page')
    if (nextLibraryId) query.set('library_id', String(nextLibraryId))
    navigate(`/item/${nextId}${query.size ? `?${query}` : ''}`)
  }, [currentId, location.search, navigate, wallItems.length])

  const loadDetail = useCallback(async (preserve = false) => {
    const current = scope.begin()
    requestRef.current = current
    const idValue = id
    const scrollY = window.scrollY
    const restore = history.target
    if (!preserve) {
      setDetail(undefined)
      setSimilar([])
      setPlotExpanded(false)
      setFilesExpanded(false)
      setTvEpisodes([])
      window.scrollTo(0, 0)
    }
    setError('')
    if (!isValidItemId(idValue)) { navigate(`/items${location.search}`, { replace: true }); return }
    try {
      const result = await catalogApi.detail(idValue, current.signal)
      if (!current.isCurrent()) return
      setDetail(result)
      setSelectedVersionId((previous) => preserve && result.versions?.some((entry) => entry.id === previous && entry.playable)
        ? previous : result.versions?.find((entry) => entry.id === result.movie.id && entry.playable)?.id
          ?? result.versions?.find((entry) => entry.playable)?.id ?? result.movie.id)
      void mediaApi.similar(idValue, current.signal).then((items) => { if (current.isCurrent()) setSimilar(items) }).catch(() => {})

      const query = new URLSearchParams(location.search)
      const tvSeries = query.get('tv_series')
      if (tvSeries) {
        try {
          const episodes = await televisionApi.episodes(tvSeries, query.get('tv_season') || '', 0, current.signal)
          if (current.isCurrent()) setTvEpisodes(episodes.items)
        } catch {
          if (current.isCurrent()) setTvEpisodes([])
        }
      } else if (useReactMediaWallStore.getState().items.length === 0) {
        const wallQuery = new URLSearchParams(location.search)
        wallQuery.delete('limit')
        wallQuery.delete('offset')
        if (!wallQuery.has('library_id') && result.movie.library_id) wallQuery.set('library_id', String(result.movie.library_id))
        useReactMediaWallStore.getState().prepare(wallQuery, false)
        void useReactMediaWallStore.getState().loadMore(current.signal)
      }

      if (current.isCurrent()) {
        if (preserve) window.scrollTo(0, scrollY)
        else if (restore) window.scrollTo(0, restore.scrollY)
      }
    } catch (cause) {
      if (current.isCurrent()) {
        const message = cause instanceof Error ? cause.message : '加载失败'
        setError(message)
        if (preserve) showToast(message, 'error')
      }
    }
  }, [history.target, id, location.search, navigate, scope, showToast])

  const loadRef = useRef(loadDetail)
  loadRef.current = loadDetail

  useEffect(() => { mounted.current = true; void loadDetail(); return () => { mounted.current = false; scope.cancel(); movieScope.cancel() } }, [id, location.search, loadDetail, movieScope, scope, taskRevision])

  const runAction = useCallback(async (kind: 'probe' | 'reread', movieId: number) => {
    const key = `${kind}:${movieId}`
    if (busy.has(key)) return
    setBusy((current) => new Set(current).add(key))
    try {
      if (kind === 'probe') {
        showToast('正在探测…')
        showToast(probeSummary(await mediaApi.probe(movieId)))
      } else {
        const status = await mediaApi.reread(movieId)
        showToast(`状态已更新：${statusText[status] || status}`)
      }
      if (mounted.current) {
        useReactMediaWallStore.getState().markDirty()
        const request = requestRef.current
        if (request?.isCurrent()) await useReactMediaWallStore.getState().refresh(request.signal)
        await loadRef.current(true)
      }
    } catch (cause) {
      showToast(cause instanceof Error ? cause.message : '操作失败', 'error')
    } finally {
      setBusy((current) => { const next = new Set(current); next.delete(key); return next })
    }
  }, [busy, showToast])

  const openScrape = useCallback(() => {
    if (!detail) return
    const owner = `${location.pathname}${location.search}`
    const movieId = detail.movie.id
    void scrapeOpen(movieId, () => {
      if (mounted.current && locationRef.current === owner) return loadRef.current(true)
    })
  }, [detail, location.pathname, location.search, scrapeOpen])

  const play = useCallback(() => {
    if (detail) playerMedia(detail.movie, playbackSelection, selectedVersion)
  }, [detail, playbackSelection, playerMedia, selectedVersion])

  const next = useCallback(async (delta: -1 | 1) => {
    if (tvIndex >= 0) {
      const target = tvEpisodes[tvIndex + delta]
      if (target) saveAndOpen(Number(target.Id))
      return
    }
    const nextIndex = wallIndex + delta
    if (nextIndex >= 0 && nextIndex < wallItems.length) { saveAndOpen(wallItems[nextIndex]!.id); return }
    if (delta === 1 && wallIndex >= 0 && !wallDone) {
      const request = movieScope.begin()
      const loaded = await useReactMediaWallStore.getState().loadMore(request.signal)
      if (loaded) {
        const items = useReactMediaWallStore.getState().items
        if (items[wallIndex + 1]) saveAndOpen(items[wallIndex + 1]!.id)
      }
    }
  }, [movieScope, saveAndOpen, tvEpisodes, tvIndex, wallDone, wallIndex, wallItems])

  useEffect(() => {
    function keyboard(event: KeyboardEvent) {
      if (event.key === 'Escape' && !event.defaultPrevented && !(event.target instanceof Element
        && event.target.closest('input, textarea, select, [contenteditable="true"], [role="dialog"]'))) {
        event.preventDefault()
        back()
      }
      if (event.defaultPrevented || event.target instanceof Element
        && event.target.closest('input, textarea, select, [contenteditable="true"], [role="dialog"]')
        || useReactPlayerStore.getState().current || document.querySelector('#artwork-overlay')) return
      if ((event.key === 'ArrowLeft' || event.key === '[') && hasPrev) { event.preventDefault(); void next(-1) }
      if ((event.key === 'ArrowRight' || event.key === ']') && hasNext) { event.preventDefault(); void next(1) }
    }
    window.addEventListener('keydown', keyboard)
    return () => window.removeEventListener('keydown', keyboard)
  }, [back, hasNext, hasPrev, next])

  const onPlaybackSelection = useCallback((selection: import('../../lib/player-tracks').TrackSelection | undefined) => setPlaybackSelection(selection), [])
  const onFavoriteChanged = useCallback((favorite: boolean) => setDetail((current) => current ? { ...current, favorite } : current), [])

  if (!detail) return <section className="react-detail-loading" aria-live="polite">
    {error ? <p role="alert">{error}</p> : <p>正在加载影片…</p>}
    {error && <button className="react-detail-action" type="button" onClick={() => void loadDetail()}>重试</button>}
    <button className="react-detail-action" type="button" onClick={back}><ArrowLeft size={14} />返回媒体墙</button>
  </section>

  const movie = detail.movie
  const metadataGenres = movie.Genres ?? []
  const metadataTags = movie.Tags ?? []
  const metadataStudios = movie.Studios?.length ? movie.Studios : [movie.Maker || movie.Label || ''].filter(Boolean)

  function browseEntity(key: string, value: string) {
    const query = new URLSearchParams()
    query.set(key, value)
    const libraryId = new URLSearchParams(location.search).get('library_id')
    if (libraryId) query.set('library_id', libraryId)
    navigate(`/items?${query}`)
  }

  async function copyPath(path: string) {
    try { await navigator.clipboard.writeText(path); setCopiedPath(path); window.setTimeout(() => setCopiedPath(''), 2000) }
    catch { showToast('复制路径失败', 'error') }
  }

  function streamParameters(stream: NonNullable<DetailDto['files']>[number]['streams'][number]) {
    const parts = stream.Type === 'Video'
      ? [stream.Codec?.toUpperCase(), stream.Width && stream.Height ? `${stream.Width}×${stream.Height}` : '', stream.BitRate ? `${Math.round(stream.BitRate / 1000)} kbps` : '', stream.Profile, stream.BitDepth ? `${stream.BitDepth}bit` : '']
      : [stream.Codec?.toUpperCase(), stream.ChannelLayout || (stream.Channels ? `${stream.Channels}ch` : ''), stream.BitRate ? `${Math.round(stream.BitRate / 1000)} kbps` : '', stream.Language, stream.SampleRate ? `${stream.SampleRate} Hz` : '']
    return parts.filter(Boolean).join(' · ') || '—'
  }

  return <article className="react-item-detail">
    <header className="react-item-hero">
      <div className="react-item-hero-image">{!hideImages && heroImage && <DetailImage src={heroImage} fallback={movie.cover_url} alt="" loading="eager" />}</div>
      <div className="react-item-hero-scrim" />
      <div className="react-item-hero-topbar">
        <button className="react-detail-action" id="item-back" type="button" title="返回媒体墙 (Esc)" onClick={back}><ArrowLeft size={14} /><span>返回媒体墙</span></button>
        <div className="react-item-navigation">
          <button className="react-detail-action" id="item-prev" type="button" aria-label="前一部" title={prevTitle ? `前一部：${prevTitle}` : '前一部'} disabled={!hasPrev} onClick={() => void next(-1)}><ChevronLeft size={14} /><span>前一部</span></button>
          <button className="react-detail-action" id="item-next" type="button" aria-label="后一部" title={nextTitle ? `后一部：${nextTitle}` : '后一部'} disabled={!hasNext} onClick={() => void next(1)}><span>后一部</span><ChevronRight size={14} /></button>
        </div>
      </div>

      <div className="react-item-hero-body">
        <div className="react-item-poster-wrap">{!hideImages && poster ? <DetailImage className="react-item-poster" src={poster} fallback={movie.cover_url} alt={`${movie.Title} 封面`} loading="eager" /> : <span className="react-item-poster react-detail-image-placeholder" aria-label={hideImages ? '封面已隐藏' : '暂无封面'}><Film size={32} aria-hidden="true" /></span>}</div>
        <div className="react-item-hero-copy">
          {movie.television && <p className="react-item-eyebrow">{movie.television.title} · {movie.television.season === 0 ? '特别篇' : `第 ${movie.television.season} 季`} · 第 {movie.television.episode} 集</p>}
          <div className="react-item-title-row"><h1>{movie.Title || movie.id}</h1>
            {['success', 'manual'].includes(movie.Status) && <FavoriteToggleButton itemId={movie.id} favorite={detail.favorite} label={movie.Title} onChanged={onFavoriteChanged} />}
          </div>
          {movie.OriginalTitle && movie.OriginalTitle !== movie.Title && <p className="react-item-original">{movie.OriginalTitle}</p>}
          <div className="react-item-meta-pills">
            {movie.Rating > 0 && <span className="react-item-rating"><Star size={12} fill="currentColor" />{movie.Rating}</span>}
            {movie.Year > 0 && <span>{movie.Year}</span>}
            {movie.RuntimeSeconds > 0 && <span>{fmtDuration(movie.RuntimeSeconds)}</span>}
            {numberUnlessInTitle(movie.Title, movie.Number) && <span className="mono">{numberUnlessInTitle(movie.Title, movie.Number)}</span>}
            {administrator && <span className={`react-item-status status-${movie.Status}`}>{statusText[movie.Status] || movie.Status}</span>}
            {movie.collection && <span>合集 {movie.collection}</span>}
          </div>
          {techBadges(detail).length > 0 && <div className="react-item-tech-badges">{techBadges(detail).map((badge) => <span key={badge}>{badge}</span>)}</div>}
          {metadataTags.length > 0 && <div className="react-item-tags">{metadataTags.map((tag) => <button key={tag} type="button" onClick={() => browseEntity('tag', tag)}>#{tag}</button>)}</div>}
          <p className="react-item-plot-short">{movie.Plot}</p>
          <div className="react-item-actions">
            {detail.playable && <button className="react-detail-play-button" id="detail-play" type="button" onClick={play}><Play size={15} fill="currentColor" />播放</button>}
            {!detail.playable && <span className="react-item-unplayable">该媒体不可播放（待补录 / 协议不兼容）</span>}
            {administrator && <button className="react-detail-action" id="detail-probe" title="探测信息" disabled={busy.has(`probe:${movie.id}`)} onClick={() => void runAction('probe', movie.id)}><Activity size={14} /><span>探测信息</span></button>}
            {administrator && !movie.television && <button className="react-detail-action" id="detail-scrape" title="刮削" disabled={busy.size > 0} onClick={openScrape}><Search size={14} /><span>刮削</span></button>}
            {administrator && <button className="react-detail-action" id="detail-reread" title="重读源" disabled={busy.has(`reread:${movie.id}`)} onClick={() => void runAction('reread', movie.id)}><RefreshCw size={14} /><span>重读源</span></button>}
          </div>
          {(detail.versions?.length ?? 0) > 1 && <label className="react-detail-version"><span>播放版本</span><select aria-label="播放版本" value={selectedVersionId ?? movie.id} onChange={(event) => setSelectedVersionId(Number(event.currentTarget.value))}>
            {detail.versions?.map((entry) => <option key={entry.id} value={entry.id} disabled={!entry.playable}>{entry.name}{entry.playable ? '' : ' · 不可播放'}</option>)}
          </select></label>}
          {detail.playable && <DetailPlaybackControls key={selectedVersionId ?? movie.id} itemId={selectedVersionId ?? movie.id} refreshKey={detail} onSelection={onPlaybackSelection} />}
        </div>
      </div>
    </header>

    <div className="react-item-sections">
      {movie.Plot && <section className="detail-section"><h3>影片简介</h3><p className={`react-detail-plot${plotClamped(movie.Plot) && !plotExpanded ? ' is-clamped' : ''}`} id="item-plot">{movie.Plot}</p>
        {plotClamped(movie.Plot) && <button id="plot-toggle" className="react-detail-text-button" aria-expanded={plotExpanded} aria-controls="item-plot" onClick={() => setPlotExpanded((expanded) => !expanded)}>{plotExpanded ? '收起简介' : '展开全部'}</button>}
      </section>}

      {movie.trailer_url?.trim() && <section className="detail-section"><h3>预告片</h3><button className="react-detail-trailer" id="item-trailer" type="button" title="播放预告片" aria-label="播放预告片" onClick={() => playerTrailer(movie, movie.trailer_url || '')}>
        {heroImage && <DetailImage src={heroImage} fallback={movie.cover_url} alt="" />}<span><Play size={25} fill="currentColor" /></span>
      </button></section>}

      <ArtworkGallery key={movie.id} sources={backdropSources} hidden={hideImages} />

      <section className="detail-section"><h3>演职人员 <small>{detail.actors?.length ?? 0}</small></h3>
        {detail.actors?.length ? <div className="react-detail-actors">{detail.actors.map((actor) => <div className="react-detail-actor" key={actor.name}>
          <button type="button" title={`查看 ${actor.name} 相关影片`} data-entity-key="person" data-entity-value={actor.name} onClick={() => browseEntity('person', actor.name)}>
            {!hideImages && actor.has_image ? <DetailImage src={imageURL(entityIdOf('person', actor.name), 'Primary', 200, actor.image_tag)} alt="" /> : <span className="react-detail-avatar-fallback">{actor.name.trim().slice(0, 1) || '?'}</span>}
            <small>{actor.name}</small>
          </button>
          <FavoriteToggleButton itemId={entityIdOf('person', actor.name)} favorite={actor.favorite} label={actor.name} />
        </div>)}</div> : <p className="react-detail-muted">NFO 中没有演员信息。</p>}
      </section>

      {!movie.television && movie.Number?.trim() && <AvdbCommunitySection itemId={movie.id} number={movie.Number} localLibraryId={movie.library_id} savedList={savedList}
        onOpen={(localId) => saveAndOpen(localId, movie.library_id)} />}

      <SimilarItems items={similar} hideImages={hideImages} onOpen={(nextId) => saveAndOpen(Number(nextId))} />

      {administrator && <section className="detail-section"><h3>媒体信息 <small>{detail.files?.length ?? 0} 个文件</small></h3>
        {(detail.files ?? []).map((file) => <article className="react-detail-file" key={file.index}>
          <div className="react-detail-file-head"><strong>{file.role === 'main' ? '主文件' : `分段 ${file.index - 1}`}</strong><span className="mono">{file.name}</span><small>{file.size ? `${(file.size / 1024 / 1024).toFixed(file.size / 1024 / 1024 < 10 ? 1 : 0)} MB` : '—'}</small>{!file.probed && <span className="status-pending">未探测</span>}</div>
          {file.streams.length > 0 ? <div className="react-detail-table-wrap"><table><thead><tr><th>#</th><th>类型</th><th>参数</th><th>标记</th></tr></thead><tbody>{file.streams.map((stream, index) => <tr key={`${file.index}:${index}`}>
            <td className="mono">#{stream.Index ?? ''}</td><td>{stream.Type || '—'}</td><td className="mono">{streamParameters(stream)}</td>
            <td>{[stream.IsDefault ? '默认' : '', stream.IsForced ? '强制' : '', stream.IsExternal ? '外挂' : ''].filter(Boolean).join(' ') || '—'}</td>
          </tr>)}</tbody></table></div> : <p className="react-detail-muted">未探测</p>}
          <div className="react-detail-path-row"><p className="mono" title={file.path}>{file.path}</p><button className="react-detail-action" type="button" title={copiedPath === file.path ? '已复制' : '复制文件路径'} onClick={() => void copyPath(file.path)}>
            {copiedPath === file.path ? <Check size={13} /> : <Copy size={13} />}{copiedPath === file.path ? '已复制' : '复制'}
          </button></div>
        </article>)}
      </section>}

      <section className="detail-section"><h3>元数据</h3><dl className="react-detail-metadata">
        <MetadataRow label="番号">{movie.Number}</MetadataRow><MetadataRow label="原名">{movie.OriginalTitle}</MetadataRow>
        <MetadataRow label="年份">{movie.Year || ''}</MetadataRow><MetadataRow label="分级">{movie.official_rating}</MetadataRow>
        <MetadataRow label="类型">{metadataGenres.map((value) => <button key={value} onClick={() => browseEntity('genre', value)}>{value}</button>)}</MetadataRow>
        <MetadataRow label="标签">{metadataTags.map((value) => <button key={value} onClick={() => browseEntity('tag', value)}>{value}</button>)}</MetadataRow>
        <MetadataRow label="厂商">{metadataStudios.map((value) => <button key={value} onClick={() => browseEntity('studio', value)}>{value}</button>)}</MetadataRow>
        <MetadataRow label="导演">{movie.Director}</MetadataRow>
        <MetadataRow label="合集">{movie.collection && <button onClick={() => browseEntity('collection', movie.collection || '')}>{movie.collection}</button>}</MetadataRow>
        <MetadataRow label="媒体库">{detail.library_name}</MetadataRow><MetadataRow label="来源">{movie.source_protocol?.toUpperCase()}</MetadataRow>
      </dl></section>

      {administrator && <details className="detail-section react-detail-file-timeline" open={filesExpanded} onToggle={(event) => setFilesExpanded(event.currentTarget.open)}>
        <summary>文件与时间</summary><dl className="react-detail-metadata">
          <MetadataRow label="源文件">{movie.source_path}</MetadataRow><MetadataRow label="NFO">{movie.NFOPath}</MetadataRow>
          <MetadataRow label="最后修改">{fmtTime(detail.modified_at)}</MetadataRow><MetadataRow label="入库时间">{fmtTime(movie.created_at)}</MetadataRow>
          <MetadataRow label="上次刮削">{fmtTime(movie.last_scrape_at)}</MetadataRow><MetadataRow label="刮削结果">{movie.last_scrape_error}</MetadataRow>
        </dl>
      </details>}
    </div>
  </article>
}
