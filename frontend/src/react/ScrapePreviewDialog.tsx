import { Film, X } from 'lucide-react'
import { useEffect, useRef, useState, type MouseEvent } from 'react'
import { numberUnlessInTitle } from '../lib/media'
import { useReactPreferencesStore } from './preferences-store'
import { useReactScrapePreviewStore } from './scrape-preview-store'
import { useReactTasksStore } from './tasks-store'
import './scrape-preview-dialog.css'

function ScrapeImage({ src, alt = '', className = '' }: { src: string; alt?: string; className?: string }) {
  const hideImages = useReactPreferencesStore((state) => state.hideImages)
  const [failed, setFailed] = useState(false)
  useEffect(() => setFailed(false), [hideImages, src])
  if (hideImages || !src || failed) return <span className={`noimg react-scrape-placeholder ${className}`} role="img" aria-label={alt || (failed ? '图片加载失败' : '图片不可用')}><Film size={20} aria-hidden="true" /></span>
  return <img className={className} src={src} alt={alt} loading="lazy" decoding="async" onError={() => setFailed(true)} />
}

export function ScrapePreviewDialog() {
  const current = useReactScrapePreviewStore((state) => state.current)
  const preview = useReactScrapePreviewStore((state) => state.preview)
  const inspect = useReactScrapePreviewStore((state) => state.inspect)
  const loading = useReactScrapePreviewStore((state) => state.loading)
  const writing = useReactScrapePreviewStore((state) => state.writing)
  const error = useReactScrapePreviewStore((state) => state.error)
  const overwrite = useReactScrapePreviewStore((state) => state.overwrite)
  const select = useReactScrapePreviewStore((state) => state.select)
  const setOverwrite = useReactScrapePreviewStore((state) => state.setOverwrite)
  const confirm = useReactScrapePreviewStore((state) => state.confirm)
  const close = useReactScrapePreviewStore((state) => state.close)
  const mutationBusy = useReactTasksStore((state) => state.mutationBusy)
  const shell = useRef<HTMLDivElement>(null)
  const closeButton = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    if (!current) return
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    closeButton.current?.focus()

    function keyboard(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        event.preventDefault()
        event.stopImmediatePropagation()
        close()
        return
      }
      if (event.key !== 'Tab' || !shell.current) return
      const focusable = Array.from(shell.current.querySelectorAll<HTMLElement>(
        'button, input, select, a[href], [tabindex]:not([tabindex="-1"])',
      )).filter((element) => !element.hasAttribute('disabled') && element.tabIndex >= 0)
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
      document.body.style.overflow = previousOverflow
      window.removeEventListener('keydown', keyboard, true)
    }
  }, [close, current])

  if (!current) return null

  function closeFromBackdrop(event: MouseEvent<HTMLDivElement>) {
    if (event.target === event.currentTarget) close()
  }

  const candidateButtons = preview?.candidates.map((candidate) => {
    const active = inspect?.provider === candidate.provider && inspect.id === candidate.id
    const details = [numberUnlessInTitle(candidate.title, candidate.number), candidate.provider,
      candidate.score ? `★ ${candidate.score}` : '', candidate.exact ? '番号命中' : ''].filter(Boolean).join(' · ')
    return <button key={`${candidate.provider}:${candidate.id}`} className={`scrape-candidate${active ? ' is-active' : ''}`}
      type="button" disabled={writing} aria-pressed={active} onClick={() => void select(candidate)}>
      {candidate.thumb ? <ScrapeImage src={candidate.thumb} alt="" /> : <span className="noimg react-scrape-placeholder" aria-hidden="true"><Film size={20} /></span>}
      <span><strong>{candidate.title || '—'}</strong><small>{details}</small></span>
    </button>
  })

  return <div id="scrape-overlay" className="scrape-overlay" onClick={closeFromBackdrop}>
    <div ref={shell} className="scrape-modal" style={{ height: 'min(740px,calc(100dvh - 48px))' }}
      role="dialog" aria-modal="true" aria-labelledby="scrape-preview-title">
      <div className="scrape-head">
        <strong id="scrape-preview-title">刮削预览与确认</strong>
        <button ref={closeButton} id="scrape-close" className="icon-btn" type="button" title="取消 (Esc)" aria-label="取消 (Esc)" onClick={close}>
          <X size={16} aria-hidden="true" />
        </button>
      </div>
      <div id="scrape-body" className="scrape-body" style={{ flex: 1, minHeight: 0 }}>
        {error && <p className="hint" role="alert" style={{ color: 'var(--danger)' }}>
          {error} <button className="btn btn-sm" type="button" onClick={() => void useReactScrapePreviewStore.getState().open(current.movieId, current.confirmed)}>重试</button>
        </p>}
        {loading && <p className="hint" role="status">{preview ? '正在读取详情…' : '正在搜索候选…'}</p>}
        {preview && !preview.candidates.length && <p className="hint">没有搜到候选（搜索词：{preview.query || '—'}）。可先在详情里补番号，或在 NFO 里填好番号后重试。</p>}
        {preview?.candidates.length ? <div className="scrape-cols">
          <div>
            <p className="hint" style={{ margin: '0 0 8px' }}>候选 {preview.candidates.length} 条 · 搜索词 <code>{preview.query}</code>
              {preview.expected_number && <> · 预期番号 <code>{preview.expected_number}</code></>}
            </p>
            <div className="scrape-candidates">{candidateButtons}</div>
          </div>
          {inspect && <div className="scrape-detail">
            <div className="scrape-detail-head">
              {inspect.poster && <ScrapeImage src={inspect.poster} alt="候选海报" />}
              <div>
                <h3 style={{ margin: '0 0 6px' }}>{inspect.title || '—'}</h3>
                <p className="hint" style={{ margin: '0 0 8px' }}>
                  {[inspect.number, inspect.provider, inspect.release_date, inspect.runtime ? `${inspect.runtime} 分钟` : '', inspect.score ? `★ ${inspect.score}` : ''].filter(Boolean).join(' · ')}
                </p>
                <p className="detail-plot">{inspect.summary || '（无简介）'}</p>
              </div>
            </div>
            <div>
              <h3 className="scrape-h3">字段差异（预览只显示原文，翻译在确认后执行）</h3>
              <dl className="scrape-diff">{inspect.fields.map((field) => <div key={field.key} className={`scrape-diff-row${field.change ? ' is-change' : ''}`}>
                <dt>{field.label}</dt><dd>
                  {field.old && <span className="old">{field.old}{' '}</span>}
                  {field.next ? <span className="next">{field.next}</span> : <span className="old">（不写入）</span>}
                </dd>
              </div>)}</dl>
            </div>
            <div>
              <h3 className="scrape-h3">图片</h3>
              <div className="scrape-images">{inspect.images.map((image) => <div key={image.name} className={`scrape-image${image.exists ? ' is-kept' : ''}`}>
                {image.preview ? <ScrapeImage src={image.preview} alt="" /> : <span className="noimg react-scrape-placeholder" aria-hidden="true"><Film size={20} /></span>}
                <span>{image.name}{image.exists ? '（已存在，只补缺失时不覆盖）' : ''}</span>
              </div>)}</div>
            </div>
          </div>}
        </div> : null}
      </div>
      <div className="scrape-foot" style={{ padding: 16, flexShrink: 0, borderTop: '1px solid var(--line)' }}>
        {inspect && <label className="switch-lg"><input id="scrape-force" type="checkbox" checked={overwrite} disabled={writing}
          onChange={(event) => setOverwrite(event.currentTarget.checked)} /> 强制覆盖（不勾选 = 只补缺失）</label>}
        <button id="scrape-cancel-btn" className="btn" type="button" onClick={close}>取消</button>
        <button id="scrape-confirm" className="btn btn-accent" type="button" disabled={!inspect || loading || writing || mutationBusy} onClick={() => void confirm()}>
          {writing ? '正在写入…' : '确认写入'}
        </button>
      </div>
    </div>
  </div>
}
