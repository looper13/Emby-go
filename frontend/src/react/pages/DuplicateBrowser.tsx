import { RefreshCw, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent, type MouseEvent } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { ApiError } from '../../api/client'
import type { LibraryDto } from '../../api/contracts'
import {
  duplicateRuleLabels,
  duplicatesApi,
  type DuplicateDeletePreview,
  type DuplicateGroup,
  type DuplicateMovie,
  type DuplicateSelection,
  type DuplicateSelectionItem,
  type DuplicateStrmContents,
} from '../../api/duplicates'
import { usePageRequest } from '../hooks/use-page-request'

const GROUP_LIMIT = 20
const MEMBER_LIMIT = 20
const BULK_PAGE_SIZE = 20

function statusLabel(status: string) {
  return ({ success: '已入库', manual: '手动录入', pending: '待补元数据', incompatible: '不兼容' } as Record<string, string>)[status] || status
}

function displayDate(value: string) {
  return value && !Number.isNaN(Date.parse(value)) ? new Date(value).toLocaleString() : '未知'
}

function StrmContentViewer({ movie, libraryId, groupKey }: { movie: DuplicateMovie; libraryId: number; groupKey: string }) {
  const scope = usePageRequest()
  const [result, setResult] = useState<DuplicateStrmContents | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const files = [movie.source_path, ...movie.additional_parts].map(localPath => ({
    localPath,
    loaded: result?.files.find(file => file.local_path === localPath),
  }))

  const load = useCallback(async () => {
    const request = scope.begin()
    setResult(null)
    setLoading(true)
    setError('')
    try {
      const contents = await duplicatesApi.strm(movie.id, libraryId, groupKey, request.signal)
      if (request.isCurrent()) setResult(contents)
    } catch (cause) {
      if (request.isCurrent()) {
        setError(cause instanceof Error || cause instanceof DOMException
          ? cause.name === 'TimeoutError' ? '读取 STRM 内容超时，请检查媒体库目录后重试。' : cause.message
          : '读取 STRM 内容失败')
      }
    } finally {
      if (request.isCurrent()) setLoading(false)
    }
  }, [groupKey, libraryId, movie.id, scope])

  useEffect(() => {
    void load()
    return () => scope.cancel()
  }, [load, scope])

  return <div className="react-duplicate-strm" aria-busy={loading}>
    {loading && <p className="react-page-muted" role="status">正在读取 STRM 文件内容…</p>}
    {error && <p className="react-page-banner danger" role="alert">{error} <button className="react-detail-action" type="button" onClick={() => void load()}>重试读取</button></p>}
    {files.map(({ localPath, loaded }, index) => <div className="react-duplicate-strm-file" key={localPath}>
      <p className="react-duplicate-path" data-private-path>{index ? '分段：' : ''}{localPath}</p>
      {loaded && (loaded.error
        ? <p className="react-page-muted react-duplicate-strm-error">{loaded.error}</p>
        : <>
          <p className="react-page-muted react-duplicate-strm-label">STRM 内容</p>
          {loaded.content
            ? <pre className="react-duplicate-strm-content" data-private-path>{loaded.content}</pre>
            : <p className="react-page-muted">STRM 文件为空。</p>}
        </>)}
    </div>)}
  </div>
}

type DeleteResult = { status: 'success' | 'failed'; error: string; removed: string[] }

function DuplicateBulkDeleteDialog({ selection, libraryName, onClose }: {
  selection: DuplicateSelection
  libraryName: string
  onClose: (completed: number) => void
}) {
  const scope = usePageRequest()
  const shell = useRef<HTMLDivElement>(null)
  const list = useRef<HTMLDivElement>(null)
  const closeButton = useRef<HTMLButtonElement>(null)
  const active = useRef(true)
  const runningRef = useRef(false)
  const stopRequestedRef = useRef(false)
  const phaseRef = useRef<'idle' | 'preview' | 'delete'>('idle')
  const completedRef = useRef(0)
  const onCloseRef = useRef(onClose)
  const [running, setRunning] = useState(false)
  const [confirmed, setConfirmed] = useState(false)
  const [offset, setOffset] = useState(0)
  const [onlyFailed, setOnlyFailed] = useState(false)
  const [phase, setPhase] = useState<'idle' | 'preview' | 'delete'>('idle')
  const [stopRequested, setStopRequested] = useState(false)
  const [preview, setPreview] = useState<DuplicateDeletePreview | null>(null)
  const [current, setCurrent] = useState<DuplicateSelectionItem | null>(null)
  const [results, setResults] = useState<Map<number, DeleteResult>>(() => new Map())

  const pending = useMemo(() => selection.items.filter(item => !results.has(item.movie.id)), [results, selection.items])
  const failed = useMemo(() => selection.items.filter(item => results.get(item.movie.id)?.status === 'failed'), [results, selection.items])
  const completed = results.size - failed.length
  const listed = onlyFailed ? failed : selection.items
  const page = listed.slice(offset, offset + BULK_PAGE_SIZE)
  runningRef.current = running
  stopRequestedRef.current = stopRequested
  phaseRef.current = phase
  completedRef.current = completed
  onCloseRef.current = onClose

  useEffect(() => {
    active.current = true
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    closeButton.current?.focus()
    function keyboard(event: globalThis.KeyboardEvent) {
      if (event.key === 'Escape') {
        event.preventDefault()
        event.stopImmediatePropagation()
        if (!runningRef.current) onCloseRef.current(completedRef.current)
        return
      }
      if (event.key !== 'Tab' || !shell.current) return
      const focusable = Array.from(shell.current.querySelectorAll<HTMLElement>(
        'button:not([disabled]), input:not([disabled]), select:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])',
      )).filter(element => element.tabIndex >= 0)
      const first = focusable[0] ?? closeButton.current
      const last = focusable.at(-1) ?? first
      if (event.shiftKey && (document.activeElement === first || !shell.current.contains(document.activeElement))) {
        event.preventDefault()
        last?.focus()
      } else if (!event.shiftKey && (document.activeElement === last || !shell.current.contains(document.activeElement))) {
        event.preventDefault()
        first?.focus()
      }
    }
    window.addEventListener('keydown', keyboard, true)
    return () => {
      active.current = false
      scope.cancel()
      document.body.style.overflow = previousOverflow
      window.removeEventListener('keydown', keyboard, true)
      if (previousFocus?.isConnected) previousFocus.focus()
    }
  }, [scope])

  useEffect(() => {
    const lastOffset = Math.max(0, Math.ceil(listed.length / BULK_PAGE_SIZE) - 1) * BULK_PAGE_SIZE
    setOffset(currentOffset => Math.min(currentOffset, lastOffset))
  }, [listed.length])

  useEffect(() => {
    if (list.current) list.current.scrollTop = 0
  }, [offset, onlyFailed])

  function closeFromBackdrop(event: MouseEvent<HTMLDivElement>) {
    if (event.target === event.currentTarget && !runningRef.current) onCloseRef.current(completedRef.current)
  }

  function handleDialogKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      if (!runningRef.current) onCloseRef.current(completedRef.current)
    }
  }

  function stop() {
    stopRequestedRef.current = true
    setStopRequested(true)
    if (phaseRef.current === 'preview') scope.cancel()
  }

  async function run() {
    if (runningRef.current || !confirmed) return
    const queue = pending.length ? pending : failed
    if (!queue.length) return
    runningRef.current = true
    stopRequestedRef.current = false
    setRunning(true)
    setConfirmed(false)
    setStopRequested(false)
    for (const item of queue) {
      if (!active.current || stopRequestedRef.current) break
      setCurrent(item)
      const proof = { keep_id: item.keep.id, selection_token: item.selection_token }
      try {
        setPhase('preview')
        phaseRef.current = 'preview'
        setPreview(null)
        const request = scope.begin()
        const checked = await duplicatesApi.previewDelete(item.movie.id, selection.library_id, item.key, request.signal, proof)
        if (!active.current || stopRequestedRef.current || !request.isCurrent()) break
        setPreview(checked)
        setPhase('delete')
        phaseRef.current = 'delete'
        await duplicatesApi.deleteCopy(item.movie.id, selection.library_id, item.key, checked, proof)
        if (!active.current) break
        setResults(previous => new Map(previous).set(item.movie.id, { status: 'success', error: '', removed: [] }))
      } catch (cause) {
        if (!active.current || (stopRequestedRef.current && phaseRef.current === 'preview')) break
        const message = cause instanceof Error || cause instanceof DOMException
          ? cause.name === 'TimeoutError' ? '当前副本核验超时，请检查 STRM 与 AList 后重试。' : cause.message || '副本删除失败'
          : '副本删除失败'
        const removed = cause instanceof ApiError && cause.payload && typeof cause.payload === 'object'
          && 'removed_remote_paths' in cause.payload && Array.isArray(cause.payload.removed_remote_paths)
          ? cause.payload.removed_remote_paths.filter((value): value is string => typeof value === 'string')
          : []
        setResults(previous => new Map(previous).set(item.movie.id, { status: 'failed', error: message, removed }))
      } finally {
        if (active.current) { setCurrent(null); setPreview(null) }
      }
    }
    if (active.current) { runningRef.current = false; phaseRef.current = 'idle'; setRunning(false); setPhase('idle'); setConfirmed(false) }
  }

  const dialog = <div className="react-bulk-overlay" onMouseDown={closeFromBackdrop}>
    <div ref={shell} className="react-bulk-dialog" role="dialog" aria-modal="true" aria-labelledby="duplicate-bulk-title" onKeyDown={handleDialogKeyDown}>
      <div className="react-bulk-head"><h3 id="duplicate-bulk-title">批量删除多余副本</h3><button ref={closeButton} className="react-detail-action" type="button" disabled={running} onClick={() => onClose(completed)}>{!pending.length ? '关闭' : '取消 / 关闭'}</button></div>
      <p><strong>{libraryName}</strong> · {duplicateRuleLabels[selection.rule]}一份 · {selection.groups} 组 · 全量选择 {selection.total} 份</p>
      <p className="react-page-muted">待删清单覆盖全部结果页，只删除本次选中的副本。每组指定保留一份，执行时再次检查保留副本的索引和路径未变；不会读取保留副本的 STRM。加入时间是首次入库时间，路径长度按主文件完整路径的字符数计算；相同值保留 ID 较小的一份，未知时间排在有效时间后。</p>
      <p className="react-page-banner danger">将通过 AList 删除远程原文件，再清理本地 STRM（含 CD 分段）和影片索引，保留 NFO、封面及其他文件。请核对下方待删/保留路径，确认保留副本可播放。网盘删除不可跨系统回滚。</p>
      <p role="status">已处理 {results.size} / {selection.total} 份（成功 {completed}，失败 {failed.length}），未处理 {pending.length} 份{running ? `，正在${phase === 'preview' ? '核验' : '删除'}当前副本` : !pending.length ? failed.length ? '，本轮处理结束，可仅重试失败项' : '，批量删除完成' : results.size || stopRequested ? '，队列已停止，可确认后继续' : ''}</p>
      {running && current && <>
        <p className="react-duplicate-path react-page-mono" data-private-path>{current.movie.source_path}</p>
        {preview?.files.map(file => <p key={file.local_path} className="react-duplicate-path react-page-mono" data-private-path>远程：{file.remote_path}</p>)}
        <button className="react-detail-action" type="button" disabled={stopRequested} onClick={stop}>{stopRequested ? '正在停止，当前删除完成后停下' : '停止队列'}</button>
        <p className="react-page-muted">每次只处理一个副本。停止或离开页面不会继续下一份；已发出的删除会完成当前副本的清理记账。</p>
      </>}
      {failed.length > 0 && <p className="react-page-banner danger" role="alert">{failed.length} 份副本处理失败，已记录原因{running ? '，继续处理本轮后续副本' : '；已成功项不会再次处理'}。失败项不计为删除成功，保留尚未删除的本地 STRM 与索引。网络响应丢失时请先重新查重确认当前状态；保留副本或路径变化需关闭后重新全量选择。</p>}
      {(failed.length > 0 || onlyFailed) && <div className="react-page-toolbar"><button id="duplicate-bulk-filter-failed" className="react-detail-action" type="button" aria-pressed={onlyFailed} onClick={() => { setOnlyFailed(value => !value); setOffset(0) }}>{onlyFailed ? '查看全部清单' : `只看失败 ${failed.length} 份`}</button></div>}
      <div ref={list} className="react-bulk-list" tabIndex={0} aria-label="全量待删及保留副本清单">
        {!page.length && <p className="react-page-muted">暂无失败副本。</p>}
        {page.map(item => {
          const result = results.get(item.movie.id)
          const isCurrent = current?.movie.id === item.movie.id
          const state = isCurrent ? phase === 'preview' ? '正在核验' : '正在删除' : result?.status === 'success' ? '已成功' : result?.status === 'failed' ? '失败，待重试' : '待删除'
          return <article key={item.movie.id} className="react-bulk-item" data-duplicate-result={item.movie.id}>
            <strong className="react-page-mono">{item.key}</strong><span className="react-page-muted"> · {state}</span>
            <p className="react-duplicate-path"><span className="react-text-danger">删除：</span>{item.movie.library_name} · {displayDate(item.movie.created_at)} · {item.movie.path_length} 字符</p>
            <p className="react-duplicate-path react-page-mono" data-private-path>{item.movie.source_path}</p>
            {item.movie.additional_parts.map(part => <p key={part} className="react-duplicate-path react-page-mono" data-private-path>分段：{part}</p>)}
            <p className="react-duplicate-path">保留：{item.keep.library_name} · {displayDate(item.keep.created_at)} · {item.keep.path_length} 字符</p>
            <p className="react-duplicate-path react-page-mono" data-private-path>{item.keep.source_path}</p>
            {result?.status === 'failed' && <>
              <p className="react-page-banner danger">失败原因：{result.error}</p>
              {result.removed.map(path => <p key={path} className="react-duplicate-path react-page-mono" data-private-path>此副本远程已删除：{path}</p>)}
            </>}
          </article>
        })}
      </div>
      {listed.length > BULK_PAGE_SIZE && <div className="react-page-toolbar react-bulk-pagination"><button className="react-detail-action" type="button" disabled={!offset} onClick={() => setOffset(value => Math.max(0, value - BULK_PAGE_SIZE))}>上一页清单</button><span>{offset + 1}–{Math.min(offset + BULK_PAGE_SIZE, listed.length)} / {listed.length}</span><button className="react-detail-action" type="button" disabled={offset + BULK_PAGE_SIZE >= listed.length} onClick={() => setOffset(value => value + BULK_PAGE_SIZE)}>下一页清单</button></div>}
      {!running && (pending.length > 0 || failed.length > 0) && <>
        <label className="react-page-check react-bulk-confirm"><input id="duplicate-bulk-confirmed" type="checkbox" checked={confirmed} onChange={event => setConfirmed(event.currentTarget.checked)} />我已核对{pending.length ? '待删' : '失败'}清单与保留副本，确认逐项核验并{pending.length ? '批量删除' : '仅重试失败项'}</label>
        <button id="duplicate-bulk-confirm" className="react-primary-button react-bulk-danger" type="button" disabled={!confirmed} onClick={() => void run()}>{!pending.length ? `重新核验并重试失败 ${failed.length} 份` : results.size || stopRequested ? `继续处理未完成 ${pending.length} 份` : '确认批量删除'}</button>
      </>}
    </div>
  </div>

  return createPortal(dialog, document.body)
}

export function DuplicateBrowser({ libraries }: { libraries: LibraryDto[] }) {
  const [libraryId, setLibraryId] = useState(0)
  const [groups, setGroups] = useState<{ items: DuplicateGroup[]; total: number; total_movies: number; extra_copies: number } | null>(null)
  const [groupOffset, setGroupOffset] = useState(0)
  const [selected, setSelected] = useState<DuplicateGroup | null>(null)
  const [members, setMembers] = useState<{ items: DuplicateMovie[]; total: number } | null>(null)
  const [memberOffset, setMemberOffset] = useState(0)
  const [loading, setLoading] = useState(false)
  const [memberLoading, setMemberLoading] = useState(false)
  const [selectionLoading, setSelectionLoading] = useState(false)
  const [error, setError] = useState('')
  const [memberError, setMemberError] = useState('')
  const [selectionError, setSelectionError] = useState('')
  const [notice, setNotice] = useState('')
  const [busyMovie, setBusyMovie] = useState<number | null>(null)
  const [rule, setRule] = useState<DuplicateSelection['rule']>('oldest')
  const [selection, setSelection] = useState<DuplicateSelection | null>(null)
  const [excluded, setExcluded] = useState<Set<number>>(() => new Set())
  const [bulkOpen, setBulkOpen] = useState(false)
  const groupScope = usePageRequest()
  const memberScope = usePageRequest()
  const selectionScope = usePageRequest()
  const retryGroupOffset = useRef(0)
  const retryMemberOffset = useRef(0)
  const deletionScope = usePageRequest()
  const directories = libraries.filter(library => library.SourceType !== 'avdb_javdb')
  const selectionMap = useMemo(() => new Map(selection?.items.map(item => [item.movie.id, item]) ?? []), [selection])
  const keepIds = useMemo(() => new Set(selection?.items.map(item => item.keep.id) ?? []), [selection])
  const batch = useMemo<DuplicateSelection | null>(() => {
    if (!selection) return null
    const items = selection.items.filter(item => !excluded.has(item.movie.id))
    return { ...selection, items, total: items.length, groups: new Set(items.map(item => item.key)).size }
  }, [excluded, selection])

  function resetMembers() {
    memberScope.cancel()
    setSelected(null)
    setMembers(null)
    setMemberLoading(false)
    setMemberError('')
    setMemberOffset(0)
    retryMemberOffset.current = 0
  }

  function resetSelection() {
    selectionScope.cancel()
    setSelection(null)
    setExcluded(new Set())
    setSelectionLoading(false)
    setSelectionError('')
  }

  const loadGroups = useCallback(async (nextOffset = 0) => {
    const request = groupScope.begin()
    resetMembers()
    setLoading(true)
    setError('')
    retryGroupOffset.current = nextOffset
    try {
      const result = await duplicatesApi.groups(libraryId, GROUP_LIMIT, nextOffset, request.signal)
      if (request.isCurrent()) { setGroups(result); setGroupOffset(nextOffset) }
    } catch (cause) {
      if (request.isCurrent()) setError(cause instanceof Error ? cause.message : '查重失败')
    } finally {
      if (request.isCurrent()) setLoading(false)
    }
  }, [groupScope, libraryId])

  async function scan() {
    if (bulkOpen) return
    resetSelection()
    setNotice('')
    await loadGroups()
  }

  async function openGroup(group: DuplicateGroup, nextOffset = 0) {
    const request = memberScope.begin()
    if (selected?.key !== group.key) setMembers(null)
    setSelected(group)
    setMemberLoading(true)
    setMemberError('')
    retryMemberOffset.current = nextOffset
    try {
      const result = await duplicatesApi.members(libraryId, group.key, MEMBER_LIMIT, nextOffset, request.signal)
      if (request.isCurrent()) { setMembers(result); setMemberOffset(nextOffset) }
    } catch (cause) {
      if (request.isCurrent()) setMemberError(cause instanceof Error ? cause.message : '读取影片位置失败')
    } finally {
      if (request.isCurrent()) setMemberLoading(false)
    }
  }

  async function selectAll() {
    const request = selectionScope.begin()
    setSelection(null)
    setExcluded(new Set())
    setSelectionLoading(true)
    setSelectionError('')
    try {
      const result = await duplicatesApi.selection(libraryId, rule, request.signal)
      if (request.isCurrent()) setSelection(result)
    } catch (cause) {
      if (request.isCurrent()) setSelectionError(cause instanceof Error || cause instanceof DOMException ? cause.message : '全量选择失败')
    } finally {
      if (request.isCurrent()) setSelectionLoading(false)
    }
  }

  function toggleSelected(id: number) {
    setExcluded(previous => {
      const next = new Set(previous)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  async function deleteCopy(movie: DuplicateMovie, group: DuplicateGroup) {
    if (busyMovie !== null) return
    setBusyMovie(movie.id)
    setError('')
    setNotice('')
    try {
      const request = deletionScope.begin()
      const preview = await duplicatesApi.previewDelete(movie.id, libraryId, group.key, request.signal)
      if (!request.isCurrent()) return
      const paths = preview.files.map(file => `${file.local_path}${file.remote_path ? ` → ${file.remote_path}` : ''}`).join('\n')
      if (!window.confirm(`删除副本「${movie.title || movie.number}」及远程文件？\n${paths}\n此操作不会删除保留副本。`)) return
      await duplicatesApi.deleteCopy(movie.id, libraryId, group.key, preview)
      setNotice('重复副本已删除；磁盘文件与查重统计已更新。')
      resetSelection()
      await loadGroups()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '删除重复副本失败')
    } finally {
      setBusyMovie(null)
    }
  }

  async function closeBulk(completed: number) {
    setBulkOpen(false)
    if (completed > 0) {
      resetSelection()
      setNotice(`批量删除已完成 ${completed} 份，查重统计已刷新。`)
      await loadGroups()
    }
  }

  useEffect(() => {
    if (libraryId && !directories.some(library => library.Id === libraryId)) {
      groupScope.cancel()
      setLibraryId(0)
      resetSelection()
      resetMembers()
      setGroups(null)
      setGroupOffset(0)
      setLoading(false)
      setError('')
    }
  }, [directories, groupScope, libraryId])

  return <section className="react-page-panel" id="duplicate-movies" aria-busy={loading}>
    <header className="react-page-heading"><div><h2>影片查重</h2><p className="react-page-muted">按番号统计当前媒体索引中的重复影片；先增量扫描以纳入最近的磁盘变化。</p></div></header>
    <div className="react-page-toolbar"><label className="react-page-field">查重范围<select value={libraryId} disabled={bulkOpen} onChange={event => {
      const next = Number(event.currentTarget.value)
      setLibraryId(next)
      groupScope.cancel()
      setGroups(null)
      setGroupOffset(0)
      setError('')
      setLoading(false)
      resetMembers()
      resetSelection()
    }}>
      <option value={0}>全部媒体库（含跨库重复）</option>{directories.map(library => <option key={library.Id} value={library.Id}>{library.Name}</option>)}
    </select></label><button className="react-detail-action" type="button" disabled={loading || !directories.length || bulkOpen} onClick={() => void scan()}><RefreshCw size={14} />{loading ? '正在查重…' : '开始查重'}</button></div>
    <p className="react-page-muted">忽略番号大小写、空格和分隔符差异，保留前导零与版本后缀；空番号和电视节目不参与，CD 分段按一部计算。查看位置会读取本地 STRM 内容，不访问远程链接。删除前会先预览服务器确认的本地及远程路径。</p>
    {notice && <p className="react-page-banner" role="status">{notice}</p>}
    {error && <p className="react-page-banner danger" role="alert">{error} <button className="react-detail-action" type="button" onClick={() => void loadGroups(retryGroupOffset.current)}>重试</button></p>}
    {loading && <p className="react-page-muted" role="status">正在统计所选范围内的重复番号…</p>}
    {groups && !error && <>
      <p className="react-page-muted" role="status">共 {groups.total} 组重复番号，涉及 {groups.total_movies} 部影片，比每个番号一份多出 {groups.extra_copies} 份。</p>
      {!groups.total && <p className="react-page-muted">未发现重复影片。</p>}
      {groups.total > 0 && <div className="react-bulk-controls">
        <label className="react-page-field">每组保留规则<select value={rule} disabled={bulkOpen} onChange={event => { setRule(event.currentTarget.value as DuplicateSelection['rule']); resetSelection() }}>
          {Object.entries(duplicateRuleLabels).map(([value, label]) => <option key={value} value={value}>{label}一份</option>)}
        </select></label>
        <button id="duplicate-select-all" className="react-detail-action" type="button" disabled={loading || selectionLoading || busyMovie !== null || bulkOpen} onClick={() => void selectAll()}>{selectionLoading ? '正在全量选择…' : '全量选择多余副本'}</button>
        <button id="duplicate-bulk-delete" className="react-detail-action danger" type="button" disabled={!batch?.total || selectionLoading || busyMovie !== null || bulkOpen} onClick={() => setBulkOpen(true)}>批量删除 {batch?.total || 0} 份</button>
        {selection && <button className="react-detail-action" type="button" disabled={bulkOpen} onClick={resetSelection}>清空选择</button>}
      </div>}
      {selectionLoading && <p className="react-page-muted" role="status">正在读取当前范围的全部重复组，不限当前页；此操作不会删除文件。</p>}
      {selectionError && <p className="react-page-banner danger" role="alert">{selectionError} <button className="react-detail-action" type="button" onClick={() => void selectAll()}>重试全量选择</button></p>}
      {batch && <p className="react-page-muted" role="status">已全量选中 {batch.total} 份，涉及 {batch.groups} 组；{duplicateRuleLabels[batch.rule]}一份。选择跨页保留，可取消勾选个别待删副本；规则或媒体库范围变化会清空选择。</p>}
      {groups.items.map(group => <article className="react-duplicate-group" key={group.key}>
        <div className="react-duplicate-head"><strong className="react-page-mono">{group.number}</strong><span>{group.count} 份</span><button className="react-detail-action" type="button" disabled={loading || memberLoading && selected?.key === group.key || bulkOpen} aria-expanded={selected?.key === group.key} data-duplicate-key={group.key} onClick={() => selected?.key === group.key ? resetMembers() : void openGroup(group)}>{selected?.key === group.key ? '收起' : '查看位置'}</button></div>
        {selected?.key === group.key && <div className="react-duplicate-members" aria-busy={memberLoading}>
          {memberLoading && <p className="react-page-muted" role="status">正在读取影片位置…</p>}
          {memberError && <p className="react-page-banner danger" role="alert">{memberError} <button className="react-detail-action" type="button" onClick={() => void openGroup(group, retryMemberOffset.current)}>重试</button></p>}
          {members && !memberError && <>
            {members.total < 2 && <p className="react-page-muted">该分组当前不足两部影片，请重新查重以更新统计。</p>}
            {members.items.map(movie => <article className="react-duplicate-movie" key={movie.id}>
              <div className="react-duplicate-info"><div className="react-duplicate-title-row"><Link to={`/item/${movie.id}`}>{movie.title || movie.number}</Link><small>{statusLabel(movie.status)}</small></div><small>{movie.library_name} · {movie.number}</small><code>{movie.source_path}</code>
                {selectionMap.has(movie.id) && <label className="react-duplicate-choice"><input type="checkbox" data-duplicate-selected={movie.id} checked={!excluded.has(movie.id)} disabled={bulkOpen} onChange={() => toggleSelected(movie.id)} />批量待删副本</label>}
                {!selectionMap.has(movie.id) && keepIds.has(movie.id) && <small className="react-page-muted">按规则保留此副本</small>}
                <StrmContentViewer movie={movie} libraryId={libraryId} groupKey={group.key} />
              </div>
              <button className="react-detail-action danger" type="button" disabled={busyMovie !== null || loading || memberLoading || members.total < 2 || bulkOpen} onClick={() => void deleteCopy(movie, group)}><Trash2 size={14} />{busyMovie === movie.id ? '处理中…' : '删除副本'}</button>
            </article>)}
            {members.total > MEMBER_LIMIT && <div className="react-duplicate-pagination"><button className="react-detail-action" type="button" disabled={memberLoading || !memberOffset} onClick={() => void openGroup(group, memberOffset - MEMBER_LIMIT)}>上一页影片</button><span>{memberOffset + 1}–{Math.min(memberOffset + MEMBER_LIMIT, members.total)} / {members.total}</span><button className="react-detail-action" type="button" disabled={memberLoading || memberOffset + MEMBER_LIMIT >= members.total} onClick={() => void openGroup(group, memberOffset + MEMBER_LIMIT)}>下一页影片</button></div>}
          </>}
        </div>}
      </article>)}
      {groups.total > GROUP_LIMIT && <div className="react-duplicate-pagination"><button className="react-detail-action" type="button" disabled={loading || !groupOffset} onClick={() => void loadGroups(groupOffset - GROUP_LIMIT)}>上一页分组</button><span>{groupOffset + 1}–{Math.min(groupOffset + GROUP_LIMIT, groups.total)} / {groups.total}</span><button className="react-detail-action" type="button" disabled={loading || groupOffset + GROUP_LIMIT >= groups.total} onClick={() => void loadGroups(groupOffset + GROUP_LIMIT)}>下一页分组</button></div>}
    </>}
    {!groups && !loading && !error && <p className="react-page-muted">选择范围后点击“开始查重”，可展开每组查看影片所在媒体库、文件路径和 STRM 内容。</p>}
    {bulkOpen && batch && <DuplicateBulkDeleteDialog key="bulk-delete" selection={batch} libraryName={libraryId ? libraries.find(library => library.Id === libraryId)?.Name || '当前媒体库' : '全部媒体库（含跨库重复）'} onClose={completed => void closeBulk(completed)} />}
  </section>
}
