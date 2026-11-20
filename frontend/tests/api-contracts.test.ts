import { afterEach, describe, expect, it, vi } from 'vitest'
import { accountApi } from '../src/api/account'
import { parseKeys, parseProbes, parseSettings, parseTasks } from '../src/api/admin'
import { parseActors } from '../src/api/actors'
import { parseBackgroundProgress } from '../src/api/background'
import { apiClient } from '../src/api/client'
import { useReactAuthStore } from '../src/react/auth-store'
import { parseDetail, parseLibraries, parseSimilar, parseWall } from '../src/api/contracts'
import { parseDuplicateGroups, parseDuplicateSelection } from '../src/api/duplicates'
import { favoriteEntityId, favoriteDestination, favoritesApi, parseFavoriteItem } from '../src/api/favorites'
import { parseOfflineState } from '../src/api/cloud115'
import { parseConfigState } from '../src/api/system-config'
import { parseScheduled, scheduledApi } from '../src/api/scheduled'
import { parseInspect, parsePreview, parseScrapeSettings, scrapeApi } from '../src/api/scrape'
import { parseScanProgress } from '../src/api/tasks'
import { parseTelevision } from '../src/api/television'
import { parseJavdbListFavorites, parseJavdbListMovies, parseJavdbMatch, parseJavdbRelatedLists, parseJavdbReviews } from '../src/api/javdb-community'
import { parseR18Page, parseR18Tags, parseR18Video, r18Api, safeR18URL } from '../src/api/r18'
import { entityIdOf, numberUnlessInTitle, titleCarriesNumber } from '../src/lib/media'
import { manualPayload } from '../src/lib/manual'
import { readR18CatalogQuery, serializeR18CatalogQuery } from '../src/composables/r18-catalog'
import tasksFixture from './fixtures/json/tasks.json'
import probesFixture from './fixtures/json/probe.json'
import settingsFixture from './fixtures/json/settings.json'
import scrapeFixture from './fixtures/json/scrape-settings.json'
import scheduledFixture from './fixtures/json/scheduled.json'
import detailFixture from './fixtures/json/item-detail.json'
import wallFixture from './fixtures/json/items-wall.json'
import similarFixture from './fixtures/json/similar.json'
import authFixture from './fixtures/json/auth-login.json'
import runningScanFixture from './fixtures/json/scan-progress-running.json'
import ct01 from './fixtures/ct/ct-01-number.json'
import ct02 from './fixtures/ct/ct-02-entity-id.json'
import { configFixture } from './fixtures/system-config'

afterEach(() => {
  useReactAuthStore.setState({ token: null, user: null, phase: 'anonymous' })
  vi.restoreAllMocks()
})

describe('shared frontend API and DTO contracts', () => {
  it('parses management DTOs, accepts Go nil slices, and rejects malformed values', () => {
    expect(parseTasks(tasksFixture).items[0]?.type).toBe('scan')
    expect(parseProbes(probesFixture).items[0]?.path).toBe('/no-such-path')
    expect(parseSettings(settingsFixture).cache).toBe('redis')
    expect(parseKeys({ items: null, total: 0 }).items).toEqual([])
    for (const parse of [parseTasks, parseProbes, parseKeys, parseSettings]) expect(() => parse(null)).toThrow()
    expect(() => parseKeys({ items: [{ Key: 'wrong' }], total: 1 })).toThrow()
  })

  it('retains exact manual payload normalization and rejects unsafe media URLs', () => {
    const fields = { source_path: '/media/a.strm', source_url: 'https://example.com/a.mp4', title: '测试', year: '2024', genres: '剧情， 偶像, ', tags: 'a,b', studios: 'Studio' }
    const body = manualPayload(fields)
    expect(body).toMatchObject({ year: 2024, genres: ['剧情', '偶像'], tags: ['a', 'b'], studios: ['Studio'] })
    expect(body).not.toHaveProperty('library_id')
    expect(manualPayload({ ...fields, year: '', genres: '', tags: '', studios: '' }).year).toBe(0)
    expect(() => manualPayload({ source_path: '/a', source_url: 'ftp://example.com/a', title: 'a' })).toThrow('http/https')
    expect(() => manualPayload({ source_path: '/a', source_url: 'https://example.com/a', title: 'a', year: 'not-a-number' })).toThrow('年份')
  })

  it('keeps actor, background, scan, and scheduled DTOs strict around counters and optional values', () => {
    const actors = { items: [{ name: 'Example', movie_count: 2, has_image: false, image_tag: '', favorite: false }], total: 1, stats: { total: 1, ready: 0, missing: 1 } }
    expect(parseActors(actors)).toEqual(actors)
    expect(() => parseActors({ ...actors, items: [{ ...actors.items[0], has_image: 'true' }] })).toThrow()
    expect(() => parseActors({ ...actors, stats: { ...actors.stats, missing: -1 } })).toThrow()
    const idle = parseBackgroundProgress({ running: false, total: 0, done: 0, success: 0, skipped: 0, failed: 0, current: '', cancelled: false })
    expect(idle.failures).toEqual([])
    expect(() => parseBackgroundProgress({ ...idle, done: '4' })).toThrow()
    expect(() => parseBackgroundProgress({ ...idle, failures: [false] })).toThrow()
    expect(parseScanProgress(runningScanFixture).phase).toBe('process')
    expect(() => parseScanProgress({ ...runningScanFixture, running: 'false' })).toThrow()
    expect(parseScheduled(scheduledFixture).items).toEqual([])
    expect(() => parseScheduled({ ...scheduledFixture, items: [{ id: 1, name: 'Night', type: 'probe', cron: '@daily', params: '{}', enabled: 'false', next_run: '', last_status: '', last_run_at: '', last_message: '' }] })).toThrow()
  })

  it('keeps config, scrape, and offline response validation strict', () => {
    const config = configFixture()
    expect(parseConfigState(config).config.r18_preferred_cloud).toBe('guangya')
    expect(() => parseConfigState({ ...config, restart_fields: 'port' })).toThrow()
    expect(parseScrapeSettings(scrapeFixture).overwrite).toBe(false)
    const preview = { movie_id: 5, query: 'ABF-005', expected_number: 'ABF-005', recommended: 0, candidates: [{ provider: 'fanza', id: 'a', number: 'ABF-005', title: 'ABF-005 Alpha', score: 7.5, thumb: '', exact: true }] }
    expect(parsePreview(preview).candidates).toHaveLength(1)
    expect(() => parsePreview({ ...preview, recommended: 2 })).toThrow()
    const inspect = { provider: 'fanza', id: 'a', number: 'ABF-005', title: 'Alpha', summary: 'plot', poster: '', score: 7.5, release_date: '2024', runtime: 89, overwrite: false, fields: [], images: [] }
    expect(parseInspect(inspect).overwrite).toBe(false)
    expect(() => parseInspect({ ...inspect, overwrite: 'false' })).toThrow()
    const offline = { configured: true, tasks: [], sources: [] }
    expect(parseOfflineState(offline).tasks).toEqual([])
    expect(() => parseOfflineState({ ...offline, sources: [{ Id: 'unsafe', Name: 'movie.mkv', Container: 'mkv', DirectStreamUrl: 'https://evil.example/video.mkv' }] })).toThrow()
  })

  it('keeps duplicate reports bounded and refuses incomplete bulk selections', () => {
    const groups = { items: [{ key: 'ABF018', number: 'ABF-018', count: 2 }], total: 1, total_movies: 2, extra_copies: 1 }
    expect(parseDuplicateGroups(groups)).toEqual(groups)
    expect(() => parseDuplicateGroups({ ...groups, total: -1 })).toThrow()
    const keeper = { id: 10, library_id: 1, library_name: 'Main', number: 'ABF-018', title: 'Keep', source_path: '/films/keep.strm', status: 'success', additional_parts: [], created_at: '2024-01-01T00:00:00Z', path_length: 18 }
    const movie = { ...keeper, id: 11, title: 'Copy', source_path: '/films/copy.strm' }
    const selection = { rule: 'oldest', library_id: 0, items: [{ key: 'ABF018', movie, keep: keeper, selection_token: 'signed-selection' }], total: 1, groups: 1 }
    expect(parseDuplicateSelection(selection)).toEqual(selection)
    expect(() => parseDuplicateSelection({ ...selection, total: 100 })).toThrow()
    expect(() => parseDuplicateSelection({ ...selection, items: [{ ...selection.items[0], selection_token: '' }] })).toThrow()
  })

  it('preserves authenticated account, actor, and favorite API contracts', async () => {
    const account = { User: { ...authFixture.User, Policy: { ...authFixture.User.Policy, EnableAllFolders: false, EnabledFolders: [] } }, Libraries: [{ Id: '7', Name: 'Films', Type: 'movies' }] }
    const request = vi.spyOn(apiClient, 'request').mockResolvedValue(account)
    const signal = new AbortController().signal
    await accountApi.get(signal)
    expect(request).toHaveBeenLastCalledWith('/api/admin/account', { auth: 'required', signal })
    request.mockResolvedValue(null)
    await accountApi.savePolicy('1', { EnableAllFolders: false, EnabledFolders: [] })
    expect(request).toHaveBeenLastCalledWith('/Users/1/Policy', { auth: 'required', method: 'POST', json: { EnableAllFolders: false, EnabledFolders: [] } })
    useReactAuthStore.setState({ token: 'fixture-token', user: { ...authFixture.User, Policy: { ...authFixture.User.Policy, IsAdministrator: true } }, phase: 'authenticated' })
    request.mockResolvedValue({ ItemId: 'person:a/b', IsFavorite: true })
    expect(await favoritesApi.set('person:a/b', true)).toBe(true)
    expect(request).toHaveBeenLastCalledWith('/Users/1/FavoriteItems/person%3Aa%2Fb', { auth: 'required', method: 'POST' })
    expect(() => parseFavoriteItem({ Id: '5', Name: 'Movie', Type: 'Movie', ImageTags: {}, UserData: { IsFavorite: 'true' } })).toThrow()
    expect(favoriteDestination({ Id: favoriteEntityId('person', 'Name'), Name: 'Name', Type: 'Person', ImageTags: {}, UserData: { IsFavorite: false }, ParentId: '', SeriesId: '' })).toEqual({ path: '/items', query: { person: 'Name' } })
  })

  it('keeps television and media DTO validators strict', () => {
    expect(parseLibraries({ items: [{ Id: 1, Name: 'Library', Path: '/media', Type: 'tvshows' }], total: 1 }).items?.[0]?.Type).toBe('tvshows')
    expect(() => parseLibraries({ items: [{ Id: 1, Name: 'Library', Path: '/media', Type: 'music' }], total: 1 })).toThrow()
    const series = { Id: 'tv-series:2:L3Nob3c', Name: 'Drama', Type: 'Series', ImageTags: { Primary: 'cover' } }
    expect(() => parseTelevision({ Items: [{ ...series, Id: '5' }], TotalRecordCount: 1 })).toThrow()
    expect(parseDetail(detailFixture).files?.[0]?.streams[0]?.Type).toBe('Video')
    expect(parseWall(wallFixture).items.length).toBeGreaterThan(0)
    expect(parseSimilar(similarFixture)[0]?.Id).toBe('3')
    expect(() => parseDetail({ ...detailFixture, actors: [{ name: 'x', has_image: 'false' }] })).toThrow()
  })

  it('preserves CT-01/02 normalization and URL-safe route parameter behavior', () => {
    for (const item of ct01.cases) {
      expect(titleCarriesNumber(item.input.title, item.input.number)).toBe(item.expected_carries)
      expect(numberUnlessInTitle(item.input.title, item.input.number)).toBe(item.expected_carries ? '' : item.input.number)
    }
    for (const item of ct02.cases) expect(entityIdOf(item.input.kind, item.input.name)).toBe(item.expected)
    const query = readR18CatalogQuery({ mode: 'categories', type: '0', tag_ids: '0:23,0:1,0:23', main_filter: 'm,p,p,c', month: '6', duration: '90-120' })
    expect(serializeR18CatalogQuery(query)).toMatchObject({ tag_ids: '0:1,0:23', main_filter: 'c,m,p', month: '6', duration: '90-120' })
    expect(() => serializeR18CatalogQuery({ ...query, tag_ids: ['0:../'] })).toThrow()
  })

  it('keeps AVdb metadata, local list matches, provider URLs, and first-party routes validated', async () => {
    const video = {
      id: 'fixture', content_id: 'fixture', service_code: 'javdb', number: 'TEST-001', title: 'Fixture', original_title: '',
      release_date: '2026-01-01', cover_url: '/api/r18/libraries/2/videos/fixture/images/poster', images: [],
      duration: 90, score: 4, magnets_count: 8, has_local: true, tags: [], has_preview: false, preview_url: '',
    }
    expect(parseR18Video(video).rank).toBe(0)
    expect(parseR18Page({ items: [video], page: 1, page_size: 24, has_more: false, has_previous: false }).total).toBeNull()
    expect(() => parseR18Video({ ...video, has_local: 'true' })).toThrow()
    expect(() => parseR18Page({ items: [video], page: 0, page_size: 24, has_more: false, has_previous: false })).toThrow()
    expect(parseR18Tags({ categories: [{ id: 'main', name: '基本', type: '0', tags: [{ id: 'p', name: '可播放', type: '0' }] }] }).categories[0]?.tags[0]?.id).toBe('p')
    expect(() => parseR18Tags({ categories: [{ id: 'unsafe/path', name: '基本', type: '0', tags: [] }] })).toThrow()
    expect(safeR18URL('javascript:alert(1)')).toBe('')
    expect(safeR18URL('https://user:pass@example.test/video')).toBe('')

    const page = { number: 'TEST-001', video_id: 'fixture', library_id: 2, page: 1, page_size: 15, has_more: false }
    expect(parseJavdbMatch({ status: 'matched', ...page, library_name: 'AVdb', video: { id: 'fixture', number: 'TEST-001', title: 'Fixture' } }).video?.id).toBe('fixture')
    expect(parseJavdbReviews({ ...page, items: [{ id: 'review-1', username: 'User', content: 'Review', score: '4.5', status: '看过', created_at: '2026-11-18' }] }).items[0]?.score).toBe('4.5')
    expect(parseJavdbRelatedLists({ ...page, items: [{ id: 'list-1', name: '精选清单', movie_count: 3, favorite: false }] }).items[0]?.favorite).toBe(false)
    expect(parseJavdbListFavorites({ items: [{ library_id: 2, movie_id: 5, list_id: 'list-1', related_page: 1, name: '精选清单' }], total: 1 }).total).toBe(1)
    const movies = parseJavdbListMovies({ ...page, items: [{ id: 'film-1', number: 'TEST-001', title: 'Fixture', cover_url: video.cover_url, has_local: true, local_movies: [{ id: 7, number: 'TEST-001', title: '本库影片' }] }], list_id: 'list-1', local_library_id: 1, next_page: 2, has_more: false })
    expect(movies.items[0]?.local_movies[0]?.id).toBe(7)
    expect(() => parseJavdbListMovies({ ...movies, items: [{ ...movies.items[0], has_local: false }] })).toThrow()

    const request = vi.spyOn(apiClient, 'request').mockResolvedValue({ categories: [] })
    const signal = new AbortController().signal
    await r18Api.tags('2', 'all', signal)
    expect(request).toHaveBeenCalledExactlyOnceWith('/api/r18/libraries/2/tags?type=all', expect.objectContaining({ auth: 'required', signal, timeoutMs: 22000 }))
  })

  it('keeps provider reads marked read-only and sends explicit false only on writes', async () => {
    const request = vi.spyOn(apiClient, 'request').mockResolvedValue({ valid: true, next_runs: [] })
    await scheduledApi.validate('@daily', new AbortController().signal)
    expect(request.mock.lastCall?.[1]).toMatchObject({ method: 'POST', readOnly: true, json: { cron: '@daily' } })
    const inspect = { provider: 'fanza', id: 'a', number: 'ABF-005', title: 'Alpha', summary: '', poster: '', score: 0, release_date: '', runtime: 0, overwrite: false, fields: [], images: [] }
    request.mockResolvedValue(inspect)
    await scrapeApi.inspect(5, 'fanza', 'a', new AbortController().signal)
    expect(request.mock.lastCall?.[1]).toMatchObject({ method: 'POST', readOnly: true })
    request.mockResolvedValue({ title: 'saved', images: 0 })
    await scrapeApi.confirm(5, 'fanza', 'a', false)
    expect(request.mock.lastCall?.[1]).toMatchObject({ json: { provider: 'fanza', id: 'a', overwrite: false }, readOnly: false })
  })

  it('keeps malformed scan values rejected', () => {
    expect(() => parseScanProgress({ running: false, done: '1' })).toThrow()
  })
})
