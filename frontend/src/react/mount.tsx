import { StrictMode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import App from './App'
import { activateAuthRuntime } from './auth-store'
import { activateMediaWallRuntime } from './media-wall-store'
import { activatePlayerRuntime } from './player-store'
import { activateScrapePreviewRuntime } from './scrape-preview-store'
import { activateTasksRuntime } from './tasks-store'
import { useReactPreferencesStore } from './preferences-store'
import { useThemeStore } from './theme-store'

interface MountedRoot {
  root: Root
  dispose: () => void
}

const roots = new WeakMap<HTMLElement, MountedRoot>()

export function mountReactApplication(container: HTMLElement): () => void {
  const mounted = roots.get(container)
  if (mounted) return mounted.dispose

  useThemeStore.getState().initialize()
  useReactPreferencesStore.getState().initialize()
  const deactivateAuth = activateAuthRuntime()
  const deactivateMediaWall = activateMediaWallRuntime()
  const deactivateTasks = activateTasksRuntime()
  const deactivatePlayer = activatePlayerRuntime()
  const deactivateScrapePreview = activateScrapePreviewRuntime()
  const root = createRoot(container)
  const dispose = () => {
    if (roots.get(container)?.root !== root) return
    roots.delete(container)
    root.unmount()
    deactivateScrapePreview()
    deactivatePlayer()
    deactivateTasks()
    deactivateMediaWall()
    deactivateAuth()
  }
  roots.set(container, { root, dispose })
  root.render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
  return dispose
}
