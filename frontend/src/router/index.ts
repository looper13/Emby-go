import { createRouter, createWebHashHistory, type RouterHistory } from 'vue-router';
import type { Pinia } from 'pinia';
import { useAuthStore } from '../stores/auth';
import { resolveLoginTarget, sanitizeReturnTo } from './legacy-url';
import LoginPage from '../pages/LoginPage.vue';
import OverviewPage from '../pages/OverviewPage.vue';
import ConsoleLayout from '../layouts/ConsoleLayout.vue';
import MediaWallPage from '../pages/MediaWallPage.vue';
import ItemDetailPage from '../pages/ItemDetailPage.vue';
import { useMediaWallStore } from '../stores/media-wall';
import { createViewHistory, viewHistories } from './view-history';
import LibrariesPage from '../pages/LibrariesPage.vue';
import ManualPage from '../pages/ManualPage.vue';
import SettingsPage from '../pages/SettingsPage.vue';
import ApiKeysPage from '../pages/ApiKeysPage.vue';
import TasksPage from '../pages/TasksPage.vue';
import ProbePage from '../pages/ProbePage.vue';
import ScrapePage from '../pages/ScrapePage.vue';
import ScheduledPage from '../pages/ScheduledPage.vue';

export function createAppRouter(pinia: Pinia, history: RouterHistory = createWebHashHistory(window.location.pathname)) {
  const router = createRouter({
    history,
    scrollBehavior(to) { return to.name === 'items' || to.name === 'item' ? false : { top: 0 }; },
    routes: [
      { path: '/login', name: 'login', component: LoginPage },
      {
        path: '/', component: ConsoleLayout, meta: { requiresAuth: true },
        children: [
          { path: 'scrape', name: 'scrape', component: ScrapePage, meta: { title: '刮削', crumb: 'Archive / Scrape', navigation: 'scrape' } },
          { path: 'scheduled', name: 'scheduled', component: ScheduledPage, meta: { title: '计划任务', crumb: 'Archive / Scheduled', navigation: 'scheduled' } },
          { path: 'libraries', name: 'libraries', component: LibrariesPage, meta: { title: '媒体库', crumb: 'Archive / Libraries', navigation: 'libraries' } },
          { path: 'manual', name: 'manual', component: ManualPage, meta: { title: '手动补录', crumb: 'Archive / Manual Entry', navigation: 'manual' } },
          { path: 'settings', name: 'settings', component: SettingsPage, meta: { title: '设置', crumb: 'Archive / Settings', navigation: 'settings' } },
          { path: 'apikeys', name: 'apikeys', component: ApiKeysPage, meta: { title: 'API 密钥', crumb: 'Archive / API Keys', navigation: 'apikeys' } },
          { path: 'tasks', name: 'tasks', component: TasksPage, meta: { title: '任务', crumb: 'Archive / Tasks', navigation: 'tasks' } },
          { path: 'probe', name: 'probe', component: ProbePage, meta: { title: '接口探针', crumb: 'Archive / Probe', navigation: 'probe' } },
          { path: 'items', name: 'items', component: MediaWallPage,
            meta: { title: '媒体墙', crumb: 'Archive / Items', navigation: 'items' } },
          { path: 'item/:id', name: 'item', component: ItemDetailPage,
            beforeEnter: to => sanitizeReturnTo(to.path) ? true : '/items',
            meta: { title: '影片详情', crumb: '媒体墙 / 详情', navigation: 'items' } },
          { path: '', redirect: '/overview' },
          {
            path: 'overview', name: 'overview', component: OverviewPage,
            meta: { title: '总览', crumb: 'Archive / Overview', navigation: 'overview' },
          },
        ],
      },
      { path: '/:pathMatch(.*)*', redirect: '/overview' },
    ],
  });

  viewHistories.set(router, createViewHistory(router, history, () => useMediaWallStore(pinia).items.length));

  router.beforeEach(async (to) => {
    const auth = useAuthStore(pinia);
    if (to.name === 'login') {
      if (auth.authenticated) return resolveLoginTarget(to.query.returnTo);
      return true;
    }
    if (!to.meta.requiresAuth) return true;
    try {
      if (await auth.checkSession()) return true;
    } catch {
      // The login page offers retry; an unreachable server does not clear a token.
    }
    const returnTo = sanitizeReturnTo(to.fullPath);
    return { path: '/login', query: returnTo ? { returnTo } : {}, replace: true };
  });

  router.afterEach(to => {
    if ((window.location.pathname === '/' || window.location.pathname === '/web')
      && to.meta.requiresAuth && useAuthStore(pinia).authenticated) {
      window.location.replace('/admin#' + resolveLoginTarget(to.fullPath));
    }
  });

  return router;
}
