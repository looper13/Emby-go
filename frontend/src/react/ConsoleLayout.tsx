import {
  Activity, CirclePlus, Clock3, Film, Folder, Heart, Image, ImageOff, KeyRound, LayoutGrid,
  LogOut, Moon, PanelLeftClose, PanelLeftOpen, Search, Settings, Sparkles, Sun, UserRound, Users, Radar,
} from 'lucide-react'
import { useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useReactAuthStore } from './auth-store'
import { useAppRuntimeStore } from './app-store'
import { useReactPreferencesStore } from './preferences-store'
import { useReactTasksStore } from './tasks-store'
import { useThemeStore } from './theme-store'
import { GlobalSearchDialog } from './GlobalSearchDialog'
import './console-layout.css'

const links = [
  { page: 'overview', label: '总览', icon: LayoutGrid, group: '媒体', adminOnly: true },
  { page: 'libraries', label: '媒体库', icon: Folder, group: '媒体', adminOnly: true },
  { page: 'items', label: '影视库', icon: Film, group: '媒体' },
  { page: 'favorites', label: '收藏', icon: Heart, group: '媒体' },
  { page: 'r18', label: 'AVdb 在线', icon: Sparkles, group: '媒体' },
  { page: 'actors', label: '演员', icon: Users, group: '媒体', adminOnly: true },
  { page: 'manual', label: '手动补录', icon: CirclePlus, group: '维护', adminOnly: true },
  { page: 'scrape', label: '刮削', icon: Sparkles, group: '维护', adminOnly: true },
  { page: 'scheduled', label: '计划任务', icon: Clock3, group: '维护', adminOnly: true },
  { page: 'tasks', label: '任务', icon: Activity, group: '维护', adminOnly: true },
  { page: 'probe', label: '接口探针', icon: Radar, group: '维护', adminOnly: true },
  { page: 'settings', label: '设置', icon: Settings, group: '系统', adminOnly: true },
  { page: 'apikeys', label: 'API 密钥', icon: KeyRound, group: '系统', adminOnly: true },
  { page: 'users', label: '用户管理', icon: Users, group: '系统', adminOnly: true },
  { page: 'account', label: '我的账号', icon: UserRound, group: '系统' },
] as const

function routeTitle(path: string) {
  const page = path.split('/')[1] || 'items'
  return links.find((entry) => entry.page === page)?.label ?? (page === 'item' ? '影片详情' : '媒体工作区')
}

export function ConsoleLayout() {
  const location = useLocation()
  const navigate = useNavigate()
  const user = useReactAuthStore((state) => state.user)
  const submitting = useReactAuthStore((state) => state.submitting)
  const logout = useReactAuthStore((state) => state.logout)
  const administrator = user?.Policy.IsAdministrator === true
  const collapsed = useReactPreferencesStore((state) => state.sidebarCollapsed)
  const hideImages = useReactPreferencesStore((state) => state.hideImages)
  const toggleSidebar = useReactPreferencesStore((state) => state.toggleSidebar)
  const toggleHideImages = useReactPreferencesStore((state) => state.toggleHideImages)
  const theme = useThemeStore((state) => state.theme)
  const toggleTheme = useThemeStore((state) => state.toggle)
  const showToast = useAppRuntimeStore((state) => state.showToast)
  const runTask = useReactTasksStore((state) => state.run)
  const mutationBusy = useReactTasksStore((state) => state.mutationBusy)
  const taskBusy = useReactTasksStore((state) => state.busy)
  const [searchOpen, setSearchOpen] = useState(false)
  const visibleLinks = links.filter((entry) => administrator || !('adminOnly' in entry && entry.adminOnly))

  useEffect(() => {
    function handleGlobalKey(event: KeyboardEvent) {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k' && !event.isComposing) {
        event.preventDefault()
        setSearchOpen((open) => !open)
      }
    }
    window.addEventListener('keydown', handleGlobalKey)
    return () => window.removeEventListener('keydown', handleGlobalKey)
  }, [])

  async function signOut() {
    try {
      await logout()
      if (!useReactAuthStore.getState().token) navigate('/login', { replace: true })
    } catch (cause) {
      showToast(cause instanceof Error ? cause.message : '退出失败，请重试', 'error')
    }
  }

  return (
    <div className="react-console" data-sidebar-collapsed={collapsed}>
      <aside className="react-console-sidebar" aria-label="主导航">
        <div className="react-console-brand">
          <span className="react-console-mark" aria-hidden="true"><Film size={19} /></span>
          <span className="react-console-brand-copy"><strong>Emby-go</strong><small>MEDIA ARCHIVE</small></span>
          <button className="react-icon-button react-sidebar-collapse" type="button" aria-label="收起侧栏" title="收起侧栏" onClick={toggleSidebar}>
            <PanelLeftClose size={16} />
          </button>
        </div>

        <nav className="react-console-nav">
          {visibleLinks.map((entry, index) => {
            const Previous = visibleLinks[index - 1]
            const Icon = entry.icon
            return (
              <div key={entry.page}>
                {index === 0 || entry.group !== Previous?.group
                  ? <div className="react-nav-group">{entry.group}</div>
                  : null}
                <NavLink
                  className={({ isActive }) => `react-nav-link${isActive ? ' is-active' : ''}`}
                  to={`/${entry.page}`}
                  title={collapsed ? entry.label : undefined}
                  aria-label={entry.label}
                  aria-current={location.pathname === `/${entry.page}` ? 'page' : undefined}
                >
                  <Icon size={17} strokeWidth={1.8} aria-hidden="true" />
                  <span>{entry.label}</span>
                </NavLink>
              </div>
            )
          })}
        </nav>

        <footer className="react-console-user">
          <span className="react-user-avatar" aria-hidden="true">{user?.Name?.slice(0, 1).toUpperCase() || 'U'}</span>
          <span className="react-user-copy"><strong>{user?.Name || '用户'}</strong><small>{administrator ? 'ADMIN' : 'MEMBER'}</small></span>
          <button className="react-icon-button" type="button" title="退出登录" aria-label="退出登录" disabled={submitting} onClick={() => void signOut()}>
            <LogOut size={16} />
          </button>
        </footer>
      </aside>

      <div className="react-console-body">
        <header className="react-console-topbar">
          <div className="react-console-heading">
            <button className={`react-icon-button react-sidebar-expand${collapsed ? ' is-visible' : ''}`} type="button" title="展开侧栏" aria-label="展开侧栏" onClick={toggleSidebar}>
              <PanelLeftOpen size={17} />
            </button>
            <div><p>EMBY-GO / ARCHIVE</p><h1>{routeTitle(location.pathname)}</h1></div>
          </div>
          <div className="react-console-search">
            <button className="react-console-search-trigger" type="button" title="全局搜索 (Ctrl + K)"
              aria-label="全局搜索 (Ctrl + K)" aria-haspopup="dialog" aria-expanded={searchOpen} onClick={() => setSearchOpen(true)}>
              <Search size={15} aria-hidden="true" /><span>搜索媒体、演员、库...</span><kbd>Ctrl K</kbd>
            </button>
          </div>
          <div className="react-console-actions">
            {administrator && <button className="react-primary-button" type="button" disabled={mutationBusy} onClick={() => void runTask()}>
              <Activity size={15} aria-hidden="true" /><span>{taskBusy ? '扫描中…' : '开始扫描'}</span>
            </button>}
            <button className={`react-icon-button${hideImages ? ' is-selected' : ''}`} type="button"
              title={hideImages ? '显示媒体图片' : '隐藏媒体图片'} aria-label={hideImages ? '显示媒体图片' : '隐藏媒体图片'} onClick={toggleHideImages}>
              {hideImages ? <ImageOff size={17} /> : <Image size={17} />}
            </button>
            <button className="react-icon-button" type="button" title={theme === 'dark' ? '切换为浅色模式' : '切换为深色模式'}
              aria-label={theme === 'dark' ? '切换为浅色模式' : '切换为深色模式'} onClick={toggleTheme}>
              {theme === 'dark' ? <Sun size={17} /> : <Moon size={17} />}
            </button>
          </div>
        </header>
        <main className="react-console-content" id="content" aria-live="polite"><Outlet /></main>
      </div>

      <nav className="react-mobile-nav" aria-label="移动端导航">
        {(['items', 'favorites', 'account'] as const).map((page) => {
          const entry = links.find((link) => link.page === page)!
          const Icon = entry.icon
          return <NavLink key={page} to={`/${page}`} aria-current={location.pathname === `/${page}` ? 'page' : undefined}>
            <Icon size={18} aria-hidden="true" /><span>{entry.label}</span>
          </NavLink>
        })}
      </nav>
      <GlobalSearchDialog open={searchOpen} onClose={() => setSearchOpen(false)} />
    </div>
  )
}
