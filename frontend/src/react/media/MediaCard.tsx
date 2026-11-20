import { Activity, Film, Play, RefreshCw, Star, Trash2 } from 'lucide-react'
import { useState, type KeyboardEvent, type MouseEvent } from 'react'
import type { MovieDto, WallUserData } from '../../api/contracts'
import { imageURL } from '../../lib/image-url'
import { numberUnlessInTitle, statusText } from '../../lib/media'
import { useReactPreferencesStore } from '../preferences-store'
import { useImageRetry } from './use-image-retry'

interface Props {
  movie: MovieDto
  tags?: Record<string, string>
  userdata?: WallUserData
  busy?: Set<string>
  canManage?: boolean
  onOpen: (id: number) => void
  onQuickPlay: (id: number) => void
  onAction: (kind: 'probe' | 'reread' | 'remove', id: number) => void
}

function MediaPoster({ movie, tags, title }: { movie: MovieDto; tags?: Record<string, string>; title: string }) {
  const hideImages = useReactPreferencesStore((state) => state.hideImages)
  const primary = movie.PosterPath ? imageURL(movie.id, 'Primary', 420, tags?.Primary) : ''
  const fallback = movie.cover_url || ''
  const image = useImageRetry(primary || fallback, fallback, hideImages)

  if (hideImages) return <div className="react-wall-placeholder is-hidden" role="img" aria-label={title}>
    <Film size={25} aria-hidden="true" /><span>{title}</span>
  </div>
  if (!image.current) return <div className="react-wall-placeholder" role="img" aria-label={title}>
    <Film size={25} aria-hidden="true" /><span>{image.retrying ? '正在重试…' : image.failed ? '图片加载失败' : title}</span>
  </div>
  return <img key={image.revision} src={image.current} alt="" loading="lazy" decoding="async"
    onLoad={() => image.onLoad(image.current)} onError={() => image.onError(image.current)} />
}

export function MediaCard({ movie, tags, userdata, busy = new Set(), canManage = false, onOpen, onQuickPlay, onAction }: Props) {
  const title = movie.Title || (canManage ? movie.source_path : `#${movie.id}`)
  const progress = userdata && movie.RuntimeSeconds > 0 && userdata.position_ticks > 0
    ? Math.min(100, Math.round(userdata.position_ticks / (movie.RuntimeSeconds * 10_000_000) * 100)) : 0
  const number = numberUnlessInTitle(movie.Title, movie.Number)

  function activate(event: KeyboardEvent<HTMLElement>) {
    if (event.target !== event.currentTarget || (event.key !== 'Enter' && event.key !== ' ')) return
    event.preventDefault()
    onOpen(movie.id)
  }

  function stopAnd(action: () => void) {
    return (event: MouseEvent<HTMLButtonElement>) => { event.stopPropagation(); action() }
  }

  return (
    <article className="wall-card react-wall-card" data-play={movie.id} data-status={movie.Status} tabIndex={0} role="button"
      aria-label={`查看 ${title} 详情`} onClick={() => onOpen(movie.id)} onKeyDown={activate}>
      <div className="wall-poster react-wall-poster">
        <MediaPoster movie={movie} tags={tags} title={title} />
        <div className="wall-badges">
          {canManage && movie.Status !== 'success' && <span className={`wall-badge ${movie.Status}`}>{statusText[movie.Status] || movie.Status}</span>}
          {userdata?.played && <span className="wall-badge played">已看</span>}
          {userdata?.favorite && <span className="wall-badge fav" aria-label="已收藏">♥</span>}
          {movie.AdditionalParts?.length ? <span className="wall-badge multi">CD×{movie.AdditionalParts.length + 1}</span> : null}
        </div>
        {progress > 0 && <div className="wall-progress" aria-label={`已播放 ${progress}%`}><i style={{ width: `${progress}%` }} /></div>}

        {canManage && <div className="wall-actions" aria-label="影片管理操作">
          <button className="icon-btn" type="button" data-probe={movie.id} title="探测媒体信息" aria-label={`探测 ${title}`}
            disabled={busy.has(`probe:${movie.id}`)} onClick={stopAnd(() => onAction('probe', movie.id))}><Activity size={14} /></button>
          <button className="icon-btn" type="button" data-reread={movie.id} title="重读 STRM 与 NFO" aria-label={`重读 ${title}`}
            disabled={busy.has(`reread:${movie.id}`)} onClick={stopAnd(() => onAction('reread', movie.id))}><RefreshCw size={14} /></button>
          <button className="icon-btn danger" type="button" data-delete={movie.id} title="删除影片及文件" aria-label={`删除 ${title}`}
            disabled={busy.has(`remove:${movie.id}`)} onClick={stopAnd(() => onAction('remove', movie.id))}><Trash2 size={14} /></button>
        </div>}

        {['success', 'manual'].includes(movie.Status) && <button className="wall-play" type="button" data-quickplay={movie.id}
          title="直接播放" aria-label={`播放 ${title}`} onClick={stopAnd(() => onQuickPlay(movie.id))}><Play size={19} fill="currentColor" /></button>}
      </div>
      <div className="wall-meta react-wall-meta">
        <strong title={title}>{title}</strong>
        <small>
          {movie.television ? <span className="meta-tag">{movie.television.title} · S{movie.television.season}E{movie.television.episode}</span>
            : number ? <span className="meta-tag mono">{number}</span> : null}
          {movie.Rating > 0 && <span className="meta-rating"><Star size={10} fill="currentColor" aria-hidden="true" />{movie.Rating}</span>}
          {movie.Year > 0 && <span className="meta-year">{movie.Year}</span>}
          {movie.OriginalTitle && !movie.television && <span className="meta-orig" title={movie.OriginalTitle}>{movie.OriginalTitle}</span>}
          {!movie.television && !movie.Year && !movie.Rating && !movie.OriginalTitle && <span className="meta-protocol">{movie.source_protocol}</span>}
        </small>
      </div>
    </article>
  )
}
