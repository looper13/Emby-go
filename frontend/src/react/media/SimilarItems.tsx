import { ChevronLeft, ChevronRight, Film } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import type { SimilarDto } from '../../api/contracts'
import { imageURL } from '../../lib/image-url'
import { fmtDuration } from '../../lib/media'

interface Props {
  items: SimilarDto[]
  hideImages: boolean
  onOpen: (id: string) => void
}

export function SimilarItems({ items, hideImages, onOpen }: Props) {
  const row = useRef<HTMLDivElement>(null)
  const [overflowing, setOverflowing] = useState(false)
  const [atStart, setAtStart] = useState(true)
  const [atEnd, setAtEnd] = useState(true)

  const updateScroll = useCallback(() => {
    const element = row.current
    const remaining = element ? Math.max(0, element.scrollWidth - element.clientWidth) : 0
    setOverflowing(remaining > 1)
    setAtStart(!element || element.scrollLeft <= 1)
    setAtEnd(!element || element.scrollLeft >= remaining - 1)
  }, [])

  useEffect(() => {
    const element = row.current
    if (!element) return

    const observer = typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(updateScroll)
    observer?.observe(element)
    updateScroll()
    return () => observer?.disconnect()
  }, [items, updateScroll])

  function move(direction: -1 | 1) {
    const element = row.current
    if (!element) return
    element.scrollBy({
      left: direction * element.clientWidth,
      behavior: window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth',
    })
  }

  if (!items.length) return null

  return <section className="detail-section" id="item-similar">
    <div className="react-detail-similar-heading">
      <h3>相似影片 <small>{items.length}</small></h3>
      {overflowing && <div className="react-detail-similar-nav">
        <button className="react-icon-button" type="button" title="上一组相似影片" aria-label="上一组相似影片" disabled={atStart} onClick={() => move(-1)}><ChevronLeft size={16} /></button>
        <button className="react-icon-button" type="button" title="下一组相似影片" aria-label="下一组相似影片" disabled={atEnd} onClick={() => move(1)}><ChevronRight size={16} /></button>
      </div>}
    </div>
    <div ref={row} className="react-detail-similar-row" onScroll={updateScroll}>
      {items.map((item) => <button key={item.Id} type="button" className="react-detail-similar" data-similar={item.Id} title={item.Name} onClick={() => onOpen(item.Id)}>
        <span className="react-detail-similar-poster">
          {!hideImages && item.ImageTags?.Primary
            ? <img src={imageURL(item.Id, 'Primary', 320, item.ImageTags.Primary)} alt="" loading="lazy" decoding="async" />
            : <Film size={23} aria-hidden="true" />}
        </span>
        <strong>{item.Name || item.Id}</strong>
        <small>{[item.ProductionYear, item.RunTimeTicks ? fmtDuration(item.RunTimeTicks / 10_000_000) : '', item.CommunityRating ? `★ ${item.CommunityRating}` : ''].filter(Boolean).join(' · ')}</small>
      </button>)}
    </div>
  </section>
}
