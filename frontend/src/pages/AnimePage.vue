<script setup lang="ts">
import { computed, nextTick, onScopeDispose, ref, watch } from "vue";
import { onBeforeRouteLeave, useRoute, useRouter } from "vue-router";
import { animeApi } from "../api/anime";
import { useAdminRead } from "../composables/useAdminRead";
import ReadState from "../components/common/ReadState.vue";
import { useToastStore } from "../stores/toasts";
import AnimeReleases from "../components/anime/AnimeReleases.vue";
const route = useRoute(),
  router = useRouter(),
  toasts = useToastStore();
const now = new Date();
const year = ref(Number(route.query.year) || now.getFullYear()),
  season = ref(
    Number(route.query.season) || Math.floor(now.getMonth() / 3) + 1,
  ),
  weekday = ref(String(route.query.weekday ?? "-1")),
  search = ref(String(route.query.search ?? ""));
const source = ref(String(route.query.source ?? "")),
  playable = ref(route.query.playable === "true");
const offset = computed(() => Number(route.query.offset) || 0);
const years = Array.from(
  { length: now.getFullYear() - 1989 },
  (_, i) => now.getFullYear() + 1 - i,
);
const query = () =>
  new URLSearchParams({
    year: String(year.value),
    season: String(season.value),
    weekday: weekday.value,
    search: search.value,
    limit: "50",
    offset: String(offset.value),
    source: source.value,
    playable: String(playable.value),
  });
const { data, error, loading, load } = useAdminRead((signal) =>
  animeApi.catalog(query(), signal),
);
const searching = ref(false),
  resourceSearch = ref(false),
  resourceOffset = ref(0),
  resourceQuery = ref("");
const resources = useAdminRead(async (signal) =>
  resourceSearch.value
    ? animeApi.search(resourceQuery.value, resourceOffset.value, signal)
    : { items: [], total: 0 },
);
const busy = ref(false);
let timer: ReturnType<typeof setInterval> | undefined;
watch(
  () => data.value?.state.status,
  (status) => {
    clearInterval(timer);
    if (status === "running" || status === "queued")
      timer = setInterval(() => {
        if (!loading.value) void load();
      }, 1500);
  },
);
onScopeDispose(() => clearInterval(timer));
watch(
  () => route.query,
  () => {
    year.value = Number(route.query.year) || now.getFullYear();
    season.value =
      Number(route.query.season) || Math.floor(now.getMonth() / 3) + 1;
    weekday.value = String(route.query.weekday ?? "-1");
    search.value = String(route.query.search ?? "");
    source.value = String(route.query.source ?? "");
    playable.value = route.query.playable === "true";
    resourceSearch.value = false;
    void load();
  },
);
onBeforeRouteLeave(() => {
  sessionStorage.setItem(
    "anime-view",
    JSON.stringify({
      query: route.fullPath,
      top: window.scrollY,
      entry:
        document.activeElement?.closest<HTMLElement>(".anime-card")?.dataset
          .entry,
    }),
  );
});
watch(data, async (value) => {
  if (!value) return;
  const raw = sessionStorage.getItem("anime-view");
  if (!raw) return;
  try {
    const saved = JSON.parse(raw);
    if (saved.query === route.fullPath) {
      await nextTick();
      window.scrollTo(0, Number(saved.top) || 0);
      if (
        Number.isSafeInteger(Number(saved.entry)) &&
        Number(saved.entry) > 0
      ) {
        document
          .querySelector<HTMLElement>(`[data-entry="${Number(saved.entry)}"]`)
          ?.focus({ preventScroll: true });
      }
      sessionStorage.removeItem("anime-view");
    }
  } catch {
    /* Invalid saved positions are disposable. */
  }
});
async function filter() {
  resourceSearch.value = false;
  await router.replace({
    path: "/anime",
    query: {
      year: year.value,
      season: season.value,
      weekday: weekday.value,
      search: search.value,
      source: source.value || undefined,
      playable: playable.value ? "true" : undefined,
    },
  });
}
async function page(next: number) {
  await router.replace({ query: { ...route.query, offset: next } });
  window.scrollTo(0, 0);
}
async function refresh() {
  if (busy.value) return;
  busy.value = true;
  try {
    await animeApi.refresh({ year: year.value, season: season.value });
    await load();
  } catch (e) {
    toasts.show(e instanceof Error ? e.message : "刷新失败", "error");
  } finally {
    busy.value = false;
  }
}
async function findResources() {
  if (!search.value.trim()) return;
  resourceOffset.value = 0;
  resourceQuery.value = search.value.trim();
  searching.value = true;
  resourceSearch.value = true;
  await resources.load();
  searching.value = false;
}
async function resourcePage(delta: number) {
  resourceOffset.value = Math.max(0, resourceOffset.value + delta);
  await resources.load();
}
</script>
<template>
  <section class="panel">
    <div class="panel-head">
      <h2>动漫</h2>
      <div class="row-actions">
        <RouterLink class="btn" to="/anime/settings">来源与设置</RouterLink
        ><button
          class="btn btn-accent"
          :disabled="busy || data?.state.status === 'running'"
          @click="refresh"
        >
          刷新本季度
        </button>
      </div>
    </div>
    <p class="hint">
      番剧资料来自
      Bangumi，目录与发布资源来自蜜柑。关联已有媒体并检查通过后即可播放。
    </p>
    <form class="anime-filters" @submit.prevent="filter">
      <label
        >资料<select v-model="source" @change="filter">
          <option value="">全部来源</option>
          <option value="bangumi">已匹配 Bangumi</option>
          <option value="mikan">仅蜜柑基础资料</option>
        </select></label
      >
      <label class="anime-playable-filter"
        ><input
          v-model="playable"
          type="checkbox"
          @change="filter"
        />仅可播放</label
      >
      <label
        >年份<select v-model.number="year" @change="filter">
          <option v-for="y in years" :key="y" :value="y">{{ y }}</option>
        </select></label
      >
      <label
        >季度<select v-model.number="season" @change="filter">
          <option :value="1">冬季 · 1 月</option>
          <option :value="2">春季 · 4 月</option>
          <option :value="3">夏季 · 7 月</option>
          <option :value="4">秋季 · 10 月</option>
        </select></label
      >
      <label
        >放送<select v-model="weekday" @change="filter">
          <option value="-1">全部</option>
          <option
            v-for="(name, i) in [
              '周日',
              '周一',
              '周二',
              '周三',
              '周四',
              '周五',
              '周六',
              '剧场版',
              'OVA',
            ]"
            :key="i"
            :value="String(i)"
          >
            {{ name }}
          </option>
        </select></label
      >
      <label class="anime-search"
        >标题<input
          v-model="search"
          maxlength="100"
          placeholder="搜索本季度番剧" /></label
      ><button class="btn">搜索</button
      ><button
        type="button"
        class="btn"
        :disabled="!search.trim() || searching"
        @click="findResources"
      >
        搜索蜜柑发布
      </button>
    </form>
    <ReadState :error="error" :loading="loading" :empty="!data" @retry="load" />
    <p v-if="data?.state.status === 'running'" role="status">
      正在更新资料，已有内容仍可浏览。<button
        class="btn btn-sm"
        @click="animeApi.cancel().then(load)"
      >
        取消刷新
      </button>
    </p>
    <p v-else-if="data?.state.error" role="alert">
      {{ data.state.error }} · 当前显示已保存的资料。
    </p>
  </section>
  <section v-if="resourceSearch" class="panel">
    <h2>蜜柑发布搜索</h2>
    <ReadState
      :error="resources.error.value"
      :loading="resources.loading.value"
      :empty="!resources.data.value"
      @retry="resources.load"
    /><AnimeReleases
      v-if="resources.data.value"
      :items="resources.data.value.items"
    />
    <p class="hint">
      “{{ resourceQuery }}”共
      {{ resources.data.value?.total ?? 0 }} 条，每页最多 50
      条。发布记录不会自动成为播放源。
    </p>
    <div class="anime-pagination">
      <button
        class="btn"
        :disabled="resources.loading.value || resourceOffset === 0"
        @click="resourcePage(-50)"
      >
        上一页
      </button>
      <span>第 {{ Math.floor(resourceOffset / 50) + 1 }} 页</span>
      <button
        class="btn"
        :disabled="
          resources.loading.value ||
          resourceOffset + 50 >= (resources.data.value?.total ?? 0)
        "
        @click="resourcePage(50)"
      >
        下一页
      </button>
    </div>
  </section>
  <div v-else-if="data?.items.length" class="anime-grid">
    <RouterLink
      v-for="item in data.items"
      :key="item.id"
      :to="{ path: `/anime/entries/${item.id}`, query: route.query }"
      class="anime-card"
      :data-entry="item.id"
    >
      <div class="anime-cover">
        <img
          :src="`/api/anime/images/${item.id}`"
          :alt="item.name"
          loading="lazy"
          @error="
            ($event.target as HTMLImageElement).style.visibility = 'hidden'
          "
        /><span>动漫</span>
      </div>
      <strong>{{ item.name }}</strong
      ><small>{{ item.subject_id ? "已匹配资料" : "待获取详情" }}</small>
    </RouterLink>
  </div>
  <section v-else-if="data && data.state.status !== 'running'" class="panel">
    <p>当前筛选下暂无条目。可调整筛选或刷新季度目录。</p>
  </section>
  <div v-if="data && !resourceSearch" class="anime-pagination">
    <button
      class="btn"
      :disabled="offset === 0"
      @click="page(Math.max(0, offset - 50))"
    >
      上一页</button
    ><span>共 {{ data.total }} 部</span
    ><button
      class="btn"
      :disabled="offset + 50 >= data.total"
      @click="page(offset + 50)"
    >
      下一页
    </button>
  </div>
</template>
<style>
.anime-filters {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  align-items: end;
}
.anime-filters label {
  display: grid;
  gap: 6px;
}
.anime-search {
  flex: 1;
  min-width: 180px;
}
.anime-search input {
  min-width: 0;
  width: 100%;
}
.anime-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: 20px;
}
.anime-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  color: inherit;
  text-decoration: none;
  min-width: 0;
}
.anime-card strong {
  overflow-wrap: anywhere;
  line-height: 1.5;
}
.anime-card small,
.anime-muted {
  color: var(--muted);
}
.anime-cover {
  aspect-ratio: 2/3;
  background: var(--panel);
  border-radius: 10px;
  overflow: hidden;
  position: relative;
}
.anime-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  position: relative;
  z-index: 1;
}
.anime-cover span {
  position: absolute;
  inset: 40% 0;
  text-align: center;
  opacity: 0.4;
}
.anime-pagination {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 20px;
  margin: 24px 0;
}
.anime-resource {
  border-bottom: 1px solid var(--line);
  padding: 16px 0;
  overflow-wrap: anywhere;
}
.anime-resource p {
  margin: 6px 0;
}
.anime-resource .row-actions {
  justify-content: flex-start;
  flex-wrap: wrap;
}
.anime-hero {
  display: grid;
  grid-template-columns: 180px minmax(0, 1fr);
  gap: 24px;
}
.anime-cover-placeholder {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 180px;
  color: var(--muted);
  background: var(--panel);
  border-radius: 10px;
}
.anime-hero > img {
  width: 100%;
  border-radius: 10px;
}
.anime-summary {
  white-space: pre-wrap;
  line-height: 1.8;
  overflow-wrap: anywhere;
}
.anime-episode {
  display: flex;
  gap: 16px;
  align-items: center;
  justify-content: space-between;
  padding: 14px 0;
  border-bottom: 1px solid var(--line);
}
.anime-episode > div {
  min-width: 0;
}
.anime-episode strong {
  overflow-wrap: anywhere;
}
.anime-episode .row-actions {
  flex-wrap: wrap;
}
.anime-dialog {
  color: var(--text);
  background: var(--panel, #202020);
  border: 1px solid var(--line);
  border-radius: 12px;
  width: min(720px, calc(100vw - 32px));
  max-height: 85vh;
  padding: 24px;
  overflow: auto;
}
.anime-dialog::backdrop {
  background: #0009;
}
.anime-candidate {
  display: block;
  border-bottom: 1px solid var(--line);
  padding: 12px 0;
  overflow-wrap: anywhere;
}
.anime-candidate input {
  width: auto;
  margin-right: 8px;
}
.anime-candidate small {
  display: block;
  margin-top: 6px;
}
.anime-state {
  font-size: 13px;
  margin: 8px 0;
  color: var(--muted);
}
@media (max-width: 600px) {
  .anime-hero {
    grid-template-columns: 1fr;
  }
  .anime-hero > img {
    width: 140px;
  }
  .anime-episode {
    align-items: flex-start;
    flex-direction: column;
  }
  .anime-filters > * {
    max-width: 100%;
  }
  .anime-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 16px;
  }
}
</style>
