<script setup lang="ts">
import { LayoutGrid, Film, Folder, CirclePlus, Settings, KeyRound, Activity, Search, TrendingUp, Clock, Sparkles, Users, Heart, UserRound, LogOut, Sun, Moon, Image, ImageOff, PanelLeftClose, PanelLeftOpen } from '@lucide/vue';
import { useRoute, useRouter } from 'vue-router';
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { useTasksStore } from '../stores/tasks';
import { useAuthStore } from '../stores/auth';
import { useToastStore } from '../stores/toasts';
import { useTheme } from '../composables/useTheme';
import { useMediaVisibility } from '../composables/useMediaVisibility';
import { useSidebar } from '../composables/useSidebar';
import { useCardSize } from '../composables/useCardSize';
import GlobalSearchDialog from '../components/common/GlobalSearchDialog.vue';

const route = useRoute();
const router = useRouter();
const tasks = useTasksStore();
const auth = useAuthStore(), toasts = useToastStore();
const theme = useTheme();
const mediaVisibility = useMediaVisibility();
const { isCollapsed, toggle } = useSidebar();
useCardSize(); // Initialize global media card size CSS variable

const searchOpen = ref(false);

function handleGlobalKey(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault();
    searchOpen.value = !searchOpen.value;
  }
}

onMounted(() => {
  window.addEventListener('keydown', handleGlobalKey);
});

onUnmounted(() => {
  window.removeEventListener('keydown', handleGlobalKey);
});

async function logout() {
  try { await auth.logout(); if (!auth.authenticated) await router.replace('/login'); }
  catch (cause) { toasts.show(cause instanceof Error ? cause.message : '退出失败，请重试', 'error'); }
}
const navigation = computed(() => [
  { page: 'overview', label: '总览', icon: LayoutGrid, group: '媒体浏览' },
  { page: 'libraries', label: '媒体库', icon: Folder, group: '媒体浏览' },
  { page: 'favorites', label: '收藏', icon: Heart, group: '媒体浏览' },
  { page: 'items', label: '媒体墙', icon: Film, group: '媒体浏览' },
  { page: 'r18', label: 'JavDB 在线', icon: Folder, group: '媒体浏览' },
  { page: 'actors', label: '演员', icon: Users, group: '媒体浏览' },
  { page: 'manual', label: '手动补录', icon: CirclePlus, group: '录入与工具' },
  { page: 'settings', label: '设置', icon: Settings, group: '系统与账号' },
  { page: 'apikeys', label: 'API 密钥', icon: KeyRound, group: '系统与账号' },
  { page: 'account', label: '我的账号', icon: UserRound, group: '系统与账号' },
  { page: 'users', label: '用户管理', icon: Users, group: '系统与账号' },
  { page: 'scrape', label: '刮削', icon: Sparkles, group: '自动化与运维' },
  { page: 'scheduled', label: '计划任务', icon: Clock, group: '自动化与运维' },
  { page: 'tasks', label: '任务', icon: Activity, group: '自动化与运维' },
  { page: 'probe', label: '接口探针', icon: Search, group: '自动化与运维' },
].filter(item => auth.isAdministrator || ['items', 'r18', 'favorites', 'account'].includes(item.page)));
const detailTitle = ref('');
function updateTitle(value: string) { detailTitle.value = value; }
watch(() => route.fullPath, () => { detailTitle.value = ''; });
</script>

<template>
  <div id="app-shell" :class="{ 'sidebar-collapsed': isCollapsed }">
    <aside class="sidebar" :class="{ 'is-collapsed': isCollapsed }">
      <div class="brand">
        <div
          class="brand-mark-wrap"
          :title="isCollapsed ? '展开侧边栏' : undefined"
          :role="isCollapsed ? 'button' : undefined"
          :tabindex="isCollapsed ? 0 : undefined"
          @click="isCollapsed && toggle()"
          @keydown.enter="isCollapsed && toggle()"
          @keydown.space.prevent="isCollapsed && toggle()"
        >
          <span class="brand-mark" aria-hidden="true">
            <Film :size="20" aria-hidden="true" />
          </span>
        </div>
        <div v-show="!isCollapsed" class="brand-text"><strong>Emby-go</strong><small>媒体档案控制台</small></div>
        <button
          v-if="!isCollapsed"
          class="icon-btn sidebar-collapse-btn"
          type="button"
          title="收回侧边栏"
          aria-label="收回侧边栏"
          @click="toggle"
        >
          <PanelLeftClose :size="16" aria-hidden="true" />
        </button>
      </div>
      <nav id="nav" class="nav" aria-label="主导航">
        <template v-for="(item, index) in navigation" :key="item.page">
          <div
            v-if="index === 0 || item.group !== navigation[index - 1]?.group"
            class="nav-group-label"
            :class="{ 'is-collapsed': isCollapsed }"
            aria-hidden="true"
          >
            <span v-show="!isCollapsed">{{ item.group }}</span>
          </div>
          <button
            class="nav-item"
            :class="{ 'is-active': route.meta.navigation === item.page }"
            :data-page="item.page"
            :aria-current="route.meta.navigation === item.page ? 'page' : undefined"
            :title="isCollapsed ? item.label : undefined"
            :aria-label="item.label"
            @click="router.push('/' + item.page)"
          >
            <component :is="item.icon" :size="17" :stroke-width="1.8" aria-hidden="true" />
            <span v-show="!isCollapsed">{{ item.label }}</span>
          </button>
        </template>
      </nav>
      <footer class="side-foot" :title="isCollapsed ? 'EMBY-GO · CONSOLE (在线)' : undefined">
        <span class="status-dot" aria-hidden="true"></span>
        <span v-show="!isCollapsed">EMBY-GO · CONSOLE</span>
      </footer>
    </aside>
    <main class="main">
      <header class="topbar">
        <div class="topbar-left">
          <button
            class="icon-btn sidebar-toggle-btn"
            type="button"
            :title="isCollapsed ? '展开侧边栏' : '收回侧边栏'"
            :aria-label="isCollapsed ? '展开侧边栏' : '收回侧边栏'"
            @click="toggle"
          >
            <PanelLeftOpen v-if="isCollapsed" :size="18" aria-hidden="true" />
            <PanelLeftClose v-else :size="18" aria-hidden="true" />
          </button>
          <div class="topbar-titles">
            <p id="page-eyebrow" class="crumb">{{ route.meta.crumb }}</p>
            <h1 id="page-title">{{ detailTitle || route.meta.title }}</h1>
          </div>
        </div>

        <!-- 顶栏全局搜索触发栏 (Ctrl+K) -->
        <div class="topbar-search">
          <button
            class="global-search-trigger"
            type="button"
            title="全局搜索 (Ctrl + K)"
            aria-label="全局搜索 (Ctrl + K)"
            @click="searchOpen = true"
          >
            <Search :size="15" aria-hidden="true" />
            <span class="search-trigger-placeholder">搜索媒体、演员、库...</span>
            <kbd class="search-trigger-kbd">Ctrl K</kbd>
          </button>
        </div>

        <div class="top-actions">
          <div class="connection-indicator" title="服务已连接">
            <span class="status-dot" aria-hidden="true"></span>
          </div>
          <button class="icon-btn media-image-toggle" :class="{ 'is-active': mediaVisibility.hideImages.value }" type="button" :title="mediaVisibility.hideImages.value ? '显示媒体图片（封面、剧照）' : '隐藏媒体图片（封面、剧照）'" :aria-label="mediaVisibility.hideImages.value ? '显示媒体图片' : '隐藏媒体图片'" @click="mediaVisibility.toggleHideImages()">
            <ImageOff v-if="mediaVisibility.hideImages.value" :size="18" aria-hidden="true" />
            <Image v-else :size="18" aria-hidden="true" />
          </button>
          <button class="icon-btn theme-toggle" type="button" :title="theme.theme.value === 'dark' ? '切换为浅色模式' : '切换为深色模式'" :aria-label="theme.theme.value === 'dark' ? '切换为浅色模式' : '切换为深色模式'" @click="theme.toggleTheme()">
            <Sun v-if="theme.theme.value === 'dark'" :size="18" aria-hidden="true" />
            <Moon v-else :size="18" aria-hidden="true" />
          </button>
          <button class="icon-btn" type="button" :title="`${auth.user?.Name || ''}：退出登录`" aria-label="退出登录" :disabled="auth.submitting" @click="logout"><LogOut :size="18" aria-hidden="true" /></button>
          <button v-if="auth.isAdministrator" id="scan" class="btn btn-accent" title="增量扫描：只更新新增或变化的影片；需要重新读取全部影片时使用全量重建索引" :disabled="tasks.mutationBusy" @click="tasks.run()"><TrendingUp :size="15" :stroke-width="1.8" aria-hidden="true" /><span>{{ tasks.busy ? '扫描中…' : '开始扫描' }}</span></button>
        </div>
      </header>
      <div id="content" class="content" aria-live="polite"><RouterView v-slot="{ Component }"><component :is="Component" v-on="route.name === 'item' ? { title: updateTitle } : {}" /></RouterView></div>
    </main>

    <!-- 全局聚光灯搜索弹窗 -->
    <GlobalSearchDialog :open="searchOpen" @close="searchOpen = false" />
  </div>
</template>

<style scoped>
.topbar-search {
  flex: 1;
  max-width: 320px;
  margin: 0 16px;
}

.global-search-trigger {
  width: 100%;
  height: 34px;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 12px;
  border-radius: 999px;
  background: var(--panel, rgba(255, 255, 255, 0.06));
  border: 1px solid var(--line, rgba(255, 255, 255, 0.12));
  color: var(--muted);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.global-search-trigger:hover {
  background: var(--panel-hover, rgba(255, 255, 255, 0.1));
  border-color: var(--accent);
  color: var(--text);
}

.search-trigger-placeholder {
  flex: 1;
  text-align: left;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--dim);
}

.search-trigger-kbd {
  font-family: var(--font-mono, monospace);
  font-size: 11px;
  padding: 1px 5px;
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.2);
  border: 1px solid var(--line, rgba(255, 255, 255, 0.12));
  color: var(--muted);
}

@media (max-width: 768px) {
  .search-trigger-placeholder,
  .search-trigger-kbd {
    display: none;
  }
  .topbar-search {
    max-width: 34px;
    margin: 0 8px;
  }
  .global-search-trigger {
    width: 34px;
    padding: 0;
    justify-content: center;
    border-radius: 50%;
  }
}
</style>
