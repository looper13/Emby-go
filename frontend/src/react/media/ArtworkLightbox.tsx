import { ChevronLeft, ChevronRight, X } from 'lucide-react'
import { useEffect, useRef } from 'react'

interface Props {
  sources: string[]
  index: number
  onChange: (index: number) => void
  onClose: () => void
}

export function ArtworkLightbox({ sources, index, onChange, onClose }: Props) {
  const shell = useRef<HTMLDivElement>(null)
  const closeButton = useRef<HTMLButtonElement>(null)
  const previousOverflow = useRef('')

  useEffect(() => {
    previousOverflow.current = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    closeButton.current?.focus()
    function keydown(event: KeyboardEvent) {
      if (event.key === 'Escape') { event.preventDefault(); event.stopImmediatePropagation(); onClose() }
      else if (event.key === 'ArrowLeft') { event.preventDefault(); onChange((index - 1 + sources.length) % sources.length) }
      else if (event.key === 'ArrowRight') { event.preventDefault(); onChange((index + 1) % sources.length) }
      else if (event.key === 'Tab' && shell.current) {
        const nodes = Array.from(shell.current.querySelectorAll<HTMLElement>('button'))
        if (!nodes.length) return
        if (event.shiftKey && document.activeElement === nodes[0]) { event.preventDefault(); nodes.at(-1)?.focus() }
        else if (!event.shiftKey && document.activeElement === nodes.at(-1)) { event.preventDefault(); nodes[0]?.focus() }
      }
    }
    window.addEventListener('keydown', keydown, true)
    return () => {
      document.body.style.overflow = previousOverflow.current
      window.removeEventListener('keydown', keydown, true)
    }
  }, [sources.length, index, onChange, onClose])

  const source = sources[index]
  if (!source) return null
  return <div className="react-artwork-overlay" id="artwork-overlay" onClick={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <div ref={shell} className="react-artwork-shell" role="dialog" aria-modal="true" aria-labelledby="react-artwork-title">
      <div className="react-artwork-head"><strong id="react-artwork-title">剧照 {index + 1} / {sources.length}</strong>
        <button ref={closeButton} className="react-icon-button" type="button" id="artwork-close" title="关闭 (Esc)" aria-label="关闭剧照" onClick={onClose}><X size={16} /></button>
      </div>
      <div className="react-artwork-stage" onClick={(event) => { if (event.target === event.currentTarget) onClose() }}>
        {sources.length > 1 && <button className="react-artwork-nav is-prev" type="button" id="artwork-prev" title="上一张 (←)" aria-label="上一张剧照" onClick={() => onChange((index - 1 + sources.length) % sources.length)}><ChevronLeft size={25} /></button>}
        <img className="react-artwork-image" src={source} alt={`剧照 ${index + 1}`} decoding="async" />
        {sources.length > 1 && <button className="react-artwork-nav is-next" type="button" id="artwork-next" title="下一张 (→)" aria-label="下一张剧照" onClick={() => onChange((index + 1) % sources.length)}><ChevronRight size={25} /></button>}
      </div>
      {sources.length > 1 && <>
        <img className="react-artwork-preload" src={sources[(index + 1) % sources.length]} alt="" aria-hidden="true" />
        <img className="react-artwork-preload" src={sources[(index - 1 + sources.length) % sources.length]} alt="" aria-hidden="true" />
      </>}
    </div>
  </div>
}
