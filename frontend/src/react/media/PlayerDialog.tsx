import { Film, LoaderCircle, X } from 'lucide-react'
import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent } from 'react'
import { apiClient } from '../../api/client'
import { managePlayer, loadPlayerFactory, playbackType, type PlayerOptions } from '../../lib/player'
import {
  offsetVtt, parsePlaybackSource, resolveTrackSelection, selectionMatches, subtitlePath,
  subtitleTrackOptions, supportsNativeAudio, trackLabel, type PlaybackSource, type TrackEngine,
} from '../../lib/player-tracks'
import { useAppRuntimeStore } from '../app-store'
import { useReactAuthStore } from '../auth-store'
import './player-dialog.css'

interface Props {
  options: PlayerOptions & { id: number }
  onClose: () => void
}

function clock(seconds: number) {
  const value = Math.max(0, Math.floor(seconds))
  return `${Math.floor(value / 60)}:${String(value % 60).padStart(2, '0')}`
}

function userPath(suffix: string) {
  const id = useReactAuthStore.getState().user?.Id
  if (!id) throw new Error('登录已失效')
  return `/Users/${encodeURIComponent(id)}/${suffix}`
}

export function PlayerDialog({ options, onClose }: Props) {
  const token = useReactAuthStore((state) => state.token)
  const showToast = useAppRuntimeStore((state) => state.showToast)
  const shell = useRef<HTMLDivElement>(null)
  const stage = useRef<HTMLDivElement>(null)
  const closeButton = useRef<HTMLButtonElement>(null)
  const mountedToken = useRef(token)
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose
  const [trackHost, setTrackHost] = useState<HTMLElement>()
  const [changingAudio, setChangingAudio] = useState(false)
  const changingAudioRef = useRef(false)
  const [trackError, setTrackError] = useState('')
  const [remuxSession, setRemuxSession] = useState('')
  const [duration, setDuration] = useState(0)
  const [position, setPosition] = useState(0)
  const seekRef = useRef<(seek: number) => void>(() => {})

  useEffect(() => {
    const controller = new AbortController()
    let active = true
    let generation = 0
    let session: ReturnType<typeof managePlayer> | undefined
    let source: PlaybackSource | undefined
    let engine: TrackEngine | undefined
    let subtitleRequest: AbortController | undefined
    let subtitleObjectURL: string | undefined
    let subtitleElement: HTMLTrackElement | undefined
    let progressTimer: ReturnType<typeof setInterval> | undefined
    let offset = 0
    let remuxID = ''
    let selectedAudio = ''
    let selectedSubtitle = '-1'
    let initialNativeAudio: number | undefined
    let disableNativeSubtitles = false
    let began = false
    let resumed = false
    const baseOptions = { ...options }

    function clearSubtitle() {
      subtitleElement?.remove()
      subtitleElement = undefined
      if (subtitleObjectURL) URL.revokeObjectURL(subtitleObjectURL)
      subtitleObjectURL = undefined
    }

    function reportProgress(stopped = false) {
      if (useReactAuthStore.getState().token !== mountedToken.current) return
      if (!stopped && remuxID && options.itemId) {
        void apiClient.request(`/Videos/${encodeURIComponent(options.sourceItemId || options.itemId)}/Playback/${encodeURIComponent(remuxID)}`, {
          auth: 'required', method: 'PATCH', readOnly: true,
        }).catch(() => {})
      }
      const video = session?.player.video
      if (!began || !options.itemId || !video || !Number.isFinite(video.currentTime) || video.currentTime <= 0) return
      void apiClient.request(`/Sessions/Playing/${stopped ? 'Stopped' : 'Progress'}`, {
        auth: 'required', method: 'POST', json: {
          ItemId: options.itemId,
          PositionTicks: Math.round((offset + video.currentTime) * 10_000_000),
        },
      }).catch(() => {})
    }

    async function markPlayed() {
      if (!active || useReactAuthStore.getState().token !== mountedToken.current || !options.itemId) return
      try {
        await apiClient.request(userPath(`PlayedItems/${encodeURIComponent(options.itemId)}`), { auth: 'required', method: 'POST' })
      } catch { /* Progress reporting must not interrupt playback. */ }
    }

    function playbackState() {
      const video = session?.player.video
      return video ? { time: video.currentTime, paused: video.paused, muted: video.muted, volume: video.volume, rate: video.playbackRate } : undefined
    }

    async function chooseSubtitle(value: string) {
      subtitleRequest?.abort()
      subtitleRequest = new AbortController()
      const request = subtitleRequest
      try {
        if (value === '-1') {
          selectedSubtitle = value
          engine?.selectSubtitle('-1')
          clearSubtitle()
          return
        }
        const track = source?.MediaStreams.find((item) => `server:${item.Index}` === value && item.Type === 'Subtitle')
        if (track && !track.DeliveryUrl && !track.IsExternal && supportsNativeAudio(source)) {
          const declared = source!.MediaStreams.filter((item) => item.Type === 'Subtitle' && !item.IsExternal)
          const available = engine?.subtitles() ?? []
          const target = available.length === declared.length ? available[declared.findIndex((item) => item.Index === track.Index)] : undefined
          if (!target) throw new Error('浏览器未提供所选字幕轨道')
          if (engine?.subtitleId() !== target.id) engine?.selectSubtitle(target.id)
          selectedSubtitle = value
          return
        }
        const allowed = source ? subtitleTrackOptions(source, supportsNativeAudio(source)) : []
        if (!track?.DeliveryUrl || !allowed.some((item) => `server:${item.value}` === value && !item.disabled)) {
          throw new Error('该字幕暂不支持网页加载')
        }
        const responseText = await apiClient.request(subtitlePath(track.DeliveryUrl), {
          auth: 'required', responseType: 'text', signal: request.signal,
        })
        if (!active || request.signal.aborted || typeof responseText !== 'string') return
        const text = offset > 0 ? offsetVtt(responseText, offset) : responseText
        const objectURL = URL.createObjectURL(new Blob([text], { type: 'text/vtt' }))
        const element = document.createElement('track')
        element.dataset.embySubtitle = 'true'
        element.addEventListener('load', () => {
          const line = Math.max(60, Math.min(80, 100 - 9000 / (session?.player.video?.clientHeight || 360)))
          for (const cue of Array.from(element.track.cues ?? [])) {
            (cue as VTTCue).snapToLines = false
            ;(cue as VTTCue).line = line
          }
        })
        element.kind = 'subtitles'
        element.label = trackLabel(track)
        element.src = objectURL
        engine?.selectSubtitle('-1')
        clearSubtitle()
        subtitleObjectURL = objectURL
        subtitleElement = element
        session?.player.video?.appendChild(element)
        element.track.mode = 'showing'
        selectedSubtitle = value
      } catch (cause) {
        if (!request.signal.aborted && active) setTrackError(cause instanceof Error ? cause.message : '字幕加载失败')
      }
    }

    async function mountPlayback(nextOptions: PlayerOptions, state?: ReturnType<typeof playbackState>) {
      const serial = ++generation
      const factory = await loadPlayerFactory(nextOptions.type)
      if (!active || serial !== generation || !stage.current) return
      subtitleRequest?.abort()
      clearSubtitle()
      setTrackHost(undefined)
      session?.destroy()
      engine = undefined
      session = managePlayer(stage.current, {
        ...nextOptions,
        onTracks(next) {
          if (!active || serial !== generation) return
          engine = { ...next }
          if (!remuxID && initialNativeAudio !== undefined) {
            const tracks = source?.MediaStreams.filter((item) => item.Type === 'Audio' && !item.IsExternal) ?? []
            const target = next.audio().length === tracks.length ? next.audio()[tracks.findIndex((item) => item.Index === initialNativeAudio)] : undefined
            if (target) { initialNativeAudio = undefined; next.selectAudio(target.id) }
          }
          const nativeSubtitles = source?.MediaStreams.filter((item) => item.Type === 'Subtitle' && !item.IsExternal) ?? []
          const subtitleIndex = nativeSubtitles.findIndex((item) => `server:${item.Index}` === selectedSubtitle && !item.DeliveryUrl)
          const subtitle = !remuxID && next.subtitles().length === nativeSubtitles.length ? next.subtitles()[subtitleIndex] : undefined
          if (subtitle) {
            if (next.subtitleId() !== subtitle.id) next.selectSubtitle(subtitle.id)
          } else if (disableNativeSubtitles && next.subtitleId() !== '-1') next.selectSubtitle('-1')
        },
      }, factory, (message) => showToast(message, 'error'))
      const host = stage.current.querySelector<HTMLElement>('.art-video-player') ?? stage.current
      setTrackHost(host)
      let loaded = false
      session.player.on('video:loadedmetadata', () => {
        if (!active || serial !== generation || loaded || !session?.player.video) return
        loaded = true
        const video = session.player.video
        setDuration(Number.isFinite(video.duration) ? video.duration : (source?.RunTimeTicks ?? 0) / 10_000_000)
        if (state) {
          video.currentTime = state.time
          video.muted = state.muted
          video.volume = state.volume
          video.playbackRate = state.rate
          if (state.paused) video.pause()
          else void session.player.play().catch(() => {})
        } else if (!resumed && nextOptions.resumeTicks) {
          video.currentTime = nextOptions.resumeTicks / 10_000_000
          resumed = true
        }
        if (selectedSubtitle !== '-1') void chooseSubtitle(selectedSubtitle)
        if (initialNativeAudio !== undefined) setTrackError('浏览器尚未提供所选音轨；可返回详情页启用 FFmpeg 后重试')
      })
      session.player.on('video:timeupdate', () => {
        if (serial === generation) setPosition(offset + (session?.player.video?.currentTime ?? 0))
      })
      session.player.on('video:playing', () => { began = true })
      session.player.on('video:ended', () => { if (serial === generation) void markPlayed() })
    }

    async function chooseAudio(value: string, seek?: number) {
      if (changingAudioRef.current || !options.itemId || !value.startsWith('server:')) return
      changingAudioRef.current = true
      setTrackError('')
      setChangingAudio(true)
      const state = playbackState()
      const start = seek ?? offset + (state?.time ?? 0)
      try {
        const result = await apiClient.request(`/Videos/${encodeURIComponent(options.sourceItemId || options.itemId)}/Playback`, {
          auth: 'required', method: 'POST', readOnly: true, signal: controller.signal,
          json: { AudioStreamIndex: Number(value.slice(7)), StartTimeTicks: Math.round(start * 10_000_000), PreviousSession: remuxID },
        }) as { SessionId: string; Url: string; StartTimeTicks: number }
        if (!active) return
        remuxID = result.SessionId
        offset = result.StartTimeTicks / 10_000_000
        setRemuxSession(remuxID)
        setPosition(offset)
        await mountPlayback({ ...baseOptions, type: 'm3u8', url: result.Url, proxyURL: undefined, resumeTicks: undefined, hlsStartPosition: 0 }, state ? { ...state, time: 0 } : undefined)
        selectedAudio = value
      } catch (cause) {
        if (active && !controller.signal.aborted) {
          setTrackError(cause instanceof Error ? cause.message : '音轨切换失败')
          if (remuxID) {
            remuxID = ''
            offset = 0
            selectedAudio = `server:${source?.DefaultAudioStreamIndex}`
            setRemuxSession('')
            setPosition(0)
            await mountPlayback(baseOptions, state ? { ...state, time: start } : undefined)
          }
        }
      } finally {
        changingAudioRef.current = false
        if (active) setChangingAudio(false)
      }
    }
    seekRef.current = (seek) => { void chooseAudio(selectedAudio, seek) }

    function keyboard(event: KeyboardEvent) {
      if (document.getElementById('scrape-overlay')) return
      if (event.key === 'Escape') {
        event.preventDefault()
        event.stopImmediatePropagation()
        onCloseRef.current()
        return
      }
      if (event.key !== 'Tab' || !shell.current) return
      const candidates = Array.from(shell.current.querySelectorAll<HTMLElement>(
        'button, a[href], input, select, [tabindex]:not([tabindex="-1"])',
      )).filter((element) => !element.hasAttribute('disabled') && element.getClientRects().length > 0)
      const first = candidates[0] ?? closeButton.current
      const last = candidates.at(-1) ?? first
      if (event.shiftKey && (document.activeElement === first || !shell.current.contains(document.activeElement))) {
        event.preventDefault()
        last?.focus()
      } else if (!event.shiftKey && (document.activeElement === last || !shell.current.contains(document.activeElement))) {
        event.preventDefault()
        first?.focus()
      }
    }

    document.body.classList.add('player-open')
    closeButton.current?.focus()
    window.addEventListener('keydown', keyboard, true)

    async function start() {
      try {
        if (options.itemId) {
          try {
            source = parsePlaybackSource(await apiClient.request(`/Items/${encodeURIComponent(options.sourceItemId || options.itemId)}/PlaybackInfo`, {
              auth: 'required', method: 'POST', readOnly: true, signal: controller.signal,
            }))
            baseOptions.type = playbackType(source.Container)
            if (baseOptions.resumeTicks == null && source.ResumePositionTicks) baseOptions.resumeTicks = source.ResumePositionTicks
            const selected = resolveTrackSelection(source, options.selection, supportsNativeAudio(source))
            selectedAudio = `server:${selected.audioIndex}`
            selectedSubtitle = selected.subtitleIndex === -1 ? '-1' : `server:${selected.subtitleIndex}`
            disableNativeSubtitles = selected.subtitleIndex !== -1
              || (selectionMatches(source, options.selection) && options.selection?.subtitleIndex === -1)
            if (options.selection && !selectionMatches(source, options.selection)) setTrackError('媒体轨道已变化，已使用默认选项')
            if (selected.audioIndex !== -1 && selected.audioIndex !== source.DefaultAudioStreamIndex) {
              if (source.SupportsAudioSelection) await chooseAudio(selectedAudio, (baseOptions.resumeTicks ?? 0) / 10_000_000)
              else initialNativeAudio = selected.audioIndex
            }
          } catch (cause) {
            if (active && !controller.signal.aborted) setTrackError(cause instanceof Error ? cause.message : '选轨信息加载失败')
          }
        }
        if (!session) await mountPlayback(baseOptions)
        if (active && options.itemId) progressTimer = setInterval(() => reportProgress(), 10_000)
      } catch {
        if (active) {
          showToast('播放器加载失败，请刷新后重试', 'error')
          onCloseRef.current()
        }
      }
    }

    void start()
    return () => {
      active = false
      generation += 1
      controller.abort()
      subtitleRequest?.abort()
      clearSubtitle()
      if (useReactAuthStore.getState().token === mountedToken.current && remuxID && options.itemId) {
        void apiClient.request(`/Videos/${encodeURIComponent(options.sourceItemId || options.itemId)}/Playback/${encodeURIComponent(remuxID)}`, {
          auth: 'required', method: 'DELETE', readOnly: true,
        }).catch(() => {})
      }
      if (progressTimer) clearInterval(progressTimer)
      reportProgress(true)
      session?.destroy()
      window.removeEventListener('keydown', keyboard, true)
      document.body.classList.remove('player-open')
    }
  }, [options, showToast])

  function closeFromBackdrop(event: MouseEvent<HTMLDivElement>) {
    if (event.target === event.currentTarget) onCloseRef.current()
  }

  function stopTrackEvent(event: ReactKeyboardEvent<HTMLDivElement>) {
    if (event.key === 'Escape' || event.key === 'Tab') event.stopPropagation()
  }

  return (
    <div id="player-overlay" className="react-player-overlay" onClick={closeFromBackdrop}>
      <div ref={shell} className="react-player-shell" role="dialog" aria-modal="true" aria-labelledby="player-title">
        <div className="react-player-head">
          <div className="react-player-title-wrap"><Film size={16} aria-hidden="true" /><strong id="player-title" title={options.title}>{options.title}</strong></div>
          <button ref={closeButton} id="player-close" className="react-player-close" type="button" title="关闭 (Esc)" aria-label="关闭 (Esc)" onClick={onClose}>
            <X size={18} aria-hidden="true" />
          </button>
        </div>
        {!trackHost && <div className="react-player-loading" role="status"><LoaderCircle size={19} aria-hidden="true" />
          <span>{changingAudio ? '正在准备所选音轨…' : '正在加载播放器…'}</span></div>}
        <div id="player-stage" ref={stage} className="react-player-stage" />
        {trackHost && options.itemId && (changingAudio || trackError || remuxSession) && (
          <div className="react-player-track-toolbar" onKeyDown={stopTrackEvent} onClick={(event) => event.stopPropagation()}>
            {changingAudio && <span role="status">正在切换音轨…</span>}
            {trackError && <span className="react-player-track-error" role="alert">{trackError}</span>}
            {remuxSession && duration > 0 && <label className="react-player-track-seek">播放位置
              <input aria-label="重封装播放位置" type="range" min="0" max={Math.max(0, duration - 1)} step="1" value={position} disabled={changingAudio}
                onChange={(event) => seekRef.current(Number(event.currentTarget.value))} />
              <span>{clock(position)} / {clock(duration)}</span>
            </label>}
          </div>
        )}
      </div>
    </div>
  )
}
