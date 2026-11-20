import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '../src/api/client'
import type { PlayerFactory, PlayerHandle } from '../src/lib/player'
import * as playerLib from '../src/lib/player'
import type { PlaybackSource, TrackEngine } from '../src/lib/player-tracks'
import { PlayerDialog } from '../src/react/media/PlayerDialog'
import { useReactAuthStore } from '../src/react/auth-store'
import authFixture from './fixtures/json/auth-login.json'

const source: PlaybackSource = {
  Id: 'source', TrackRevision: 'rev-1', Container: 'mp4', DefaultAudioStreamIndex: 1,
  DefaultSubtitleStreamIndex: -1, SupportsAudioSelection: true, RunTimeTicks: 1_200_000_000,
  MediaStreams: [{ Type: 'Video', Index: 0 }, { Type: 'Audio', Index: 1, IsDefault: true }],
}

const cleanupFns: Array<() => void> = []

function playerFactory(options?: { tracks?: TrackEngine }) {
  const videos: HTMLVideoElement[] = []
  const players: Array<PlayerHandle & { destroy: ReturnType<typeof vi.fn> }> = []
  const factory: PlayerFactory = (stage, playerOptions) => {
    const video = document.createElement('video')
    Object.defineProperty(video, 'currentTime', { value: 0, writable: true, configurable: true })
    Object.defineProperty(video, 'duration', { value: 120, configurable: true })
    videos.push(video)
    stage.append(video)
    const handle: PlayerHandle & { destroy: ReturnType<typeof vi.fn> } = {
      url: playerOptions.url,
      muted: false,
      video,
      on: (name, callback) => video.addEventListener(name.replace('video:', ''), callback),
      play: vi.fn().mockResolvedValue(undefined),
      destroy: vi.fn(() => video.remove()),
    }
    players.push(handle)
    if (options?.tracks) playerOptions.onTracks?.(options.tracks)
    return handle
  }
  return { factory, videos, players }
}

function setAuthenticated() {
  useReactAuthStore.setState({ token: 'test-token', user: authFixture.User, initialized: true, phase: 'authenticated' })
  cleanupFns.push(apiClient.setAuthHandlers({ getToken: () => 'test-token', onUnauthorized: vi.fn() }))
}

function mockPlaybackInfo(playbackSource: PlaybackSource = source) {
  return vi.spyOn(apiClient, 'request').mockImplementation(async (path) => {
    if (path.endsWith('/PlaybackInfo')) return { MediaSources: [playbackSource] }
    if (path.endsWith('/Playback')) return { SessionId: 'remux-1', Url: '/Videos/5/hls/remux/index.m3u8', StartTimeTicks: 20_000_000 }
    return null
  })
}

afterEach(() => {
  cleanupFns.splice(0).reverse().forEach((cleanup) => cleanup())
  useReactAuthStore.setState({ token: null, user: null, initialized: null, phase: 'anonymous', error: '', expiration: 0 })
  document.body.classList.remove('player-open')
  vi.restoreAllMocks()
})

describe('React player dialog', () => {
  it('loads the stream, traps Escape in the dialog, and destroys the player on close', async () => {
    setAuthenticated()
    mockPlaybackInfo()
    const instance = playerFactory()
    vi.spyOn(playerLib, 'loadPlayerFactory').mockResolvedValue(instance.factory)
    const onClose = vi.fn()
    const { unmount } = render(<PlayerDialog options={{ id: 1, itemId: '5', title: 'Sample', url: '/Videos/5/stream', proxyURL: '/Videos/5/proxy', type: 'mp4' }} onClose={onClose} />)

    expect(await screen.findByRole('dialog', { name: 'Sample' })).toBeInTheDocument()
    await waitFor(() => expect(instance.videos).toHaveLength(1))
    expect(document.body).toHaveClass('player-open')
    expect(playerLib.loadPlayerFactory).toHaveBeenCalledWith('mp4')
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledOnce()
    unmount()
    expect(instance.players[0]!.destroy).toHaveBeenCalledOnce()
    expect(document.body).not.toHaveClass('player-open')
  })

  it('applies a selected external audio stream through a remux and removes its session on unmount', async () => {
    setAuthenticated()
    const playbackSource = { ...source, MediaStreams: [
      { Type: 'Audio', Index: 1, ContainerIndexKnown: true, DisplayTitle: '日语' },
      { Type: 'Audio', Index: 2, IsExternal: true, ContainerIndexKnown: true, DisplayTitle: '英语 FLAC' },
    ] }
    const request = mockPlaybackInfo(playbackSource)
    const instance = playerFactory()
    const loadFactory = vi.spyOn(playerLib, 'loadPlayerFactory').mockResolvedValue(instance.factory)
    const { unmount } = render(<PlayerDialog options={{ id: 1, itemId: '5', sourceItemId: '52', title: 'Sample', url: '/Videos/52/stream', type: 'mp4', resumeTicks: 420_000_000,
      selection: { sourceId: 'source', revision: 'rev-1', audioIndex: 2, subtitleIndex: -1 } }} onClose={vi.fn()} />)

    await waitFor(() => expect(loadFactory).toHaveBeenCalledWith('m3u8'))
    expect(request).toHaveBeenCalledWith('/Videos/52/Playback', expect.objectContaining({ json: {
      AudioStreamIndex: 2, StartTimeTicks: 420_000_000, PreviousSession: '',
    } }))
    await waitFor(() => expect(instance.videos).toHaveLength(1))
    act(() => instance.videos[0]!.dispatchEvent(new Event('loadedmetadata')))
    expect(await screen.findByLabelText('重封装播放位置')).toBeInTheDocument()
    unmount()
    await waitFor(() => expect(request).toHaveBeenCalledWith('/Videos/52/Playback/remux-1', expect.objectContaining({ method: 'DELETE' })))
    expect(instance.players[0]!.destroy).toHaveBeenCalledOnce()
  })

  it('locks the remux seek slider while an audio selection is pending', async () => {
    setAuthenticated()
    const playbackSource = { ...source, MediaStreams: [
      { Type: 'Audio', Index: 1, IsDefault: true }, { Type: 'Audio', Index: 2, IsExternal: true },
    ] }
    const request = mockPlaybackInfo(playbackSource)
    const results: Array<(result: { SessionId: string; Url: string; StartTimeTicks: number }) => void> = []
    request.mockImplementation(async (path) => {
      if (path.endsWith('/PlaybackInfo')) return { MediaSources: [playbackSource] }
      if (path.endsWith('/Playback')) return await new Promise((resolve) => { results.push(resolve) })
      return null
    })
    const instance = playerFactory()
    vi.spyOn(playerLib, 'loadPlayerFactory').mockResolvedValue(instance.factory)
    const { unmount } = render(<PlayerDialog options={{ id: 1, itemId: '5', title: 'Sample', url: '/Videos/5/stream', type: 'mp4',
      selection: { sourceId: 'source', revision: 'rev-1', audioIndex: 2, subtitleIndex: -1 } }} onClose={vi.fn()} />)

    await waitFor(() => expect(results).toHaveLength(1))
    results[0]!({ SessionId: 'remux-1', Url: '/Videos/5/hls/remux/index.m3u8', StartTimeTicks: 0 })
    await waitFor(() => expect(instance.videos).toHaveLength(1))
    act(() => instance.videos[0]!.dispatchEvent(new Event('loadedmetadata')))
    const seek = await screen.findByLabelText('重封装播放位置')
    fireEvent.change(seek, { target: { value: '40' } })
    await waitFor(() => expect(results).toHaveLength(2))
    expect(seek).toBeDisabled()
    results[1]!({ SessionId: 'remux-2', Url: '/Videos/5/hls/remux-2/index.m3u8', StartTimeTicks: 400_000_000 })
    await waitFor(() => expect(instance.videos).toHaveLength(2))
    unmount()
  })

  it('honors HLS track selection without starting an audio remux', async () => {
    setAuthenticated()
    const playbackSource: PlaybackSource = { ...source, Container: 'm3u8', SupportsAudioSelection: false, DefaultAudioStreamIndex: 1, MediaStreams: [
      { Type: 'Audio', Index: 1 }, { Type: 'Audio', Index: 2 }, { Type: 'Subtitle', Index: 3, Codec: 'webvtt' },
    ] }
    const request = mockPlaybackInfo(playbackSource)
    const audio = vi.fn()
    const subtitle = vi.fn()
    const engine: TrackEngine = {
      audio: () => [{ id: '0', label: '日语' }, { id: '1', label: '英语' }],
      subtitles: () => [{ id: '0', label: '中文' }],
      selectAudio: audio, selectSubtitle: subtitle, audioId: () => '0', subtitleId: () => '-1',
    }
    const instance = playerFactory({ tracks: engine })
    vi.spyOn(playerLib, 'loadPlayerFactory').mockResolvedValue(instance.factory)
    const { unmount } = render(<PlayerDialog options={{ id: 1, itemId: '5', title: 'Sample', url: '/Videos/5/stream', type: 'm3u8',
      selection: { sourceId: 'source', revision: 'rev-1', audioIndex: 2, subtitleIndex: 3 } }} onClose={vi.fn()} />)

    await waitFor(() => expect(audio).toHaveBeenCalledWith('1'))
    expect(subtitle).toHaveBeenCalledWith('0')
    expect(request.mock.calls.some(([path]) => String(path).endsWith('/Playback'))).toBe(false)
    expect(request.mock.calls.some(([path]) => String(path).includes('/Subtitles/'))).toBe(false)
    unmount()
  })

  it('does not automatically fetch an unsupported default subtitle', async () => {
    setAuthenticated()
    const playbackSource: PlaybackSource = { ...source, DefaultSubtitleStreamIndex: 4, MediaStreams: [
      { Type: 'Subtitle', Index: 4, IsExternal: true, IsDefault: true, IsTextSubtitleStream: true, Codec: 'smi', DeliveryUrl: '/Videos/5/mediasource_1/Subtitles/4/Stream.smi' },
    ] }
    const request = mockPlaybackInfo(playbackSource)
    const instance = playerFactory()
    vi.spyOn(playerLib, 'loadPlayerFactory').mockResolvedValue(instance.factory)
    render(<PlayerDialog options={{ id: 1, itemId: '5', title: 'Sample', url: '/Videos/5/stream', type: 'mp4' }} onClose={vi.fn()} />)

    await waitFor(() => expect(instance.videos).toHaveLength(1))
    act(() => instance.videos[0]!.dispatchEvent(new Event('loadedmetadata')))
    expect(request.mock.calls.every(([path]) => !String(path).includes('/Subtitles/'))).toBe(true)
  })
})
