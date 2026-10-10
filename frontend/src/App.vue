<script setup lang="ts">
import { onScopeDispose, ref, watch } from 'vue';
import { RefreshCw } from '@lucide/vue';
import { useRoute, useRouter } from 'vue-router';
import { useAuthStore } from './stores/auth';
import { sanitizeReturnTo } from './router/legacy-url';
import { useMediaWallStore } from './stores/media-wall';
import { useToastStore } from './stores/toasts';
import { viewHistories } from './router/view-history';
import { useTasksStore } from './stores/tasks';
import TaskProgress from './components/tasks/TaskProgress.vue';
import { usePlayerStore } from './stores/player';
import PlayerDialog from './components/media/PlayerDialog.vue';
import ScrapePreviewDialog from './components/scrape/ScrapePreviewDialog.vue';
import { useScrapePreviewStore } from './stores/scrape-preview';
import { useScheduledStore } from './stores/scheduled';

const auth = useAuthStore();
const route = useRoute();
const router = useRouter();
const updateAvailable = ref(false);
const wall = useMediaWallStore();
const toasts = useToastStore();
const tasks = useTasksStore();
const player = usePlayerStore();
const scrapePreview = useScrapePreviewStore(), scheduled = useScheduledStore();
watch(() => route.fullPath, () => scrapePreview.close());
watch(() => auth.authenticated, (authenticated) => { if (!authenticated) { scrapePreview.close(); scheduled.editingId = null; } });
watch(() => route.fullPath, () => player.close());
watch(() => auth.authenticated, (authenticated) => { if (!authenticated) player.close(); });
watch(() => auth.authenticated, (authenticated) => { if (authenticated) void tasks.restoreAll(); else tasks.reset(); }, { immediate: true });
watch(() => auth.token, () => wall.clear());
watch(() => tasks.revision, () => { wall.dirty = true; });
onScopeDispose(() => { scrapePreview.close(); player.close(); tasks.reset(); toasts.clear(); viewHistories.get(router)?.dispose(); });

function preloadError(event: Event) {
  event.preventDefault();
  updateAvailable.value = true;
}
function refresh() { window.location.reload(); }

window.addEventListener('vite:preloadError', preloadError);
onScopeDispose(() => window.removeEventListener('vite:preloadError', preloadError));
watch(() => auth.expiration, () => {
  if (!route.meta.requiresAuth) return;
  toasts.show('登录已失效，请重新登录', 'error');
  const returnTo = sanitizeReturnTo(route.fullPath);
  void router.replace({ path: '/login', query: returnTo ? { returnTo } : {} });
});
</script>

<template>
  <RouterView />
  <TaskProgress v-if="auth.authenticated" />
  <PlayerDialog v-if="auth.authenticated && player.current" :key="player.current.id" :options="player.current" @close="player.close" />
  <ScrapePreviewDialog v-if="auth.authenticated && scrapePreview.current" />
  <div id="toasts" class="toasts" aria-live="assertive">
    <div v-for="toast in toasts.items" :key="toast.id" class="toast" :class="[toast.kind, { out: toast.out }]" role="status">{{ toast.message }}</div>
    <div v-if="updateAvailable" class="toast error" role="alert">
      <p>页面资源已更新。刷新会丢失未保存的输入。</p>
      <button class="btn" type="button" @click="refresh">
        <RefreshCw :size="15" :stroke-width="1.8" aria-hidden="true" /><span>刷新页面</span>
      </button>
    </div>
  </div>
</template>
