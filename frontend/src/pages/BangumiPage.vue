<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { ChevronLeft, ChevronRight, Search, Settings } from '@lucide/vue';
import { animeApi } from '../api/anime';
import { useAdminRead } from '../composables/useAdminRead';
import { useAuthStore } from '../stores/auth';
import ReadState from '../components/common/ReadState.vue';
import MediaImage from '../components/media/MediaImage.vue';
import ArtworkJob from '../components/anime/ArtworkJob.vue';
import ArchiveSync from '../components/anime/ArchiveSync.vue';
const route = useRoute(), router = useRouter();
const auth = useAuthStore();
const year = ref(String(route.query.year ?? '0')), season = ref(String(route.query.season ?? '0')), search = ref(String(route.query.search ?? ''));
const offset = computed(() => Math.max(0, Number(route.query.offset) || 0));
const years = Array.from({ length: new Date().getFullYear() - 1899 }, (_, i) => new Date().getFullYear() + 1 - i);
const catalog = useAdminRead(signal => animeApi.bangumi(new URLSearchParams({ year: year.value, season: season.value, search: search.value, offset: String(offset.value), limit: '50' }), signal));
const imageVersion = ref(0);
watch(() => route.query, () => {
  year.value = String(route.query.year ?? '0'); season.value = String(route.query.season ?? '0'); search.value = String(route.query.search ?? ''); void catalog.load();
});
async function filter() { await router.replace({ path: '/anime', query: { year: year.value, season: season.value, search: search.value || undefined } }); }
async function page(delta: number) { await router.replace({ query: { ...route.query, offset: Math.max(0, offset.value + delta) } }); }
</script>
<template>
  <section class="panel">
    <div class="panel-head"><h2>动漫</h2><div class="row-actions">
      <RouterLink class="btn" to="/anime/mikan">蜜柑发布</RouterLink>
      <RouterLink v-if="auth.isAdministrator" class="btn" to="/anime/settings"><Settings :size="16" />来源与设置</RouterLink>
    </div></div>
    <ArchiveSync v-if="auth.isAdministrator" @updated="catalog.load" />
    <form class="anime-filters" @submit.prevent="filter">
      <label>首播年份<select v-model="year"><option value="0">全部年份</option><option v-for="y in years" :key="y" :value="String(y)">{{ y }}</option></select></label>
      <label>首播季度<select v-model="season"><option value="0">全部季度</option><option value="1">冬季</option><option value="2">春季</option><option value="3">夏季</option><option value="4">秋季</option></select></label>
      <label class="anime-search">标题<input v-model="search" maxlength="100" placeholder="中文名或原名" /></label>
      <button class="btn btn-accent"><Search :size="16" />搜索</button>
    </form>
  </section>
  <ReadState :error="catalog.error.value" :loading="catalog.loading.value" :empty="!catalog.data.value" @retry="catalog.load" />
  <ArtworkJob v-if="auth.isAdministrator" :subjects="catalog.data.value?.items.map(item => item.bangumi_id) ?? []" @updated="imageVersion++" />
  <p v-if="catalog.data.value && !catalog.data.value.items.length" class="empty-state">暂无匹配的动画</p>
  <div v-else class="anime-grid">
    <RouterLink v-for="item in catalog.data.value?.items" :key="item.bangumi_id" class="anime-card" :to="{ path: `/anime/subjects/${item.bangumi_id}`, query: route.query }">
      <div class="anime-cover"><MediaImage :src="`/api/anime/subjects/${item.bangumi_id}/image?v=${imageVersion}`" :alt="item.name" loading="lazy" /></div>
      <strong>{{ item.name }}</strong><small>{{ item.platform }} · {{ item.date || '首播日期未知' }}</small>
    </RouterLink>
  </div>
  <div class="anime-pagination"><button class="btn" title="上一页" aria-label="上一页" :disabled="offset === 0" @click="page(-50)"><ChevronLeft :size="18" /></button><span>{{ catalog.data.value?.total.toLocaleString() ?? 0 }} 部 · 第 {{ Math.floor(offset / 50) + 1 }} 页</span><button class="btn" title="下一页" aria-label="下一页" :disabled="offset + 50 >= (catalog.data.value?.total ?? 0)" @click="page(50)"><ChevronRight :size="18" /></button></div>
</template>
<style scoped>
.anime-card strong{overflow-wrap:anywhere}.anime-cover{aspect-ratio:2/3}.anime-cover :deep(img){width:100%;height:100%;object-fit:cover}.btn{display:inline-flex;gap:6px;align-items:center}
</style>
