import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, Link, Outlet, RouterProvider } from 'react-router-dom'
import { ApiError } from '../src/api/client'
import { accountApi } from '../src/api/account'
import { adminApi } from '../src/api/admin'
import { actorsApi } from '../src/api/actors'
import { favoritesApi } from '../src/api/favorites'
import type { LibraryDto, UserDto } from '../src/api/contracts'
import { cloud115Api } from '../src/api/cloud115'
import { duplicatesApi } from '../src/api/duplicates'
import { javdbCommunityApi, type JavdbListFavorite } from '../src/api/javdb-community'
import { guangyaApi } from '../src/api/guangya'
import { r18Api } from '../src/api/r18'
import { parseScrapeSettings, scrapeApi } from '../src/api/scrape'
import { scheduledApi, type ScheduledTask } from '../src/api/scheduled'
import { systemConfigApi } from '../src/api/system-config'
import { usersApi, type ManagedUser } from '../src/api/users'
import { AppProviders } from '../src/react/AppProviders'
import { useReactAuthStore } from '../src/react/auth-store'
import { AccountPage, UsersPage } from '../src/react/pages/identity-pages'
import { LibrariesPage, SettingsPage } from '../src/react/pages/library-settings-pages'
import { ApiKeysPage, FavoritesPage, ManualPage, ProbePage, TasksPage } from '../src/react/pages/basic-pages'
import { ActorsPage, ScheduledPage, ScrapePage } from '../src/react/pages/operations-pages'
import { useReactMediaWallStore } from '../src/react/media-wall-store'
import { useReactScheduledStore } from '../src/react/scheduled-store'
import { useReactTasksStore } from '../src/react/tasks-store'
import { configFixture } from './fixtures/system-config'
import authFixture from './fixtures/json/auth-login.json'
import settingsFixture from './fixtures/json/settings.json'
import scrapeFixture from './fixtures/json/scrape-settings.json'
import scheduledFixture from './fixtures/json/scheduled.json'

const adminUser = authFixture.User as UserDto
const viewerUser: UserDto = { ...adminUser, Id: '7', Name: 'viewer', Policy: { IsAdministrator: false } }

const library: LibraryDto = {
  Id: 7, Name: 'Films', Path: '/media/one', Paths: ['/media/one', '/media/two'], Type: 'movies',
  SourceType: 'nfo_directory', ProxyEnabled: false, ResolveRedirects: false, Revision: 3, ProxyHeaders: {},
}

function managedUser(id: string, name: string, administrator: boolean): ManagedUser {
  return {
    ...adminUser,
    Id: id,
    Name: name,
    Policy: {
      ...adminUser.Policy,
      IsAdministrator: administrator,
      IsDisabled: false,
      EnableAllFolders: administrator,
      EnabledFolders: [],
    },
  }
}

function Root() {
  return <><Link to="/target">打开目标页</Link><Outlet /></>
}

const pageRoutes = [
  { path: '/account', element: <AccountPage /> },
  { path: '/users', element: <UsersPage /> },
  { path: '/libraries', element: <LibrariesPage /> },
  { path: '/settings', element: <SettingsPage /> },
  { path: '/manual', element: <ManualPage /> },
  { path: '/favorites', element: <FavoritesPage /> },
  { path: '/actors', element: <ActorsPage /> },
  { path: '/scheduled', element: <ScheduledPage /> },
  { path: '/scrape', element: <ScrapePage /> },
  { path: '/apikeys', element: <ApiKeysPage /> },
  { path: '/probe', element: <ProbePage /> },
  { path: '/tasks', element: <TasksPage /> },
  { path: '/login', element: <p>登录页</p> },
  { path: '/target', element: <p>目标页</p> },
]

const views: Array<ReturnType<typeof render>> = []
const routers: Array<ReturnType<typeof createMemoryRouter>> = []

function renderPage(path: string) {
  const router = createMemoryRouter([{ element: <Root />, children: pageRoutes }], { initialEntries: [path] })
  routers.push(router)
  const view = render(<AppProviders><RouterProvider router={router} /></AppProviders>)
  views.push(view)
  return { router, view }
}

function signIn(user: UserDto = adminUser) {
  useReactAuthStore.setState({ token: 'fixture-token', user, phase: 'authenticated' })
}

function accountFixture() {
  return {
    user: adminUser,
    policy: { EnableAllFolders: true, EnabledFolders: [] as string[] },
    libraries: [{ Id: '7', Name: 'Films', Type: 'movies' }],
  }
}

afterEach(() => {
  views.splice(0).forEach(view => view.unmount())
  routers.splice(0).forEach(router => router.dispose())
  useReactAuthStore.setState({ token: null, user: null, initialized: null, phase: 'anonymous', error: '', submitting: false, expiration: 0 })
  useReactMediaWallStore.getState().clear()
  useReactScheduledStore.getState().reset()
  useReactTasksStore.getState().reset()
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('React account and user pages', () => {
  it('saves explicit library grants locally and asks before abandoning account drafts', async () => {
    const user = userEvent.setup()
    vi.spyOn(accountApi, 'get').mockResolvedValue(accountFixture())
    const save = vi.spyOn(accountApi, 'savePolicy').mockResolvedValue(null)
    const clearWall = vi.spyOn(useReactMediaWallStore.getState(), 'clear')
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    signIn()
    renderPage('/account')

    await screen.findByRole('heading', { name: '媒体库访问权限' })
    await user.click(screen.getByLabelText('允许访问所有媒体库'))
    await user.click(screen.getByLabelText('Films'))
    await user.click(screen.getByRole('button', { name: '保存权限' }))

    await waitFor(() => expect(save).toHaveBeenCalledOnce())
    expect(save).toHaveBeenCalledWith('1', { EnableAllFolders: false, EnabledFolders: ['7'] })
    expect(accountApi.get).toHaveBeenCalledOnce()
    expect(clearWall).toHaveBeenCalledOnce()
    expect(screen.getByRole('button', { name: '保存权限' })).toBeDisabled()

    await user.clear(screen.getByLabelText('用户名'))
    await user.type(screen.getByLabelText('用户名'), 'changed')
    await user.click(screen.getByRole('link', { name: '打开目标页' }))
    expect(confirm).toHaveBeenCalledWith('放弃未保存的账号设置？')
    expect(screen.queryByText('目标页')).not.toBeInTheDocument()

    confirm.mockReturnValue(true)
    await user.click(screen.getByRole('link', { name: '打开目标页' }))
    expect(await screen.findByText('目标页')).toBeInTheDocument()
  })

  it('updates a selected user permission without an extra list read and confirms switching a dirty user', async () => {
    const user = userEvent.setup()
    const list = vi.spyOn(usersApi, 'list').mockResolvedValue({
      users: [managedUser('1', 'admin', true), managedUser('2', 'viewer', false)],
      libraries: [{ Id: '7', Name: 'Films', Type: 'movies' }],
    })
    const save = vi.spyOn(usersApi, 'savePolicy').mockResolvedValue(null)
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    signIn()
    renderPage('/users')

    await screen.findByRole('button', { name: /viewer/ })
    await user.click(screen.getByRole('button', { name: /viewer/ }))
    await user.click(screen.getByLabelText('Films'))
    await user.click(screen.getByRole('button', { name: '保存权限' }))

    await waitFor(() => expect(save).toHaveBeenCalledOnce())
    expect(save).toHaveBeenCalledWith('2', { EnableAllFolders: false, EnabledFolders: ['7'] })
    expect(list).toHaveBeenCalledOnce()
    expect(screen.getByRole('button', { name: '保存权限' })).toBeDisabled()

    await user.click(screen.getByLabelText('禁用账号'))
    await user.click(screen.getByRole('button', { name: /admin/ }))
    expect(confirm).toHaveBeenCalledWith('放弃未保存的用户设置？')
    expect(screen.getByRole('button', { name: /viewer/ })).toHaveAttribute('aria-pressed', 'true')
    confirm.mockReturnValue(true)
    await user.click(screen.getByRole('button', { name: /admin/ }))
    expect(screen.getByRole('button', { name: /admin/ })).toHaveAttribute('aria-pressed', 'true')
  })

  it('validates account password confirmation, retains failed credentials and signs out after success', async () => {
    const user = userEvent.setup()
    vi.spyOn(accountApi, 'get').mockResolvedValue(accountFixture())
    const update = vi.spyOn(accountApi, 'update').mockRejectedValueOnce(new Error('wrong password')).mockResolvedValueOnce(null)
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    signIn()
    renderPage('/account')

    const username = await screen.findByRole('textbox', { name: '用户名' })
    await waitFor(() => expect(username).toHaveValue(adminUser.Name))
    await user.clear(username)
    await waitFor(() => expect(username).toHaveValue(''))
    await user.type(username, 'owner')
    expect(username).toHaveValue('owner')
    await user.type(screen.getByLabelText('当前密码'), 'current-password')
    await user.type(screen.getByLabelText('新密码（可选）'), 'new-password-123')
    await user.type(screen.getByLabelText('确认新密码'), 'different-password')
    await user.click(screen.getByRole('button', { name: '保存账号' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('两次输入的新密码不一致')
    expect(update).not.toHaveBeenCalled()

    await user.clear(screen.getByLabelText('确认新密码'))
    await user.type(screen.getByLabelText('确认新密码'), 'new-password-123')
    await user.click(screen.getByRole('button', { name: '保存账号' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('wrong password')
    expect(screen.getByLabelText('当前密码')).toHaveValue('current-password')
    expect(screen.getByLabelText('新密码（可选）')).toHaveValue('new-password-123')

    await user.click(screen.getByRole('button', { name: '保存账号' }))
    expect(await screen.findByText('登录页')).toBeInTheDocument()
    expect(update).toHaveBeenLastCalledWith({ Username: 'owner', CurrentPassword: 'current-password', NewPassword: 'new-password-123' })
  })

  it('validates user creation, keeps failed input and selects the created account', async () => {
    const user = userEvent.setup()
    const created = managedUser('3', 'family', false)
    const list = vi.spyOn(usersApi, 'list')
      .mockResolvedValueOnce({ users: [managedUser('1', 'admin', true), managedUser('2', 'viewer', false)], libraries: [] })
      .mockResolvedValue({ users: [managedUser('1', 'admin', true), managedUser('2', 'viewer', false), created], libraries: [] })
    const create = vi.spyOn(usersApi, 'create').mockRejectedValueOnce(new Error('用户名已存在')).mockResolvedValueOnce(created)
    signIn()
    renderPage('/users')

    await user.click(await screen.findByRole('button', { name: '新增用户' }))
    await user.type(screen.getByRole('textbox', { name: '用户名' }), 'family')
    await user.type(screen.getByLabelText('密码'), 'password-1234')
    await user.type(screen.getByLabelText('确认密码'), 'different')
    await user.click(screen.getByRole('button', { name: '创建用户' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('两次输入的密码不一致')
    expect(create).not.toHaveBeenCalled()

    await user.clear(screen.getByLabelText('确认密码'))
    await user.type(screen.getByLabelText('确认密码'), 'password-1234')
    await user.click(screen.getByRole('button', { name: '创建用户' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('用户名已存在')
    expect(screen.getByRole('textbox', { name: '用户名' })).toHaveValue('family')
    expect(screen.getByLabelText('密码')).toHaveValue('password-1234')

    await user.click(screen.getByRole('button', { name: '创建用户' }))
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2))
    expect(create).toHaveBeenLastCalledWith('family', 'password-1234')
    expect(screen.getByRole('button', { name: /family/ })).toHaveAttribute('aria-pressed', 'true')
  })

  it('updates and deletes another user while preserving the administrator session', async () => {
    const user = userEvent.setup()
    const list = vi.spyOn(usersApi, 'list')
      .mockResolvedValueOnce({ users: [managedUser('1', 'admin', true), managedUser('2', 'viewer', false)], libraries: [] })
      .mockResolvedValueOnce({ users: [managedUser('1', 'admin', true)], libraries: [] })
    const update = vi.spyOn(usersApi, 'update').mockResolvedValue(undefined)
    const remove = vi.spyOn(usersApi, 'remove').mockResolvedValue(undefined)
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    signIn()
    renderPage('/users')

    await user.click(await screen.findByRole('button', { name: /viewer/ }))
    await user.click(screen.getByLabelText('禁用账号'))
    await user.click(screen.getByRole('button', { name: '保存账号' }))
    await waitFor(() => expect(update).toHaveBeenCalledWith('2', { Username: 'viewer', NewPassword: '', IsDisabled: true }))
    expect(confirm).toHaveBeenCalledWith('保存后，viewer 的已登录会话将失效。继续？')
    expect(useReactAuthStore.getState().token).toBe('fixture-token')

    await user.click(screen.getByRole('button', { name: '删除用户' }))
    await waitFor(() => expect(remove).toHaveBeenCalledWith('2'))
    expect(confirm).toHaveBeenLastCalledWith('删除用户 viewer 及其收藏和播放记录？媒体文件不会删除。')
    expect(await screen.findByRole('button', { name: /admin/ })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /viewer/ })).not.toBeInTheDocument()
    expect(list).toHaveBeenCalledTimes(2)
  })
})

describe('React library and manual pages', () => {
  it('confirms index-removing directory edits and preserves a draft across revision conflicts', async () => {
    const user = userEvent.setup()
    const readLibraries = vi.spyOn(adminApi, 'libraries')
      .mockResolvedValueOnce({ items: [library], total: 1 })
      .mockResolvedValue({ items: [{ ...library, Revision: 4 }], total: 1 })
    const update = vi.spyOn(adminApi, 'updateLibrary')
      .mockRejectedValueOnce(new ApiError('媒体库已被修改', 409))
      .mockResolvedValueOnce({ ...library, Name: 'Draft', Revision: 5 })
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    signIn()
    renderPage('/libraries')

    await user.click(await screen.findByRole('button', { name: '编辑' }))
    expect(readLibraries).toHaveBeenCalledOnce()
    await user.clear(screen.getByRole('textbox', { name: '名称' }))
    await user.type(screen.getByRole('textbox', { name: '名称' }), 'Draft')
    await user.click(screen.getByRole('button', { name: '移除目录 2' }))
    await user.click(screen.getByRole('button', { name: '保存修改' }))
    expect(confirm).toHaveBeenCalledWith('移除目录会清除不再属于此媒体库的影片索引及其播放进度/收藏。不会删除磁盘文件，确认保存？')
    expect(update).not.toHaveBeenCalled()

    confirm.mockReturnValue(true)
    await user.click(screen.getByRole('button', { name: '保存修改' }))
    await waitFor(() => expect(update).toHaveBeenCalledOnce())
    expect(update.mock.calls[0]?.[1]).toMatchObject({ Name: 'Draft', Paths: ['/media/one'], Revision: 3 })
    expect(await screen.findByText(/核对当前草稿/)).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '名称' })).toHaveValue('Draft')

    await user.click(screen.getByRole('button', { name: '保存修改' }))
    await waitFor(() => expect(update).toHaveBeenCalledTimes(2))
    expect(update.mock.calls[1]?.[1]).toMatchObject({ Name: 'Draft', Paths: ['/media/one'], Revision: 4 })
  })

  it('creates a mixed media library with proxy settings and sanitized path inputs', async () => {
    const user = userEvent.setup()
    const created: LibraryDto = {
      ...library, Name: 'Mixed library', Path: '/media/mixed', Paths: ['/media/mixed'], Type: 'mixed',
      ProxyEnabled: true, ResolveRedirects: true, ProxyHeaders: { Referer: 'https://origin.example/' },
    }
    vi.spyOn(adminApi, 'libraries')
      .mockResolvedValueOnce({ items: [], total: 0 })
      .mockResolvedValue({ items: [created], total: 1 })
    const create = vi.spyOn(adminApi, 'createLibrary').mockResolvedValue(created)
    signIn()
    renderPage('/libraries')

    await user.click(await screen.findByRole('button', { name: '添加媒体库' }))
    await user.selectOptions(screen.getByRole('combobox', { name: '内容类型' }), 'mixed')
    await user.type(screen.getByRole('textbox', { name: '名称' }), ' Mixed library ')
    await user.type(screen.getByRole('textbox', { name: '目录路径' }), ' /media/mixed ')
    await user.click(screen.getByLabelText('解析上游 302 直链'))
    await user.click(screen.getByLabelText('由 Emby-go 代理媒体流'))
    fireEvent.change(screen.getByRole('textbox', { name: '播放源请求头（JSON）' }), { target: { value: '{"Referer":"https://origin.example/"}' } })
    const submit = screen.getAllByRole('button', { name: '添加媒体库' }).at(-1)
    expect(submit).toBeDefined()
    await user.click(submit!)

    await waitFor(() => expect(create).toHaveBeenCalledOnce())
    expect(create).toHaveBeenCalledWith({
      Name: 'Mixed library', Paths: ['/media/mixed'], Type: 'mixed', ProxyEnabled: true,
      ResolveRedirects: true, ProxyHeaders: { Referer: 'https://origin.example/' },
    })
    expect(await screen.findAllByText('Mixed library')).toHaveLength(2)
  })

  it('does not delete an index until confirmed, then refreshes the library list', async () => {
    const user = userEvent.setup()
    vi.spyOn(adminApi, 'libraries')
      .mockResolvedValueOnce({ items: [library], total: 1 })
      .mockResolvedValue({ items: [], total: 0 })
    const remove = vi.spyOn(adminApi, 'deleteLibrary').mockResolvedValue(null)
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValue(true)
    signIn()
    renderPage('/libraries')

    const removeButton = await screen.findByRole('button', { name: '删除 Films' })
    await user.click(removeButton)
    expect(confirm).toHaveBeenCalledWith('删除媒体库「Films」及其影片索引？不会删除磁盘文件，但会清除该库的播放进度和收藏。')
    expect(remove).not.toHaveBeenCalled()

    await user.click(removeButton)
    await waitFor(() => expect(remove).toHaveBeenCalledWith(library.Id))
    await waitFor(() => expect(screen.queryByRole('button', { name: '删除 Films' })).not.toBeInTheDocument())
  })

  it('saves a playback proxy toggle with the current library revision', async () => {
    const user = userEvent.setup()
    vi.spyOn(adminApi, 'libraries')
      .mockResolvedValueOnce({ items: [library], total: 1 })
      .mockResolvedValue({ items: [{ ...library, ProxyEnabled: true }], total: 1 })
    const update = vi.spyOn(adminApi, 'updateLibrary').mockResolvedValue({ ...library, ProxyEnabled: true })
    signIn()
    renderPage('/libraries')

    const toggle = await screen.findByRole('checkbox', { name: '播放代理' })
    await user.click(toggle)
    await waitFor(() => expect(update).toHaveBeenCalledOnce())
    expect(update).toHaveBeenCalledWith(library.Id, { ProxyEnabled: true, ResolveRedirects: false, Revision: library.Revision })
    await waitFor(() => expect(screen.getByRole('checkbox', { name: '播放代理' })).toBeChecked())
  })

  it('saves upstream redirect resolution with the current library revision', async () => {
    const user = userEvent.setup()
    vi.spyOn(adminApi, 'libraries')
      .mockResolvedValueOnce({ items: [library], total: 1 })
      .mockResolvedValue({ items: [{ ...library, ResolveRedirects: true }], total: 1 })
    const update = vi.spyOn(adminApi, 'updateLibrary').mockResolvedValue({ ...library, ResolveRedirects: true })
    signIn()
    renderPage('/libraries')

    await user.click(await screen.findByRole('checkbox', { name: '解析直链' }))
    await waitFor(() => expect(update).toHaveBeenCalledOnce())
    expect(update).toHaveBeenCalledWith(library.Id, { ProxyEnabled: false, ResolveRedirects: true, Revision: library.Revision })
    await waitFor(() => expect(screen.getByRole('checkbox', { name: '解析直链' })).toBeChecked())
  })

  it('keeps manual entry fields after a failed write and clears them after success', async () => {
    const user = userEvent.setup()
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [library], total: 1 })
    const write = vi.spyOn(adminApi, 'manual').mockRejectedValueOnce(new Error('write failed')).mockResolvedValueOnce({ id: 5, status: 'success' })
    signIn()
    renderPage('/manual')

    await user.type(await screen.findByLabelText('标题 *'), 'Manual film')
    await user.type(screen.getByLabelText('番号'), 'ABF-018')
    await user.type(screen.getByLabelText('年份'), '2024')
    await user.type(screen.getByLabelText('原名'), 'Original film')
    await user.type(screen.getByLabelText('.strm 目标路径 *'), '/media/Manual.strm')
    await user.type(screen.getByLabelText('媒体直链（http/https）*'), 'https://media.example/video.mp4')
    await user.type(screen.getByLabelText('简介'), 'A short synopsis.')
    await user.type(screen.getByLabelText('类型（逗号分隔）'), '剧情， 偶像, ')
    await user.type(screen.getByLabelText('标签（逗号分隔）'), '精品, 测试')
    await user.type(screen.getByLabelText('制作商'), 'Studio One, Studio Two')
    await user.click(screen.getByRole('button', { name: '写入并入库' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('write failed')
    expect(screen.getByLabelText('标题 *')).toHaveValue('Manual film')
    expect(write).toHaveBeenCalledOnce()

    await user.click(screen.getByRole('button', { name: '写入并入库' }))
    await waitFor(() => expect(write).toHaveBeenCalledTimes(2))
    expect(write).toHaveBeenNthCalledWith(2, {
      title: 'Manual film',
      number: 'ABF-018',
      year: 2024,
      original_title: 'Original film',
      source_path: '/media/Manual.strm',
      source_url: 'https://media.example/video.mp4',
      plot: 'A short synopsis.',
      genres: ['剧情', '偶像'],
      tags: ['精品', '测试'],
      studios: ['Studio One', 'Studio Two'],
    })
    expect(await screen.findByText('「Manual film」已入库')).toBeInTheDocument()
    expect(screen.getByLabelText('标题 *')).toHaveValue('')
  })

  it('rejects a non-http media URL in the manual form before writing', async () => {
    const user = userEvent.setup()
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [library], total: 1 })
    const write = vi.spyOn(adminApi, 'manual')
    signIn()
    renderPage('/manual')

    await user.type(await screen.findByLabelText('标题 *'), 'Manual film')
    await user.type(screen.getByLabelText('.strm 目标路径 *'), '/media/Manual.strm')
    await user.type(screen.getByLabelText('媒体直链（http/https）*'), 'ftp://media.example/video.mp4')
    await user.click(screen.getByRole('button', { name: '写入并入库' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('媒体直链必须为 http/https')
    expect(write).not.toHaveBeenCalled()
    expect(screen.getByLabelText('标题 *')).toHaveValue('Manual film')
  })

  it('reopens and removes AVdb list favorites without querying media favorites', async () => {
    const user = userEvent.setup()
    const link: JavdbListFavorite = { library_id: 2, movie_id: 5, list_id: 'list_1', related_page: 3, name: '精选清单', movie_count: 24 }
    vi.spyOn(javdbCommunityApi, 'listFavorites').mockResolvedValue({ items: [link], total: 1 })
    const remove = vi.spyOn(javdbCommunityApi, 'removeListFavorite').mockResolvedValue()
    const mediaFavorites = vi.spyOn(favoritesApi, 'list')
    signIn()
    renderPage('/favorites?type=JavDB')

    await screen.findByText('精选清单')
    const listLink = screen.getByRole('link', { name: /精选清单/ })
    expect(listLink.getAttribute('href')).toContain('/item/5?javdb_library_id=2&javdb_list_id=list_1&javdb_related_page=3')
    expect(mediaFavorites).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: '取消收藏清单 · 精选清单' }))
    await waitFor(() => expect(remove).toHaveBeenCalledWith(2, 'list_1'))
  })
})

describe('React system configuration', () => {
  function mockSettings() {
    vi.spyOn(adminApi, 'settings').mockResolvedValue(settingsFixture)
    vi.spyOn(systemConfigApi, 'read').mockResolvedValue(configFixture())
  }

  it('sends only changed fields and reports the saved restart state', async () => {
    const user = userEvent.setup()
    mockSettings()
    const next = configFixture()
    next.config.server_name = 'New archive'
    next.effective.server_name = 'New archive'
    next.revision = 'rev-2'
    next.restart_fields = ['port']
    const save = vi.spyOn(systemConfigApi, 'save').mockResolvedValue(next)
    signIn()
    renderPage('/settings')

    const serverName = await screen.findByRole('textbox', { name: '服务名称' })
    await waitFor(() => expect(serverName).toHaveValue('Emby-go'))
    await user.clear(serverName)
    await waitFor(() => expect(serverName).toHaveValue(''))
    await user.type(serverName, 'New archive')
    await user.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(save).toHaveBeenCalledOnce())
    expect(save).toHaveBeenCalledWith('rev-1', { server_name: 'New archive' })
    expect(await screen.findByText('配置已保存。等待重启：监听端口。')).toBeInTheDocument()
    expect(await screen.findByText('与已保存配置一致')).toBeInTheDocument()
  })

  it('restores the draft and confirms before reloading configuration', async () => {
    const user = userEvent.setup()
    mockSettings()
    const read = vi.mocked(systemConfigApi.read)
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    signIn()
    renderPage('/settings')

    const name = await screen.findByRole('textbox', { name: '服务名称' })
    await user.clear(name)
    await user.type(name, 'draft')
    await user.click(screen.getByRole('button', { name: '恢复已保存设置' }))
    expect(name).toHaveValue('Emby-go')

    await user.clear(name)
    await user.type(name, 'discard me')
    await user.click(screen.getByRole('button', { name: '重新加载文件' }))
    expect(confirm).toHaveBeenCalledWith('重新加载会丢弃未保存的修改，是否继续？')
    expect(read).toHaveBeenCalledOnce()
    expect(name).toHaveValue('discard me')

    const reloaded = configFixture()
    reloaded.config.server_name = 'From file'
    reloaded.revision = 'rev-3'
    confirm.mockReturnValue(true)
    read.mockResolvedValueOnce(reloaded)
    await user.click(screen.getByRole('button', { name: '重新加载文件' }))
    await waitFor(() => expect(screen.getByRole('textbox', { name: '服务名称' })).toHaveValue('From file'))
    expect(read).toHaveBeenCalledTimes(2)
  })

  it('preserves explicit secret text and sends structured domain edits as patches', async () => {
    const user = userEvent.setup()
    mockSettings()
    const save = vi.spyOn(systemConfigApi, 'save').mockResolvedValue(configFixture())
    signIn()
    renderPage('/settings')

    await user.type(await screen.findByLabelText('Redis 密码'), ' secret ')
    await user.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(save).toHaveBeenCalledOnce())
    expect(save).toHaveBeenCalledWith('rev-1', { redis_password: ' secret ' })
  })

  it('keeps a 115 cookie draft after a rejected save without rendering it as text', async () => {
    const user = userEvent.setup()
    mockSettings()
    const cloudConfig = { configured: false, account_id: '', folder_id: '', folder_name: '', revision: 0 }
    vi.spyOn(cloud115Api, 'config').mockResolvedValue(cloudConfig)
    vi.spyOn(guangyaApi, 'config').mockResolvedValue(cloudConfig)
    const save = vi.spyOn(cloud115Api, 'save').mockRejectedValue(new Error('connection rejected'))
    signIn()
    renderPage('/settings')

    const cookie = await screen.findByLabelText('115 Cookie')
    expect(cookie).toHaveAttribute('type', 'password')
    await user.type(cookie, 'UID=fixture; CID=fixture; SEID=fixture')
    await user.click(screen.getByRole('button', { name: /验证并保存 115连接/ }))

    await waitFor(() => expect(save).toHaveBeenCalledOnce())
    expect(save).toHaveBeenCalledWith({ cookie: 'UID=fixture; CID=fixture; SEID=fixture', revision: 0 })
    expect(await screen.findByRole('alert')).toHaveTextContent('connection rejected')
    expect(cookie).toHaveValue('UID=fixture; CID=fixture; SEID=fixture')
    expect(document.body.textContent).not.toContain('UID=fixture; CID=fixture; SEID=fixture')
  })

  it('trims and preserves Guangya credentials after a rejected connection save', async () => {
    const user = userEvent.setup()
    mockSettings()
    const cloudConfig = { configured: false, account_id: '', folder_id: '', folder_name: '', revision: 0 }
    vi.spyOn(cloud115Api, 'config').mockResolvedValue(cloudConfig)
    vi.spyOn(guangyaApi, 'config').mockResolvedValue(cloudConfig)
    const save = vi.spyOn(guangyaApi, 'save').mockRejectedValue(new Error('connection rejected'))
    signIn()
    renderPage('/settings')

    const accessToken = await screen.findByLabelText('访问令牌')
    await user.type(accessToken, ' fixture-access ')
    await user.type(screen.getByLabelText('刷新令牌'), ' fixture-refresh ')
    await user.type(screen.getByLabelText('设备 ID'), ' fixture-device ')
    await user.click(screen.getByRole('button', { name: '验证并保存光鸭连接' }))

    await waitFor(() => expect(save).toHaveBeenCalledOnce())
    expect(save).toHaveBeenCalledWith({
      access_token: 'fixture-access', refresh_token: 'fixture-refresh', device_id: 'fixture-device', revision: 0,
    })
    expect(await screen.findByRole('alert')).toHaveTextContent('connection rejected')
    expect(accessToken).toHaveValue(' fixture-access ')
    expect(accessToken).toHaveAttribute('type', 'password')
  })
})

describe('React actor and scheduled-task pages', () => {
  it('keeps actor imports read-only until the application task is explicitly started', async () => {
    const user = userEvent.setup()
    const settings = parseScrapeSettings(scrapeFixture)
    vi.spyOn(scrapeApi, 'settings').mockResolvedValue(settings)
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [library], total: 1 })
    vi.spyOn(actorsApi, 'list').mockResolvedValue({
      items: [{ name: 'Fixture Actor', movie_count: 2, has_image: false, image_tag: '', favorite: false }],
      total: 1, stats: { total: 1, ready: 0, missing: 1 },
    })
    const preview = vi.spyOn(scrapeApi, 'importPreview').mockResolvedValue({
      total: 4, ready: 1, existing: 1, conflicts: 1, damaged: 1, missing: 0,
      samples: ['/actors/Fixture Actor.jpg'], source: '/old', destination: '/new',
    })
    const run = vi.spyOn(useReactTasksStore.getState().scrape, 'run').mockResolvedValue(true)
    signIn()
    renderPage('/actors')

    await screen.findByText('Fixture Actor')
    await user.click(screen.getByText('头像来源与存储设置'))
    await user.click(screen.getByRole('button', { name: '检查可导入头像' }))
    expect(await screen.findByText(/待导入 1 · 已存在 1 · 冲突 1/)).toBeInTheDocument()
    expect(preview).toHaveBeenCalledWith(expect.any(AbortSignal))
    expect(run).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: '开始复制导入' }))
    expect(run).toHaveBeenCalledWith({}, 'import')
  })

  it('loads the next actor page using the selected server offset', async () => {
    const user = userEvent.setup()
    const settings = parseScrapeSettings(scrapeFixture)
    vi.spyOn(scrapeApi, 'settings').mockResolvedValue(settings)
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [library], total: 1 })
    const actors = vi.spyOn(actorsApi, 'list').mockResolvedValue({
      items: [{ name: 'Fixture Actor', movie_count: 2, has_image: false, image_tag: '', favorite: false }],
      total: 49, stats: { total: 49, ready: 0, missing: 49 },
    })
    signIn()
    renderPage('/actors')

    await screen.findByText('Fixture Actor')
    await user.click(screen.getByRole('button', { name: '下一页' }))
    await waitFor(() => expect(actors).toHaveBeenCalledTimes(2))
    expect(actors.mock.calls[1]?.[0]).toMatchObject({ library_id: 0, offset: 48, limit: 48 })
  })

  it('loads the actor query once, reapplies filters and preserves avatar settings after a failed save', async () => {
    const user = userEvent.setup()
    const settings = parseScrapeSettings(scrapeFixture)
    const actorList = vi.spyOn(actorsApi, 'list').mockResolvedValue({
      items: [{ name: 'Fixture Actor', movie_count: 3, has_image: false, image_tag: '', favorite: false }],
      total: 1,
      stats: { total: 1, ready: 0, missing: 1 },
    })
    vi.spyOn(scrapeApi, 'settings').mockResolvedValue(settings)
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [library], total: 1 })
    const save = vi.spyOn(scrapeApi, 'save').mockRejectedValue(new Error('settings unavailable'))
    signIn()
    renderPage('/actors')

    await screen.findByText('Fixture Actor')
    expect(actorList).toHaveBeenCalledOnce()
    await user.selectOptions(screen.getByLabelText('头像状态'), 'missing')
    await waitFor(() => expect(actorList).toHaveBeenCalledTimes(2))
    expect(actorList.mock.calls[1]?.[0]).toMatchObject({ library_id: 0, status: 'missing', offset: 0 })

    await user.click(screen.getByText('头像来源与存储设置'))
    await user.selectOptions(screen.getByRole('combobox', { name: /头像来源/ }), 'gfriends')
    const avatarRoot = await screen.findByRole('textbox', { name: /头像图片根地址/ })
    await user.clear(avatarRoot)
    await user.type(avatarRoot, 'https://images.example/actors/')
    await user.click(screen.getByRole('button', { name: '保存头像设置' }))

    await waitFor(() => expect(save).toHaveBeenCalledOnce())
    expect(save.mock.calls[0]?.[0]).toMatchObject({
      avatar_source: 'gfriends',
      gfriends: { base_url: 'https://images.example/actors/' },
    })
    expect(screen.getByRole('textbox', { name: /头像图片根地址/ })).toHaveValue('https://images.example/actors/')
    expect(await screen.findByText('有未保存的修改')).toBeInTheDocument()
  })

  it('loads scheduled tasks once and saves a validated task using the existing payload', async () => {
    const user = userEvent.setup()
    const list = vi.spyOn(scheduledApi, 'list').mockResolvedValue(scheduledFixture)
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [library], total: 1 })
    vi.spyOn(scheduledApi, 'validate').mockResolvedValue({ valid: true, error: '', next_runs: [] })
    const save = vi.spyOn(scheduledApi, 'save').mockResolvedValue(undefined)
    signIn()
    renderPage('/scheduled')

    await screen.findByRole('heading', { name: '新建计划任务' })
    await waitFor(() => expect(list).toHaveBeenCalledOnce())
    await user.type(screen.getByLabelText('名称'), 'Nightly scan')
    await user.type(screen.getByLabelText('cron 表达式'), '0 3 * * *')
    await waitFor(() => expect(scheduledApi.validate).toHaveBeenCalled())
    await user.click(screen.getByRole('button', { name: '创建计划任务' }))

    await waitFor(() => expect(save).toHaveBeenCalledOnce())
    expect(save).toHaveBeenCalledWith(null, {
      name: 'Nightly scan', type: 'scan', cron: '0 3 * * *', enabled: true, params: { library_id: 0 },
    })
  })

  it('restores explicit false schedule values and drops probe parameters after switching type', async () => {
    const user = userEvent.setup()
    const task: ScheduledTask = {
      id: 9, name: 'Night probe', type: 'probe', cron: '@daily',
      params: '{"library_id":7,"status":"","only_missing":false,"limit":4}',
      enabled: false, next_run: '', last_status: '', last_run_at: '', last_message: '',
    }
    vi.spyOn(scheduledApi, 'list').mockResolvedValue({ ...scheduledFixture, items: [task] })
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [library], total: 1 })
    vi.spyOn(scheduledApi, 'validate').mockResolvedValue({ valid: true, error: '', next_runs: [] })
    const save = vi.spyOn(scheduledApi, 'save').mockResolvedValue(undefined)
    signIn()
    renderPage('/scheduled')

    await screen.findByText('Night probe')
    const row = document.querySelector('[data-scheduled="9"]') as HTMLElement
    await user.click(await within(row).findByRole('button', { name: '编辑' }))
    expect(await screen.findByRole('heading', { name: '编辑计划任务 · Night probe' })).toBeInTheDocument()
    expect(screen.getByLabelText('启用计划任务')).not.toBeChecked()
    expect(screen.getByLabelText('仅处理缺失元数据 / 未探测项目')).not.toBeChecked()
    expect(screen.getByRole('combobox', { name: '影片状态' })).toHaveValue('')
    expect(screen.getByRole('spinbutton', { name: '处理上限（0 = 不限制）' })).toHaveValue(4)

    await user.click(screen.getByRole('button', { name: '保存修改' }))
    await waitFor(() => expect(save).toHaveBeenNthCalledWith(1, 9, {
      name: 'Night probe', type: 'probe', cron: '@daily', enabled: false,
      params: { library_id: 7, status: '', only_missing: false, limit: 4 },
    }))

    await screen.findByRole('heading', { name: '新建计划任务' })
    const refreshedRow = document.querySelector('[data-scheduled="9"]') as HTMLElement
    await user.click(within(refreshedRow).getByRole('button', { name: '编辑' }))
    await screen.findByRole('heading', { name: '编辑计划任务 · Night probe' })
    await user.selectOptions(screen.getByRole('combobox', { name: '任务类型' }), 'reindex')
    await user.click(screen.getByRole('button', { name: '保存修改' }))
    await waitFor(() => expect(save).toHaveBeenNthCalledWith(2, 9, {
      name: 'Night probe', type: 'reindex', cron: '@daily', enabled: false, params: {},
    }))
  })

  it('ignores a late cron result after a newer expression has failed validation', async () => {
    const old = { resolve: (_result: { valid: boolean; error: string; next_runs: string[] }) => {} }
    const pending = new Promise<{ valid: boolean; error: string; next_runs: string[] }>(resolve => { old.resolve = resolve })
    vi.spyOn(scheduledApi, 'list').mockResolvedValue(scheduledFixture)
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [], total: 0 })
    const validate = vi.spyOn(scheduledApi, 'validate').mockImplementation(expression => expression === '@daily'
      ? pending : Promise.resolve({ valid: false, error: 'new invalid', next_runs: [] }))
    signIn()
    renderPage('/scheduled')
    const cron = await screen.findByRole('textbox', { name: 'cron 表达式' })

    fireEvent.change(cron, { target: { value: '@daily' } })
    await waitFor(() => expect(validate).toHaveBeenCalledTimes(1))
    const oldSignal = validate.mock.calls[0]?.[1]
    fireEvent.change(cron, { target: { value: 'new' } })
    await waitFor(() => expect(validate).toHaveBeenCalledTimes(2))
    expect(oldSignal?.aborted).toBe(true)
    expect(await screen.findByText('表达式非法：new invalid')).toBeInTheDocument()

    await act(async () => {
      old.resolve({ valid: true, error: '', next_runs: ['old result'] })
      await pending
    })
    expect(screen.getByText('表达式非法：new invalid')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '创建计划任务' })).toBeDisabled()
  })

  it('keeps scraper changes after a rejected save and restores without writing', async () => {
    const user = userEvent.setup()
    const settings = parseScrapeSettings(scrapeFixture)
    vi.spyOn(scrapeApi, 'settings').mockResolvedValue(settings)
    vi.spyOn(scrapeApi, 'candidates').mockResolvedValue(4)
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [library], total: 1 })
    const save = vi.spyOn(scrapeApi, 'save').mockRejectedValue(new Error('write unavailable'))
    signIn()
    renderPage('/scrape')

    const url = await screen.findByRole('textbox', { name: /MetaTube 地址/ })
    await user.type(url, 'https://meta.example/api')
    await user.click(screen.getByRole('button', { name: '保存并即时生效' }))
    await waitFor(() => expect(save).toHaveBeenCalledOnce())
    expect(url).toHaveValue('https://meta.example/api')
    expect(screen.getByText('有未保存的修改')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '恢复已保存设置' }))
    expect(screen.getByRole('textbox', { name: /MetaTube 地址/ })).toHaveValue('')
    expect(save).toHaveBeenCalledOnce()
  })

  it('does not revoke an API key when deletion is cancelled', async () => {
    const user = userEvent.setup()
    vi.spyOn(adminApi, 'keys').mockResolvedValue({
      items: [{ key: 'fixture-key', name: 'Client', created_at: '2026-11-19T00:00:00Z' }], total: 1,
    })
    const remove = vi.spyOn(adminApi, 'deleteKey').mockResolvedValue(undefined)
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    signIn()
    renderPage('/apikeys')

    await user.click(await screen.findByRole('button', { name: '删除 Client' }))
    expect(confirm).toHaveBeenCalledWith('删除该 API 密钥？使用它的客户端将立即失效。')
    expect(remove).not.toHaveBeenCalled()
  })

  it('keeps an API key name after a failed create and clears it after success', async () => {
    const user = userEvent.setup()
    const list = vi.spyOn(adminApi, 'keys')
      .mockResolvedValueOnce({ items: [], total: 0 })
      .mockResolvedValue({ items: [{ key: 'fixture-key', name: 'Desktop', created_at: '2026-11-19T00:00:00Z' }], total: 1 })
    const create = vi.spyOn(adminApi, 'createKey')
      .mockRejectedValueOnce(new Error('service unavailable'))
      .mockResolvedValueOnce({ key: 'fixture-key', name: 'Desktop', created_at: '2026-11-19T00:00:00Z' })
    signIn()
    renderPage('/apikeys')

    const name = await screen.findByRole('textbox', { name: '名称' })
    await user.type(name, 'Desktop')
    await user.click(screen.getByRole('button', { name: '创建密钥' }))
    expect(await screen.findByText('service unavailable')).toBeInTheDocument()
    expect(name).toHaveValue('Desktop')

    await user.click(screen.getByRole('button', { name: '创建密钥' }))
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2))
    expect(create).toHaveBeenCalledWith('Desktop')
    expect(screen.getByRole('textbox', { name: '名称' })).toHaveValue('')
    expect(await screen.findByText('fixture-key')).toBeInTheDocument()
  })

  it('does not clear probe history when confirmation is cancelled', async () => {
    const user = userEvent.setup()
    vi.spyOn(adminApi, 'probes').mockResolvedValue({
      items: [{ id: 3, method: 'GET', path: '/Items/NextUp', created_at: '2026-11-19T00:00:00Z' }],
    })
    const clear = vi.spyOn(adminApi, 'clearProbes').mockResolvedValue(undefined)
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    signIn()
    renderPage('/probe')

    await user.click(await screen.findByRole('button', { name: '清空记录' }))
    expect(confirm).toHaveBeenCalledWith('清空全部未知接口探针记录？')
    expect(clear).not.toHaveBeenCalled()
  })

  it('shows current server task state after a single initial read', async () => {
    const read = vi.spyOn(adminApi, 'tasks').mockResolvedValue({
      running: true,
      items: [{ id: 4, type: 'scan', status: 'running', started_at: '2026-11-19T00:00:00Z' }],
    })
    signIn()
    renderPage('/tasks')

    expect(await screen.findByText('有任务正在进行中。')).toBeInTheDocument()
    expect(screen.getByText('进行中')).toBeInTheDocument()
    expect(read).toHaveBeenCalledOnce()
  })

  it('shows task error summaries and keeps plain-text details expanded after refresh', async () => {
    const user = userEvent.setup()
    const summary = '局部刷新有 52 个条目处理失败: /mnt/data/library'
    const detail = '失败明细：\n\n1. 文件: /mnt/data/library/movie.nfo\n原因: <script>window.bad = true</script>'
    const read = vi.spyOn(adminApi, 'tasks').mockResolvedValue({
      running: false,
      items: [{ id: 14, type: 'scan', status: 'failed', started_at: '2026-11-19T00:00:00Z', error: `${summary}\n${detail}` }],
    })
    signIn()
    renderPage('/tasks')

    expect(await screen.findByText(summary)).toBeInTheDocument()
    const details = document.querySelector('.react-task-error details')
    expect(details).not.toHaveAttribute('open')
    expect(details?.querySelector('pre')?.textContent).toBe(detail)
    expect(document.querySelector('script, img')).toBeNull()

    await user.click(screen.getByText('查看失败明细'))
    expect(details).toHaveAttribute('open')
    await user.click(screen.getByRole('button', { name: '刷新任务' }))
    await waitFor(() => expect(read).toHaveBeenCalledTimes(2))
    expect(document.querySelector('.react-task-error details')).toHaveAttribute('open')
  })

  it('keeps single-line and empty task errors compact', async () => {
    vi.spyOn(adminApi, 'tasks').mockResolvedValue({
      running: false,
      items: [
        { id: 15, type: 'scan', status: 'failed', started_at: '2026-11-19T00:00:00Z', error: '扫描失败: /mnt/data/library' },
        { id: 16, type: 'scan', status: 'success', started_at: '2026-11-19T00:00:00Z' },
      ],
    })
    signIn()
    renderPage('/tasks')

    expect(await screen.findByText('扫描失败: /mnt/data/library')).toBeInTheDocument()
    const cells = document.querySelectorAll('.react-task-error')
    expect(cells).toHaveLength(2)
    expect(cells[0]?.textContent).toBe('扫描失败: /mnt/data/library')
    expect(cells[1]?.textContent).toBe('—')
    expect(screen.queryByRole('button', { name: '查看失败明细' })).not.toBeInTheDocument()
  })

  it('requires a delete preview and confirmation before removing a duplicate copy', async () => {
    const user = userEvent.setup()
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [library], total: 1 })
    vi.spyOn(duplicatesApi, 'groups').mockResolvedValue({
      items: [{ key: 'ABF018', number: 'ABF-018', count: 2 }], total: 1, total_movies: 2, extra_copies: 1,
    })
    vi.spyOn(duplicatesApi, 'members').mockResolvedValue({
      items: [
        { id: 10, library_id: 7, library_name: 'Films', number: 'ABF-018', title: 'Copy A', source_path: '/fixture/a.strm', status: 'success', additional_parts: [] },
        { id: 11, library_id: 7, library_name: 'Films', number: 'ABF-018', title: 'Copy B', source_path: '/fixture/b.strm', status: 'success', additional_parts: [] },
      ],
      total: 2,
    })
    const previewDelete = vi.spyOn(duplicatesApi, 'previewDelete').mockResolvedValue({
      movie_id: 10, alist_url: '', preview_token: 'fixture-preview', expires_at: 1000,
      files: [{ local_path: '/fixture/a.strm', remote_path: '', removed: false, remote: { size: 0 } }],
    })
    const deleteCopy = vi.spyOn(duplicatesApi, 'deleteCopy').mockResolvedValue(undefined)
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    signIn()
    renderPage('/libraries')

    await user.click(await screen.findByRole('button', { name: '开始查重' }))
    await user.click(await screen.findByRole('button', { name: '查看位置' }))
    await user.click((await screen.findAllByRole('button', { name: /删除副本/ }))[0]!)

    await waitFor(() => expect(previewDelete).toHaveBeenCalledOnce())
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('/fixture/a.strm'))
    expect(deleteCopy).not.toHaveBeenCalled()
  })

  it('deletes only the previewed duplicate after confirmation and keeps the result after refresh', async () => {
    const user = userEvent.setup()
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [library], total: 1 })
    const groups = vi.spyOn(duplicatesApi, 'groups')
      .mockResolvedValueOnce({ items: [{ key: 'ABF018', number: 'ABF-018', count: 2 }], total: 1, total_movies: 2, extra_copies: 1 })
      .mockResolvedValueOnce({ items: [], total: 0, total_movies: 0, extra_copies: 0 })
    vi.spyOn(duplicatesApi, 'members').mockResolvedValue({
      items: [
        { id: 10, library_id: 7, library_name: 'Films', number: 'ABF-018', title: 'Copy A', source_path: '/fixture/a.strm', status: 'success', additional_parts: [] },
        { id: 11, library_id: 7, library_name: 'Films', number: 'ABF-018', title: 'Copy B', source_path: '/fixture/b.strm', status: 'success', additional_parts: [] },
      ],
      total: 2,
    })
    const preview = {
      movie_id: 10, alist_url: '', preview_token: 'confirmed-preview', expires_at: 1000,
      files: [{ local_path: '/fixture/a.strm', remote_path: '/cloud/a.mp4', removed: false, remote: { size: 42 } }],
    }
    const previewDelete = vi.spyOn(duplicatesApi, 'previewDelete').mockResolvedValue(preview)
    const deleteCopy = vi.spyOn(duplicatesApi, 'deleteCopy').mockResolvedValue(undefined)
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    signIn()
    renderPage('/libraries')

    await user.click(await screen.findByRole('button', { name: '开始查重' }))
    await user.click(await screen.findByRole('button', { name: '查看位置' }))
    await user.click((await screen.findAllByRole('button', { name: /删除副本/ }))[0]!)

    await waitFor(() => expect(deleteCopy).toHaveBeenCalledOnce())
    expect(previewDelete).toHaveBeenCalledWith(10, 0, 'ABF018', expect.any(AbortSignal))
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('/fixture/a.strm → /cloud/a.mp4'))
    expect(deleteCopy).toHaveBeenCalledWith(10, 0, 'ABF018', preview)
    await waitFor(() => expect(groups).toHaveBeenCalledTimes(2))
    expect(await screen.findByText('重复副本已删除；磁盘文件与查重统计已更新。')).toBeInTheDocument()
    expect(await screen.findByText('未发现重复影片。')).toBeInTheDocument()
  })

  it('keeps an AVdb API key blank when editing a saved connection', async () => {
    const user = userEvent.setup()
    const onlineLibrary: LibraryDto = { ...library, Id: 8, Name: 'AVdb', SourceType: 'avdb_javdb', Revision: 2 }
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({ items: [onlineLibrary], total: 1 })
    vi.spyOn(r18Api, 'connection').mockResolvedValue({
      library: onlineLibrary,
      connection: { name: onlineLibrary.Name, base_url: 'https://avdb.fixture', revision: 2 },
      hasApiKey: true,
    })
    const save = vi.spyOn(r18Api, 'saveConnection').mockResolvedValue(onlineLibrary)
    signIn()
    renderPage('/libraries')

    await user.click(await screen.findByRole('button', { name: 'API 配置' }))
    const apiKey = await screen.findByLabelText('API Key')
    expect(apiKey).toHaveValue('')
    expect(apiKey).toHaveAttribute('placeholder', '已配置，留空保留现有密钥')
    await user.click(screen.getByRole('button', { name: '保存连接配置' }))

    await waitFor(() => expect(save).toHaveBeenCalledOnce())
    expect(save).toHaveBeenCalledWith({ name: 'AVdb', base_url: 'https://avdb.fixture', revision: 2 }, 8)
    expect(apiKey).toHaveValue('')
  })
})
