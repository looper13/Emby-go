import { createHashRouter, Navigate, Outlet, RouterProvider, useLocation } from 'react-router-dom'
import { lazy, Suspense, useEffect, useRef, useState, type ReactNode } from 'react'
import { AppProviders } from './AppProviders'
import { useAppRuntimeStore } from './app-store'
import { ConsoleLayout } from './ConsoleLayout'
import { LoginPage } from './LoginPage'
import { RequireAuth } from './RequireAuth'
import { useReactAuthStore } from './auth-store'
import { ViewHistoryProvider } from './view-history'
import { useReactMediaWallStore } from './media-wall-store'
import { useReactPlayerStore } from './player-store'
import { useReactScheduledStore } from './scheduled-store'
import { useReactScrapePreviewStore } from './scrape-preview-store'

const MediaWallPage = lazy(() => import('./media/MediaWallPage').then(module => ({ default: module.MediaWallPage })))
const ItemDetailPage = lazy(() => import('./media/ItemDetailPage').then(module => ({ default: module.ItemDetailPage })))
const PlayerDialog = lazy(() => import('./media/PlayerDialog').then(module => ({ default: module.PlayerDialog })))
const ScrapePreviewDialog = lazy(() => import('./ScrapePreviewDialog').then(module => ({ default: module.ScrapePreviewDialog })))
const OverviewPage = lazy(() => import('./pages/basic-pages').then(module => ({ default: module.OverviewPage })))
const FavoritesPage = lazy(() => import('./pages/basic-pages').then(module => ({ default: module.FavoritesPage })))
const ManualPage = lazy(() => import('./pages/basic-pages').then(module => ({ default: module.ManualPage })))
const TasksPage = lazy(() => import('./pages/basic-pages').then(module => ({ default: module.TasksPage })))
const ProbePage = lazy(() => import('./pages/basic-pages').then(module => ({ default: module.ProbePage })))
const ApiKeysPage = lazy(() => import('./pages/basic-pages').then(module => ({ default: module.ApiKeysPage })))
const AccountPage = lazy(() => import('./pages/identity-pages').then(module => ({ default: module.AccountPage })))
const UsersPage = lazy(() => import('./pages/identity-pages').then(module => ({ default: module.UsersPage })))
const LibrariesPage = lazy(() => import('./pages/library-settings-pages').then(module => ({ default: module.LibrariesPage })))
const SettingsPage = lazy(() => import('./pages/library-settings-pages').then(module => ({ default: module.SettingsPage })))
const ActorsPage = lazy(() => import('./pages/operations-pages').then(module => ({ default: module.ActorsPage })))
const ScrapePage = lazy(() => import('./pages/operations-pages').then(module => ({ default: module.ScrapePage })))
const ScheduledPage = lazy(() => import('./pages/operations-pages').then(module => ({ default: module.ScheduledPage })))
const R18CatalogPage = lazy(() => import('./pages/r18-pages').then(module => ({ default: module.R18CatalogPage })))
const R18DetailPage = lazy(() => import('./pages/r18-pages').then(module => ({ default: module.R18DetailPage })))

function AdminOnlyPage({ children }: { children: ReactNode }) {
  const administrator = useReactAuthStore((state) => state.user?.Policy.IsAdministrator === true)
  return administrator ? children : <Navigate replace to="/items" />
}

function AppRuntimeLayout() {
  return (
    <ViewHistoryProvider wallCount={() => useReactMediaWallStore.getState().items.length}>
      <AppRuntimeEffects />
      <SessionExpiryListener />
      <Outlet />
    </ViewHistoryProvider>
  )
}

function SessionExpiryListener() {
  const expiration = useReactAuthStore((state) => state.expiration)
  const showToast = useAppRuntimeStore((state) => state.showToast)
  const location = useLocation()
  const previousExpiration = useRef(expiration)

  useEffect(() => {
    if (expiration > previousExpiration.current && location.pathname !== '/login') {
      showToast('登录已失效，请重新登录', 'error')
    }
    previousExpiration.current = expiration
  }, [expiration, location.pathname, showToast])

  return null
}

function AppRuntimeEffects() {
  const location = useLocation()
  const token = useReactAuthStore((state) => state.token)
  const currentPlayer = useReactPlayerStore((state) => state.current)
  const currentScrape = useReactScrapePreviewStore((state) => state.current)
  const closePlayer = useReactPlayerStore((state) => state.close)
  const closeScrapePreview = useReactScrapePreviewStore((state) => state.close)
  const resetScheduled = useReactScheduledStore((state) => state.reset)

  useEffect(() => {
    closePlayer()
    closeScrapePreview()
  }, [location.pathname, location.search, location.hash, token, closePlayer, closeScrapePreview])

  useEffect(() => { resetScheduled() }, [token, resetScheduled])
  return <>
    {currentPlayer && token && <Suspense fallback={null}>
      <PlayerDialog key={currentPlayer.id} options={currentPlayer} onClose={closePlayer} />
    </Suspense>}
    {currentScrape && token && <Suspense fallback={null}>
      <ScrapePreviewDialog />
    </Suspense>}
  </>
}

const routes = [{
  element: <AppRuntimeLayout />,
  children: [
    { path: '/login', element: <LoginPage /> },
    { path: '/', element: <Navigate replace to="/overview" /> },
    {
      element: <RequireAuth><ConsoleLayout /></RequireAuth>,
      children: [
        { path: 'overview', element: <AdminOnlyPage><OverviewPage /></AdminOnlyPage> },
        { path: 'favorites', element: <FavoritesPage /> },
        { path: 'manual', element: <AdminOnlyPage><ManualPage /></AdminOnlyPage> },
        { path: 'tasks', element: <AdminOnlyPage><TasksPage /></AdminOnlyPage> },
        { path: 'probe', element: <AdminOnlyPage><ProbePage /></AdminOnlyPage> },
        { path: 'apikeys', element: <AdminOnlyPage><ApiKeysPage /></AdminOnlyPage> },
        { path: 'account', element: <AccountPage /> },
        { path: 'users', element: <AdminOnlyPage><UsersPage /></AdminOnlyPage> },
        { path: 'libraries', element: <AdminOnlyPage><LibrariesPage /></AdminOnlyPage> },
        { path: 'settings', element: <AdminOnlyPage><SettingsPage /></AdminOnlyPage> },
        { path: 'actors', element: <AdminOnlyPage><ActorsPage /></AdminOnlyPage> },
        { path: 'scrape', element: <AdminOnlyPage><ScrapePage /></AdminOnlyPage> },
        { path: 'scheduled', element: <AdminOnlyPage><ScheduledPage /></AdminOnlyPage> },
        { path: 'r18', element: <R18CatalogPage /> },
        { path: 'r18/:libraryId/item/:id', element: <R18DetailPage /> },
        { path: 'items', element: <MediaWallPage /> },
        { path: 'item/:id', element: <ItemDetailPage /> },
        { path: '*', element: <Navigate replace to="/overview" /> },
      ],
    },
  ],
}]

function ReactRouterApplication() {
  const [router] = useState(() => createHashRouter(routes))
  useEffect(() => () => router.dispose(), [router])
  return <RouterProvider router={router} />
}

export default function App() {
  return <AppProviders><ReactRouterApplication /></AppProviders>
}
