import {
  ArrowLeft, Check, ChevronLeft, ChevronRight, Copy, Database, ExternalLink, Film, ImageOff,
  Magnet, Play, RefreshCw, Search, Star,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import type { LibraryDto } from '../../api/contracts'
import { ApiError } from '../../api/client'
import { r18Api, safeR18ImageURL, safeR18PreviewURL, safeR18URL, type R18Page, type R18Resource, type R18TagCategory, type R18Video } from '../../api/r18'
import { useAppRuntimeStore } from '../app-store'
import { useReactAuthStore } from '../auth-store'
import { useReactPlayerStore } from '../player-store'
import { useReactPreferencesStore } from '../preferences-store'
import { usePageRequest } from '../hooks/use-page-request'
import { useViewHistory } from '../view-history'
import { useR18Playback, mainVideo, offlineActive, offlineProgressVisible, offlineStageMessage, offlineStatusLabel } from '../hooks/use-r18-playback'
import { ArtworkGallery } from '../media/ArtworkGallery'
import { PageHeading, PageState } from './PageState'
import {
  isR18Chart, readR18CatalogQuery, r18BasicFilters, r18Modes, r18SpecialGroups,
  r18Sorts, r18Types, serializeR18CatalogQuery, type R18BrowseMode, type R18CatalogQuery,
} from '../../composables/r18-catalog'
import { magnetHash } from '../../lib/r18-playback'
import './basic-pages.css'
import './r18-pages.css'

const typeLabels: Record<string, string> = { all: '全部', '0': '有码', '1': '无码', '2': '欧美', '3': 'FC2', '4': '动漫' }
const groupLabels: Record<string, string> = { subject: '题材', role: '角色', cloth: '服装', body: '身体', behavior: '行为', play_method: '玩法', category: '类别', other: '其他', tag: '标签', place: '场景' }
const periodChoices = [{ value: 'daily', label: '日榜' }, { value: 'weekly', label: '周榜' }, { value: 'monthly', label: '月榜' }] as const

function queryObject(search: string) {
  return Object.fromEntries(new URLSearchParams(search).entries())
}

function imageSize(raw: string) { return safeR18ImageURL(raw) }

function CatalogCard({ video, libraryId, from, hideImages }: { video: R18Video; libraryId: number; from: string; hideImages: boolean }) {
  const [failed, setFailed] = useState(false)
  useEffect(() => setFailed(false), [video.cover_url, hideImages])
  const image = imageSize(video.cover_url)
  return <article className="react-r18-card r18-card" data-r18-id={video.id} tabIndex={-1}>
    <Link to={`/r18/${libraryId}/item/${encodeURIComponent(video.id)}?from=${encodeURIComponent(from)}`} aria-label={`打开 ${video.number || video.content_id} ${video.title}`}>
      <div className="react-r18-art">
        {!hideImages && !failed && image ? <img src={image} alt={`${video.number || video.content_id} 封面`} loading="lazy" onError={() => setFailed(true)} /> : <span>{hideImages ? <ImageOff size={27} /> : <Film size={27} />}</span>}
        <div className="react-r18-badges">{video.has_local && <small className="local">本地 STRM 可播放</small>}{video.has_preview && <small className="preview">预告</small>}</div>
        {video.score > 0 && <small className="score"><Star size={11} fill="currentColor" />{video.score.toFixed(1)}</small>}
      </div>
      <div className="react-r18-card-copy"><strong title={video.title}>{video.title || video.original_title || video.number || video.content_id}</strong><small>{video.number || video.content_id} · {video.release_date || '日期未知'}</small>
        <small>{video.duration ? `${video.duration} 分钟` : '时长未知'} · {video.magnets_count} 条磁力</small></div>
    </Link>
  </article>
}

export function R18CatalogPage() {
  const location = useLocation()
  const navigate = useNavigate()
  const hideImages = useReactPreferencesStore(state => state.hideImages)
  const token = useReactAuthStore(state => state.token)
  const pageScope = usePageRequest()
  const tagScope = usePageRequest()
  const query = useMemo(() => readR18CatalogQuery(queryObject(location.search)), [location.search])
  const librariesCache = useRef<{ token: string | null; items: LibraryDto[] | null }>({ token: null, items: null })
  const [draft, setDraft] = useState<R18CatalogQuery>(query)
  const [libraries, setLibraries] = useState<LibraryDto[]>([])
  const [librariesToken, setLibrariesToken] = useState<string | null>(null)
  const [librariesLoaded, setLibrariesLoaded] = useState(false)
  const [page, setPage] = useState<R18Page>()
  const [pageToken, setPageToken] = useState<string | null>(null)
  const [pageLibraryId, setPageLibraryId] = useState('')
  const [pageLibraryRevision, setPageLibraryRevision] = useState<number | undefined>()
  const [pageQuery, setPageQuery] = useState('')
  const pageIdentity = useRef<{ token: string | null; libraryId: string; revision?: number }>({ token: null, libraryId: '' })
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [categories, setCategories] = useState<R18TagCategory[]>([])
  const [tagLoading, setTagLoading] = useState(false)
  const [tagError, setTagError] = useState('')
  const [tagSearch, setTagSearch] = useState('')
  const [categoryCollapsed, setCategoryCollapsed] = useState(false)
  const [openedGroups, setOpenedGroups] = useState<Set<string>>(() => new Set())
  const [groupLimits, setGroupLimits] = useState<Record<string, number>>({})
  const [selectionError, setSelectionError] = useState('')
  const [pageInput, setPageInput] = useState(String(query.page))
  const [refreshing, setRefreshing] = useState(false)
  const tagLibraryId = useRef(query.library_id)
  const currentLibrary = librariesToken === token ? libraries.find(library => String(library.Id) === query.library_id) : undefined
  const types = r18Types(draft.mode)
  const chart = isR18Chart(draft.mode)
  const searching = Boolean(draft.search.trim())
  const pageMaximum = draft.mode === 'top250' ? Math.ceil(250 / Number(draft.page_size)) : 9999
  const activeQuery = useMemo(() => {
    try { return new URLSearchParams(serializeR18CatalogQuery(query)).toString() }
    catch { return new URLSearchParams(location.search).toString() }
  }, [location.search, query])
  const visiblePage = page && pageToken === token && pageLibraryId === query.library_id ? page : undefined
  pageIdentity.current = { token: pageToken, libraryId: pageLibraryId, revision: pageLibraryRevision }
  const oldResults = Boolean(visiblePage && pageQuery !== activeQuery)
  const currentPage = visiblePage?.page ?? query.page

  const writeQuery = useCallback((state: R18CatalogQuery, targetPage = 1) => {
    try {
      const params = new URLSearchParams(serializeR18CatalogQuery(state, targetPage))
      navigate({ pathname: '/r18', search: `?${params.toString()}` })
    } catch (cause) { setError(cause instanceof Error ? cause.message : '查询条件不正确') }
  }, [navigate])

  useEffect(() => { setDraft(query); setPageInput(String(query.page)); setSelectionError('') }, [query])
  useEffect(() => {
    pageScope.cancel()
    tagScope.cancel()
    setLibraries([])
    setLibrariesToken(null)
    setLibrariesLoaded(false)
    setPage(undefined)
    setPageToken(null)
    setPageLibraryId('')
    setPageLibraryRevision(undefined)
    setPageQuery('')
    setCategories([])
    setTagError('')
    setTagSearch('')
    setLoading(false)
    setTagLoading(false)
  }, [pageScope, tagScope, token])

  useEffect(() => {
    if (tagLibraryId.current === query.library_id) return
    tagLibraryId.current = query.library_id
    tagScope.cancel()
    setCategories([])
    setTagLoading(false)
    setTagError('')
    setTagSearch('')
  }, [query.library_id, tagScope])

  function clearPage() {
    setPage(undefined)
    setPageToken(null)
    setPageLibraryId('')
    setPageLibraryRevision(undefined)
    setPageQuery('')
  }

  const load = useCallback(async (refresh = false) => {
    const request = pageScope.begin()
    const requestToken = token
    const current = () => request.isCurrent() && useReactAuthStore.getState().token === requestToken
    setLoading(true); setError('')
    let permissionReady = false
    try {
      let available = librariesCache.current.token === requestToken ? librariesCache.current.items : null
      if (available === null || refresh) {
        const result = await r18Api.libraries(request.signal)
        if (!current()) return
        available = result.items ?? []
        librariesCache.current = { token: requestToken, items: available }
        setLibraries(available); setLibrariesToken(requestToken); setLibrariesLoaded(true)
      }
      if (!available.length) { clearPage(); return }
      const targetLibrary = query.library_id || String(available[0]!.Id)
      if (!query.library_id || !available.some(library => String(library.Id) === targetLibrary)) {
        if (query.library_id && !available.some(library => String(library.Id) === targetLibrary)) throw new Error('AVdb 资料库不存在或当前账号无权访问')
        const next = new URLSearchParams(serializeR18CatalogQuery({ ...query, library_id: targetLibrary }, 1))
        navigate({ pathname: '/r18', search: `?${next.toString()}` }, { replace: true })
        return
      }
      permissionReady = true
      const target = available.find(library => String(library.Id) === targetLibrary)
      if (refresh && pageIdentity.current.token === requestToken && pageIdentity.current.libraryId === targetLibrary
        && pageIdentity.current.revision !== target?.Revision) clearPage()
      const params = new URLSearchParams(serializeR18CatalogQuery({ ...query, library_id: targetLibrary }))
      params.delete('library_id')
      if (refresh) params.set('refresh', '1')
      const result = await r18Api.videos(targetLibrary, params, request.signal)
      if (current()) {
        setPage(result)
        setPageToken(requestToken)
        setPageLibraryId(targetLibrary)
        setPageLibraryRevision(target?.Revision)
        setPageQuery(new URLSearchParams(serializeR18CatalogQuery(query)).toString())
        setPageInput(String(result.page))
      }
    } catch (cause) {
      if (current()) {
        if ((refresh && !permissionReady) || (cause instanceof ApiError && [401, 403, 404].includes(cause.status))) {
          clearPage()
          setLibrariesLoaded(false)
        }
        setError(cause instanceof Error ? cause.message : 'AVdb 在线目录加载失败')
      }
    }
    finally { if (current()) { setLoading(false); setRefreshing(false) } }
  }, [navigate, pageScope, query, token])

  useEffect(() => { void load() }, [load])

  const loadTags = useCallback(async (refresh = false) => {
    if (!currentLibrary || query.mode !== 'categories' || searching) return
    const request = tagScope.begin()
    setTagLoading(true); setTagError('')
    try {
      const result = await r18Api.tags(String(currentLibrary.Id), 'all', request.signal, refresh)
      if (request.isCurrent() && useReactAuthStore.getState().token === token) setCategories(result.categories)
    } catch (cause) { if (request.isCurrent()) setTagError(cause instanceof Error ? cause.message : 'AVdb 分类加载失败') }
    finally { if (request.isCurrent()) setTagLoading(false) }
  }, [currentLibrary, query.mode, searching, tagScope, token])
  useEffect(() => {
    if (query.mode === 'categories' && !searching) void loadTags()
    else if (query.mode !== 'categories') { tagScope.cancel(); setTagLoading(false) }
  }, [loadTags, query.mode, searching, tagScope])
  useEffect(() => () => { pageScope.cancel(); tagScope.cancel() }, [pageScope, tagScope])

  const availableTypes = useMemo(() => categories.filter(group => group.id === 'main' && (draft.type === 'all' || group.type === draft.type)).flatMap(group => group.tags), [categories, draft.type])
  const basicTags = availableTypes.filter(tag => r18BasicFilters.includes(tag.id))
  const yearTags = categories.filter(group => group.id === 'year' && (draft.type === 'all' || group.type === draft.type)).flatMap(group => group.tags)
  const monthTags = categories.filter(group => group.id === 'month' && (draft.type === 'all' || group.type === draft.type)).flatMap(group => group.tags)
  const durationTags = categories.filter(group => group.id === 'duration' && (draft.type === 'all' || group.type === draft.type)).flatMap(group => group.tags)
  const groups = categories.filter(group => !r18SpecialGroups.includes(group.id) && (draft.type === 'all' || group.type === draft.type)).map(group => ({
    ...group,
    tags: group.tags.filter(tag => !tagSearch.trim() || tag.name.toLocaleLowerCase().includes(tagSearch.trim().toLocaleLowerCase())),
  })).filter(group => group.tags.length)
  const selectedTags = draft.tag_ids.map(id => {
    const tag = categories.flatMap(group => group.tags).find(item => `${item.type}:${item.id}` === id)
    return { id, name: tag?.name ?? id }
  })

  function submit(event?: FormEvent<HTMLFormElement>, state = draft) { event?.preventDefault(); setError(''); writeQuery({ ...state, library_id: query.library_id }, 1) }
  function changeMode(mode: R18BrowseMode) { setTagSearch(''); setDraft(readR18CatalogQuery({ library_id: query.library_id, mode, page_size: draft.page_size })); writeQuery(readR18CatalogQuery({ library_id: query.library_id, mode, page_size: draft.page_size }), 1) }
  function changeType(type: string) {
    const next = { ...draft, type, tag_ids: draft.tag_ids.filter(tag => type === 'all' || tag.startsWith(`${type}:`)) }
    if (next.mode === 'top250' && type !== 'all') next.year = ''
    setDraft(next); submit(undefined, next)
  }
  function changeYear(year: string) {
    const next = { ...draft, year }
    if (next.mode === 'top250' && year) next.type = 'all'
    if (next.mode === 'latest' && year && next.sort === 'update') next.sort = 'release'
    setDraft(next); submit(undefined, next)
  }
  function updateField<K extends keyof R18CatalogQuery>(key: K, value: R18CatalogQuery[K]) {
    const next = { ...draft, [key]: value }
    setDraft(next); submit(undefined, next)
  }
  function toggleBasic(id: string) {
    const nextValues = draft.main_filter.includes(id) ? draft.main_filter.filter(value => value !== id) : [...draft.main_filter, id]
    const next = { ...draft, main_filter: nextValues }
    setDraft(next); submit(undefined, next)
  }
  function toggleTag(id: string, checked: boolean) {
    const nextTags = checked ? [...draft.tag_ids, id] : draft.tag_ids.filter(value => value !== id)
    if (checked && draft.tag_ids.length >= 5) { setSelectionError('最多选择 5 个题材标签'); return }
    setSelectionError('')
    const next = { ...draft, tag_ids: nextTags }
    setDraft(next); submit(undefined, next)
  }
  function clearCategory() {
    const next = { ...draft, tag_ids: [], main_filter: [], year: '', month: '', duration: '' }
    setDraft(next); setSelectionError(''); submit(undefined, next)
  }
  function turn(delta: number) {
    if (loading || oldResults) return
    const target = currentPage + delta
    if (target < 1 || target > pageMaximum) return
    writeQuery(query, target)
  }
  function jump() {
    if (loading || oldResults) return
    const target = Number(pageInput)
    if (!Number.isSafeInteger(target) || target < 1 || target > pageMaximum) { setError(`页码必须在 1 到 ${pageMaximum} 之间`); return }
    writeQuery(query, target)
  }
  function toggleGroup(groupId: string, open: boolean) {
    setOpenedGroups(current => {
      const next = new Set(current)
      if (open) next.add(groupId)
      else next.delete(groupId)
      return next
    })
  }
  function showMoreTags(groupId: string) {
    setGroupLimits(current => ({ ...current, [groupId]: (current[groupId] ?? 40) + 40 }))
  }
  const queryHint = searching ? '关键词搜索按 AVdb 相关性排列，不叠加分类标签、排序或年份。'
    : draft.mode === 'top250' ? 'TOP250 使用上游榜单顺序；榜单最多 250 部影片。'
      : chart ? '此模式遵循 AVdb 上游榜单，不以本地播放次数重新排序。'
        : draft.mode === 'categories' ? '题材、基础条件和发行信息会组合为一次查询。'
          : '最新影片默认按发行日期从新到旧。'

  return <div className="react-r18-catalog">
    <section className="react-r18-header"><div><p className="react-r18-eyebrow"><Database size={13} /> AVDB-ONLY API / ONLINE CATALOG</p><h2>{currentLibrary?.Name || 'AVdb 在线资料库'}</h2><p>通过 AVdb-Only API 浏览在线影片。匹配到本地 STRM 时会显示可播放标记并优先使用本地来源。</p></div>
      <button className="react-detail-action" type="button" disabled={loading} onClick={() => { setRefreshing(true); void load(true) }}><RefreshCw size={14} />刷新</button>
    </section>
    {libraries.length > 0 && <nav className="react-r18-modes" aria-label="AVdb 浏览模式">{r18Modes.map(choice => <button key={choice.value} type="button" aria-pressed={draft.mode === choice.value} className={draft.mode === choice.value ? 'selected' : ''} onClick={() => changeMode(choice.value)}>{choice.label}</button>)}</nav>}
    {libraries.length > 0 && <section className="react-page-panel react-r18-filter-panel">
      <form className="react-r18-filters" onSubmit={event => submit(event)}>
        <label className="react-page-field">资料库<select value={query.library_id} onChange={event => { setPage(undefined); const next = { ...draft, library_id: event.currentTarget.value }; setDraft(next); writeQuery(next) }}>
          {libraries.map(library => <option value={library.Id} key={library.Id}>{library.Name}</option>)}
        </select></label>
        <label className="react-page-field react-r18-search">关键词<input type="search" maxLength={120} value={draft.search} placeholder="搜索番号或影片标题" onChange={event => { const search = event.currentTarget.value; setDraft(current => ({ ...current, search })) }} /></label>
        {types.length > 1 && <label className="react-page-field">影片类型<select value={draft.type} onChange={event => changeType(event.currentTarget.value)}>{types.map(type => <option value={type} key={type}>{typeLabels[type] ?? type}</option>)}</select></label>}
        {!chart && <label className="react-page-field">资源筛选<select value={draft.filter} onChange={event => updateField('filter', event.currentTarget.value)}><option value="all">全部影片</option><option value="magnets">有磁力</option><option value="subtitle">有字幕</option><option value="can_play">上游可播放</option></select></label>}
        {!chart && <label className="react-page-field">排序方式<select value={draft.sort} disabled={searching} onChange={event => updateField('sort', event.currentTarget.value)}>{r18Sorts.filter(choice => draft.mode !== 'categories' || choice.value !== 'update').map(choice => <option value={choice.value} key={choice.value} disabled={draft.mode === 'latest' && Boolean(draft.year.trim()) && choice.value === 'update'}>{choice.label}</option>)}</select></label>}
        {!chart && <label className="react-page-field">方向<select value={draft.order} disabled={searching} onChange={event => updateField('order', event.currentTarget.value)}><option value="desc">从新 / 高到低</option><option value="asc">从旧 / 低到高</option></select></label>}
        {(!chart || draft.mode === 'top250') && <label className="react-page-field">发行年份{draft.mode === 'categories'
          ? <select value={draft.year} disabled={searching} onChange={event => changeYear(event.currentTarget.value)}><option value="">不限年份</option>{draft.year && !yearTags.some(tag => tag.id === draft.year) && <option value={draft.year}>{draft.year}</option>}{yearTags.map(tag => <option key={tag.id} value={tag.id}>{tag.name}</option>)}</select>
          : <input type="number" min={1} max={9999} value={draft.year} placeholder="不限年份" disabled={searching} onChange={event => { const year = event.currentTarget.value; setDraft(current => ({ ...current, year })) }} onBlur={() => changeYear(draft.year)} />}</label>}
        {chart && draft.mode !== 'top250' && <label className="react-page-field">榜单周期<select value={draft.period} disabled={searching} onChange={event => updateField('period', event.currentTarget.value)}>{periodChoices.map(choice => <option value={choice.value} key={choice.value}>{choice.label}</option>)}</select></label>}
        {draft.mode === 'top250' && <label className="react-page-check"><input type="checkbox" checked={draft.ignore_watched === 'true'} disabled={searching} onChange={event => updateField('ignore_watched', event.currentTarget.checked ? 'true' : 'false')} />排除 AVdb 账号已看</label>}
        <label className="react-page-field">每页<select value={draft.page_size} onChange={event => updateField('page_size', event.currentTarget.value)}><option value="24">24 项</option><option value="40">40 项</option><option value="50">50 项</option></select></label>
        <button className="react-primary-button" type="submit"><Search size={14} /><span>查询</span></button>
      </form>
      <p className="react-page-muted" id="r18-query-hint">{queryHint}</p>
      {draft.mode === 'categories' && !searching && <section className={`react-r18-category${categoryCollapsed ? ' collapsed' : ''}`} aria-label="分类条件">
        <header><div><strong>分类条件</strong><small>{draft.tag_ids.length} / 5 个题材标签 · 选中后自动查询</small></div><div className="react-page-actions"><button className="react-detail-action" type="button" onClick={clearCategory}>清除</button><button className="react-detail-action" type="button" aria-expanded={!categoryCollapsed} onClick={() => setCategoryCollapsed(value => !value)}>{categoryCollapsed ? '展开' : '收起'}</button><button className="react-icon-button" type="button" title="刷新分类" aria-label="刷新分类" disabled={tagLoading} onClick={() => void loadTags(true)}><RefreshCw size={14} /></button></div></header>
        {tagError && <p className="react-page-banner danger" role="alert">{tagError} <button className="react-detail-action" type="button" onClick={() => void loadTags(true)}>重试</button></p>}
        {tagLoading && <p className="react-page-muted" role="status">正在读取 AVdb 分类…</p>}
        {selectedTags.length > 0 && <div className="react-r18-selected-tags">{selectedTags.map(tag => <button type="button" key={tag.id} onClick={() => toggleTag(tag.id, false)}>{tag.name}<span aria-hidden="true">×</span></button>)}</div>}
        {selectionError && <p className="react-page-banner danger" role="alert">{selectionError}</p>}
        {!categoryCollapsed && categories.length > 0 && <div className="react-r18-category-body">
          <div className="react-r18-basic-options"><strong>基础条件</strong>{basicTags.map(tag => <label key={`${tag.type}:${tag.id}`}><input type="checkbox" checked={draft.main_filter.includes(tag.id)} onChange={() => toggleBasic(tag.id)} />{tag.name}</label>)}
            <label className="react-page-field">发行月份<select value={draft.month} onChange={event => updateField('month', event.currentTarget.value)}><option value="">不限月份</option>{monthTags.map(tag => <option value={tag.id} key={tag.id}>{tag.name}</option>)}</select></label>
            <label className="react-page-field">影片时长<select value={draft.duration} onChange={event => updateField('duration', event.currentTarget.value)}><option value="">不限时长</option>{durationTags.map(tag => <option value={tag.id} key={tag.id}>{tag.name}</option>)}</select></label>
            <label className="react-page-field react-r18-tag-search">搜索题材<input type="search" maxLength={120} value={tagSearch} placeholder="筛选分类标签" onChange={event => setTagSearch(event.currentTarget.value)} /></label>
          </div>
          <div className="react-r18-tag-groups">{groups.map(group => {
            const groupId = `${group.type}-${group.id}`
            const open = Boolean(tagSearch.trim()) || openedGroups.has(groupId)
            const limit = groupLimits[groupId] ?? 40
            return <details key={groupId} open={open} onToggle={event => { if (!tagSearch.trim()) toggleGroup(groupId, event.currentTarget.open) }}>
              <summary>{groupLabels[group.id] || group.name}<small>{typeLabels[group.type]} · {group.tags.length} 项</small></summary>
              {open && <><div className="react-r18-tag-options">{group.tags.slice(0, limit).map(tag => {
                const id = `${tag.type}:${tag.id}`
                return <label key={id}><input type="checkbox" checked={draft.tag_ids.includes(id)} onChange={event => toggleTag(id, event.currentTarget.checked)} />{tag.name}</label>
              })}</div>{group.tags.length > limit && <button className="react-detail-action" type="button" onClick={() => showMoreTags(groupId)}>显示更多标签</button>}</>}
            </details>
          })}</div>
        </div>}
      </section>}
    </section>}

    {visiblePage && error && <p className="react-page-banner danger" role="alert">{error}</p>}
    {visiblePage && oldResults && <p className="react-r18-stale" role={loading ? 'status' : 'alert'}>{loading ? '正在加载新条件，当前显示上次结果。' : '新条件尚未成功加载，当前显示上次结果。'}</p>}
    {visiblePage && !oldResults && loading && <p className="react-r18-stale" role="status">正在刷新，保留当前结果…</p>}
    <PageState loading={loading && !visiblePage} error={visiblePage ? '' : error} empty={Boolean(visiblePage && !visiblePage.items.length)} retry={() => void load(true)} label="AVdb 影片" />
    {visiblePage && <>
      <div className="react-r18-results-head"><span>{visiblePage.total == null ? `${visiblePage.items.length} 部` : `共 ${visiblePage.total} 部`}{refreshing ? ' · 正在刷新' : ''}</span><span>第 {currentPage} 页</span></div>
      {visiblePage.items.length > 0 && <div className="react-r18-grid">{visiblePage.items.map(video => <CatalogCard key={video.id} video={video} libraryId={Number(pageLibraryId)} from={`/r18?${pageQuery}`} hideImages={hideImages} />)}</div>}
      {(visiblePage.has_previous || visiblePage.has_more || currentPage > 1) && <nav className="react-page-pager react-r18-pager" aria-label="AVdb 目录分页">
        <button className="react-icon-button" type="button" title="上一页" aria-label="上一页" disabled={loading || oldResults || !visiblePage.has_previous} onClick={() => turn(-1)}><ChevronLeft size={17} /></button>
        <label>第 <input aria-label="跳转页码" type="number" min={1} max={pageMaximum} value={pageInput} disabled={loading || oldResults} onChange={event => setPageInput(event.currentTarget.value)} onKeyDown={event => { if (event.key === 'Enter') jump() }} /> / {pageMaximum} 页</label>
        <button className="react-detail-action" type="button" disabled={loading || oldResults} onClick={jump}>跳转</button>
        <button className="react-icon-button" type="button" title="下一页" aria-label="下一页" disabled={loading || oldResults || !visiblePage.has_more} onClick={() => turn(1)}><ChevronRight size={17} /></button>
      </nav>}
    </>}
    {!librariesLoaded && loading && !visiblePage && <p className="react-page-muted" role="status">正在验证 AVdb 资料库访问权限…</p>}
    {librariesLoaded && !libraries.length && !error && <section className="react-page-panel"><PageHeading title="尚未配置 AVdb 在线资料库" description="先在媒体库管理中添加 AVdb-Only API 地址和服务端 API Key。" action={<Link className="react-detail-action" to="/libraries">媒体库设置</Link>} /></section>}
  </div>
}

function R18Cover({ src, alt, hideImages }: { src: string; alt: string; hideImages: boolean }) {
  const [failed, setFailed] = useState(false)
  useEffect(() => setFailed(false), [src, hideImages])
  const safe = safeR18ImageURL(src)
  return !hideImages && !failed && safe
    ? <img className="react-r18-detail-cover" src={safe} alt={alt} onError={() => setFailed(true)} />
    : <span className="react-r18-detail-cover placeholder" aria-label={hideImages ? '媒体图片已隐藏' : '影片封面不可用'}>{hideImages ? <ImageOff size={29} /> : <Film size={29} />}</span>
}

function sizeLabel(resource: R18Resource) {
  const bytes = resource.size_bytes || resource.size_mb * 1024 * 1024
  return bytes > 0 ? `${(bytes / 1024 ** 3).toFixed(2)} GiB` : '大小未知'
}

export function R18DetailPage() {
  const { libraryId = '', id = '' } = useParams()
  const identity = `${libraryId}/${id}`
  const location = useLocation()
  const history = useViewHistory()
  const hideImages = useReactPreferencesStore(state => state.hideImages)
  const administrator = useReactAuthStore(state => state.user?.Policy.IsAdministrator === true)
  const openPlayer = useReactPlayerStore(state => state.open)
  const toast = useAppRuntimeStore(state => state.showToast)
  const detailScope = usePageRequest()
  const resourceScope = usePageRequest()
  const [video, setVideo] = useState<R18Video>()
  const [loadedIdentity, setLoadedIdentity] = useState('')
  const [libraryName, setLibraryName] = useState('')
  const [localSources, setLocalSources] = useState<Awaited<ReturnType<typeof r18Api.detail>>['local_sources']>([])
  const [selectedLocal, setSelectedLocal] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [localBusy, setLocalBusy] = useState(false)
  const [localError, setLocalError] = useState('')
  const [resources, setResources] = useState<R18Resource[]>([])
  const [resourceLoading, setResourceLoading] = useState(false)
  const [resourceError, setResourceError] = useState('')
  const [nextOffset, setNextOffset] = useState(0)
  const [hasMore, setHasMore] = useState(false)
  const [magnetMode, setMagnetMode] = useState(false)
  const [selectedVersion, setSelectedVersion] = useState('')
  const preview = video?.has_preview ? safeR18PreviewURL(video.preview_url) : ''
  const localSource = localSources.find(source => source.id === selectedLocal) ?? localSources[0]
  const playbackEnabled = Boolean(video && loadedIdentity === identity && (!localSources.length || magnetMode))
  const playback = useR18Playback(libraryId, id, video?.title || 'AVdb 在线影片', resources, playbackEnabled)
  const magnetResources = useMemo(() => {
    const seen = new Set<string>()
    return resources.filter(resource => {
      const key = magnetHash(resource.magnet) || resource.id
      if (seen.has(key)) return false
      seen.add(key); return true
    })
  }, [resources])
  const versionForResource = useCallback((resource: R18Resource) => playback.versions.find(version => version.id === magnetHash(resource.magnet)), [playback.versions])
  const extraVersions = playback.versions.filter(version => version.magnet && !magnetResources.some(resource => magnetHash(resource.magnet) === version.id))
  const sourceFrom = new URLSearchParams(location.search).get('from') || ''
  const backTo = (sourceFrom === '/r18' || sourceFrom.startsWith('/r18?')) && sourceFrom.length < 4096 && !/[\\\r\n]/u.test(sourceFrom)
    ? sourceFrom : `/r18?library_id=${encodeURIComponent(libraryId)}`

  const load = useCallback(async () => {
    const request = detailScope.begin()
    setLoading(true); setError(''); setVideo(undefined); setLoadedIdentity(''); setLocalSources([]); setResources([]); setMagnetMode(false)
    resourceScope.cancel()
    try {
      const result = await r18Api.detail(libraryId, id, request.signal)
      if (!request.isCurrent()) return
      setVideo(result.video); setLoadedIdentity(identity); setLibraryName(result.library_name); setLocalSources(result.local_sources); setSelectedLocal(result.local_sources[0]?.id || '')
    } catch (cause) { if (request.isCurrent()) setError(cause instanceof Error ? cause.message : 'AVdb 影片详情加载失败') }
    finally { if (request.isCurrent()) setLoading(false) }
  }, [detailScope, id, identity, libraryId, resourceScope])
  useEffect(() => { void load(); return () => { detailScope.cancel(); resourceScope.cancel() } }, [identity, load, detailScope, resourceScope])

  const loadResources = useCallback(async (more = false) => {
    if (resourceLoading) return
    const request = resourceScope.begin()
    setResourceLoading(true); setResourceError('')
    try {
      const result = await r18Api.resources(libraryId, id, more ? nextOffset : 0, request.signal)
      if (!request.isCurrent()) return
      const merged = more ? [...resources, ...result.items] : result.items
      setResources([...new Map(merged.map(resource => [resource.id, resource])).values()])
      setNextOffset(result.next_offset); setHasMore(result.has_more)
    } catch (cause) { if (request.isCurrent()) setResourceError(cause instanceof Error ? cause.message : '磁力资源加载失败') }
    finally { if (request.isCurrent()) setResourceLoading(false) }
  }, [id, libraryId, nextOffset, resourceLoading, resourceScope, resources])
  useEffect(() => { if (video) void loadResources() }, [video?.id])

  async function playLocal() {
    if (!localSource || localBusy || magnetMode) return
    setLocalBusy(true); setLocalError('')
    try {
      const result = await r18Api.localPlayback(libraryId, id, localSource.id)
      if (result.source.id !== localSource.id) throw new Error('本地播放版本已变化，请刷新后重试')
      useReactPlayerStore.getState().local(result.source)
    } catch (cause) { setLocalError(cause instanceof Error ? cause.message : '本地 STRM 播放失败，请刷新详情后重试') }
    finally { setLocalBusy(false) }
  }

  async function copy(magnet: string) {
    try {
      await navigator.clipboard.writeText(magnet)
      toast('已复制磁力链接')
    } catch { toast('复制失败，请选中磁力链接手动复制', 'error') }
  }

  function playPreview() {
    if (preview && video) openPlayer({ title: `预览视频 · ${video.title}`, url: preview, type: preview.endsWith('.m3u8') ? 'hls' : 'mp4' })
  }

  return <div className="react-r18-detail-page">
    <button className="react-detail-action react-r18-back" type="button" onClick={() => history.backTo(backTo)}><ArrowLeft size={14} />返回 AVdb 目录</button>
    <PageState loading={loading} error={error} empty={!video} retry={() => void load()} label="AVdb 影片详情" />
    {video && <>
      <section className="react-r18-detail-hero">
        <R18Cover src={video.cover_url} alt={`${video.number || video.content_id} 封面`} hideImages={hideImages} />
        <div className="react-r18-detail-copy"><p className="react-r18-eyebrow"><Database size={13} />{libraryName} · AVdb-Only API</p><code>{video.number || video.content_id}</code>
          <h2>{video.title}</h2>{video.original_title && video.original_title !== video.title && <p>{video.original_title}</p>}
          <dl><div><dt>发行日期</dt><dd>{video.release_date || '未知'}</dd></div><div><dt>时长</dt><dd>{video.duration ? `${video.duration} 分钟` : '未知'}</dd></div><div><dt>评分</dt><dd>{video.score ? video.score.toFixed(1) : '暂无评分'}</dd></div><div><dt>上游磁力</dt><dd>{video.magnets_count} 条</dd></div></dl>
          {localSources.length > 0 && !magnetMode && <section className="react-r18-local-playback">
            <label className="react-page-field">本地 STRM 版本<select value={selectedLocal} disabled={localBusy} onChange={event => setSelectedLocal(event.currentTarget.value)}>{localSources.map(source => <option key={source.id} value={source.id}>{source.library_name} · {source.name}</option>)}</select></label>
            <div className="react-page-actions"><button className="react-primary-button" type="button" id="detail-play" disabled={localBusy || !localSource} onClick={() => void playLocal()}><Play size={15} fill="currentColor" /><span>{localBusy ? '正在检查本地版本…' : '播放本地 STRM'}</span></button><button className="react-detail-action" type="button" onClick={() => document.getElementById('r18-magnets')?.scrollIntoView({ behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth', block: 'start' })}><Magnet size={14} />查看磁力</button><button className="react-detail-action" type="button" onClick={() => setMagnetMode(true)}>手动使用网盘磁力流程</button></div>
            <small>匹配到本地 STRM 后优先直接播放，不自动切换到磁力下载。</small>{localError && <p className="react-page-banner danger" role="alert">{localError}</p>}
          </section>}
          {(magnetMode || !localSources.length) && <section className="react-r18-cloud-controls">
            {localSources.length > 0 && <button className="react-detail-action" type="button" onClick={() => setMagnetMode(false)}>返回本地 STRM</button>}
            <label className="react-page-field">播放网盘<select value={playback.provider} disabled={playback.busy} onChange={event => playback.setProvider(event.currentTarget.value as '115' | 'guangya')}><option value="guangya">光鸭网盘</option><option value="115">115 网盘</option></select></label>
            {playback.versions.length > 0 && <label className="react-page-field">播放版本<select value={selectedVersion || playback.versions[0]?.id || ''} disabled={playback.busy} onChange={event => setSelectedVersion(event.currentTarget.value)}>{playback.versions.map((version, index) => <option key={version.id} value={version.id}>版本 {index + 1} · {version.name} · {version.source ? '已就绪' : version.task ? offlineStatusLabel(version.task.status, playback.providerName) : '未下载'}</option>)}</select></label>}
            <button className="react-primary-button" type="button" disabled={!playback.loaded || playback.busy || !playback.state.configured || !administrator || !playback.versions.length} onClick={() => { const version = playback.versions.find(item => item.id === selectedVersion) ?? playback.versions[0]; if (version) void playback.play(version) }}><Play size={15} fill="currentColor" /><span>{playback.waiting ? '准备播放中…' : '选择磁力版本并播放'}</span></button>
            <small>{administrator ? '启动后服务端后台下载会继续运行；返回页面不会中止下载。' : '只有管理员可以准备未下载的磁力版本。'}</small>
          </section>}
        </div>
      </section>

      {video.has_preview && <section className="react-page-panel react-r18-preview"><PageHeading title="预告片" description="上游预览流，不记录本地影片观看进度。" action={<button className="react-primary-button" type="button" disabled={!preview} onClick={playPreview}><Play size={14} /><span>播放预览</span></button>} />
        {video.images[0] && !hideImages && <img src={imageSize(video.images[0])} alt="影片剧照" loading="lazy" />}
      </section>}

      <section className="react-page-panel" id="r18-magnets"><PageHeading title="AVdb 磁力资源" description={`已加载 ${magnetResources.length} 个不同 BTIH 版本${hasMore ? ' · 还有更多' : ''}`} action={playbackEnabled && <button className="react-icon-button" type="button" title="刷新播放状态" aria-label="刷新播放状态" disabled={playback.busy} onClick={() => void playback.load()}><RefreshCw size={15} /></button>} />
        <p className="react-page-muted">合并 AVdb 在线与本地资源库磁力来源；本地 STRM 可播放状态只由上方本地匹配确认。</p>
        {playbackEnabled && !playback.loaded && <p className="react-page-muted" role="status">正在读取 {playback.providerName} 本地任务…</p>}
        {playbackEnabled && !playback.state.configured && playback.loaded && <p className="react-page-muted">尚未配置 {playback.providerName} 连接。管理员可在 <Link to="/settings">系统设置</Link> 中配置。</p>}
        {playback.error && <p className="react-page-banner danger" role="alert">{playback.error}</p>}
        {playback.waiting && <div className="react-r18-wait" role="status"><span>{playback.message}</span><button className="react-detail-action" type="button" onClick={playback.stopWaiting}>停止自动播放</button></div>}
        {magnetResources.map((resource, index) => {
          const version = versionForResource(resource)
          const canPrepare = Boolean(version?.source || administrator)
          return <article className="react-r18-resource" key={resource.id}>
            <header><div><strong>版本 {index + 1} · {resource.title || resource.number || '磁力资源'}</strong><small>{sizeLabel(resource)} · {version?.source ? '已就绪' : version?.task ? offlineStatusLabel(version.task.status, playback.providerName) : '未下载'}</small></div></header>
            <div className="react-r18-resource-meta">{resource.origins.map(origin => <span key={origin}>{origin === 'online' ? 'AVdb 在线磁力' : 'AVdb 本地资源库磁力'}</span>)}<span>{resource.website || '来源未知'}</span><span>{resource.date || '日期未知'}</span>{resource.tags.map(tag => <span key={tag}>{tag}</span>)}</div>
            <code className="react-r18-magnet">{resource.magnet}</code>
            <div className="react-page-actions"><button className="react-primary-button" type="button" disabled={!version || !canPrepare || !playback.state.configured || playback.busy} onClick={() => version && void playback.play(version)}><Play size={14} /><span>播放此版本</span></button>
              <button className="react-detail-action" type="button" onClick={() => void copy(resource.magnet)}><Copy size={14} />复制磁力</button>{safeR18URL(resource.detail_url) && <a className="react-detail-action" href={safeR18URL(resource.detail_url)} target="_blank" rel="noopener noreferrer"><ExternalLink size={14} />来源页面</a>}
            </div>
          </article>
        })}
        {extraVersions.map((version, index) => <article className="react-r18-resource" key={version.id}><header><div><strong>{version.name || `手动磁力版本 ${index + 1}`}</strong><small>{version.source ? '已就绪' : version.task ? offlineStatusLabel(version.task.status, playback.providerName) : '未下载'}</small></div></header>
          <code className="react-r18-magnet">{version.magnet}</code><button className="react-primary-button" type="button" disabled={!administrator || !playback.state.configured || playback.busy} onClick={() => void playback.play(version)}><Play size={14} /><span>播放此版本</span></button>
        </article>)}
        {!magnetResources.length && !extraVersions.length && !resourceLoading && <p className="react-page-muted">暂无匹配的磁力资源。</p>}
        {resourceError && <p className="react-page-banner danger" role="alert">{resourceError}</p>}
        {hasMore && <button className="react-detail-action react-r18-more" type="button" disabled={resourceLoading} onClick={() => void loadResources(true)}><ChevronRight size={14} />{resourceLoading ? '正在加载…' : '加载更多资源'}</button>}
        {administrator && playback.state.configured && <form className="react-r18-manual-magnet" onSubmit={event => { event.preventDefault(); void playback.addManualMagnet() }}>
          <label className="react-page-field">添加其它磁力版本<input value={playback.manualMagnet} maxLength={8192} disabled={playback.busy} placeholder="magnet:?xt=urn:btih:…" onChange={event => playback.setManualMagnet(event.currentTarget.value)} /></label>
          <button className="react-detail-action" disabled={playback.busy}><Magnet size={14} />添加并准备</button>
        </form>}
      </section>

      {playbackEnabled && <section className="react-page-panel"><PageHeading title={`${playback.providerName} 下载任务`} description="后台任务由服务端持有，关闭详情不会取消下载。" />
        {playback.state.tasks.length === 0 && <p className="react-page-muted">当前没有离线下载任务。</p>}
        {playback.state.tasks.map(task => <article className="react-r18-task" key={task.id}>
          <header><strong>{playback.versions.find(version => version.task?.id === task.id)?.name || '磁力版本'}</strong><span>{offlineStatusLabel(task.status, playback.providerName)}</span></header>
          {offlineProgressVisible(task) && <progress max={100} value={task.progress} aria-label={`${playback.providerName} 下载进度`} />}
          <p>{offlineStageMessage(task) || task.stage || task.error || `${task.progress}%`}</p>
          {typeof task.expires_at === 'number' && task.expires_at > 0 && task.status !== 'expired' && <small>自动清理：{new Date(task.expires_at * 1000).toLocaleString()}</small>}
          {task.files.length > 0 && <div className="react-r18-task-select"><label className="react-page-field">正片文件<select value={playback.selections[task.id] || task.file_id} disabled={playback.busy} onChange={event => playback.setSelection(task.id, event.currentTarget.value)}>{task.files.map(file => <option key={file.id} value={file.id}>{file.path} · {(file.size / 1024 ** 3).toFixed(2)} GiB{file.id === mainVideo(task.files)?.id ? '（推荐正片）' : ''}</option>)}</select></label><button className="react-detail-action" type="button" disabled={playback.busy || !playback.selections[task.id]} onClick={() => void playback.selectFile(task)}><Check size={14} />设为默认版本</button></div>}
          <div className="react-page-actions">{offlineActive(task) && <button className="react-detail-action" type="button" disabled={playback.busy} onClick={() => void playback.changeTask(task, 'cancel')}>取消本地等待</button>}
            {task.retryable && !offlineActive(task) && !['ready', 'waiting_selection'].includes(task.status) && <button className="react-detail-action" type="button" disabled={playback.busy} onClick={() => void playback.changeTask(task, 'retry')}>重试当前阶段</button>}</div>
        </article>)}
      </section>}

      {video.tags.length > 0 && <section className="react-page-panel"><PageHeading title="影片标签" /><div className="react-r18-tags">{video.tags.map(tag => <span key={tag}>{tag}</span>)}</div></section>}
      <ArtworkGallery key={identity} sources={video.images} hidden={hideImages} />
    </>}
  </div>
}
