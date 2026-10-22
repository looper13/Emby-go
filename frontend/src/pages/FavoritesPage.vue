<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { ChevronLeft, ChevronRight, RefreshCw, Search, Film } from '@lucide/vue';
import { favoritesApi, favoriteDestination, type FavoriteItem } from '../api/favorites';
import { usePageRequest } from '../composables/usePageRequest';
import FavoriteButton from '../components/media/FavoriteButton.vue';
import MediaImage from '../components/media/MediaImage.vue';
import { imageURL } from '../lib/image-url';
import { useMediaVisibility } from '../composables/useMediaVisibility';
const route = useRoute(), router = useRouter(), scope = usePageRequest();
const { hideImages } = useMediaVisibility();
const tabs = [['Movie,Episode', '影片与单集'], ['Series,Season', '剧集与季'], ['Person', '演员'], ['Genre', '类型'], ['Studio', '厂商'], ['Tag', '标签'], ['BoxSet', '合集']] as const;
const type = computed(() => tabs.find(([id]) => id === route.query.type)?.[0] ?? tabs[0][0]);
const offset = computed(() => { const n = Number(route.query.offset ?? 0); return Number.isSafeInteger(n) && n >= 0 ? n : 0; });
const search = ref(''), items = ref<FavoriteItem[]>([]), total = ref(0), loading = ref(false), error = ref('');
function navigate(values: Record<string, string | undefined>) { void router.replace({ path: '/favorites', query: { ...route.query, ...values } }); }
async function load() {
  const current = scope.begin(); loading.value = true; error.value = '';
  search.value = typeof route.query.search === 'string' ? route.query.search : '';
  try {
    const result = await favoritesApi.list(type.value, search.value, offset.value, current.signal);
    if (!current.isCurrent()) return;
    if (offset.value > 0 && result.items.length === 0) { navigate({ offset: String(Math.max(0, Math.ceil(result.total / 48) - 1) * 48) }); return; }
    items.value = result.items; total.value = result.total;
  } catch (cause) { if (current.isCurrent()) error.value = cause instanceof Error ? cause.message : '读取收藏失败'; }
  finally { if (current.isCurrent()) loading.value = false; }
}
watch(() => route.fullPath, () => { items.value = []; void load(); }, { immediate: true });
</script>
<template>
  <section class="favorites-page">
    <div class="favorites-tabs" role="tablist" aria-label="收藏类型"><button v-for="[id, label] in tabs" :key="id" role="tab" :aria-selected="type === id" @click="navigate({type:id,offset:undefined})">{{ label }}</button></div>
    <form class="favorites-toolbar" @submit.prevent="navigate({search:search.trim() || undefined,offset:undefined})">
      <label class="favorite-search"><Search :size="17" aria-hidden="true" /><input v-model="search" type="search" aria-label="搜索收藏" placeholder="搜索收藏"></label>
      <button class="icon-btn" type="submit" title="搜索" aria-label="搜索"><Search :size="18" /></button>
      <button class="icon-btn" type="button" title="刷新收藏" aria-label="刷新收藏" :disabled="loading" @click="load"><RefreshCw :size="18" /></button>
      <span class="hint">{{ total }} 项收藏</span>
    </form>
    <p v-if="error" role="alert">{{ error }} <button class="btn" @click="load">重试</button></p>
    <p v-if="loading" role="status">加载中…</p>
    <div v-else-if="!error && items.length" class="favorites-grid">
      <article v-for="item in items" :key="item.Id" class="favorite-card">
        <RouterLink :to="favoriteDestination(item)" class="favorite-link">
          <div class="favorite-art" :class="{ portrait: item.Type === 'Person' }"><MediaImage v-if="!hideImages && item.ImageTags.Primary" :src="imageURL(item.Id,'Primary',320,item.ImageTags.Primary)" :alt="item.Name" loading="lazy" /><Film v-else :size="36" aria-hidden="true" /></div>
          <strong>{{ item.Name }}</strong>
        </RouterLink>
        <div class="favorite-card-actions"><FavoriteButton :item-id="item.Id" :favorite="item.UserData.IsFavorite" :label="item.Name" @changed="load" /></div>
      </article>
    </div>
    <p v-else-if="!error" class="favorites-empty">{{ search ? '没有匹配的收藏' : '暂无收藏' }}</p>
    <nav v-if="total > 48 || offset > 0" class="favorite-pagination" aria-label="收藏分页">
      <button class="icon-btn" title="上一页" aria-label="上一页" :disabled="loading || offset === 0" @click="navigate({offset:String(Math.max(0,offset-48))})"><ChevronLeft :size="20" /></button>
      <span>{{ Math.floor(offset/48)+1 }} / {{ Math.max(1,Math.ceil(total/48)) }}</span>
      <button class="icon-btn" title="下一页" aria-label="下一页" :disabled="loading || offset+48 >= total" @click="navigate({offset:String(offset+48)})"><ChevronRight :size="20" /></button>
    </nav>
  </section>
</template>
<style scoped>
.favorites-page { min-width: 0; }
.favorites-tabs { display: flex; flex-wrap: wrap; border-bottom: 1px solid var(--line); gap: 4px 18px; margin-bottom: 18px; }
.favorites-tabs button { border: 0; border-bottom: 2px solid transparent; border-radius: 0; background: transparent; color: var(--muted); padding: 10px 4px; font-family: var(--font-sans); font-size: 13.5px; font-weight: 500; cursor: pointer; transition: color 0.15s ease, border-color 0.15s ease; }
.favorites-tabs button:hover { color: var(--text); }
.favorites-tabs [aria-selected="true"] { border-color: var(--accent); color: var(--accent); font-weight: 600; }
.favorites-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; margin: 16px 0 22px; }
.favorite-search { display: flex; align-items: center; gap: 8px; flex: 1; max-width: 360px; min-width: 120px; }
.favorite-search input { min-width: 0; width: 100%; }
.favorites-toolbar .hint { margin: 0 0 0 auto; color: var(--dim); font-size: 12.5px; }
.favorites-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(148px, 1fr)); gap: 22px 16px; }
.favorite-card { min-width: 0; transition: transform 0.18s cubic-bezier(0.2, 0, 0, 1); }
.favorite-card:hover { transform: translateY(-3px); }
.favorite-link { color: inherit; text-decoration: none; display: block; }
.favorite-art { display: grid; place-items: center; aspect-ratio: 0.7; border-radius: 8px; overflow: hidden; background: var(--panel-2); border: 1px solid var(--line); color: var(--dim); transition: border-color 0.18s ease, box-shadow 0.18s ease; }
.favorite-art.portrait { aspect-ratio: 3/4; }
.favorite-card:hover .favorite-art { border-color: var(--accent); box-shadow: 0 6px 18px rgba(43, 122, 254, 0.25); }
.favorite-art :deep(img) { width: 100%; height: 100%; object-fit: cover; transition: transform 0.28s cubic-bezier(0.2, 0, 0, 1); }
.favorite-card:hover .favorite-art :deep(img) { transform: scale(1.03); }
.favorite-link strong { display: block; margin-top: 8px; font-size: 13.5px; font-weight: 600; line-height: 1.4; color: var(--text); overflow-wrap: anywhere; }
.favorite-card:hover strong { color: var(--accent); }
.favorite-card-actions { display: flex; justify-content: flex-end; margin-top: 6px; }
.favorites-empty { padding: 48px 0; color: var(--muted); text-align: center; font-size: 14px; }
.favorite-pagination { display: flex; align-items: center; justify-content: center; gap: 16px; margin-top: 28px; }
@media(max-width:480px) { .favorites-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px 12px; } }
</style>
