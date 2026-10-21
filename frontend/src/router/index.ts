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
import ActorsPage from '../pages/ActorsPage.vue';
import FavoritesPage from '../pages/FavoritesPage.vue';
import ScheduledPage from '../pages/ScheduledPage.vue';
import AnimePage from '../pages/AnimePage.vue';
import AnimeDetailPage from '../pages/AnimeDetailPage.vue';
import AnimeSettingsPage from '../pages/AnimeSettingsPage.vue';
import BangumiPage from '../pages/BangumiPage.vue';
import BangumiDetailPage from '../pages/BangumiDetailPage.vue';
import AccountPage from '../pages/AccountPage.vue';
import UsersPage from '../pages/UsersPage.vue';

export function createAppRouter(pinia: Pinia, history: RouterHistory = createWebHashHistory(window.location.pathname)) {
  const router = createRouter({
    history,
    scrollBehavior(to) { return to.name === 'items' || to.name === 'item' ? false : { top: 0 }; },
    routes: [
      { path: '/login', name: 'login', component: LoginPage },
      {
        path: '/', component: ConsoleLayout, meta: { requiresAuth: true },
        children: [
          { path: 'account', name: 'account', component: AccountPage, meta: { title: '我的账号', crumb: 'Archive / Account', navigation: 'account' } },
          { path: 'users', name: 'users', component: UsersPage, meta: { adminOnly: true, title: '用户管理', crumb: 'Archive / Users', navigation: 'users' } },
          { path: 'anime', name: 'anime', component: BangumiPage, meta: { title: '动漫', crumb: 'Archive / Anime', navigation: 'anime' } },
          { path: 'anime/mikan', name: 'anime-mikan', component: AnimePage, meta: { title: '蜜柑发布', crumb: '动漫 / 蜜柑发布', navigation: 'anime' } },
          { path: 'anime/subjects/:id', name: 'bangumi-detail', component: BangumiDetailPage, meta: { title: '番剧详情', crumb: '动漫 / 详情', navigation: 'anime' } },
          { path: 'anime/settings', name: 'anime-settings', component: AnimeSettingsPage, meta: { adminOnly: true, title: '动漫设置', crumb: '动漫 / 设置', navigation: 'anime' } },
          { path: 'anime/entries/:id', name: 'anime-detail', component: AnimeDetailPage, meta: { title: '番剧详情', crumb: '动漫 / 详情', navigation: 'anime' } },
          { path: 'actors', name: 'actors', component: ActorsPage, meta: { adminOnly: true, title: '演员', crumb: 'Archive / Actors', navigation: 'actors' } },
          { path: 'favorites', name: 'favorites', component: FavoritesPage, meta: { title: '收藏', crumb: 'Archive / Favorites', navigation: 'favorites' } },
          { path: 'scrape', name: 'scrape', component: ScrapePage, meta: { adminOnly: true, title: '刮削', crumb: 'Archive / Scrape', navigation: 'scrape' } },
          { path: 'scheduled', name: 'scheduled', component: ScheduledPage, meta: { adminOnly: true, title: '计划任务', crumb: 'Archive / Scheduled', navigation: 'scheduled' } },
          { path: 'libraries', name: 'libraries', component: LibrariesPage, meta: { adminOnly: true, title: '媒体库', crumb: 'Archive / Libraries', navigation: 'libraries' } },
          { path: 'manual', name: 'manual', component: ManualPage, meta: { adminOnly: true, title: '手动补录', crumb: 'Archive / Manual Entry', navigation: 'manual' } },
          { path: 'settings', name: 'settings', component: SettingsPage, meta: { adminOnly: true, title: '设置', crumb: 'Archive / Settings', navigation: 'settings' } },
          { path: 'apikeys', name: 'apikeys', component: ApiKeysPage, meta: { adminOnly: true, title: 'API 密钥', crumb: 'Archive / API Keys', navigation: 'apikeys' } },
          { path: 'tasks', name: 'tasks', component: TasksPage, meta: { adminOnly: true, title: '任务', crumb: 'Archive / Tasks', navigation: 'tasks' } },
          { path: 'probe', name: 'probe', component: ProbePage, meta: { adminOnly: true, title: '接口探针', crumb: 'Archive / Probe', navigation: 'probe' } },
          { path: 'items', name: 'items', component: MediaWallPage,
            meta: { title: '媒体墙', crumb: 'Archive / Items', navigation: 'items' } },
          { path: 'item/:id', name: 'item', component: ItemDetailPage,
            beforeEnter: to => sanitizeReturnTo(to.path) ? true : '/items',
            meta: { title: '影片详情', crumb: '媒体墙 / 详情', navigation: 'items' } },
          { path: '', redirect: '/overview' },
          {
            path: 'overview', name: 'overview', component: OverviewPage,
            meta: { adminOnly: true, title: '总览', crumb: 'Archive / Overview', navigation: 'overview' },
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
      if (await auth.checkSession()) return to.meta.adminOnly && !auth.isAdministrator ? '/items' : true;
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
