import {
  ChevronDown, ExternalLink, Film, Heart, List, MessageSquare, Play, RefreshCw,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import type { LibraryDto } from '../../api/contracts'
import {
  javdbCommunityApi, type JavdbListMovies, type JavdbMatch, type JavdbPage,
  type JavdbRelatedList, type JavdbReview,
} from '../../api/javdb-community'
import { r18Api } from '../../api/r18'
import { usePageRequest } from '../hooks/use-page-request'
import { useReactAuthStore } from '../auth-store'
import { useAppRuntimeStore } from '../app-store'
import { FavoriteToggleButton } from './FavoriteToggleButton'
import './avdb-community.css'

interface Props {
  itemId: number
  number: string
  localLibraryId: number
  savedList?: { libraryId: number; listId: string; relatedPage: number }
  onOpen: (id: number) => void
}

type Tab = 'reviews' | 'lists'
type Phase = 'loading' | 'matching' | 'ready' | 'unavailable' | 'error'

export function AvdbCommunitySection({ itemId, number, localLibraryId, savedList, onOpen }: Props) {
  const administrator = useReactAuthStore((state) => state.user?.Policy.IsAdministrator === true)
  const showToast = useAppRuntimeStore((state) => state.showToast)
  const matchScope = usePageRequest()
  const reviewScope = usePageRequest()
  const listScope = usePageRequest()
  const movieScope = usePageRequest()
  const [libraries, setLibraries] = useState<LibraryDto[]>([])
  const [selectedLibrary, setSelectedLibrary] = useState(0)
  const selectedLibraryRef = useRef(0)
  selectedLibraryRef.current = selectedLibrary
  const [match, setMatch] = useState<JavdbMatch>()
  const matchRef = useRef<JavdbMatch | undefined>(undefined)
  const [phase, setPhase] = useState<Phase>('loading')
  const [problem, setProblem] = useState('')
  const [tab, setTab] = useState<Tab>('reviews')
  const [reviews, setReviews] = useState<JavdbPage<JavdbReview>>()
  const [lists, setLists] = useState<JavdbPage<JavdbRelatedList>>()
  const [reviewBusy, setReviewBusy] = useState(false)
  const [listBusy, setListBusy] = useState(false)
  const [reviewError, setReviewError] = useState('')
  const [listError, setListError] = useState('')
  const [reviewAttempt, setReviewAttempt] = useState(1)
  const [listAttempt, setListAttempt] = useState(1)
  const [selectedList, setSelectedList] = useState<JavdbRelatedList>()
  const selectedListRef = useRef<JavdbRelatedList | undefined>(undefined)
  const [relatedPage, setRelatedPage] = useState(1)
  const [listMovies, setListMovies] = useState<JavdbListMovies>()
  const [movieBusy, setMovieBusy] = useState(false)
  const [movieError, setMovieError] = useState('')
  const [movieAttempt, setMovieAttempt] = useState(1)
  const [expandedReviews, setExpandedReviews] = useState<Set<string>>(() => new Set())
  const [savingList, setSavingList] = useState<string | null>(null)
  const listMoviesRef = useRef<JavdbListMovies | undefined>(undefined)
  const savedKey = `${savedList?.libraryId ?? ''}:${savedList?.listId ?? ''}:${savedList?.relatedPage ?? ''}`
  const target = useMemo(() => savedList, [savedKey])
  const matched = phase === 'ready' && match?.status === 'matched'
  const busy = phase === 'loading' || phase === 'matching'
  const localMovieCount = listMovies?.items.filter((movie) => movie.has_local).length ?? 0

  const resetListMovies = useCallback(() => {
    movieScope.cancel()
    selectedListRef.current = undefined
    listMoviesRef.current = undefined
    setSelectedList(undefined)
    setListMovies(undefined)
    setMovieBusy(false)
    setMovieError('')
    setMovieAttempt(1)
  }, [movieScope])

  const loadListMovies = useCallback(async (page = 1, refresh = false) => {
    const identity = matchRef.current
    const list = selectedListRef.current
    const localLibrary = localLibraryId
    if (phase !== 'ready' || identity?.status !== 'matched' || !identity.video || !list) return
    const current = movieScope.begin()
    setMovieBusy(true)
    setMovieError('')
    setMovieAttempt(page)
    try {
      const result = await javdbCommunityApi.listMovies(itemId, identity.library_id, list.id, page, relatedPage, current.signal, refresh)
      if (!current.isCurrent() || matchRef.current !== identity || selectedListRef.current !== list) return
      if (result.number !== identity.number || result.video_id !== identity.video.id || result.library_id !== identity.library_id
        || result.local_library_id !== localLibrary || localLibraryId !== localLibrary || result.list_id !== list.id) {
        throw new Error('影片番号、媒体库或清单已更新，请刷新详情后重新匹配。')
      }
      const previous = listMoviesRef.current?.items ?? []
      const allItems = page === 1 ? result.items : [...previous, ...result.items]
      const next = { ...result, items: [...new Map(allItems.map((movie) => [movie.id, movie])).values()] }
      listMoviesRef.current = next
      setListMovies(next)
    } catch (cause) {
      if (current.isCurrent() && matchRef.current === identity && selectedListRef.current === list) {
        setMovieError(cause instanceof Error ? cause.message : 'AVdb 清单影片加载失败')
      }
    } finally {
      if (current.isCurrent() && matchRef.current === identity && selectedListRef.current === list) setMovieBusy(false)
    }
  }, [itemId, localLibraryId, movieScope, phase, relatedPage])

  const loadLists = useCallback(async (page = 1, refresh = false) => {
    const identity = matchRef.current
    if (phase !== 'ready' || identity?.status !== 'matched') return
    const current = listScope.begin()
    setListBusy(true)
    setListError('')
    setListAttempt(page)
    try {
      const result = await javdbCommunityApi.lists(itemId, identity.library_id, page, current.signal, refresh)
      if (!current.isCurrent() || matchRef.current !== identity) return
      if (result.number !== identity.number || result.video_id !== identity.video?.id || result.library_id !== identity.library_id) {
        throw new Error('影片番号或 AVdb 资料来源已更新，请重新匹配后查看清单。')
      }
      resetListMovies()
      setLists(result)
      const saved = target
      if (saved && saved.libraryId === identity.library_id && saved.relatedPage === page) {
        const found = result.items.find((list) => list.id === saved.listId)
        if (found) {
          selectedListRef.current = found
          setSelectedList(found)
          setRelatedPage(result.page)
        } else setMovieError('收藏的 AVdb 清单已无法从当前影片打开。')
      }
    } catch (cause) {
      if (current.isCurrent() && matchRef.current === identity) setListError(cause instanceof Error ? cause.message : 'AVdb 清单加载失败')
    } finally {
      if (current.isCurrent() && matchRef.current === identity) setListBusy(false)
    }
  }, [itemId, listScope, resetListMovies, target, phase])

  const loadReviews = useCallback(async (page = 1, refresh = false) => {
    const identity = matchRef.current
    if (phase !== 'ready' || identity?.status !== 'matched' || !identity.video) return
    const current = reviewScope.begin()
    setReviewBusy(true)
    setReviewError('')
    setReviewAttempt(page)
    try {
      const result = await javdbCommunityApi.reviews(itemId, identity.library_id, page, current.signal, refresh)
      if (!current.isCurrent() || matchRef.current !== identity) return
      if (result.number !== identity.number || result.video_id !== identity.video.id || result.library_id !== identity.library_id) {
        throw new Error('影片番号或 AVdb 资料来源已更新，请重新匹配后查看评论。')
      }
      setReviews(result)
      setExpandedReviews(new Set())
    } catch (cause) {
      if (current.isCurrent() && matchRef.current === identity) setReviewError(cause instanceof Error ? cause.message : 'AVdb 评论加载失败')
    } finally {
      if (current.isCurrent() && matchRef.current === identity) setReviewBusy(false)
    }
  }, [itemId, phase, reviewScope])

  const loadMatch = useCallback(async (current: ReturnType<typeof matchScope.begin>, refresh = false) => {
    setPhase('matching')
    setProblem('')
    try {
      const result = await javdbCommunityApi.match(itemId, selectedLibrary, current.signal, refresh)
      if (!current.isCurrent()) return
      matchRef.current = result
      setMatch(result)
      setPhase('ready')
      if (result.status === 'matched') {
        if (target && target.libraryId === result.library_id) {
          setTab('lists')
          setListAttempt(target.relatedPage)
        } else setTab('reviews')
      }
    } catch (cause) {
      if (current.isCurrent()) { setProblem(cause instanceof Error ? cause.message : 'AVdb 匹配失败'); setPhase('error') }
    }
  }, [itemId, matchScope, selectedLibrary, target])

  const initialize = useCallback(async () => {
    const current = matchScope.begin()
    reviewScope.cancel()
    listScope.cancel()
    resetListMovies()
    matchRef.current = undefined
    setMatch(undefined)
    setReviews(undefined)
    setLists(undefined)
    setReviewError('')
    setListError('')
    setReviewBusy(false)
    setListBusy(false)
    setExpandedReviews(new Set())
    setTab('reviews')
    setProblem('')
    setPhase('loading')
    if (!number.trim()) { setPhase('unavailable'); return }
    try {
      const result = await r18Api.libraries(current.signal)
      if (!current.isCurrent()) return
      const available = result.items ?? []
      setLibraries(available)
      if (!available.length) { setSelectedLibrary(0); setPhase('unavailable'); return }
      const preferred = target?.libraryId
      const libraryId = preferred && available.some((library) => library.Id === preferred)
        ? preferred : available.some((library) => library.Id === selectedLibraryRef.current) ? selectedLibraryRef.current : available[0]!.Id
      setSelectedLibrary(libraryId)
      const matchResult = await javdbCommunityApi.match(itemId, libraryId, current.signal)
      if (!current.isCurrent()) return
      matchRef.current = matchResult
      setMatch(matchResult)
      setPhase('ready')
      if (matchResult.status === 'matched') setTab(target?.libraryId === matchResult.library_id ? 'lists' : 'reviews')
    } catch (cause) {
      if (current.isCurrent()) { setProblem(cause instanceof Error ? cause.message : 'AVdb 资料加载失败'); setPhase('error') }
    }
  }, [itemId, listScope, matchScope, number, resetListMovies, reviewScope, target])

  useEffect(() => { void initialize(); return () => matchScope.cancel() }, [initialize, matchScope])

  useEffect(() => {
    if (phase !== 'ready' || match?.status !== 'matched') return
    if (tab === 'lists') {
      if (!lists && !listBusy && !listError) void loadLists(target?.libraryId === match.library_id ? target.relatedPage : 1)
    } else if (!reviews && !reviewBusy && !reviewError) void loadReviews()
  }, [listBusy, listError, lists, loadLists, loadReviews, match, phase, reviewBusy, reviewError, tab, target])

  useEffect(() => {
    if (tab === 'lists' && selectedList) void loadListMovies(movieAttempt)
  // Selection starts the list request; pagination remains explicit.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedList])

  function retryMatch() {
    if (!selectedLibrary) { void initialize(); return }
    const current = matchScope.begin()
    resetListMovies()
    setReviews(undefined)
    setLists(undefined)
    matchRef.current = undefined
    setMatch(undefined)
    setPhase('matching')
    void loadMatch(current, true)
  }

  function changeLibrary(libraryId: number) {
    setSelectedLibrary(libraryId)
    const current = matchScope.begin()
    reviewScope.cancel()
    listScope.cancel()
    resetListMovies()
    setReviews(undefined)
    setLists(undefined)
    setReviewBusy(false)
    setListBusy(false)
    setReviewError('')
    setListError('')
    matchRef.current = undefined
    setMatch(undefined)
    setPhase('matching')
    void javdbCommunityApi.match(itemId, libraryId, current.signal, true).then((result) => {
      if (!current.isCurrent()) return
      matchRef.current = result
      setMatch(result)
      setPhase('ready')
      if (result.status === 'matched') {
        setTab('reviews')
      }
    }).catch((cause) => {
      if (current.isCurrent()) { setProblem(cause instanceof Error ? cause.message : 'AVdb 匹配失败'); setPhase('error') }
    })
  }

  function selectList(list: JavdbRelatedList) {
    if (selectedListRef.current?.id === list.id) { resetListMovies(); return }
    resetListMovies()
    selectedListRef.current = list
    setSelectedList(list)
    setRelatedPage(lists?.page ?? 1)
  }

  function changeListFavorite(id: string, favorite: boolean) {
    setLists((current) => current ? { ...current, items: current.items.map((list) => list.id === id ? { ...list, favorite } : list) } : current)
  }

  async function toggleListFavorite(list: JavdbRelatedList) {
    const identity = matchRef.current
    if (!identity || identity.status !== 'matched' || !lists || savingList) return
    const next = !list.favorite
    setSavingList(list.id)
    try {
      if (next) await javdbCommunityApi.setListFavorite(itemId, lists.library_id, list.id, lists.page)
      else await javdbCommunityApi.removeListFavorite(lists.library_id, list.id)
      changeListFavorite(list.id, next)
    } catch (cause) {
      showToast(cause instanceof Error ? cause.message : '清单收藏操作失败', 'error')
    } finally { setSavingList(null) }
  }

  function toggleReview(id: string) {
    setExpandedReviews((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  if (!number.trim()) return null

  return (
    <section className="detail-section react-avdb-community" aria-labelledby="avdb-community-title">
      <div className="react-avdb-heading">
        <h2 id="avdb-community-title"><MessageSquare size={16} aria-hidden="true" />AVdb 评论与清单</h2>
        <button className="react-detail-action" type="button" aria-label="重新匹配 AVdb 番号" disabled={busy} onClick={retryMatch}>
          <RefreshCw size={14} aria-hidden="true" /><span>重新匹配</span>
        </button>
      </div>

      <div className="react-avdb-source">
        <span className="mono">{number}</span>
        {libraries.length > 1
          ? <label><span>资料来源</span><select aria-label="AVdb 资料来源" value={selectedLibrary} disabled={busy}
            onChange={(event) => changeLibrary(Number(event.currentTarget.value))}>
            {libraries.map((library) => <option key={library.Id} value={library.Id}>{library.Name}</option>)}
          </select></label>
          : libraries.length > 0 && <span className="react-avdb-muted">{libraries[0]?.Name}</span>}
        {matched && match?.video && <Link className="react-avdb-detail-link" to={`/r18/${match.library_id}/item/${match.video.id}`}>
          查看 AVdb 影片资料 <ExternalLink size={13} aria-hidden="true" />
        </Link>}
      </div>

      {busy && <p className="react-avdb-state" role="status">{phase === 'loading' ? '正在读取可访问的 AVdb 资料库…' : '正在按完整番号匹配 AVdb 影片…'}</p>}
      {phase === 'unavailable' && <div className="react-avdb-state"><p>还没有可访问的 AVdb 资料库。</p>
        {administrator ? <Link className="react-detail-action" to="/libraries">添加 AVdb 资料库</Link> : <p>请联系管理员添加资料库并授权访问。</p>}
      </div>}
      {phase === 'error' && <div className="react-avdb-state"><p role="alert">{problem}</p><button className="react-detail-action" type="button" onClick={retryMatch}>重试匹配</button></div>}
      {phase === 'ready' && match?.status === 'not_found' && <p className="react-avdb-state">未找到与 {number} 完整匹配的 AVdb 影片，可核对番号后重新匹配。</p>}
      {phase === 'ready' && match?.status === 'no_number' && <p className="react-avdb-state">当前影片没有可用于匹配的番号。</p>}

      {matched && <>
        <div className="react-avdb-tabs" role="tablist" aria-label="AVdb 资料类型">
          <button type="button" role="tab" aria-selected={tab === 'reviews'} onClick={() => setTab('reviews')}><MessageSquare size={14} />评论{reviews?.total !== undefined ? ` ${reviews.total}` : ''}</button>
          <button type="button" role="tab" aria-selected={tab === 'lists'} onClick={() => setTab('lists')}><List size={14} />相关清单{lists?.total !== undefined ? ` ${lists.total}` : ''}</button>
        </div>
        <p className="react-avdb-help">资料通过服务端 AVdb 接口读取；页面不会直接访问资料源站。</p>

        {tab === 'reviews' && <div className="react-avdb-content" aria-label="AVdb 评论" aria-busy={reviewBusy}>
          {reviewError && <div className="react-avdb-error"><p role="alert">{reviewError}</p><button className="react-detail-action" disabled={reviewBusy} onClick={() => void loadReviews(reviewAttempt, true)}>重试评论</button></div>}
          {reviewBusy && <p className="react-avdb-state" role="status">正在读取第 {reviewAttempt} 页评论…</p>}
          {reviews && !reviews.items.length && !reviewBusy && !reviewError && <p className="react-avdb-state">当前影片暂无评论。</p>}
          {reviews?.items.length ? <div className="react-avdb-reviews">{reviews.items.map((review) => <article key={review.id} className="react-avdb-review">
            <div className="react-avdb-review-meta"><strong>{review.username}</strong>{review.score && <span>评分 {review.score}</span>}{review.status && <span>{review.status}</span>}{review.created_at && <span>{review.created_at}</span>}</div>
            {review.content && <p>{review.content.length > 600 && !expandedReviews.has(review.id) ? `${review.content.slice(0, 600)}…` : review.content}</p>}
            {review.content.length > 600 && <button className="react-avdb-text-button" aria-expanded={expandedReviews.has(review.id)} onClick={() => toggleReview(review.id)}>{expandedReviews.has(review.id) ? '收起评论' : '展开完整评论'}</button>}
          </article>)}</div> : null}
          {reviews && (reviews.items.length || reviews.page > 1) && <nav className="react-avdb-pagination" aria-label="AVdb 评论分页">
            <button className="react-detail-action" disabled={reviewBusy || reviews.page <= 1} onClick={() => void loadReviews(reviews.page - 1)}>上一页评论</button>
            <span>第 {reviews.page} 页{reviews.total !== undefined ? ` · 共 ${reviews.total} 条` : ''}</span>
            <button className="react-detail-action" disabled={reviewBusy || !reviews.has_more} onClick={() => void loadReviews(reviews.page + 1)}>下一页评论</button>
          </nav>}
        </div>}

        {tab === 'lists' && <div className="react-avdb-content" aria-label="AVdb 相关清单" aria-busy={listBusy}>
          {listError && <div className="react-avdb-error"><p role="alert">{listError}</p><button className="react-detail-action" disabled={listBusy} onClick={() => void loadLists(listAttempt, true)}>重试清单</button></div>}
          {listBusy && <p className="react-avdb-state" role="status">正在读取第 {listAttempt} 页清单…</p>}
          {lists && !lists.items.length && !listBusy && !listError && <p className="react-avdb-state">当前影片暂无相关清单。</p>}
          {lists?.items.length ? <div className="react-avdb-lists">{lists.items.map((list) => <div key={list.id} className="react-avdb-list-row">
            <button type="button" className={`react-avdb-list${selectedList?.id === list.id ? ' is-selected' : ''}`} aria-expanded={selectedList?.id === list.id}
              aria-controls="avdb-list-movies" onClick={() => selectList(list)}>
              <List size={18} aria-hidden="true" /><span><strong>{list.name}</strong><small>{list.movie_count !== undefined ? `清单 ${list.movie_count} 部 · ` : ''}查看清单影片</small></span>
              <ChevronDown size={15} aria-hidden="true" />
            </button>
            <button type="button" className={`favorite-button react-avdb-list-favorite${list.favorite ? ' selected' : ''}`} title={`${list.favorite ? '取消收藏清单' : '收藏清单'} · ${list.name}`}
              aria-label={`${list.favorite ? '取消收藏清单' : '收藏清单'} · ${list.name}`} aria-pressed={list.favorite === true}
              disabled={savingList === list.id} onClick={() => void toggleListFavorite(list)}><Heart size={17} fill={list.favorite ? 'currentColor' : 'none'} /></button>
          </div>)}</div> : null}

          {selectedList && <section id="avdb-list-movies" className="react-avdb-movie-list" aria-busy={movieBusy} aria-labelledby="avdb-list-title">
            <div className="react-avdb-movie-heading"><h3 id="avdb-list-title">{selectedList.name}</h3>
              {listMovies && <span>已加载 {listMovies.items.length} 部 · 本地可播放 {localMovieCount} 部</span>}
            </div>
            {movieError && <div className="react-avdb-error"><p role="alert">{movieError}</p><button className="react-detail-action" disabled={movieBusy} onClick={() => void loadListMovies(movieAttempt, true)}>重试影片列表</button></div>}
            {movieBusy && <p className="react-avdb-state" role="status">正在读取清单影片…</p>}
            {listMovies && !listMovies.items.length && !listMovies.has_more && !movieBusy && !movieError && <p className="react-avdb-state">这份清单暂无影片。</p>}
            {listMovies?.items.length ? <div className="react-avdb-movies">{listMovies.items.map((movie) => <article key={movie.id} className="react-avdb-movie-card" data-avdb-movie={movie.id}>
              <Link className="react-avdb-movie-link" to={`/r18/${listMovies.library_id}/item/${movie.id}`} aria-label={`查看 ${movie.title || movie.number} 的 AVdb 详情`}>
                <span className="react-avdb-cover">{movie.cover_url ? <img src={movie.cover_url} loading="lazy" alt={`${movie.title || movie.number} 封面`} /> : <Film size={24} aria-hidden="true" />}
                  <span className={`react-avdb-local-badge${movie.has_local ? ' is-local' : ''}`} title={movie.has_local ? '本库已有可播放的 STRM 文件' : '本库没有可播放的 STRM 文件'}>
                    {movie.has_local && <Play size={11} fill="currentColor" aria-hidden="true" />}{movie.has_local ? '本地可播放' : '无本地 STRM'}
                  </span>
                </span>
                <div className="react-avdb-movie-copy"><span className="mono">{movie.number}</span><h4 title={movie.title || movie.number}>{movie.title || movie.number}</h4></div>
              </Link>
              {movie.has_local && <div className="react-avdb-local-actions">{movie.local_movies.map((local, index) => <button key={local.id} type="button" className="react-detail-action"
                data-local-movie={local.id} aria-label={`查看本库影片 ${local.title || local.number} 的详情`} title={local.title || local.number} onClick={() => onOpen(local.id)}>
                <Film size={13} aria-hidden="true" />本库详情{movie.local_movies.length > 1 ? ` ${index + 1}` : ''}
              </button>)}</div>}
            </article>)}</div> : null}
            {listMovies?.has_more && <button className="react-tv-load-more" disabled={movieBusy} onClick={() => void loadListMovies(listMovies.next_page)}>加载更多影片</button>}
          </section>}
          {lists && (lists.items.length || lists.page > 1) && <nav className="react-avdb-pagination" aria-label="AVdb 清单分页">
            <button className="react-detail-action" disabled={listBusy || lists.page <= 1} onClick={() => void loadLists(lists.page - 1)}>上一页清单</button>
            <span>第 {lists.page} 页{lists.total !== undefined ? ` · 共 ${lists.total} 个` : ''}</span>
            <button className="react-detail-action" disabled={listBusy || !lists.has_more} onClick={() => void loadLists(lists.page + 1)}>下一页清单</button>
          </nav>}
        </div>}
      </>}
    </section>
  )
}
