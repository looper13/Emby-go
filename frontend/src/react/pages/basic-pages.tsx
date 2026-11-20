import { Archive, Check, Copy, Film, Heart, List, Plus, RefreshCw, Search, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import type { MovieDto } from '../../api/contracts'
import { accountApi } from '../../api/account'
import { adminApi } from '../../api/admin'
import { catalogApi, type ResumeEntry } from '../../api/catalog'
import { favoritesApi, favoriteDestination, type FavoriteItem } from '../../api/favorites'
import { javdbCommunityApi, javdbListFavoriteDestination, type JavdbListFavorite } from '../../api/javdb-community'
import { imageURL } from '../../lib/image-url'
import { fmtTime, runStatuses, taskTypes } from '../../lib/format'
import { useAdminRead } from '../hooks/use-admin-read'
import { usePageRequest } from '../hooks/use-page-request'
import { useAppRuntimeStore } from '../app-store'
import { useReactAuthStore } from '../auth-store'
import { useReactPlayerStore } from '../player-store'
import { useReactPreferencesStore, type CardSize } from '../preferences-store'
import { useReactTasksStore } from '../tasks-store'
import { MediaCard } from '../media/MediaCard'
import { PageHeading, PageState } from './PageState'
import './basic-pages.css'

interface OverviewData {
  libraryCount: number
  successTotal: number
  status: { success: number; manual: number; pending: number; incompatible: number }
  resume: ResumeEntry[]
  latest: Awaited<ReturnType<typeof catalogApi.latest>>
}

function useToast() {
  return useAppRuntimeStore((state) => state.showToast)
}

function TaskErrorCell({ error }: { error?: string }) {
  const newline = error?.indexOf('\n') ?? -1
  const summary = newline < 0 ? error || '—' : error?.slice(0, newline) || '—'
  const detail = newline < 0 ? '' : error?.slice(newline + 1) || ''
  return <div className="react-task-error">
    <span>{summary}</span>
    {detail && <details><summary>查看失败明细</summary><pre>{detail}</pre></details>}
  </div>
}

function Shelf({ title, count, movies, tags, userdata, to }: {
  title: string
  count: number
  movies: MovieDto[]
  tags?: Record<string, Record<string, string>>
  userdata?: Awaited<ReturnType<typeof catalogApi.resume>>[number]['userdata'][]
  to?: string
}) {
  const navigate = useNavigate()
  const play = useReactPlayerStore((state) => state.media)
  const administrator = useReactAuthStore((state) => state.user?.Policy.IsAdministrator === true)
  return <section className="react-page-panel" aria-label={title}>
    <PageHeading title={title} description={`${count} 部`} action={to ? <Link className="react-text-link" to={to}>查看全部 →</Link> : undefined} />
    <div className="react-overview-shelf">{movies.map((movie, index) => <MediaCard key={movie.id} movie={movie}
      tags={tags?.[movie.id]} userdata={userdata?.[index]} canManage={administrator}
      onOpen={(id) => navigate(`/item/${id}`)} onQuickPlay={() => play(movie)} onAction={() => {}} />)}</div>
  </section>
}

export function OverviewPage() {
  const scope = usePageRequest()
  const tasks = useReactTasksStore((state) => state.revision)
  const userId = useReactAuthStore((state) => state.user?.Id)
  const run = useReactTasksStore((state) => state.run)
  const busy = useReactTasksStore((state) => state.mutationBusy)
  const [data, setData] = useState<OverviewData>()
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const load = useCallback(async () => {
    const request = scope.begin()
    setLoading(true)
    setError('')
    try {
      const [libraries, successTotal, status, resume, latest] = await Promise.all([
        adminApi.libraries(request.signal), catalogApi.successTotal(request.signal), catalogApi.status(request.signal),
        catalogApi.resume(userId || 1, request.signal), catalogApi.latest(request.signal),
      ])
      if (request.isCurrent()) setData({ libraryCount: libraries.total, successTotal, status, resume, latest })
    } catch (cause) {
      if (request.isCurrent()) setError(cause instanceof Error ? cause.message : '无法读取档案状态')
    } finally { if (request.isCurrent()) setLoading(false) }
  }, [scope, userId])
  useEffect(() => { void load() }, [load, tasks])
  const rows = useMemo(() => data ? ([
    ['已入库', data.status.success, 'success'], ['手动录入', data.status.manual, 'manual'],
    ['待补录', data.status.pending, 'pending'], ['不兼容源', data.status.incompatible, 'incompatible'],
  ] as const).filter(([, count]) => count > 0) : [], [data])

  return <div>
    {data && <div className="react-page-stats">
      <div className="react-page-stat"><span>媒体库</span><strong>{data.libraryCount}</strong><small>Emby 媒体库</small></div>
      <div className="react-page-stat"><span>已入库影片</span><strong>{data.successTotal}</strong><small>NFO 真源 · 可见于 Emby</small></div>
      <div className="react-page-stat"><span>待补录</span><strong>{data.status.pending}</strong><small>缺少 NFO 元数据</small></div>
      <div className="react-page-stat"><span>不兼容源</span><strong>{data.status.incompatible}</strong><small>扫描后重新归类</small></div>
    </div>}
    <PageState loading={loading && !data} error="" empty={false} retry={() => void load()} label="档案状态" />
    {data?.resume.length ? <Shelf title="继续观看" count={data.resume.length} movies={data.resume.map((entry) => entry.movie)}
      tags={Object.fromEntries(data.resume.map((entry) => [entry.movie.id, entry.tags ?? {}]))}
      userdata={data.resume.map((entry) => entry.userdata)} /> : null}
    {data?.latest.items.length ? <Shelf title="最新入库" count={data.latest.total} movies={data.latest.items}
      tags={data.latest.image_tags} userdata={data.latest.items.map((item) => data.latest.userdata?.[item.id])}
      to="/items?sort=datecreated&order=desc" /> : null}
    <section className="react-page-panel">
      <PageHeading title="档案状态" description="NFO 决定影片入库；播放时读取当前 .strm。" action={<div className="react-page-actions">
        <button className="react-icon-button" type="button" title="刷新" aria-label="刷新" disabled={loading} onClick={() => void load()}><RefreshCw size={16} /></button>
        <button className="react-primary-button" id="reindex" type="button" disabled={busy} onClick={() => void run('reindex')}><RefreshCw size={14} /><span>全量重建索引</span></button>
      </div>} />
      {error && <p className="react-page-banner danger" role="alert">{error}</p>}
      {data && (rows.length ? <div className="react-page-table-wrap"><table><thead><tr><th>状态</th><th>数量</th></tr></thead><tbody>{rows.map(([label, count, status]) =>
        <tr key={status}><td><span className={`react-page-status ${status}`}>{label}</span></td><td>{count}</td></tr>)}</tbody></table></div>
        : <div className="react-overview-empty" role="status"><Archive size={32} aria-hidden="true" /><strong>档案为空</strong><small>添加媒体库并开始扫描。</small></div>)}
    </section>
  </div>
}

export function ApiKeysPage() {
  const { data, loading, error, load, active } = useAdminRead(adminApi.keys)
  const toast = useToast()
  const [name, setName] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [deleting, setDeleting] = useState<string>()
  async function create(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (submitting || !name.trim()) return
    setSubmitting(true)
    try { await adminApi.createKey(name.trim()); setName(''); toast('密钥已创建'); if (active()) await load() }
    catch (cause) { toast(cause instanceof Error ? cause.message : '创建失败', 'error') }
    finally { setSubmitting(false) }
  }
  async function copy(key: string) {
    try { await navigator.clipboard.writeText(key); toast('已复制密钥') }
    catch { toast('复制失败，请手动选择', 'error') }
  }
  async function remove(key: string) {
    if (deleting || !window.confirm('删除该 API 密钥？使用它的客户端将立即失效。')) return
    setDeleting(key)
    try { await adminApi.deleteKey(key); toast('密钥已删除'); if (active()) await load() }
    catch (cause) { toast(cause instanceof Error ? cause.message : '删除失败', 'error') }
    finally { setDeleting(undefined) }
  }
  return <div>
    <section className="react-page-panel"><PageHeading title="API 密钥" description={`共 ${data?.total ?? 0} 个 · 供脚本和第三方客户端调用 Emby API`} />
      <PageState loading={loading} error={error} empty={Boolean(data && !data.items.length)} retry={() => void load()} label="API 密钥" />
      {data?.items.length ? <div className="react-page-table-wrap"><table><thead><tr><th>名称</th><th>密钥</th><th>创建时间</th><th>操作</th></tr></thead><tbody>
        {data.items.map((key) => <tr key={key.key}><td>{key.name || '—'}</td><td className="react-page-mono" data-secret>{key.key}</td><td className="react-page-mono">{fmtTime(key.created_at)}</td><td><div className="react-page-row-actions">
          <button className="react-icon-button" type="button" title="复制密钥" aria-label={`复制 ${key.name}`} onClick={() => void copy(key.key)}><Copy size={15} /></button>
          <button className="react-icon-button" type="button" title="删除密钥" aria-label={`删除 ${key.name}`} disabled={deleting === key.key} onClick={() => void remove(key.key)}><Trash2 size={15} /></button>
        </div></td></tr>)}
      </tbody></table></div> : null}
    </section>
    <section className="react-page-panel"><PageHeading title="创建密钥" description="密钥只用于 API 客户端，请按最小权限保管。" />
      <form className="react-page-form" onSubmit={(event) => void create(event)}>
        <label className="react-page-field">名称<input value={name} onChange={(event) => setName(event.currentTarget.value)} placeholder="如：媒体客户端" required maxLength={120} /></label>
        <div className="react-page-footer"><button className="react-primary-button" disabled={submitting}><Plus size={14} /><span>{submitting ? '正在创建…' : '创建密钥'}</span></button></div>
      </form>
    </section>
  </div>
}

export function TasksPage() {
  const { data, loading, error, load } = useAdminRead(adminApi.tasks)
  const revision = useReactTasksStore((state) => state.revision)
  const initialRevision = useRef(revision)
  const busy = useReactTasksStore((state) => state.mutationBusy)
  const run = useReactTasksStore((state) => state.run)
  useEffect(() => {
    if (revision === initialRevision.current) return
    initialRevision.current = revision
    void load()
  }, [revision, load])
  return <section className="react-page-panel"><PageHeading title="任务日志" description="后台任务继续在服务端执行，可在此查看最近状态。" action={<div className="react-page-actions">
    <button className="react-icon-button" type="button" title="刷新任务" aria-label="刷新任务" disabled={loading} onClick={() => void load()}><RefreshCw size={16} /></button>
    <button id="task-scan" className="react-primary-button" type="button" disabled={busy} onClick={() => void run()}><Film size={14} /><span>立即扫描</span></button>
  </div>} />
    {data?.running && <p className="react-page-banner warning" role="status">有任务正在进行中。</p>}
    <PageState loading={loading} error={error} empty={Boolean(data && !data.items.length)} retry={() => void load()} label="任务记录" />
    {data?.items.length ? <div className="react-page-table-wrap"><table><thead><tr><th>类型</th><th>状态</th><th>开始时间</th><th>结束时间</th><th>错误</th></tr></thead><tbody>
      {data.items.map((task) => <tr key={task.id}><td>{taskTypes[task.type] || task.type}</td><td><span className={`react-page-status ${task.status}`}>{runStatuses[task.status] || task.status}</span></td><td className="react-page-mono">{fmtTime(task.started_at)}</td><td className="react-page-mono">{fmtTime(task.ended_at)}</td><td><TaskErrorCell error={task.error} /></td></tr>)}
    </tbody></table></div> : null}
  </section>
}

export function ProbePage() {
  const { data, loading, error, load, active } = useAdminRead(adminApi.probes)
  const toast = useToast()
  const [clearing, setClearing] = useState(false)
  async function clear() {
    if (clearing || !window.confirm('清空全部未知接口探针记录？')) return
    setClearing(true)
    try { await adminApi.clearProbes(); toast('探针记录已清空'); if (active()) await load() }
    catch (cause) { toast(cause instanceof Error ? cause.message : '清空失败', 'error') }
    finally { setClearing(false) }
  }
  return <section className="react-page-panel"><PageHeading title="未知接口探针" description="记录客户端发来但本服务尚未注册的 Emby 请求，上限 1000 条。" action={<button className="react-detail-action" id="clear-probes" type="button" disabled={clearing} onClick={() => void clear()}><Trash2 size={14} /><span>清空记录</span></button>} />
    <PageState loading={loading} error={error} empty={Boolean(data && !data.items.length)} retry={() => void load()} label="探针记录" />
    {data?.items.length ? <div className="react-page-table-wrap"><table><thead><tr><th>方法</th><th>路径</th><th>时间</th></tr></thead><tbody>{data.items.map((item) =>
      <tr key={item.id}><td><span className="react-page-status">{item.method}</span></td><td className="react-page-mono">{item.path}</td><td>{fmtTime(item.created_at)}</td></tr>)}</tbody></table></div> : null}
  </section>
}

const favoriteTabs = [
  ['Movie,Episode', '影片与单集'], ['Series,Season', '剧集与季'], ['Person', '演员'], ['Genre', '类型'],
  ['Studio', '厂商'], ['Tag', '标签'], ['BoxSet', '合集'], ['JavDB', 'AVdb 清单'],
] as const
const favoriteTypeLabels: Record<string, string> = { Movie: '电影', Episode: '单集', Series: '剧集', Season: '季', Person: '演员', Genre: '类型', Studio: '厂商', Tag: '标签', BoxSet: '合集' }

export function FavoritesPage() {
  const location = useLocation()
  const navigate = useNavigate()
  const toast = useToast()
  const hideImages = useReactPreferencesStore((state) => state.hideImages)
  const cardSize = useReactPreferencesStore((state) => state.cardSize)
  const setCardSize = useReactPreferencesStore((state) => state.setCardSize)
  const token = useReactAuthStore((state) => state.token)
  const scope = usePageRequest()
  const query = useMemo(() => new URLSearchParams(location.search), [location.search])
  const type = favoriteTabs.some(([id]) => id === query.get('type')) ? query.get('type')! : favoriteTabs[0]![0]
  const offsetRaw = Number(query.get('offset') || 0)
  const offset = Number.isSafeInteger(offsetRaw) && offsetRaw >= 0 ? offsetRaw : 0
  const isList = type === 'JavDB'
  const [search, setSearch] = useState(query.get('search') || '')
  const [items, setItems] = useState<FavoriteItem[]>([])
  const [listItems, setListItems] = useState<JavdbListFavorite[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState<string>()
  const previousToken = useRef(token)
  const limit = 48
  useEffect(() => {
    if (previousToken.current === token) return
    previousToken.current = token
    setItems([])
    setListItems([])
    setTotal(0)
  }, [token])

  const load = useCallback(async () => {
    const request = scope.begin()
    const requestToken = token
    const isCurrent = () => request.isCurrent() && useReactAuthStore.getState().token === requestToken
    setLoading(true)
    setError('')
    setSearch(query.get('search') || '')
    try {
      const result = isList
        ? await javdbCommunityApi.listFavorites(query.get('search') || '', offset, request.signal)
        : await favoritesApi.list(type, query.get('search') || '', offset, request.signal)
      if (!isCurrent()) return
      if (offset > 0 && !result.items.length) {
        const finalOffset = Math.max(0, Math.ceil(result.total / limit) - 1) * limit
        const next = new URLSearchParams(location.search)
        if (finalOffset) next.set('offset', String(finalOffset)); else next.delete('offset')
        navigate(`/favorites${next.size ? `?${next}` : ''}`, { replace: true })
        return
      }
      if (isList) { setListItems(result.items as JavdbListFavorite[]); setItems([]) }
      else { setItems(result.items as FavoriteItem[]); setListItems([]) }
      setTotal(result.total)
    } catch (cause) { if (isCurrent()) setError(cause instanceof Error ? cause.message : '读取收藏失败') }
    finally { if (isCurrent()) setLoading(false) }
  }, [isList, location.search, navigate, offset, query, scope, token, type])
  useEffect(() => { void load() }, [load])

  function updateQuery(values: Record<string, string | undefined>) {
    const next = new URLSearchParams(location.search)
    for (const [key, value] of Object.entries(values)) { if (value === undefined || value === '') next.delete(key); else next.set(key, value) }
    navigate(`/favorites${next.size ? `?${next}` : ''}`)
  }

  async function removeItem(item: FavoriteItem | JavdbListFavorite, key: string, name: string) {
    if (busy) return
    setBusy(key)
    try {
      if ('list_id' in item) await javdbCommunityApi.removeListFavorite(item.library_id, item.list_id)
      else await favoritesApi.set(item.Id, false)
      toast(`已取消收藏「${name}」`)
      await load()
    } catch (cause) { toast(cause instanceof Error ? cause.message : '取消收藏失败', 'error') }
    finally { setBusy(undefined) }
  }

  return <div>
    <nav className="react-favorites-tabs" role="tablist" aria-label="收藏类型">{favoriteTabs.map(([id, label]) =>
      <button key={id} type="button" role="tab" aria-selected={type === id} onClick={() => updateQuery({ type: id, offset: undefined })}>{label}</button>)}</nav>
    <form className="react-page-toolbar" onSubmit={(event) => { event.preventDefault(); updateQuery({ search: search.trim(), offset: undefined }) }}>
      <label className="react-page-field" style={{ flex: '1 1 220px', maxWidth: 380 }}>搜索收藏<input aria-label="搜索收藏" type="search" value={search} onChange={(event) => setSearch(event.currentTarget.value)} placeholder="搜索收藏" /></label>
      <div className="react-page-actions"><button className="react-icon-button" type="submit" title="搜索" aria-label="搜索"><Search size={16} /></button>
        <button className="react-icon-button" type="button" title="刷新收藏" aria-label="刷新收藏" disabled={loading} onClick={() => void load()}><RefreshCw size={16} /></button>
        <fieldset className="react-card-sizes" aria-label="卡片尺寸"><legend>卡片尺寸</legend>{(['compact', 'normal', 'large'] as CardSize[]).map((size) =>
          <button key={size} type="button" aria-pressed={cardSize === size} title={`${size === 'compact' ? '紧凑' : size === 'normal' ? '标准' : '舒适大图'}卡片`} onClick={() => setCardSize(size)}>{size === 'compact' ? 'S' : size === 'normal' ? 'M' : 'L'}</button>)}</fieldset>
      </div>
      <span className="react-page-muted">{total} 项收藏</span>
    </form>
    <PageState loading={loading && !items.length && !listItems.length} error={error} empty={false} retry={() => void load()} label="收藏" />
    {isList && listItems.length > 0 && <div className="react-favorites-grid">{listItems.map((item) => {
      const destination = javdbListFavoriteDestination(item)
      return <article className="react-favorite-card" key={`${item.library_id}:${item.list_id}`}>
        <Link to={{ pathname: destination.path, search: new URLSearchParams(destination.query).toString() }}>
          <div className="react-favorite-art"><List size={33} aria-hidden="true" /></div>
          <div className="react-favorite-copy"><strong title={item.name}>{item.name}</strong><small>AVdb 清单{item.movie_count !== undefined ? ` · ${item.movie_count} 部` : ''}</small></div>
        </Link>
        <button className="react-favorite-remove" type="button" title={`取消收藏清单 · ${item.name}`} aria-label={`取消收藏清单 · ${item.name}`}
          disabled={busy === `list:${item.library_id}:${item.list_id}`} onClick={() => void removeItem(item, `list:${item.library_id}:${item.list_id}`, item.name)}><Heart size={15} fill="currentColor" /></button>
      </article>
    })}</div>}
    {!isList && items.length > 0 && <div className="react-favorites-grid">{items.map((item) => {
      const destination = favoriteDestination(item)
      const to = destination.query ? { pathname: destination.path, search: new URLSearchParams(destination.query).toString() } : destination.path
      return <article className="react-favorite-card" key={item.Id}>
        <Link to={to}>
          <div className="react-favorite-art">{!hideImages && item.ImageTags.Primary
            ? <img src={imageURL(item.Id, 'Primary', 360, item.ImageTags.Primary)} alt={item.Name} loading="lazy" />
            : <Film size={33} aria-hidden="true" />}</div>
          <div className="react-favorite-copy"><strong title={item.Name}>{item.Name}</strong><small>{favoriteTypeLabels[item.Type] || item.Type}</small></div>
        </Link>
        <button className="react-favorite-remove" type="button" title={`取消收藏 · ${item.Name}`} aria-label={`取消收藏 · ${item.Name}`}
          disabled={busy === `item:${item.Id}`} onClick={() => void removeItem(item, `item:${item.Id}`, item.Name)}><Heart size={15} fill="currentColor" /></button>
      </article>
    })}</div>}
    {!loading && !error && !(isList ? listItems.length : items.length) && <p className="react-page-state">{search ? '没有匹配的收藏。' : '暂无收藏。'}</p>}
    {(total > limit || offset > 0) && <nav className="react-page-pager" aria-label="收藏分页">
      <button className="react-icon-button" type="button" title="上一页" aria-label="上一页" disabled={loading || offset === 0} onClick={() => updateQuery({ offset: offset > limit ? String(offset - limit) : undefined })}>‹</button>
      <span>{Math.floor(offset / limit) + 1} / {Math.max(1, Math.ceil(total / limit))}</span>
      <button className="react-icon-button" type="button" title="下一页" aria-label="下一页" disabled={loading || offset + limit >= total} onClick={() => updateQuery({ offset: String(offset + limit) })}>›</button>
    </nav>}
  </div>
}

const manualInitial = { title: '', number: '', year: '', original_title: '', source_path: '', source_url: '', plot: '', genres: '', tags: '', studios: '' }

export function ManualPage() {
  const { data, loading, error, load, active } = useAdminRead(adminApi.libraries)
  const toast = useToast()
  const [fields, setFields] = useState(manualInitial)
  const [formError, setFormError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (submitting) return
    setSubmitting(true)
    setFormError('')
    try { await adminApi.manual((await import('../../lib/manual')).manualPayload(fields)); toast(`「${fields.title}」已入库`); setFields(manualInitial) }
    catch (cause) { setFormError(cause instanceof Error ? cause.message : '补录失败') }
    finally { setSubmitting(false) }
  }
  const update = (key: keyof typeof manualInitial) => (event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
    const value = event.currentTarget.value
    setFields(previous => ({ ...previous, [key]: value }))
  }
  return <section className="react-page-panel">
    <PageHeading title="手动补录" description="登记一条 http(s) 直链，写入 .strm 和 NFO 后进入 Emby。" />
    <PageState loading={loading && !data} error={error} empty={false} retry={() => void load()} label="媒体库" />
    <form id="manual-form" className="react-page-form" onSubmit={(event) => void submit(event)}>
      <label className="react-page-field">标题 *<input id="m-title" value={fields.title} onChange={update('title')} required /></label>
      <label className="react-page-field">番号<input id="m-number" value={fields.number} onChange={update('number')} placeholder="如 ABF-018" /></label>
      <label className="react-page-field">年份<input id="m-year" type="number" min="1900" max="2100" value={fields.year} onChange={update('year')} /></label>
      <label className="react-page-field full">原名<input id="m-original" value={fields.original_title} onChange={update('original_title')} /></label>
      <label className="react-page-field full">.strm 目标路径 *<input id="m-path" value={fields.source_path} onChange={update('source_path')} placeholder="服务器上的 .strm 绝对路径" required /></label>
      <label className="react-page-field full">媒体直链（http/https）*<input id="m-url" type="url" value={fields.source_url} onChange={update('source_url')} placeholder="https://…/video.mp4" required /></label>
      <label className="react-page-field full">简介<textarea id="m-plot" rows={4} value={fields.plot} onChange={update('plot')} /></label>
      <label className="react-page-field">类型（逗号分隔）<input value={fields.genres} onChange={update('genres')} /></label>
      <label className="react-page-field">标签（逗号分隔）<input value={fields.tags} onChange={update('tags')} /></label>
      <label className="react-page-field">制作商<input value={fields.studios} onChange={update('studios')} /></label>
      {formError && <p className="react-page-banner danger" role="alert">{formError}</p>}
      <div className="react-page-footer"><button id="manual-submit" className="react-primary-button" disabled={submitting}><Plus size={14} /><span>{submitting ? '正在写入…' : '写入并入库'}</span></button>
        <span className="react-page-muted">将归入媒体库：{data?.items?.find((library) => library.Enabled !== false)?.Name || '按路径自动归类'}</span></div>
    </form>
  </section>
}
