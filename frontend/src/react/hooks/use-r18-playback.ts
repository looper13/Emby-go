import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { cloud115Api, type CloudProvider, type CloudSource, type OfflineApi, type OfflineState, type OfflineTask } from '../../api/cloud115'
import { guangyaApi } from '../../api/guangya'
import type { R18Resource } from '../../api/r18'
import { mainVideo, magnetHash, offlineActive, offlineStatusLabel, playbackVersions, type R18PlaybackVersion } from '../../lib/r18-playback'
import { offlineProgressVisible, offlineStageMessage } from '../../lib/offline-stage'
import { playbackType } from '../../lib/player'
import { useReactAuthStore } from '../auth-store'
import { useReactPlayerStore } from '../player-store'
import { usePageRequest } from './use-page-request'

const initialState: OfflineState = { configured: false, tasks: [], sources: [] }

export function useR18Playback(library: string, video: string, title: string, resources: R18Resource[], enabled = true) {
  const [provider, setProvider] = useState<CloudProvider>('guangya')
  const [state, setState] = useState<OfflineState>(initialState)
  const [loaded, setLoaded] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [waiting, setWaiting] = useState('')
  const [selections, setSelections] = useState<Record<string, string>>({})
  const [manualMagnet, setManualMagnet] = useState('')
  const scope = usePageRequest()
  const administrator = useReactAuthStore(current => current.user?.Policy.IsAdministrator === true)
  const openPlayer = useReactPlayerStore(current => current.open)
  const api: OfflineApi = provider === '115' ? cloud115Api : guangyaApi
  const providerName = provider === '115' ? '115' : '光鸭'
  const versions = useMemo(() => playbackVersions(resources, state, provider), [provider, resources, state])
  const identity = `${library}:${video}:${provider}:${enabled ? 'enabled' : 'disabled'}`
  const identityRef = useRef(identity)
  identityRef.current = identity
  const waitingRef = useRef(waiting)
  waitingRef.current = waiting
  const selectedVersion = versions.find(version => version.id === waiting)
  const message = waiting ? [
    offlineStatusLabel(selectedVersion?.task?.status || 'queued', providerName),
    selectedVersion?.task && offlineProgressVisible(selectedVersion.task) ? `${selectedVersion.task.progress}%` : '',
    '就绪后自动播放',
  ].filter(Boolean).join(' · ') : ''

  const accept = useCallback((next: OfflineState) => {
    setState(next)
    setLoaded(true)
    setSelections(current => {
      const selectionsNext = { ...current }
      for (const task of next.tasks) {
        if (!task.files.some(file => file.id === selectionsNext[task.id])) {
          selectionsNext[task.id] = task.file_id || mainVideo(task.files)?.id || ''
        }
      }
      return selectionsNext
    })
  }, [])

  const openSource = useCallback((source: CloudSource) => {
    setWaiting('')
    openPlayer({ title, url: source.DirectStreamUrl, type: playbackType(source.Container) })
  }, [openPlayer, title])

  const advance = useCallback(async (next: OfflineState, requestIdentity: string) => {
    const versionID = waitingRef.current
    if (!versionID || identityRef.current !== requestIdentity) return
    const currentVersions = playbackVersions(resources, next, provider)
    const version = currentVersions.find(item => item.id === versionID)
    if (!version) return
    if (version.source) {
      const ready = await api.prepare(library, video, version.source.Id)
      if (identityRef.current !== requestIdentity || waitingRef.current !== versionID) return
      accept(ready)
      const source = ready.sources.find(item => item.Id === version.source!.Id) ?? ready.sources.find(item => item.Default)
      if (source) openSource(source)
      return
    }
    const task = version.task
    if (!task) return
    if (['waiting_selection', 'ready'].includes(task.status) && task.files.length && administrator) {
      const file = task.files.find(item => item.id === task.file_id) ?? mainVideo(task.files)
      if (!file) return
      setSelections(current => ({ ...current, [task.id]: file.id }))
      const ready = await api.select(library, video, task.id, file.id, undefined, false, true)
      if (identityRef.current !== requestIdentity || waitingRef.current !== versionID) return
      accept(ready)
      const selectedSource = ready.sources.find(item => item.Default) ?? ready.sources[0]
      if (selectedSource) openSource(selectedSource)
      else if (!ready.tasks.some(item => offlineActive(item))) { setWaiting(''); setError('播放源尚未就绪，请刷新状态后重试') }
      return
    }
    if (!offlineActive(task)) {
      setWaiting('')
      setError(task.error || `${offlineStatusLabel(task.status, providerName)}，请处理任务后重试`)
    }
  }, [accept, administrator, api, library, openSource, provider, providerName, resources, video])

  const load = useCallback(async () => {
    if (!enabled) return
    const request = scope.begin()
    const requestIdentity = identity
    setBusy(true)
    setError('')
    try {
      const next = await api.state(library, video, request.signal)
      if (!request.isCurrent() || identityRef.current !== requestIdentity) return
      accept(next)
      await advance(next, requestIdentity)
    } catch (cause) {
      if (request.isCurrent()) setError(cause instanceof Error ? cause.message : `${providerName} 任务状态读取失败`)
    } finally { if (request.isCurrent()) setBusy(false) }
  }, [accept, advance, api, enabled, identity, library, providerName, scope, video])
  const loadRef = useRef(load)
  loadRef.current = load

  useEffect(() => {
    setState(initialState); setLoaded(false); setError(''); setWaiting(''); setSelections({})
    setBusy(false)
    if (!enabled) { scope.cancel(); return }
    void loadRef.current()
    return () => scope.cancel()
  }, [enabled, identity, scope])

  useEffect(() => {
    if (!enabled) return
    const needsPoll = waiting || state.tasks.some(task => offlineActive(task) || task.status === 'cleanup_pending' || Boolean(task.expires_at && task.status !== 'expired'))
    if (!needsPoll || busy) return
    const timer = window.setTimeout(() => { if (!document.hidden) void load() }, waiting ? 1200 : 5000)
    return () => window.clearTimeout(timer)
  }, [busy, enabled, load, state.tasks, waiting])

  const play = useCallback(async (version: R18PlaybackVersion) => {
    if (!enabled || busy || version.provider && version.provider !== provider) return
    const requestIdentity = identity
    setBusy(true); setError(''); setWaiting(version.id); setManualMagnet('')
    try {
      const latest = await api.state(library, video)
      if (identityRef.current !== requestIdentity) return
      accept(latest)
      const currentVersion = playbackVersions(resources, latest, provider).find(item => item.id === version.id) ?? version
      if (currentVersion.source) {
        const ready = await api.prepare(library, video, currentVersion.source.Id)
        if (identityRef.current !== requestIdentity) return
        accept(ready)
        const source = ready.sources.find(item => item.Id === currentVersion.source!.Id) ?? ready.sources.find(item => item.Default)
        if (source) { openSource(source); return }
      }
      if (!administrator) throw new Error('此版本尚未就绪，请管理员先准备播放源')
      if (!latest.configured) throw new Error(`请先在设置中配置 ${providerName} 连接`)
      if (!currentVersion.task || currentVersion.task.status === 'expired') {
        const result = currentVersion.resourceId
          ? await api.submit(library, video, { resource_id: currentVersion.resourceId })
          : currentVersion.magnet ? await api.submit(library, video, { magnet: currentVersion.magnet }) : latest
        if (identityRef.current === requestIdentity) accept(result)
      } else if (currentVersion.task.retryable && !offlineActive(currentVersion.task)
        && !['ready', 'waiting_selection', 'cleanup_pending'].includes(currentVersion.task.status)) {
        const result = await api.change(library, video, currentVersion.task, 'retry')
        if (identityRef.current === requestIdentity) accept(result)
      }
      if (identityRef.current === requestIdentity) await load()
    } catch (cause) {
      if (identityRef.current === requestIdentity) { setWaiting(''); setError(cause instanceof Error ? cause.message : `${providerName} 操作失败`) }
    } finally { if (identityRef.current === requestIdentity) setBusy(false) }
  }, [accept, administrator, api, busy, enabled, identity, library, load, openSource, provider, providerName, resources, video])

  const selectFile = useCallback(async (task: OfflineTask) => {
    if (!enabled) return
    const fileId = selections[task.id]
    if (!fileId || busy) return
    const requestIdentity = identity
    setBusy(true); setError('')
    try {
      const next = await api.select(library, video, task.id, fileId, undefined, true, waitingRef.current !== '')
      if (identityRef.current !== requestIdentity) return
      accept(next)
      if (waitingRef.current) await advance(next, requestIdentity)
    } catch (cause) { if (identityRef.current === requestIdentity) setError(cause instanceof Error ? cause.message : '选择播放文件失败') }
    finally { if (identityRef.current === requestIdentity) setBusy(false) }
  }, [accept, advance, api, busy, enabled, identity, library, selections, video])

  const changeTask = useCallback(async (task: OfflineTask, action: 'retry' | 'cancel') => {
    if (!enabled || busy) return
    if (action === 'cancel' && selectedVersion?.task?.id === task.id) setWaiting('')
    const requestIdentity = identity
    setBusy(true); setError('')
    try {
      const next = await api.change(library, video, task, action)
      if (identityRef.current === requestIdentity) accept(next)
    } catch (cause) { if (identityRef.current === requestIdentity) setError(cause instanceof Error ? cause.message : '任务操作失败') }
    finally { if (identityRef.current === requestIdentity) setBusy(false) }
  }, [accept, api, busy, enabled, identity, library, selectedVersion?.task?.id, video])

  const addManualMagnet = useCallback(async () => {
    if (!enabled || !administrator || !state.configured || busy) return
    const magnet = manualMagnet.trim()
    const id = magnetHash(magnet)
    if (!id) { setError('请输入有效的 BTIH 磁力链接'); return }
    setBusy(true); setError('')
    const requestIdentity = identity
    try {
      const next = await api.submit(library, video, { magnet })
      if (identityRef.current === requestIdentity) { accept(next); setWaiting(id); setManualMagnet('') }
    } catch (cause) { if (identityRef.current === requestIdentity) setError(cause instanceof Error ? cause.message : '提交磁力失败') }
    finally { if (identityRef.current === requestIdentity) setBusy(false) }
  }, [accept, administrator, api, busy, enabled, identity, library, manualMagnet, state.configured, video])

  return {
    provider, setProvider, providerName, state, loaded, busy, error, waiting, message, versions, selections, enabled,
    setSelection: (task: string, file: string) => setSelections(current => ({ ...current, [task]: file })),
    manualMagnet, setManualMagnet, load, play, selectFile, changeTask, addManualMagnet,
    stopWaiting: () => setWaiting(''),
  }
}

export { offlineActive, offlineProgressVisible, offlineStageMessage, offlineStatusLabel, mainVideo }
