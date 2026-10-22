<script setup lang="ts">
import { LayoutGrid, Film, Folder, CirclePlus, Settings, KeyRound, Activity, Search, TrendingUp, Clock, Sparkles, Users, Heart, UserRound, LogOut, Sun, Moon } from '@lucide/vue';
import { useRoute, useRouter } from 'vue-router';
import { computed, ref, watch } from 'vue';
import { useTasksStore } from '../stores/tasks';
import { useAuthStore } from '../stores/auth';
import { useToastStore } from '../stores/toasts';
import { useTheme } from '../composables/useTheme';

const route = useRoute();
const router = useRouter();
const tasks = useTasksStore();
const auth = useAuthStore(), toasts = useToastStore();
const theme = useTheme();

async function logout() {
  try { await auth.logout(); if (!auth.authenticated) await router.replace('/login'); }
  catch (cause) { toasts.show(cause instanceof Error ? cause.message : '退出失败，请重试', 'error'); }
}
const navigation = computed(() => [
  { page: 'overview', label: '总览', icon: LayoutGrid }, { page: 'libraries', label: '媒体库', icon: Folder },
  { page: 'favorites', label: '收藏', icon: Heart },
  { page: 'items', label: '媒体墙', icon: Film }, { page: 'actors', label: '演员', icon: Users }, { page: 'manual', label: '手动补录', icon: CirclePlus },
  { page: 'settings', label: '设置', icon: Settings }, { page: 'apikeys', label: 'API 密钥', icon: KeyRound },
  { page: 'account', label: '我的账号', icon: UserRound },
  { page: 'users', label: '用户管理', icon: Users },
  { page: 'scrape', label: '刮削', icon: Sparkles }, { page: 'scheduled', label: '计划任务', icon: Clock },
  { page: 'tasks', label: '任务', icon: Activity }, { page: 'probe', label: '接口探针', icon: Search },
].filter(item => auth.isAdministrator || ['items', 'favorites', 'account'].includes(item.page)));
const detailTitle = ref('');
function updateTitle(value: string) { detailTitle.value = value; }
watch(() => route.fullPath, () => { detailTitle.value = ''; });
</script>

<template>
  <div id="app-shell">
    <aside class="sidebar">
      <div class="brand">
        <span class="brand-mark" aria-hidden="true">E</span>
        <div class="brand-text"><strong>Emby-go</strong><small>媒体档案控制台</small></div>
      </div>
      <nav id="nav" class="nav" aria-label="主导航">
        <button v-for="item in navigation" :key="item.page" class="nav-item" :class="{ 'is-active': route.meta.navigation === item.page }"
          :data-page="item.page" :aria-current="route.meta.navigation === item.page ? 'page' : undefined" @click="router.push('/' + item.page)">
          <component :is="item.icon" :size="17" :stroke-width="1.8" aria-hidden="true" /><span>{{ item.label }}</span>
        </button>
      </nav>
      <footer class="side-foot"><span class="status-dot" aria-hidden="true"></span>EMBY-GO · CONSOLE</footer>
    </aside>
    <main class="main">
      <header class="topbar">
        <div>
          <p id="page-eyebrow" class="crumb">{{ route.meta.crumb }}</p>
          <h1 id="page-title">{{ detailTitle || route.meta.title }}</h1>
        </div>
        <div class="top-actions">
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
  </div>
</template>
