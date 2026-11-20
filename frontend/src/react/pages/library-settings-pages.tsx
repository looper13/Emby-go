import {
  Database, Film, Folder, LayoutGrid, List, Plus, RefreshCw, Save, Shuffle, Trash2, Tv, Layers,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { Link, useBeforeUnload } from 'react-router-dom'
import { adminApi, type DirectoryLibraryPayload } from '../../api/admin'
import { ApiError } from '../../api/client'
import { catalogApi } from '../../api/catalog'
import { cloud115Api } from '../../api/cloud115'
import { guangyaApi } from '../../api/guangya'
import type { LibraryDto, LibraryContentType } from '../../api/contracts'
import { configLabels, systemConfigApi, type ConfigPatch, type ConfigState, type InstanceConfig } from '../../api/system-config'
import { libraryDirectoryPaths } from '../../api/contracts'
import { r18Api, type R18Connection } from '../../api/r18'
import { useAdminRead } from '../hooks/use-admin-read'
import { usePageRequest } from '../hooks/use-page-request'
import { useAppRuntimeStore } from '../app-store'
import { useReactPlayerStore } from '../player-store'
import { useReactTasksStore } from '../tasks-store'
import { useConfirmLeave } from '../hooks/use-confirm-leave'
import { DuplicateBrowser } from './DuplicateBrowser'
import { PageHeading, PageState } from './PageState'
import './basic-pages.css'
import './library-settings-pages.css'

type LibraryDraft = {
  Name: string
  Paths: string[]
  Type: LibraryContentType
  ProxyEnabled: boolean
  ResolveRedirects: boolean
  ProxyHeaders: Record<string, string>
}

function libraryDraft(library?: LibraryDto): LibraryDraft {
  return {
    Name: library?.Name ?? '',
    Paths: library ? libraryDirectoryPaths(library) : [''],
    Type: library?.Type || 'movies',
    ProxyEnabled: library?.ProxyEnabled === true,
    ResolveRedirects: library?.ResolveRedirects === true,
    ProxyHeaders: { ...(library?.ProxyHeaders ?? {}) },
  }
}

function LibraryEditor({ library, busy, serverError, onSave, onCancel }: {
  library?: LibraryDto
  busy: boolean
  serverError: string
  onSave: (payload: DirectoryLibraryPayload) => Promise<void>
  onCancel?: () => void
}) {
  const [draft, setDraft] = useState(() => libraryDraft(library))
  const [headersText, setHeadersText] = useState(() => JSON.stringify(library?.ProxyHeaders ?? {}, null, 2))
  const [error, setError] = useState('')

  useEffect(() => {
    const next = libraryDraft(library)
    setDraft(next)
    setHeadersText(JSON.stringify(next.ProxyHeaders, null, 2))
    setError('')
  }, [library?.Id])

  function patch<K extends keyof LibraryDraft>(key: K, value: LibraryDraft[K]) {
    setDraft(current => ({ ...current, [key]: value }))
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy) return
    const name = draft.Name.trim()
    const paths = [...new Set(draft.Paths.map(path => path.trim()))]
    if (!name) { setError('请填写媒体库名称'); return }
    if (!paths.length || paths.some(path => !path)) { setError('请填写每个目录路径，或移除空目录'); return }
    let headers: Record<string, string>
    try {
      const parsed: unknown = JSON.parse(headersText || '{}')
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)
        || Object.entries(parsed).some(([key, value]) => !/^[!#$%&'*+.^_`|~\da-z-]+$/iu.test(key) || typeof value !== 'string' || /[\u0000-\u001f\u007f]/u.test(value))) {
        throw new Error('请求头必须是合法的名称与文本值')
      }
      headers = Object.fromEntries(Object.entries(parsed as Record<string, string>).filter(([, value]) => value.trim()))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '请求头格式不正确')
      return
    }
    setError('')
    void onSave({ ...draft, Name: name, Paths: paths, ProxyHeaders: headers })
  }

  return <form className="react-page-form" onSubmit={submit}>
    {serverError && <p className="react-page-banner danger react-page-field full" role="alert">{serverError}</p>}
    {error && <p className="react-page-banner danger react-page-field full" role="alert">{error}</p>}
    <label className="react-page-field">内容类型<select value={draft.Type} disabled={busy || Boolean(library)} onChange={event => patch('Type', event.currentTarget.value as LibraryContentType)}>
      <option value="movies">影片</option><option value="tvshows">电视节目</option><option value="mixed">混合内容</option>
    </select></label>
    <label className="react-page-field">名称<input value={draft.Name} maxLength={120} disabled={busy} placeholder="如：电影 / 电视剧" onChange={event => patch('Name', event.currentTarget.value)} /></label>
    <div className="react-page-field full"><span>媒体目录</span><div className="react-library-path-list">
      {draft.Paths.map((path, index) => <div className="react-library-path-row" key={index}>
        <label className="react-page-field">目录路径{draft.Paths.length > 1 ? ` ${index + 1}` : ''}<input value={path} disabled={busy} placeholder="服务器上的绝对路径" onChange={event => patch('Paths', draft.Paths.map((current, i) => i === index ? event.currentTarget.value : current))} /></label>
        {draft.Paths.length > 1 && <button className="react-icon-button" type="button" aria-label={`移除目录 ${index + 1}`} title="移除目录" disabled={busy} onClick={() => patch('Paths', draft.Paths.filter((_, i) => i !== index))}>×</button>}
      </div>)}
      <button className="react-detail-action" type="button" disabled={busy} onClick={() => patch('Paths', [...draft.Paths, ''])}><Plus size={14} />添加目录</button>
      <small className="react-page-muted">同一媒体库的所有目录会一起扫描和浏览。</small>
    </div></div>
    <label className="react-page-check"><input type="checkbox" checked={draft.ResolveRedirects} disabled={busy} onChange={event => patch('ResolveRedirects', event.currentTarget.checked)} />解析上游 302 直链</label>
    <label className="react-page-check"><input type="checkbox" checked={draft.ProxyEnabled} disabled={busy} onChange={event => patch('ProxyEnabled', event.currentTarget.checked)} />由 Emby-go 代理媒体流</label>
    <label className="react-page-field full">播放源请求头（JSON）<textarea className="react-page-mono" rows={4} value={headersText} disabled={busy || !draft.ProxyEnabled} onChange={event => setHeadersText(event.currentTarget.value)} placeholder={'{\n  "Referer": "https://example.com/"\n}'} /></label>
    <div className="react-page-footer"><button className="react-primary-button" disabled={busy}><Save size={14} /><span>{busy ? '正在保存…' : library ? '保存修改' : '添加媒体库'}</span></button>{onCancel && <button className="react-detail-action" type="button" disabled={busy} onClick={onCancel}>取消</button>}</div>
  </form>
}

function AvdbConnectionEditor({ library, onSaved, onCancel }: {
  library?: LibraryDto
  onSaved: (next: LibraryDto) => void
  onCancel?: () => void
}) {
  const [name, setName] = useState(library?.Name ?? 'AVdb 在线资料库')
  const [baseURL, setBaseURL] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [hasKey, setHasKey] = useState(false)
  const [revision, setRevision] = useState(library?.Revision)
  const [loading, setLoading] = useState(Boolean(library))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const scope = usePageRequest()

  useEffect(() => {
    if (!library) { setLoading(false); return }
    const request = scope.begin()
    setLoading(true)
    void r18Api.connection(library.Id, request.signal).then(result => {
      if (!request.isCurrent()) return
      setName(result.library.Name)
      setBaseURL(result.connection.base_url)
      setHasKey(result.hasApiKey)
      setRevision(result.connection.revision)
    }).catch(cause => {
      if (request.isCurrent()) setError(cause instanceof Error ? cause.message : '读取 AVdb 连接失败')
    }).finally(() => { if (request.isCurrent()) setLoading(false) })
    return () => scope.cancel()
  }, [library, scope])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy || loading) return
    if (!name.trim() || !baseURL.trim() || (!library || !hasKey) && !apiKey.trim()) {
      setError('请填写名称、AVdb-Only API 地址和 API Key')
      return
    }
    if (library && revision === undefined) {
      setError('此 AVdb 资料库缺少版本号，请刷新后重试')
      return
    }
    setBusy(true)
    setError('')
    try {
      const connection: R18Connection = {
        name: name.trim(), base_url: baseURL.trim(),
        ...(library ? { revision } : {}),
        ...(apiKey.trim() ? { api_key: apiKey.trim() } : {}),
      }
      const saved = await r18Api.saveConnection(connection, library?.Id)
      setApiKey('')
      onSaved(saved)
    } catch (cause) { setError(cause instanceof Error ? cause.message : '保存 AVdb 连接失败') }
    finally { setBusy(false) }
  }

  return <form className="react-page-form" onSubmit={event => void submit(event)}>
    <p className="react-page-muted react-page-field full">连接 AVdb-Only API 读取 JavDB 在线目录和磁力。API Key 只提交到服务端保存，不会回显。</p>
    {error && <p className="react-page-banner danger react-page-field full" role="alert">{error}</p>}
    <label className="react-page-field">资料库名称<input value={name} maxLength={120} disabled={loading || busy} onChange={event => setName(event.currentTarget.value)} /></label>
    <label className="react-page-field">AVdb-Only API 地址<input type="url" value={baseURL} disabled={loading || busy} placeholder="https://avdb-api.example.com" onChange={event => setBaseURL(event.currentTarget.value)} /></label>
    <label className="react-page-field full">API Key<input type="password" autoComplete="new-password" data-secret value={apiKey} required={!library || !hasKey} disabled={loading || busy} placeholder={hasKey ? '已配置，留空保留现有密钥' : '输入服务端 API Key'} onChange={event => setApiKey(event.currentTarget.value)} /></label>
    <div className="react-page-footer"><button className="react-primary-button" disabled={loading || busy}><Database size={14} /><span>{busy ? '正在验证并保存…' : library ? '保存连接配置' : '连接并添加资料库'}</span></button>{onCancel && <button className="react-detail-action" type="button" disabled={busy} onClick={onCancel}>取消</button>}</div>
  </form>
}

function normalizedPath(path: string) {
  const original = path.trim().replace(/\\/gu, '/')
  const value = original.replace(/\/+$/u, '') || '/'
  if (/^[a-z]:$/iu.test(value)) return `${value.toLocaleLowerCase()}/`
  return /^[a-z]:(?:\/|$)/iu.test(value) || value.startsWith('//') ? value.toLocaleLowerCase() : value
}

function removesDirectory(previous: string[], next: string[]) {
  const roots = next.map(normalizedPath)
  return previous.map(normalizedPath).some(path => !roots.some(root => {
    if (path === root) return true
    if (root === '/' || /^[a-z]:\/$/iu.test(root)) return path.startsWith(root)
    return path.startsWith(`${root}/`)
  }))
}

export function LibrariesPage() {
  const { data, loading, error, load, loadData, active } = useAdminRead(adminApi.libraries)
  const revision = useReactTasksStore(state => state.revision)
  const initialRevision = useRef(revision)
  const runTask = useReactTasksStore(state => state.run)
  const mutationBusy = useReactTasksStore(state => state.mutationBusy)
  const play = useReactPlayerStore(state => state.media)
  const showToast = useAppRuntimeStore(state => state.showToast)
  const [editing, setEditing] = useState<LibraryDto | 'new' | null>(null)
  const [r18Editing, setR18Editing] = useState<LibraryDto | 'new' | null>(null)
  const [view, setView] = useState<'grid' | 'table'>(() => {
    try { return localStorage.getItem('emby_library_view_mode') === 'table' ? 'table' : 'grid' } catch { return 'grid' }
  })
  const [busy, setBusy] = useState(false)
  const [busyLibrary, setBusyLibrary] = useState<number | null>(null)
  const [editError, setEditError] = useState('')
  const [createFormRevision, setCreateFormRevision] = useState(0)

  useEffect(() => {
    if (revision === initialRevision.current) return
    initialRevision.current = revision
    void load()
  }, [load, revision])
  function setViewMode(next: 'grid' | 'table') { setView(next); try { localStorage.setItem('emby_library_view_mode', next) } catch { /* storage is optional */ } }
  async function saveDirectory(payload: DirectoryLibraryPayload) {
    if (busy) return
    setBusy(true)
    const library = editing !== 'new' ? editing : undefined
    setEditError('')
    try {
      if (library) {
        if (library.Revision === undefined) throw new Error('此媒体库缺少版本号，请刷新后重试')
        if (removesDirectory(libraryDirectoryPaths(library), payload.Paths)
          && !window.confirm('移除目录会清除不再属于此媒体库的影片索引及其播放进度/收藏。不会删除磁盘文件，确认保存？')) return
        await adminApi.updateLibrary(library.Id, { ...payload, Revision: library.Revision })
        showToast(`已保存媒体库「${payload.Name}」`)
      } else {
        await adminApi.createLibrary(payload)
        showToast(`已添加媒体库「${payload.Name}」`)
      }
      if (!active()) return
      if (library) setEditing(null)
      else setCreateFormRevision(value => value + 1)
      if (active()) await load()
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : '保存媒体库失败'
      setEditError(message)
      showToast(message, 'error')
      if (cause instanceof ApiError && cause.status === 409 && library && active()) {
        const latest = await loadData()
        const current = latest?.items?.find(item => item.Id === library.Id)
        if (current) {
          setEditing(current)
          setEditError(`${message}。已刷新媒体库，请核对当前草稿后再次保存。`)
        }
      }
    }
    finally { setBusy(false) }
  }
  async function togglePlayback(library: LibraryDto, field: 'ProxyEnabled' | 'ResolveRedirects', value: boolean) {
    if (busyLibrary !== null || library.Revision === undefined) return
    setBusyLibrary(library.Id)
    try {
      await adminApi.updateLibrary(library.Id, { ProxyEnabled: library.ProxyEnabled === true, ResolveRedirects: library.ResolveRedirects === true, [field]: value, Revision: library.Revision })
      showToast(`媒体库「${library.Name}」已${value ? '开启' : '关闭'}${field === 'ResolveRedirects' ? '直链解析' : '播放代理'}`)
      if (active()) await load()
    } catch (cause) { showToast(cause instanceof Error ? cause.message : '保存失败', 'error'); if (active()) await load() }
    finally { setBusyLibrary(null) }
  }
  async function remove(library: LibraryDto) {
    if (busy || !window.confirm(`删除媒体库「${library.Name}」及其影片索引？不会删除磁盘文件，但会清除该库的播放进度和收藏。`)) return
    setBusy(true)
    try { await adminApi.deleteLibrary(library.Id); showToast('媒体库已删除'); if (active()) await load() }
    catch (cause) { showToast(cause instanceof Error ? cause.message : '删除媒体库失败', 'error') }
    finally { setBusy(false) }
  }
  async function randomPlay(library: LibraryDto) {
    if (busyLibrary !== null) return
    setBusyLibrary(library.Id)
    try {
      const query = new URLSearchParams({ library_id: String(library.Id), sort: 'random', seed: String(crypto.getRandomValues(new Uint32Array(1))[0] || 1), limit: '1', offset: '0' })
      if (library.Type === 'mixed') query.set('media_type', 'Movie')
      const result = await catalogApi.items(query)
      if (result.items[0]) play(result.items[0]); else showToast(`媒体库「${library.Name}」暂无可用影片`, 'error')
    } catch (cause) { showToast(cause instanceof Error ? cause.message : '随机播放失败', 'error') }
    finally { setBusyLibrary(null) }
  }
  const libraries = data?.items ?? []
  const directories = libraries.filter(library => library.SourceType !== 'avdb_javdb')

  return <div>
    <section className="react-page-panel"><PageHeading title="媒体库" description={`共 ${data?.total ?? 0} 个 · 扫描与浏览均以库为单位`} action={<div className="react-page-actions">
      <button className={`react-icon-button${view === 'grid' ? ' is-selected' : ''}`} type="button" title="卡片视图" aria-label="卡片视图" aria-pressed={view === 'grid'} onClick={() => setViewMode('grid')}><LayoutGrid size={16} /></button>
      <button className={`react-icon-button${view === 'table' ? ' is-selected' : ''}`} type="button" title="列表视图" aria-label="列表视图" aria-pressed={view === 'table'} onClick={() => setViewMode('table')}><List size={16} /></button>
      <button className="react-primary-button" type="button" onClick={() => { setEditing('new'); setR18Editing(null) }}><Plus size={14} /><span>添加媒体库</span></button>
    </div>} />
      <PageState loading={loading} error={error} empty={Boolean(data && !libraries.length)} retry={() => void load()} label="媒体库" />
      {libraries.length > 0 && view === 'grid' && <div className="react-library-grid">{libraries.map(library => {
        const online = library.SourceType === 'avdb_javdb'
        const Icon = library.Type === 'tvshows' ? Tv : library.Type === 'mixed' ? Layers : online ? Database : Film
        const paths = online ? ['AVdb-Only API'] : libraryDirectoryPaths(library)
        return <article className="react-library-card" key={library.Id}>
          <header><span className={`react-library-icon ${online ? 'online' : library.Type || 'movies'}`}><Icon size={19} /></span><div><strong>{library.Name}</strong><small>#{library.Id} · {online ? 'AVdb 在线资料库' : library.Type === 'tvshows' ? '电视节目' : library.Type === 'mixed' ? '混合内容' : '影片'}</small></div></header>
          <div className="react-library-paths">{paths.map((path, index) => <code key={path} title={path}><span>{paths.length > 1 ? `路径 ${index + 1}` : '路径'}</span>{path}</code>)}</div>
          {!online && <div className="react-library-switches">
            <label><input type="checkbox" checked={library.ProxyEnabled === true} disabled={busyLibrary === library.Id || editing?.toString() === String(library.Id)} onChange={event => void togglePlayback(library, 'ProxyEnabled', event.currentTarget.checked)} />播放代理</label>
            <label><input type="checkbox" checked={library.ResolveRedirects === true} disabled={busyLibrary === library.Id || editing?.toString() === String(library.Id)} onChange={event => void togglePlayback(library, 'ResolveRedirects', event.currentTarget.checked)} />解析直链</label>
          </div>}
          <footer>
            <Link className="react-detail-action" to={{ pathname: online ? '/r18' : '/items', search: `?library_id=${library.Id}` }}><Folder size={14} />浏览</Link>
            {!online && <>
              <button className="react-detail-action" type="button" disabled={busyLibrary === library.Id} onClick={() => void randomPlay(library)}><Shuffle size={14} />随机播放</button>
              <button className="react-detail-action" type="button" disabled={mutationBusy} onClick={() => void runTask('scan', library.Id)}><RefreshCw size={14} />增量扫描</button>
            </>}
            {online ? <button className="react-detail-action" type="button" onClick={() => setR18Editing(library)}>API 配置</button>
              : <button className="react-detail-action" type="button" disabled={busy || library.Revision === undefined} onClick={() => { setEditing(library); setR18Editing(null) }}><Save size={14} />编辑</button>}
            <button className="react-icon-button danger" type="button" title="删除媒体库索引" aria-label={`删除 ${library.Name}`} disabled={busy} onClick={() => void remove(library)}><Trash2 size={14} /></button>
          </footer>
        </article>
      })}</div>}
      {libraries.length > 0 && view === 'table' && <div className="react-page-table-wrap"><table><thead><tr><th>名称</th><th>内容类型</th><th>目录 / 来源</th><th>播放方式</th><th>操作</th></tr></thead><tbody>
        {libraries.map(library => <tr key={library.Id}><td><strong>{library.Name}</strong><br /><small>#{library.Id}</small></td><td>{library.SourceType === 'avdb_javdb' ? 'AVdb 在线资料库' : library.Type || '影片'}</td><td className="react-page-mono">{library.SourceType === 'avdb_javdb' ? 'AVdb-Only API' : libraryDirectoryPaths(library).join('\n')}</td><td>{library.SourceType === 'avdb_javdb' ? '—' : `代理 ${library.ProxyEnabled ? '开' : '关'} · 302 ${library.ResolveRedirects ? '开' : '关'}`}</td><td><div className="react-page-row-actions"><Link className="react-detail-action" to={{ pathname: library.SourceType === 'avdb_javdb' ? '/r18' : '/items', search: `?library_id=${library.Id}` }}>浏览</Link><button className="react-detail-action" type="button" disabled={busy || (library.SourceType !== 'avdb_javdb' && library.Revision === undefined)} onClick={() => library.SourceType === 'avdb_javdb' ? setR18Editing(library) : setEditing(library)}>编辑</button><button className="react-icon-button danger" type="button" aria-label={`删除 ${library.Name}`} disabled={busy} onClick={() => void remove(library)}><Trash2 size={14} /></button></div></td></tr>)}
      </tbody></table></div>}
    </section>

    {editing && <section className="react-page-panel"><PageHeading title={editing === 'new' ? '添加媒体库' : `编辑媒体库 · ${editing.Name}`} />
      <LibraryEditor key={editing === 'new' ? `new:${createFormRevision}` : String(editing.Id)} library={editing === 'new' ? undefined : editing} busy={busy} serverError={editError} onSave={saveDirectory} onCancel={editing === 'new' ? undefined : () => { setEditError(''); setEditing(null) }} />
    </section>}
    <DuplicateBrowser libraries={directories} />
    {r18Editing && <section className="react-page-panel"><PageHeading title={r18Editing === 'new' ? '添加 AVdb 在线资料库' : `编辑 AVdb 连接 · ${r18Editing.Name}`} />
      <AvdbConnectionEditor library={r18Editing === 'new' ? undefined : r18Editing} onCancel={r18Editing === 'new' ? undefined : () => setR18Editing(null)} onSaved={library => { setR18Editing(null); showToast(`已保存「${library.Name}」`); if (active()) void load() }} />
    </section>}
    {!r18Editing && <section className="react-page-panel"><details><summary className="react-page-details-summary">添加 AVdb-Only 在线资料库</summary>
      <AvdbConnectionEditor onSaved={library => { showToast(`已添加「${library.Name}」`); if (active()) void load() }} />
    </details></section>}
  </div>
}

type ConfigDraft = Omit<InstanceConfig, 'redis_password_configured' | 'alist_token_configured'>

function configDraft(value: InstanceConfig): ConfigDraft {
  const { redis_password_configured: _redisConfigured, alist_token_configured: _alistConfigured, ...draft } = value
  return structuredClone(draft)
}

function ConfigEditor({ state, onSaved, onReload }: {
  state: ConfigState
  onSaved: (next: ConfigState) => void
  onReload: () => Promise<void>
}) {
  const [draft, setDraft] = useState(() => configDraft(state.config))
  const [redisPassword, setRedisPassword] = useState('')
  const [clearRedisPassword, setClearRedisPassword] = useState(false)
  const [alistToken, setAlistToken] = useState('')
  const [clearAlistToken, setClearAlistToken] = useState(false)
  const [saving, setSaving] = useState(false)
  const [reloading, setReloading] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [baseline, setBaseline] = useState(() => JSON.stringify(configDraft(state.config)))
  const toast = useAppRuntimeStore(value => value.showToast)
  const dirty = JSON.stringify(draft) !== baseline || Boolean(redisPassword || clearRedisPassword || alistToken || clearAlistToken)
  const busy = saving || reloading
  useConfirmLeave(dirty, '有未保存的配置修改，确定离开？')
  useBeforeUnload(useCallback((event: BeforeUnloadEvent) => {
    if (dirty) { event.preventDefault(); event.returnValue = '' }
  }, [dirty]))

  useEffect(() => {
    const next = configDraft(state.config)
    setDraft(next); setBaseline(JSON.stringify(next))
    setRedisPassword(''); setClearRedisPassword(false); setAlistToken(''); setClearAlistToken(false); setError('')
  }, [state.revision, state.config])

  function patch<K extends keyof ConfigDraft>(key: K, value: ConfigDraft[K]) { setDraft(current => ({ ...current, [key]: value })) }

  function restore() {
    const next = configDraft(state.config)
    setDraft(next); setBaseline(JSON.stringify(next))
    setRedisPassword(''); setClearRedisPassword(false); setAlistToken(''); setClearAlistToken(false)
    setError(''); setMessage('')
  }

  async function reload() {
    if (busy || dirty && !window.confirm('重新加载会丢弃未保存的修改，是否继续？')) return
    setReloading(true); setError(''); setMessage('')
    try { await onReload() }
    catch (cause) { setError(cause instanceof Error ? cause.message : '重新加载配置失败') }
    finally { setReloading(false) }
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy || !state.writable) return
    if (draft.server_domains.length > 32) { setError('备用域名最多可配置 32 条'); return }
    for (const domain of draft.server_domains) {
      if (!domain.name.trim() || !domain.url.trim()) { setError('请填写每条备用域名的名称和 URL'); return }
      try {
        if (!['http:', 'https:'].includes(new URL(domain.url).protocol)) throw new Error()
      } catch { setError(`备用域名 ${domain.name} 必须使用有效的 http 或 https 地址`); return }
    }
    const changes = Object.fromEntries(Object.entries(draft).filter(([key, value]) =>
      JSON.stringify(value) !== JSON.stringify(state.config[key as keyof ConfigDraft])))
    const patchValue: ConfigPatch = { ...changes,
      ...(redisPassword ? { redis_password: redisPassword } : {}),
      ...(clearRedisPassword ? { clear_redis_password: true } : {}),
      ...(alistToken ? { alist_token: alistToken } : {}),
      ...(clearAlistToken ? { clear_alist_token: true } : {}) }
    setSaving(true); setError(''); setMessage('')
    try {
      const next = await systemConfigApi.save(state.revision, patchValue)
      onSaved(next)
      const restartFields = next.restart_fields.map(field => configLabels[field] || field)
      setMessage(`配置已保存。${restartFields.length ? `等待重启：${restartFields.join('、')}。` : '无需重启。'}`)
      toast('系统配置已保存')
    } catch (cause) { setError(cause instanceof Error ? cause.message : '保存失败；草稿已保留') }
    finally { setSaving(false) }
  }

  return <form className="react-page-form" onSubmit={event => void submit(event)}>
    <p className="react-page-muted react-page-field full react-config-id">
      <span>实例标识：<code>{state.server_id}</code></span>
      <span>配置版本 {state.revision}</span>
    </p>
    {!state.writable && <p className="react-page-banner warning react-page-field full" role="status">配置文件只读，当前运行配置仍可查看。</p>}
    {state.warnings.map(warning => <p className="react-page-banner warning react-page-field full" key={warning}>{warning}</p>)}
    {error && <p className="react-page-banner danger react-page-field full" role="alert">{error}</p>}
    {message && <p className="react-page-banner react-page-field full" role="status">{message}</p>}
    <label className="react-page-field">服务名称<input value={draft.server_name} maxLength={200} required disabled={busy || !state.writable} onChange={event => patch('server_name', event.currentTarget.value)} /></label>
    <label className="react-page-field">监听端口<input type="number" min={1} max={65535} required value={draft.port} disabled={busy || !state.writable} onChange={event => patch('port', Number(event.currentTarget.value))} /><small className="react-page-muted">当前运行端口：{state.effective.port}</small></label>
    <label className="react-page-check"><input type="checkbox" checked={draft.debug} disabled={busy || !state.writable} onChange={event => patch('debug', event.currentTarget.checked)} />调试模式</label>
    <label className="react-page-check"><input type="checkbox" checked={!draft.disable_library_monitor} disabled={busy || !state.writable} onChange={event => patch('disable_library_monitor', !event.currentTarget.checked)} />启用媒体库监控</label>
    <label className="react-page-field">监控模式<select value={draft.library_monitor_mode} disabled={busy || !state.writable} onChange={event => patch('library_monitor_mode', event.currentTarget.value as ConfigDraft['library_monitor_mode'])}><option value="realtime">实时监听</option><option value="polling">兼容模式（每 30 秒检查）</option></select></label>
    <label className="react-page-field">数据库路径<input className="react-page-mono" data-private-path required value={draft.db_path} disabled={busy || !state.writable} onChange={event => patch('db_path', event.currentTarget.value)} /><small className="react-page-muted" data-private-path>当前运行路径：{state.effective.db_path}</small></label>
    <label className="react-page-field">演员资料目录<input className="react-page-mono" data-private-path value={draft.actors_dir} disabled={busy || !state.writable} onChange={event => patch('actors_dir', event.currentTarget.value)} /><small className="react-page-muted" data-private-path>当前运行路径：{state.effective.actors_dir}</small></label>
    <label className="react-page-field">Redis 地址<input className="react-page-mono" required value={draft.redis_addr} disabled={busy || !state.writable} onChange={event => patch('redis_addr', event.currentTarget.value)} /></label>
    <label className="react-page-field">Redis 数据库<input type="number" min={0} required value={draft.redis_db} disabled={busy || !state.writable} onChange={event => patch('redis_db', Number(event.currentTarget.value))} /></label>
    <label className="react-page-field full">Redis 密码<input type="password" autoComplete="new-password" data-secret value={redisPassword} disabled={busy || !state.writable || clearRedisPassword} placeholder={state.config.redis_password_configured ? '已配置（留空保持不变）' : '未配置（留空保持不变）'} onChange={event => setRedisPassword(event.currentTarget.value)} /></label>
    <label className="react-page-check"><input type="checkbox" checked={clearRedisPassword} disabled={busy || !state.writable} onChange={event => { setClearRedisPassword(event.currentTarget.checked); if (event.currentTarget.checked) setRedisPassword('') }} />清除已保存的 Redis 密码</label>
    <label className="react-page-field">AList 地址<input type="url" value={draft.alist_url} disabled={busy || !state.writable} onChange={event => patch('alist_url', event.currentTarget.value)} /></label>
    <label className="react-page-field">AList Token<input type="password" autoComplete="new-password" data-secret value={alistToken} disabled={busy || !state.writable || clearAlistToken} placeholder={state.config.alist_token_configured ? '已配置（留空保持不变）' : '未配置'} onChange={event => setAlistToken(event.currentTarget.value)} /></label>
    <label className="react-page-check"><input type="checkbox" checked={clearAlistToken} disabled={busy || !state.writable} onChange={event => { setClearAlistToken(event.currentTarget.checked); if (event.currentTarget.checked) setAlistToken('') }} />清除已保存的 AList Token</label>
    <label className="react-page-field">ffprobe 路径<input className="react-page-mono" data-private-path value={draft.ffprobe_path} disabled={busy || !state.writable} onChange={event => patch('ffprobe_path', event.currentTarget.value)} /></label>
    <label className="react-page-field">单条探测超时（秒）<input type="number" min={1} max={3600} required value={draft.probe_timeout_seconds} disabled={busy || !state.writable} onChange={event => patch('probe_timeout_seconds', Number(event.currentTarget.value))} /></label>
    <label className="react-page-field">探测并发数<input type="number" min={1} max={8} required value={draft.probe_concurrency} disabled={busy || !state.writable} onChange={event => patch('probe_concurrency', Number(event.currentTarget.value))} /></label>
    <label className="react-page-check"><input type="checkbox" checked={draft.ffmpeg_enabled} disabled={busy || !state.writable} onChange={event => patch('ffmpeg_enabled', event.currentTarget.checked)} />启用 FFmpeg 播放处理</label>
    <label className="react-page-field">FFmpeg 路径<input className="react-page-mono" data-private-path value={draft.ffmpeg_path} disabled={busy || !state.writable} onChange={event => patch('ffmpeg_path', event.currentTarget.value)} /></label>
    <label className="react-page-field">播放处理并发<input type="number" min={1} max={4} value={draft.playback_concurrency} disabled={busy || !state.writable} onChange={event => patch('playback_concurrency', Number(event.currentTarget.value))} /></label>
    <label className="react-page-field">每个播放会话缓存上限（MiB）<input type="number" min={64} max={65536} value={draft.playback_cache_mb} disabled={busy || !state.writable} onChange={event => patch('playback_cache_mb', Number(event.currentTarget.value))} /></label>
    <label className="react-page-field">在线数据库优先下载网盘<select value={draft.r18_preferred_cloud} disabled={busy || !state.writable} onChange={event => patch('r18_preferred_cloud', event.currentTarget.value as ConfigDraft['r18_preferred_cloud'])}><option value="guangya">光鸭网盘（默认）</option><option value="115">115 网盘</option></select></label>
    <section className="react-page-field full"><span>Emby 备用域名</span><p className="react-page-muted">向支持线路切换的客户端提供候选入口，最多 32 条。</p>
      {draft.server_domains.map((domain, index) => <div className="react-page-domain-row" key={index}>
        <label className="react-page-field">线路 {index + 1} 名称<input required maxLength={200} value={domain.name} disabled={busy || !state.writable} onChange={event => patch('server_domains', draft.server_domains.map((item, i) => i === index ? { ...item, name: event.currentTarget.value } : item))} /></label>
        <label className="react-page-field">HTTP / HTTPS 地址<input type="url" required value={domain.url} disabled={busy || !state.writable} onChange={event => patch('server_domains', draft.server_domains.map((item, i) => i === index ? { ...item, url: event.currentTarget.value } : item))} /></label>
        <button className="react-icon-button" type="button" title={`删除线路 ${index + 1}`} aria-label={`删除线路 ${index + 1}`} disabled={busy || !state.writable} onClick={() => patch('server_domains', draft.server_domains.filter((_, i) => i !== index))}><Trash2 size={14} /></button>
      </div>)}
      <button className="react-detail-action" type="button" disabled={busy || !state.writable || draft.server_domains.length >= 32} onClick={() => patch('server_domains', [...draft.server_domains, { name: '', url: '' }])}><Plus size={14} />添加线路</button>
      {!draft.server_domains.length && <p className="react-page-muted">尚未配置备用线路。</p>}
    </section>
    <div className="react-page-footer"><button className="react-primary-button" disabled={busy || !state.writable || !dirty}><Save size={14} /><span>{saving ? '正在保存…' : '保存设置'}</span></button>
      <button className="react-detail-action" type="button" disabled={busy || !dirty} onClick={restore}><RefreshCw size={14} />恢复已保存设置</button>
      <button className="react-detail-action" type="button" disabled={busy} onClick={() => void reload()}><RefreshCw size={14} />重新加载文件</button>
      <small className="react-page-muted" role="status">{dirty ? '有未保存的修改' : '与已保存配置一致'}</small>
    </div>
  </form>
}

function CloudConnectionPanel({ provider }: { provider: '115' | 'guangya' }) {
  const [config, setConfig] = useState<Awaited<ReturnType<typeof cloud115Api.config>> | null>(null)
  const [cookie, setCookie] = useState('')
  const [accessToken, setAccessToken] = useState('')
  const [refreshToken, setRefreshToken] = useState('')
  const [deviceId, setDeviceId] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const scope = usePageRequest()
  const title = provider === '115' ? '115 连接' : '光鸭网盘连接'
  const load = useCallback(async () => {
    const request = scope.begin(); setError('')
    try { const next = provider === '115' ? await cloud115Api.config(request.signal) : await guangyaApi.config(request.signal); if (request.isCurrent()) setConfig(next) }
    catch (cause) { if (request.isCurrent()) setError(cause instanceof Error ? cause.message : `${title}加载失败`) }
  }, [provider, scope, title])
  useEffect(() => { void load(); return () => scope.cancel() }, [load, scope])
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!config || busy) return
    setBusy(true); setError(''); setNotice('')
    try {
      if (provider === '115') {
        await cloud115Api.save({ cookie, revision: config.revision })
        setCookie('')
        setNotice('115 Cookie 已验证并保存。')
      } else {
        await guangyaApi.save({ access_token: accessToken.trim(), refresh_token: refreshToken.trim(), device_id: deviceId.trim(), revision: config.revision })
        setAccessToken(''); setRefreshToken(''); setDeviceId('')
        setNotice('光鸭连接已验证并保存。')
      }
      await load()
    } catch (cause) { setError(cause instanceof Error ? cause.message : `${title}保存失败`) }
    finally { setBusy(false) }
  }
  return <section className="react-page-panel"><PageHeading title={title} description={provider === '115' ? '离线下载与临时直链播放' : 'AVdb 在线影片的离线下载与临时直链播放'} />
    <p className="react-page-muted">{provider === '115'
      ? '填写 115 Cookie（UID、CID、SEID，可包含 KID）。凭据加密保存在服务端，不回显；缓存目录由系统自动准备。'
      : '填写光鸭登录会话的 access_token、refresh_token 与 device_id。凭据加密保存在服务端，不回显。'}</p>
    {config?.configured && <p className="react-page-banner">已连接账号 {config.account_id}。{config.folder_name ? `缓存目录：${config.folder_name}。` : '留空会保留既有凭据。'}</p>}
    {error && <p className="react-page-banner danger" role="alert">{error}</p>}{notice && <p className="react-page-banner" role="status">{notice}</p>}
    <form className="react-page-form" onSubmit={event => void save(event)}>
      {provider === '115' ? <label className="react-page-field full">115 Cookie<input type="password" autoComplete="new-password" data-secret maxLength={16384} required={!config?.configured} value={cookie} disabled={busy || !config} placeholder={config?.configured ? '留空保留已保存 Cookie' : 'UID=…; CID=…; SEID=…'} onChange={event => setCookie(event.currentTarget.value)} /></label> : <>
        <label className="react-page-field">访问令牌<input type="password" autoComplete="new-password" data-secret maxLength={16384} required={!config?.configured} value={accessToken} disabled={busy || !config} placeholder={config?.configured ? '留空保留已保存令牌' : 'access_token'} onChange={event => setAccessToken(event.currentTarget.value)} /></label>
        <label className="react-page-field">刷新令牌<input type="password" autoComplete="new-password" data-secret maxLength={16384} value={refreshToken} disabled={busy || !config} placeholder="refresh_token（可选）" onChange={event => setRefreshToken(event.currentTarget.value)} /></label>
        <label className="react-page-field full">设备 ID<input type="password" autoComplete="new-password" data-secret maxLength={256} value={deviceId} disabled={busy || !config} placeholder="与登录会话一致；首次配置可留空生成" onChange={event => setDeviceId(event.currentTarget.value)} /></label>
      </>}
      <div className="react-page-footer"><button className="react-primary-button" disabled={busy || !config}><Save size={14} /><span>{busy ? '验证连接中…' : `验证并保存${provider === '115' ? ' 115' : '光鸭'}连接`}</span></button><button className="react-detail-action" type="button" disabled={busy} onClick={() => void load()}><RefreshCw size={14} />刷新状态</button></div>
    </form>
    <p className="react-page-muted">任务缓存保留 24 小时后自动删除，包括尚未下载完成或已取消的任务；到期播放源会停用。请勿在任务缓存目录中保存永久文件。</p>
  </section>
}

export function SettingsPage() {
  const runtime = useAdminRead(adminApi.settings)
  const configRead = useAdminRead(systemConfigApi.read)
  const [config, setConfig] = useState<ConfigState | null>(null)
  useEffect(() => { if (configRead.data) setConfig(configRead.data) }, [configRead.data])
  const runtimeRows = useMemo(() => {
    if (!runtime.data) return []
    const stats = Object.entries(runtime.data.cache_stats).filter(([key]) => !['token', 'apikey'].includes(key)).map(([, value]) => value)
    const hits = stats.reduce((sum, value) => sum + value.hits, 0)
    const misses = stats.reduce((sum, value) => sum + value.misses, 0)
    const shared = stats.reduce((sum, value) => sum + value.shared, 0)
    return [
      ['监听地址', runtime.data.listen], ['数据库路径', runtime.data.db_path], ['缓存后端', `${runtime.data.cache}${runtime.data.redis_addr ? ` · ${runtime.data.redis_addr}/${runtime.data.redis_db}` : ''}`],
      ['Redis 状态', `${runtime.data.redis_online ? '在线' : '离线'}${runtime.data.redis_failures ? ` · 连续失败 ${runtime.data.redis_failures} 次` : ''}`],
      ['缓存命中率', hits + misses ? `${(hits / (hits + misses) * 100).toFixed(1)}% · 命中 ${hits} / 未命中 ${misses}` : '暂无请求'],
      ['合并重复加载', `${shared} 次`], ['媒体库监控', runtime.data.disable_library_monitor ? '已关闭' : runtime.data.library_monitor_mode === 'polling' ? '兼容轮询' : '实时监听'],
    ]
  }, [runtime.data])
  return <div>
    <section className="react-page-panel"><PageHeading title="系统配置" description={config?.writable ? `配置文件：${config.config_path}` : '正在读取配置'} />
      <PageState loading={configRead.loading && !config} error={configRead.error} empty={false} retry={() => void configRead.load()} label="配置" />
      {config && <ConfigEditor state={config} onSaved={next => setConfig(next)} onReload={async () => {
        const next = await configRead.loadData()
        if (!next) throw new Error('重新加载配置失败')
        setConfig(next)
      }} />}
    </section>
    <section className="react-page-panel"><PageHeading title="当前运行状态" description="只读运行值与进程内统计。" action={<button className="react-icon-button" type="button" title="刷新运行状态" aria-label="刷新运行状态" disabled={runtime.loading} onClick={() => void runtime.load()}><RefreshCw size={15} /></button>} />
      <PageState loading={runtime.loading} error={runtime.error} empty={false} retry={() => void runtime.load()} label="运行状态" />
      {runtime.data && <div className="react-page-table-wrap"><table><tbody>{runtimeRows.map(([label, value]) => <tr key={label}><th>{label}</th><td className="react-page-mono" data-private-path={label === '数据库路径' ? '' : undefined}>{value}</td></tr>)}</tbody></table></div>}
      <p className="react-page-muted">MetaTube、GFriends 与翻译配置位于刮削页面。仅当服务部署支持写入时可保存系统配置。</p>
    </section>
    <CloudConnectionPanel provider="115" />
    <CloudConnectionPanel provider="guangya" />
  </div>
}
