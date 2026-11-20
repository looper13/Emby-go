import {
  Check, ChevronLeft, ChevronRight, Film, Heart, ImageOff, ListFilter, Plus, RefreshCw, RotateCcw,
  Save, Search, ShieldCheck, Sparkles, Trash2, UserRound, Users,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { adminApi } from '../../api/admin'
import { actorsApi, type ActorStatus } from '../../api/actors'
import type { LibraryDto } from '../../api/contracts'
import { scrapeApi, type ActorImportPreview, type Candidate, type ScrapeSettings, type SourceTest } from '../../api/scrape'
import { scheduledApi, type ScheduledPayload, type ScheduledTask } from '../../api/scheduled'
import { useAdminRead } from '../hooks/use-admin-read'
import { usePageRequest } from '../hooks/use-page-request'
import { useAppRuntimeStore } from '../app-store'
import { useReactPreferencesStore } from '../preferences-store'
import { useReactScheduledStore } from '../scheduled-store'
import { useReactTasksStore } from '../tasks-store'
import { PageHeading, PageState } from './PageState'
import { FavoriteToggleButton } from '../media/FavoriteToggleButton'
import { imageURL } from '../../lib/image-url'
import { entityIdOf } from '../../lib/media'
import { fmtTime, runStatuses, taskTypes } from '../../lib/format'
import './basic-pages.css'
import './operations-pages.css'

const pageSize = 48
const actorSources = {
  metatube: 'MetaTube',
  gfriends: 'GFriends',
  gfriends_then_metatube: 'GFriends 优先，MetaTube 回退',
} as const

function safeScrapeDraft(settings: ScrapeSettings): ScrapeSettings {
  return { ...settings, metatube_token: '', translate: { ...settings.translate, api_key: '' }, gfriends: { ...settings.gfriends } }
}

function BackgroundProgress({ kind }: { kind: 'scrape' }) {
  const task = useReactTasksStore(state => state[kind])
  const [showFailures, setShowFailures] = useState(false)
  const progress = task.progress
  if (!task.visible && !task.busy && !progress?.finished_at) return null
  return <section className="react-background-progress" aria-live="polite" aria-busy={task.busy}>
    <header><strong>{task.submitting ? '正在启动任务…' : task.busy ? progress?.kind === 'scrape_avatars' ? '头像处理进行中' : progress?.kind === 'actor_import' ? '旧头像导入进行中' : '刮削任务进行中' : progress?.cancelled ? '任务已中止' : progress?.aborted ? '任务异常中止' : '任务已结束'}</strong>
      <span>{progress ? `${progress.done} / ${progress.total}` : ''}</span></header>
    {progress && <>
      {progress.total > 0 && <progress max={progress.total} value={progress.done} aria-label="任务进度" />}
      <p>{progress.current || progress.error || `成功 ${progress.success} · 跳过 ${progress.skipped} · 失败 ${progress.failed}`}</p>
      {progress.error && <p className="react-page-banner danger">{progress.error}</p>}
      {progress.failures.length > 0 && <><button className="react-detail-action" type="button" aria-expanded={showFailures} onClick={() => setShowFailures(value => !value)}>{showFailures ? '隐藏' : '查看'}失败示例（{progress.failures.length}）</button>
        {showFailures && <ul>{progress.failures.map((failure, index) => <li key={`${index}:${failure}`}>{failure}</li>)}</ul>}</>}
    </>}
    {task.unavailable && <p className="react-page-muted">暂时无法读取进度，服务端任务仍会继续。</p>}
    {task.cancelling && <p className="react-page-muted" role="status">正在请求服务端中止…</p>}
    {task.busy && <button className="react-detail-action danger" type="button" disabled={task.cancelling} onClick={() => void task.cancel()}>中止任务</button>}
  </section>
}

function AvatarSettings({ settings, onSave, onTest, dirty, setDirty, busy }: {
  settings: ScrapeSettings
  onSave: (patch: Partial<ScrapeSettings>) => Promise<void>
  onTest: (target: SourceTest) => Promise<void>
  dirty: boolean
  setDirty: (value: boolean) => void
  busy: boolean
}) {
  const [source, setSource] = useState(settings.avatar_source)
  const [oldDir, setOldDir] = useState(settings.avatars_dir)
  const [gfriends, setGfriends] = useState({ ...settings.gfriends })
  const [testing, setTesting] = useState(false)
  const [error, setError] = useState('')
  const baseline = useRef('')
  useEffect(() => {
    setSource(settings.avatar_source); setOldDir(settings.avatars_dir); setGfriends({ ...settings.gfriends })
    baseline.current = JSON.stringify({ source: settings.avatar_source, oldDir: settings.avatars_dir, gfriends: settings.gfriends })
    setDirty(false)
  }, [settings, setDirty])
  const current = JSON.stringify({ source, oldDir, gfriends })
  useEffect(() => setDirty(current !== baseline.current), [current, setDirty])
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setError('')
    try { await onSave({ avatar_source: source, avatars_dir: oldDir.trim(), gfriends: { ...gfriends } }) }
    catch (cause) { setError(cause instanceof Error ? cause.message : '保存头像设置失败') }
  }
  async function test() {
    if (testing || busy || dirty) return
    setTesting(true)
    try { await onTest('gfriends') }
    catch (cause) { setError(cause instanceof Error ? cause.message : 'GFriends 检查失败') }
    finally { setTesting(false) }
  }
  function restore() {
    setSource(settings.avatar_source); setOldDir(settings.avatars_dir); setGfriends({ ...settings.gfriends }); setError('')
  }
  const status = settings.gfriends_status
  return <section className="react-page-panel">
    <PageHeading title="头像来源与存储" description="演员头像对所有影片共享；保留已有头像和非空 NFO 地址。" />
    <form className="react-page-form" onSubmit={event => void save(event)}>
      {error && <p className="react-page-banner danger react-page-field full" role="alert">{error}</p>}
      <label className="react-page-field full">头像来源<select value={source} disabled={busy} onChange={event => setSource(event.currentTarget.value as ScrapeSettings['avatar_source'])}>
        <option value="metatube">MetaTube</option><option value="gfriends">GFriends</option><option value="gfriends_then_metatube">GFriends 优先，MetaTube 回退</option>
      </select><small>GFriends 无需 MetaTube Token；已存在头像优先保留。</small></label>
      {source !== 'metatube' && <>
        <label className="react-page-field full">GFriends 索引地址<input type="url" value={gfriends.index_url} disabled={busy} onChange={event => { const index_url = event.currentTarget.value; setGfriends(current => ({ ...current, index_url })) }} /></label>
        <label className="react-page-field full">头像图片根地址<input type="url" value={gfriends.base_url} disabled={busy} onChange={event => { const base_url = event.currentTarget.value; setGfriends(current => ({ ...current, base_url })) }} /></label>
        <label className="react-page-field">索引刷新间隔（小时）<input type="number" min={1} max={720} value={gfriends.refresh_hours} disabled={busy} onChange={event => { const refresh_hours = Number(event.currentTarget.value); setGfriends(current => ({ ...current, refresh_hours })) }} /></label>
        <div className="react-page-field"><span>索引状态</span><div className="react-page-muted" role="status">{status?.version ? `${status.groups} 个来源 · ${status.entries} 条映射${status.stale ? ' · 使用旧缓存' : ''}` : '尚未下载索引'}</div>
          {status?.updated_at && <small>最近成功检查：{new Date(status.updated_at).toLocaleString()}</small>}{status?.error && <small>{status.error}</small>}
          <button className="react-detail-action" type="button" disabled={dirty || busy || testing} onClick={() => void test()}><RefreshCw size={14} />{testing ? '正在检查…' : '测试并刷新索引'}</button>
        </div>
      </>}
      <label className="react-page-field full">旧头像来源目录<input className="react-page-mono" data-private-path value={oldDir} disabled={busy} onChange={event => setOldDir(event.currentTarget.value)} /><small>新头像写入统一资料目录；旧目录仅用于兼容读取和复制导入。</small></label>
      {source !== 'gfriends' && <p className="react-page-muted react-page-field full">MetaTube 地址和 Token 沿用刮削配置。<Link to="/scrape">打开刮削配置</Link></p>}
      <div className="react-page-footer"><button className="react-primary-button" disabled={busy}><Save size={14} /><span>保存头像设置</span></button><button className="react-detail-action" type="button" disabled={busy || !dirty} onClick={restore}><RotateCcw size={14} />恢复</button><span className="react-page-muted">{dirty ? '有未保存的修改' : '设置已保存'}</span></div>
    </form>
  </section>
}

export function ActorsPage() {
  const toast = useAppRuntimeStore(state => state.showToast)
  const hideImages = useReactPreferencesStore(state => state.hideImages)
  const tasks = useReactTasksStore()
  const taskRevision = useReactTasksStore(state => state.revision)
  const [libraryId, setLibraryId] = useState(0)
  const [status, setStatus] = useState<ActorStatus>('all')
  const [search, setSearch] = useState('')
  const [term, setTerm] = useState('')
  const [favoritesOnly, setFavoritesOnly] = useState(false)
  const [offset, setOffset] = useState(0)
  const [avatarDirty, setAvatarDirty] = useState(false)
  const [importPreview, setImportPreview] = useState<ActorImportPreview>()
  const [importLoading, setImportLoading] = useState(false)
  const [importError, setImportError] = useState('')
  const [avatarSaving, setAvatarSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const settingsRead = useAdminRead(async signal => {
    const [settings, libs] = await Promise.all([scrapeApi.settings(signal), adminApi.libraries(signal)])
    return { settings, libraries: libs.items ?? [] }
  })
  const actorsRead = useAdminRead(signal => actorsApi.list({ library_id: libraryId, status, search: term, offset, limit: pageSize, ...(favoritesOnly ? { favorite: 'true' } : {}) }, signal))
  const actorReadKey = JSON.stringify([libraryId, status, term, favoritesOnly, offset, taskRevision])
  const lastActorReadKey = useRef(actorReadKey)
  const libraries = useMemo(() => settingsRead.data?.libraries ?? [], [settingsRead.data])
  const actorData = actorsRead.data
  useEffect(() => { setOffset(0) }, [libraryId, status, term, favoritesOnly])
  useEffect(() => {
    if (actorReadKey === lastActorReadKey.current) return
    lastActorReadKey.current = actorReadKey
    void actorsRead.load()
  }, [actorReadKey, actorsRead.load])
  useEffect(() => {
    if (!actorData || offset === 0 || actorData.items.length) return
    setOffset(Math.max(0, Math.ceil(actorData.total / pageSize) - 1) * pageSize)
  }, [actorData, offset])
  const sourceName = actorSources[settingsRead.data?.settings.avatar_source ?? 'metatube']

  async function saveAvatar(patch: Partial<ScrapeSettings>) {
    if (avatarSaving) return
    setAvatarSaving(true)
    try {
      const next = await scrapeApi.save(patch)
      toast('头像设置已保存并即时生效')
      if (settingsRead.active() && settingsRead.data) {
        settingsRead.load()
        setImportPreview(undefined)
        void next
      }
    } catch (cause) { toast(cause instanceof Error ? cause.message : '保存头像设置失败', 'error') }
    finally { setAvatarSaving(false) }
  }
  async function testSource(target: SourceTest) {
    if (testing) return
    setTesting(true)
    try {
      const result = await scrapeApi.test(target)
      toast(result.ok ? `${result.detail}（${result.elapsed_ms}ms）` : `连接失败：${result.error}`, result.ok ? 'ok' : 'error')
    } catch (cause) { toast(cause instanceof Error ? cause.message : '测试失败', 'error') }
    finally { setTesting(false) }
  }
  async function checkImport() {
    setImportLoading(true); setImportError(''); setImportPreview(undefined)
    const controller = new AbortController()
    try { setImportPreview(await scrapeApi.importPreview(controller.signal)) }
    catch (cause) { setImportError(cause instanceof Error ? cause.message : '检查旧头像失败') }
    finally { setImportLoading(false) }
  }

  return <div>
    <section className="react-page-panel"><PageHeading title="演员头像管理" description={`头像来源：${sourceName} · 批量任务按所选媒体库筛选候选，不受搜索或分页影响。`} action={<button className="react-primary-button" type="button" disabled={tasks.mutationBusy || avatarDirty || avatarSaving || !settingsRead.data?.settings.avatar_configured || !actorData?.stats.total} onClick={() => void tasks.scrape.run({ library_id: libraryId }, true)}><Sparkles size={14} /><span>{tasks.scrape.submitting ? '正在启动…' : '一键补齐头像'}</span></button>} />
      <PageState loading={settingsRead.loading} error={settingsRead.error} empty={false} retry={() => void settingsRead.load()} label="头像设置" />
      {!settingsRead.data?.settings.avatar_configured && settingsRead.data && <p className="react-page-banner warning">请先保存可用的头像来源配置。GFriends 无需 MetaTube Token。</p>}
      {avatarDirty && <p className="react-page-banner warning" role="status">头像设置有未保存修改，请保存或恢复后再启动批量任务。</p>}
      <div className="react-actor-controls"><label className="react-page-field">处理范围<select value={libraryId} onChange={event => setLibraryId(Number(event.currentTarget.value))}><option value={0}>全部媒体库</option>{libraries.map(library => <option key={library.Id} value={library.Id}>{library.Name}</option>)}</select></label>
        <span className="react-page-muted">批量范围：{libraries.find(library => library.Id === libraryId)?.Name || '全部媒体库'}</span>
      </div>
      {actorData && <div className="react-page-stats react-actor-stats"><div className="react-page-stat"><span>演员总数</span><strong>{actorData.stats.total}</strong></div><div className="react-page-stat"><span>已有头像</span><strong>{actorData.stats.ready}</strong></div><div className="react-page-stat"><span>缺少头像</span><strong>{actorData.stats.missing}</strong></div></div>}
      <BackgroundProgress kind="scrape" />
    </section>

    {settingsRead.data && <details className="react-actor-config"><summary>头像来源与存储设置</summary><div className="react-actor-config-body">
      <AvatarSettings settings={settingsRead.data.settings} onSave={saveAvatar} onTest={testSource} dirty={avatarDirty} setDirty={setAvatarDirty} busy={avatarSaving || testing || tasks.mutationBusy} />
      <section className="react-page-panel"><PageHeading title="旧头像导入" description="按当前演员姓名复制可用图片；保留原文件与影片 NFO。" />
        <p className="react-page-mono" data-private-path>{settingsRead.data.settings.avatars_dir || '(默认旧头像目录)'}</p>
        <button className="react-detail-action" type="button" disabled={avatarDirty || avatarSaving || importLoading || tasks.mutationBusy} onClick={() => void checkImport()}><Search size={14} />{importLoading ? '正在检查…' : '检查可导入头像'}</button>
        {importError && <p className="react-page-banner danger" role="alert">{importError}</p>}
        {importPreview && <><p className="react-page-muted" role="status">待导入 {importPreview.ready} · 已存在 {importPreview.existing} · 冲突 {importPreview.conflicts} · 损坏 {importPreview.damaged} · 无旧图 {importPreview.missing}</p>
          {importPreview.samples.length > 0 && <ul>{importPreview.samples.map(sample => <li key={sample}>{sample}</li>)}</ul>}
          <button className="react-primary-button" type="button" disabled={avatarDirty || !importPreview.ready || tasks.mutationBusy} onClick={() => void tasks.scrape.run({}, 'import')}><Plus size={14} /><span>开始复制导入</span></button>
        </>}
      </section>
    </div></details>}

    <section className="react-page-panel"><PageHeading title="演员列表" description={`共 ${actorData?.total ?? 0} 位`} action={<button className="react-icon-button" type="button" title="刷新演员" aria-label="刷新演员" disabled={actorsRead.loading} onClick={() => void actorsRead.load()}><RefreshCw size={15} /></button>} />
      <form className="react-page-toolbar" onSubmit={event => { event.preventDefault(); setTerm(search.trim()); setOffset(0) }}>
        <label className="react-page-field react-actor-search">搜索演员<input type="search" value={search} maxLength={120} onChange={event => setSearch(event.currentTarget.value)} placeholder="输入演员姓名" /></label>
        <label className="react-page-field">头像状态<select value={status} onChange={event => setStatus(event.currentTarget.value as ActorStatus)}><option value="all">全部</option><option value="missing">缺少头像</option><option value="ready">已有头像</option></select></label>
        <label className="react-page-check"><input type="checkbox" checked={favoritesOnly} onChange={event => setFavoritesOnly(event.currentTarget.checked)} />仅收藏</label>
        <button className="react-icon-button" type="submit" title="搜索演员" aria-label="搜索演员"><Search size={15} /></button>
      </form>
      <PageState loading={actorsRead.loading && !actorData} error={actorsRead.error} empty={Boolean(actorData && !actorData.items.length)} retry={() => void actorsRead.load()} label="演员" />
      {actorData?.items.length ? <>
        <div className="react-actor-grid">{actorData.items.map(actor => {
          const id = entityIdOf('person', actor.name)
          return <article className="react-actor-card" key={actor.name}>
            <Link to={{ pathname: '/items', search: `?person=${encodeURIComponent(actor.name)}${libraryId ? `&library_id=${libraryId}` : ''}` }}>
              <div className="react-actor-art">{!hideImages && actor.has_image
                ? <img src={imageURL(id, 'Primary', 260, actor.image_tag)} alt={`${actor.name} 头像`} loading="lazy" />
                : <span aria-label={hideImages ? '媒体图片已隐藏' : '缺少头像'}>{hideImages ? <ImageOff size={26} /> : <Users size={26} />}</span>}
                <small className={actor.has_image ? 'ready' : 'missing'}>{actor.has_image ? '已有头像' : '缺少头像'}</small><small className="count">{actor.movie_count} 部</small>
              </div><strong>{actor.name}</strong><span>{actor.movie_count} 部影片 · 查看作品</span>
            </Link>
            <FavoriteToggleButton itemId={id} favorite={actor.favorite === true} label={actor.name} />
          </article>
        })}</div>
        {(actorData.total > pageSize || offset > 0) && <nav className="react-page-pager" aria-label="演员分页">
          <button className="react-icon-button" type="button" title="上一页" aria-label="上一页" disabled={actorsRead.loading || offset === 0} onClick={() => setOffset(value => Math.max(0, value - pageSize))}><ChevronLeft size={17} /></button>
          <span>{Math.floor(offset / pageSize) + 1} / {Math.max(1, Math.ceil(actorData.total / pageSize))}</span>
          <button className="react-icon-button" type="button" title="下一页" aria-label="下一页" disabled={actorsRead.loading || offset + pageSize >= actorData.total} onClick={() => setOffset(value => value + pageSize)}><ChevronRight size={17} /></button>
        </nav>}
      </> : actorData && <p className="react-page-muted">{actorData.stats.total ? '没有符合筛选条件的演员。' : '当前媒体库还没有演员；扫描包含演员信息的 NFO 后会显示在这里。'}</p>}
    </section>
  </div>
}

export function ScrapePage() {
  const toast = useAppRuntimeStore(state => state.showToast)
  const tasks = useReactTasksStore()
  const revision = useReactTasksStore(state => state.revision)
  const read = useAdminRead(async signal => {
    const [settings, libraries] = await Promise.all([scrapeApi.settings(signal), adminApi.libraries(signal)])
    return { settings, libraries: libraries.items ?? [] }
  })
  const [draft, setDraft] = useState<ScrapeSettings>()
  const [baseline, setBaseline] = useState('')
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState<Set<SourceTest>>(() => new Set())
  const [libraryId, setLibraryId] = useState(0)
  const [onlyMissing, setOnlyMissing] = useState(true)
  const [force, setForce] = useState(false)
  const [count, setCount] = useState<number | null>(null)
  const [countError, setCountError] = useState('')
  const candidatesScope = usePageRequest()
  const dirty = Boolean(draft && JSON.stringify(draft) !== baseline)

  useEffect(() => { if (read.data) { const next = safeScrapeDraft(read.data.settings); setDraft(next); setBaseline(JSON.stringify(next)) } }, [read.data])
  const loadCandidates = useCallback(async (library: number, missing: boolean) => {
    const request = candidatesScope.begin(); setCount(null); setCountError('')
    try { const result = await scrapeApi.candidates(library, missing, request.signal); if (request.isCurrent()) setCount(result) }
    catch (cause) { if (request.isCurrent()) setCountError(cause instanceof Error ? cause.message : '候选读取失败') }
  }, [candidatesScope])
  useEffect(() => { void loadCandidates(libraryId, onlyMissing) }, [loadCandidates, libraryId, onlyMissing, revision])
  useEffect(() => () => candidatesScope.cancel(), [candidatesScope])

  function patch<K extends keyof ScrapeSettings>(key: K, value: ScrapeSettings[K]) { setDraft(current => current ? ({ ...current, [key]: value }) : current) }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!draft || saving) return
    setSaving(true)
    try {
      const next = await scrapeApi.save({ ...draft, translate: { ...draft.translate }, gfriends: { ...draft.gfriends } })
      const safe = safeScrapeDraft(next); setDraft(safe); setBaseline(JSON.stringify(safe)); toast('刮削设置已保存并即时生效')
      if (read.active()) await read.load()
    } catch (cause) { toast(cause instanceof Error ? cause.message : '保存刮削设置失败', 'error') }
    finally { setSaving(false) }
  }
  async function test(target: SourceTest) {
    if (testing.has(target) || saving) return
    setTesting(current => new Set(current).add(target))
    try {
      const result = await scrapeApi.test(target)
      toast(result.ok ? `${result.detail}（${result.elapsed_ms}ms）` : `连接失败：${result.error}`, result.ok ? 'ok' : 'error')
    } catch (cause) { toast(cause instanceof Error ? cause.message : '连接测试失败', 'error') }
    finally { setTesting(current => { const next = new Set(current); next.delete(target); return next }) }
  }
  if (!draft && read.data) return null
  return <div>
    <PageState loading={read.loading && !read.data} error={read.error} empty={false} retry={() => void read.load()} label="刮削配置" />
    {draft && read.data && <>
      <section className="react-page-panel"><PageHeading title="影片刮削配置" description="刮削写入影片 NFO 和本地资源；密钥字段留空表示保留已保存凭据。" action={<div className="react-page-actions">
        <button className="react-detail-action" type="button" disabled={saving || testing.has('metatube')} onClick={() => void test('metatube')}><RefreshCw size={14} />测试 MetaTube</button>
        <button className="react-detail-action" type="button" disabled={saving || testing.has('translate')} onClick={() => void test('translate')}><RefreshCw size={14} />测试翻译</button>
      </div>} />
        <form className="react-page-form" onSubmit={event => void save(event)}>
          <label className="react-page-field full">MetaTube 地址<input type="url" value={draft.metatube_url} disabled={saving} onChange={event => patch('metatube_url', event.currentTarget.value)} /></label>
          <label className="react-page-field full">MetaTube Token<input type="password" autoComplete="new-password" data-secret value={draft.metatube_token} maxLength={16384} disabled={saving} placeholder={read.data.settings.metatube_token ? '已配置，留空保留' : '可留空（后端未启用鉴权时）'} onChange={event => patch('metatube_token', event.currentTarget.value)} /></label>
          <label className="react-page-field">请求超时（秒）<input type="number" min={1} max={600} value={draft.timeout_seconds} disabled={saving} onChange={event => patch('timeout_seconds', Number(event.currentTarget.value))} /></label>
          <label className="react-page-field">并发数<input type="number" min={1} max={8} value={draft.concurrency} disabled={saving} onChange={event => patch('concurrency', Number(event.currentTarget.value))} /></label>
          <label className="react-page-field">图片质量<input type="number" min={1} max={100} value={draft.image_quality} disabled={saving} onChange={event => patch('image_quality', Number(event.currentTarget.value))} /></label>
          <label className="react-page-check"><input type="checkbox" checked={draft.download_images} disabled={saving} onChange={event => patch('download_images', event.currentTarget.checked)} />下载海报与背景图</label>
          <label className="react-page-check"><input type="checkbox" checked={draft.download_stills} disabled={saving} onChange={event => patch('download_stills', event.currentTarget.checked)} />下载全部剧照</label>
          <label className="react-page-check"><input type="checkbox" checked={draft.download_trailer} disabled={saving} onChange={event => patch('download_trailer', event.currentTarget.checked)} />下载 MP4 预告片</label>
          <label className="react-page-check"><input type="checkbox" checked={draft.overwrite} disabled={saving} onChange={event => patch('overwrite', event.currentTarget.checked)} />默认强制覆盖 NFO</label>
          <div className="react-page-field full"><h3>内嵌翻译</h3></div>
          <label className="react-page-check"><input type="checkbox" checked={draft.translate.title} disabled={saving} onChange={event => patch('translate', { ...draft.translate, title: event.currentTarget.checked })} />翻译标题</label>
          <label className="react-page-check"><input type="checkbox" checked={draft.translate.summary} disabled={saving} onChange={event => patch('translate', { ...draft.translate, summary: event.currentTarget.checked })} />翻译简介</label>
          <label className="react-page-field">目标语言<input value={draft.translate.target_lang} disabled={saving} placeholder="ZH / ZH-HANT / JA" onChange={event => patch('translate', { ...draft.translate, target_lang: event.currentTarget.value })} /></label>
          <label className="react-page-field">翻译超时（秒）<input type="number" min={1} max={300} value={draft.translate.timeout_seconds} disabled={saving} onChange={event => patch('translate', { ...draft.translate, timeout_seconds: Number(event.currentTarget.value) })} /></label>
          <label className="react-page-field full">翻译服务地址<input type="url" value={draft.translate.api_url} disabled={saving} onChange={event => patch('translate', { ...draft.translate, api_url: event.currentTarget.value })} /></label>
          <label className="react-page-field full">翻译网关 Token<input type="password" autoComplete="new-password" data-secret value={draft.translate.api_key} maxLength={16384} disabled={saving} placeholder={read.data.settings.translate.api_key ? '已配置，留空保留' : 'gateway_token'} onChange={event => patch('translate', { ...draft.translate, api_key: event.currentTarget.value })} /></label>
          <p className="react-page-muted react-page-field full">图片、剧照和预告选项适用于单条、批量及计划任务。自动批量刮削遇到非番号精确命中会跳过，之后可在影片详情页逐条确认。</p>
          <div className="react-page-footer"><button className="react-primary-button" disabled={saving}><Save size={14} /><span>{saving ? '正在保存…' : '保存并即时生效'}</span></button><button className="react-detail-action" type="button" disabled={saving || !dirty} onClick={() => { if (read.data) { const next = safeScrapeDraft(read.data.settings); setDraft(next); setBaseline(JSON.stringify(next)) } }}><RotateCcw size={14} />恢复已保存设置</button><span className="react-page-muted">{dirty ? '有未保存的修改' : '设置已保存'}</span></div>
        </form>
      </section>

      <section className="react-page-panel"><PageHeading title="立即刮削" description="读取候选数量后提交服务端后台任务；离开页面不会停止已启动的任务。" action={<span className="react-page-muted">{countError || (count === null ? '正在读取候选数…' : `本次将处理 ${count} 条`)}</span>} />
        <div className="react-page-toolbar"><label className="react-page-field">媒体库<select value={libraryId} onChange={event => setLibraryId(Number(event.currentTarget.value))}><option value={0}>全部媒体库</option>{read.data.libraries.map(library => <option key={library.Id} value={library.Id}>{library.Name}</option>)}</select></label>
          <label className="react-page-check"><input type="checkbox" checked={onlyMissing} onChange={event => setOnlyMissing(event.currentTarget.checked)} />仅处理缺失元数据</label>
          <label className="react-page-check"><input type="checkbox" checked={force} onChange={event => setForce(event.currentTarget.checked)} />本次强制覆盖</label>
          <button className="react-primary-button" type="button" disabled={tasks.mutationBusy || count === 0 || dirty} onClick={() => void tasks.scrape.run({ library_id: libraryId, only_missing: onlyMissing, overwrite: force })}><Sparkles size={14} /><span>开始刮削</span></button>
        </div>
        <BackgroundProgress kind="scrape" />
      </section>
    </>}
  </div>
}

interface ScheduleDraft { name: string; type: string; cron: string; library: number; status: string; onlyMissing: boolean; overwrite: string; limit: number; enabled: boolean }
const emptySchedule: ScheduleDraft = { name: '', type: 'scan', cron: '', library: 0, status: 'success', onlyMissing: true, overwrite: '', limit: 0, enabled: true }
const cronPresets = [['每小时', '0 * * * *'], ['每天 03:00', '0 3 * * *'], ['每周一 04:00', '0 4 * * 1'], ['每月 1 日 05:00', '0 5 1 * *']] as const

function scheduleDraft(task?: ScheduledTask): ScheduleDraft {
  let params: Record<string, unknown> = {}
  try { const parsed: unknown = JSON.parse(task?.params || '{}'); if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) params = parsed as Record<string, unknown> } catch { /* use defaults for legacy task parameters */ }
  return { name: task?.name ?? '', type: task?.type ?? 'scan', cron: task?.cron ?? '', library: typeof params.library_id === 'number' ? params.library_id : 0,
    status: typeof params.status === 'string' ? params.status : 'success', onlyMissing: params.only_missing !== false,
    overwrite: typeof params.overwrite === 'boolean' ? String(params.overwrite) : '', limit: typeof params.limit === 'number' ? params.limit : 0, enabled: task?.enabled ?? true }
}

export function ScheduledPage() {
  const read = useAdminRead(async signal => {
    const [scheduled, libraries] = await Promise.all([scheduledApi.list(signal), adminApi.libraries(signal)])
    return { ...scheduled, libraries: libraries.items ?? [] }
  })
  const toast = useAppRuntimeStore(state => state.showToast)
  const tasks = useReactTasksStore()
  const revision = useReactTasksStore(state => state.revision)
  const initialRevision = useRef(revision)
  const editingId = useReactScheduledStore(state => state.editingId)
  const setEditingId = useReactScheduledStore(state => state.setEditingId)
  const [draft, setDraft] = useState<ScheduleDraft>(emptySchedule)
  const [busyKey, setBusyKey] = useState('')
  const [cronStatus, setCronStatus] = useState('标准 5 段（分 时 日 月 周）或 @daily/@hourly 等描述符。')
  const [cronInvalid, setCronInvalid] = useState(false)
  const editing = read.data?.items.find(task => task.id === editingId) ?? null
  const editingRef = useRef<number | null>(null)
  useEffect(() => {
    if (revision === initialRevision.current) return
    initialRevision.current = revision
    void read.load()
  }, [read.load, revision])
  useEffect(() => { if (editingId !== editingRef.current) { editingRef.current = editingId; setDraft(scheduleDraft(editing ?? undefined)) } }, [editingId, editing])
  useEffect(() => {
    const expression = draft.cron.trim()
    if (!expression) { setCronStatus('请输入 cron 表达式。'); setCronInvalid(false); return }
    const controller = new AbortController()
    const timer = setTimeout(() => {
      setCronStatus('正在校验表达式…')
      void scheduledApi.validate(expression, controller.signal).then(result => {
        if (controller.signal.aborted) return
        setCronInvalid(!result.valid)
        setCronStatus(result.valid ? `接下来执行：${result.next_runs.map(fmtTime).join(' · ')}` : `表达式非法：${result.error}`)
      }).catch(cause => { if (!controller.signal.aborted) { setCronInvalid(true); setCronStatus(cause instanceof Error ? cause.message : '校验失败') } })
    }, 300)
    return () => { clearTimeout(timer); controller.abort() }
  }, [draft.cron])
  const running = read.data?.items.some(task => task.last_status === 'running') ?? false
  useEffect(() => {
    if (!running) return
    const timer = setInterval(() => void read.load(), 1500)
    return () => clearInterval(timer)
  }, [running, read.load])

  function edit(task?: ScheduledTask) {
    setEditingId(task?.id ?? null)
    setDraft(scheduleDraft(task))
  }
  function patchDraft<K extends keyof ScheduleDraft>(key: K, value: ScheduleDraft[K]) {
    setDraft(current => ({ ...current, [key]: value }))
  }
  function buildPayload(): ScheduledPayload {
    const params: Record<string, unknown> = {}
    if (draft.type !== 'reindex') params.library_id = Number(draft.library) || 0
    if (draft.type === 'probe') { params.status = draft.status; params.only_missing = draft.onlyMissing }
    if (draft.type === 'scrape') { params.only_missing = draft.onlyMissing; if (draft.overwrite !== '') params.overwrite = draft.overwrite === 'true' }
    if (['probe', 'scrape', 'scrape_avatars'].includes(draft.type) && draft.limit > 0) params.limit = Number(draft.limit)
    return { name: draft.name.trim(), type: draft.type, cron: draft.cron.trim(), enabled: draft.enabled, params }
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busyKey || cronInvalid) return
    setBusyKey('save')
    try {
      await scheduledApi.save(editingId, buildPayload())
      toast(editingId ? '计划任务已保存' : `计划任务「${draft.name.trim()}」已创建`)
      setEditingId(null); setDraft(emptySchedule); await read.load()
    } catch (cause) { toast(cause instanceof Error ? cause.message : '保存计划任务失败', 'error') }
    finally { setBusyKey('') }
  }
  async function action(task: ScheduledTask, kind: 'run' | 'toggle' | 'delete' | 'edit') {
    if (kind === 'edit') { edit(task); return }
    if (busyKey) return
    if (kind === 'delete' && !window.confirm(`删除计划任务「${task.name}」？已产生的影片数据不受影响。`)) return
    setBusyKey(`${kind}:${task.id}`)
    try {
      await scheduledApi.action(task.id, kind)
      if (kind === 'run') { toast('已触发执行，可在「任务」页查看进度'); void tasks.restoreAll() }
      else if (kind === 'delete') { if (editingId === task.id) setEditingId(null); toast('计划任务已删除') }
      else toast(`计划任务已${task.enabled ? '启用' : '禁用'}`)
      await read.load()
    } catch (cause) { toast(cause instanceof Error ? cause.message : '计划任务操作失败', 'error'); if (read.active()) await read.load() }
    finally { setBusyKey('') }
  }
  const types = read.data?.types ?? {}
  return <div>
    <PageState loading={read.loading && !read.data} error={read.error} empty={false} retry={() => void read.load()} label="计划任务" />
    <section className="react-page-panel"><PageHeading title={editing ? `编辑计划任务 · ${editing.name}` : '新建计划任务'} description="任务到点后自动执行并立即生效；上轮未结束时会跳过下一次触发。" action={editing ? <button className="react-detail-action" type="button" disabled={!!busyKey} onClick={() => edit()}><RotateCcw size={14} />取消编辑</button> : undefined} />
      {read.data && <form className="react-page-form" onSubmit={event => void submit(event)}>
        <label className="react-page-field">名称<input value={draft.name} required maxLength={120} disabled={!!busyKey} placeholder="如：夜间扫库" onChange={event => patchDraft('name', event.currentTarget.value)} /></label>
        <label className="react-page-field">任务类型<select value={draft.type} disabled={!!busyKey} onChange={event => patchDraft('type', event.currentTarget.value)}>{Object.entries(types).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select></label>
        <label className="react-page-field">cron 表达式<input className="react-page-mono" value={draft.cron} required disabled={!!busyKey} placeholder="0 3 * * *" onChange={event => patchDraft('cron', event.currentTarget.value)} /></label>
        {draft.type !== 'reindex' && <label className="react-page-field">媒体库<select value={draft.library} disabled={!!busyKey} onChange={event => patchDraft('library', Number(event.currentTarget.value))}><option value={0}>全部媒体库</option>{read.data.libraries.filter(library => library.Enabled !== false).map(library => <option key={library.Id} value={library.Id}>{library.Name}</option>)}</select></label>}
        {draft.type === 'probe' && <label className="react-page-field">影片状态<select value={draft.status} disabled={!!busyKey} onChange={event => patchDraft('status', event.currentTarget.value)}><option value="success">已入库</option><option value="manual">手动录入</option><option value="">全部</option></select></label>}
        {['probe', 'scrape'].includes(draft.type) && <label className="react-page-check"><input type="checkbox" checked={draft.onlyMissing} disabled={!!busyKey} onChange={event => patchDraft('onlyMissing', event.currentTarget.checked)} />仅处理缺失元数据 / 未探测项目</label>}
        {draft.type === 'scrape' && <label className="react-page-field">覆盖策略<select value={draft.overwrite} disabled={!!busyKey} onChange={event => patchDraft('overwrite', event.currentTarget.value)}><option value="">使用刮削默认值</option><option value="false">只补缺失</option><option value="true">强制覆盖</option></select></label>}
        {['probe', 'scrape', 'scrape_avatars'].includes(draft.type) && <label className="react-page-field">处理上限（0 = 不限制）<input type="number" min={0} step={1} value={draft.limit} disabled={!!busyKey} onChange={event => patchDraft('limit', Number(event.currentTarget.value))} /></label>}
        <label className="react-page-check"><input type="checkbox" checked={draft.enabled} disabled={!!busyKey} onChange={event => patchDraft('enabled', event.currentTarget.checked)} />启用计划任务</label>
        <div className="react-page-field full"><span>常用预设</span><div className="react-page-actions">{cronPresets.map(([label, cron]) => <button className="react-detail-action" key={cron} type="button" disabled={!!busyKey} onClick={() => setDraft(current => ({ ...current, cron }))}>{label}</button>)}</div></div>
        <p className={`react-page-banner react-page-field full${cronInvalid ? ' danger' : ''}`} role="status">{cronStatus}</p>
        <div className="react-page-footer"><button className="react-primary-button" disabled={!!busyKey || cronInvalid || !draft.cron.trim()}><Save size={14} /><span>{busyKey === 'save' ? '正在保存…' : editing ? '保存修改' : '创建计划任务'}</span></button></div>
      </form>}
    </section>
    <section className="react-page-panel"><PageHeading title="已配置任务" description={`共 ${read.data?.total ?? 0} 个`} action={<button className="react-icon-button" type="button" title="刷新任务" aria-label="刷新任务" disabled={read.loading} onClick={() => void read.load()}><RefreshCw size={15} /></button>} />
      <PageState loading={read.loading && !read.data} error={read.error} empty={Boolean(read.data && !read.data.items.length)} retry={() => void read.load()} label="计划任务" />
      {read.data?.items.map(task => <article className="react-scheduled-row" data-scheduled={task.id} key={task.id}>
        <div><strong>{task.name}</strong><span>{taskTypes[task.type] || task.type} · <code>{task.cron}</code></span><small>{task.enabled ? `下次：${fmtTime(task.next_run)}` : '已禁用'}{task.last_status ? ` · ${runStatuses[task.last_status] || task.last_status}` : ''}</small>
          {task.last_run_at && <small>{fmtTime(task.last_run_at)}{task.last_message ? ` · ${task.last_message}` : ''}</small>}</div>
        <label className="react-page-check"><input type="checkbox" checked={task.enabled} disabled={busyKey === `toggle:${task.id}`} aria-label={`启停 ${task.name}`} onChange={() => void action(task, 'toggle')} />启用</label>
        <div className="react-page-actions"><button className="react-detail-action" type="button" disabled={busyKey === `run:${task.id}` || task.last_status === 'running'} onClick={() => void action(task, 'run')}>执行</button><button className="react-detail-action" type="button" disabled={!!busyKey} onClick={() => void action(task, 'edit')}>编辑</button><button className="react-icon-button danger" type="button" title="删除任务" aria-label={`删除 ${task.name}`} disabled={busyKey === `delete:${task.id}`} onClick={() => void action(task, 'delete')}><Trash2 size={14} /></button></div>
      </article>)}
    </section>
  </div>
}
