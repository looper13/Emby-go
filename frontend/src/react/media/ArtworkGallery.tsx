import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { ArtworkLightbox } from './ArtworkLightbox'

interface Props {
  sources: string[]
  hidden?: boolean
  label?: string
}

export function ArtworkGallery({ sources, hidden = false, label = '剧照' }: Props) {
  const [active, setActive] = useState<number | null>(null)
  const triggers = useRef<Array<HTMLButtonElement | null>>([])
  const originIndex = useRef<number | null>(null)
  const returnFocus = useRef<HTMLButtonElement | null>(null)
  const sourceKey = sources.join('\u0000')

  useLayoutEffect(() => {
    setActive(null)
    originIndex.current = null
    returnFocus.current = null
  }, [hidden, sourceKey])

  useEffect(() => {
    if (active !== null || !returnFocus.current) return
    const trigger = returnFocus.current
    returnFocus.current = null
    if (trigger.isConnected) trigger.focus({ preventScroll: true })
  }, [active])

  function close() {
    returnFocus.current = originIndex.current === null ? null : triggers.current[originIndex.current] ?? null
    originIndex.current = null
    setActive(null)
  }

  if (hidden || sources.length === 0) return null
  return <>
    <section className="detail-section react-artwork-gallery" aria-label="影片剧照">
      <h3>{label} <small>{sources.length}</small></h3>
      <div className="react-detail-artwork-grid">
        {sources.map((source, index) => <button key={`${index}:${source}`} ref={element => { triggers.current[index] = element }}
          type="button" className="react-detail-artwork-thumb" title={`查看剧照 ${index + 1}`} aria-label={`查看剧照 ${index + 1}`}
          onClick={() => { originIndex.current = index; setActive(index) }}>
          <img src={source} alt={`剧照 ${index + 1}`} loading="lazy" decoding="async" />
        </button>)}
      </div>
    </section>
    {active !== null && <ArtworkLightbox sources={sources} index={active} onChange={setActive} onClose={close} />}
  </>
}
