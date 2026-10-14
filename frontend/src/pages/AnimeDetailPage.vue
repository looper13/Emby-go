<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  animeApi,
  animeStatus,
  type AnimeBinding,
  type AnimeSubject,
} from "../api/anime";
import { useAdminRead } from "../composables/useAdminRead";
import { useToastStore } from "../stores/toasts";
import { usePlayerStore } from "../stores/player";
import { apiClient } from "../api/client";
import ReadState from "../components/common/ReadState.vue";
import AnimeReleases from "../components/anime/AnimeReleases.vue";
import AnimeBindingDialog from "../components/anime/AnimeBindingDialog.vue";
const route = useRoute(),
  router = useRouter(),
  toasts = useToastStore(),
  player = usePlayerStore();
const id = computed(() => String(route.params.id));
const { data, error, loading, load } = useAdminRead((signal) =>
  animeApi.detail(id.value, signal),
);
const episodeOffset = ref(0),
  releaseOffset = ref(0),
  expandedSummary = ref(false),
  filter = ref("");
const episodes = useAdminRead((signal) =>
  animeApi.episodes(id.value, episodeOffset.value, signal),
);
const releases = useAdminRead((signal) =>
  animeApi.releases(id.value, releaseOffset.value, filter.value, signal),
);
const title = computed(
  () => data.value?.subject?.name || data.value?.entry.name || "番剧详情",
);
const bindingDialog = ref<{
    target: string;
    title: string;
    binding: AnimeBinding;
  } | null>(null),
  busy = ref(""),
  mapping = ref(false),
  mapQuery = ref(""),
  candidates = ref<AnimeSubject[]>([]),
  mappingError = ref("");
let timer: ReturnType<typeof setInterval> | undefined,
  active = true;
let mapAbort: AbortController | undefined;
const filmID = computed(() => `anime-movie:${data.value?.subject?.id}`);
async function reload() {
  await Promise.all([load(), episodes.load(), releases.load()]);
}
async function action(key: string, fn: () => Promise<unknown>) {
  if (busy.value) return;
  busy.value = key;
  const requested = id.value;
  try {
    await fn();
    if (active && id.value === requested) await reload();
  } catch (e) {
    if (active && id.value === requested) {
      toasts.show(e instanceof Error ? e.message : "操作失败", "error");
      await reload();
    }
  } finally {
    busy.value = "";
  }
}
async function poll() {
  const p = await animeApi.progress();
  if (!active) return;
  if (!p.running) {
    clearInterval(timer);
    if (p.error) toasts.show(p.error, "error");
    await reload();
  }
}
function watchTask() {
  if (!active) return;
  clearInterval(timer);
  timer = setInterval(() => void poll().catch(() => {}), 1500);
}
watch(
  () => data.value?.state.status,
  (status) => {
    if (status === "running" || status === "queued") watchTask();
  },
);
watch(id, () => {
  mapAbort?.abort();
  candidates.value = [];
  clearInterval(timer);
  bindingDialog.value = null;
  mapping.value = false;
  episodeOffset.value = 0;
  releaseOffset.value = 0;
  expandedSummary.value = false;
  void reload();
});
onScopeDispose(() => {
  active = false;
  clearInterval(timer);
  mapAbort?.abort();
});
async function refresh() {
  await action("refresh", () => animeApi.refresh({ id: Number(id.value) }));
  watchTask();
}
async function play(target: string, name: string) {
  const requested = id.value;
  await action(target, async () => {
    const source = await animeApi.playback(target);
    const detail = await apiClient.request(
      `/Items/${encodeURIComponent(target)}`,
      {
        auth: "required",
      },
    );
    const ud =
      typeof detail === "object" && detail !== null && "UserData" in detail
        ? detail.UserData
        : null;
    const ticks =
      typeof ud === "object" && ud !== null && "PlaybackPositionTicks" in ud
        ? Number(ud.PlaybackPositionTicks)
        : 0;
    if (!active || id.value !== requested) return;
    player.open({
      title: name,
      url: `/Videos/${encodeURIComponent(target)}/stream`,
      proxyURL: `/Videos/${encodeURIComponent(target)}/proxy`,
      type: source.Container === "m3u8" ? "m3u8" : "mp4",
      itemId: target,
      resumeTicks: ticks,
    });
  });
}
async function searchMapping() {
  mapAbort?.abort();
  const controller = new AbortController();
  mapAbort = controller;
  const requested = id.value;
  mappingError.value = "";
  try {
    const result = await animeApi.candidates(
      requested,
      mapQuery.value,
      controller.signal,
    );
    if (active && !controller.signal.aborted && id.value === requested)
      candidates.value = result.items;
  } catch (e) {
    if (active && !controller.signal.aborted && id.value === requested)
      mappingError.value = e instanceof Error ? e.message : "查询失败";
  }
}
function openMapping() {
  mapping.value = !mapping.value;
  if (mapping.value) {
    mapQuery.value = title.value;
    void searchMapping();
  }
}
async function mapTo(subject: AnimeSubject) {
  if (!data.value) return;
  await action("map", () =>
    animeApi.map(id.value, subject.bangumi_id, data.value!.entry.revision),
  );
  watchTask();
}
function episodePage(delta: number) {
  episodeOffset.value = Math.max(0, episodeOffset.value + delta);
  void episodes.load();
}
function check(target: string) {
  void action(target, () => animeApi.check(target));
}
function releasePage(delta: number) {
  releaseOffset.value = Math.max(0, releaseOffset.value + delta);
  void releases.load();
}
</script>
<template>
  <div
    class="row-actions"
    style="justify-content: space-between; margin-bottom: 20px"
  >
    <button
      class="btn"
      @click="router.push({ path: '/anime/mikan', query: route.query })"
    >
      ← 返回动漫库</button
    ><button
      class="btn"
      :disabled="!!busy || data?.state.status === 'running'"
      @click="refresh"
    >
      刷新资料与资源
    </button>
  </div>
  <ReadState :error="error" :loading="loading" :empty="!data" @retry="reload" />
  <template v-if="data">
    <section class="panel anime-hero">
      <img
        v-if="data.subject?.image || data.entry.image"
        :src="`/api/anime/images/${id}`"
        :alt="title"
        @error="($event.target as HTMLImageElement).style.visibility = 'hidden'"
      />
      <div v-else class="anime-cover-placeholder" aria-label="暂无封面">
        暂无封面
      </div>
      <div>
        <h2>{{ title }}</h2>
        <p v-if="data.subject?.original_name !== title" class="hint">
          {{ data.subject?.original_name }}
        </p>
        <p>
          {{ data.subject?.platform || "类型待匹配" }} ·
          {{ data.subject?.date || "日期待补充" }} · 评分
          {{ data.subject?.rating || "暂无" }}
        </p>
        <p class="anime-state">
          {{
            data.entry.manual
              ? "人工确认的 Bangumi 映射"
              : data.subject
                ? "源站明确对应的 Bangumi 资料"
                : "尚未匹配 Bangumi，不生成章节"
          }}
          ·
          {{
            data.state.status === "running" || data.state.status === "queued"
              ? "正在更新资料"
              : data.state.last_success_at
                ? new Date(data.state.last_success_at).toLocaleString()
                : "尚未更新"
          }}
        </p>
        <p v-if="data.state.error" role="alert">
          {{ data.state.error }} · 已保存资料仍可使用。
        </p>
        <div class="row-actions" style="justify-content: flex-start">
          <a
            v-if="data.subject"
            class="btn btn-sm"
            :href="`https://bgm.tv/subject/${data.subject.bangumi_id}`"
            target="_blank"
            rel="noopener noreferrer"
            >Bangumi ↗</a
          ><button class="btn btn-sm" @click="openMapping">
            {{ data.subject ? "纠正资料映射" : "匹配 Bangumi 资料" }}
          </button>
        </div>
      </div>
    </section>
    <section v-if="mapping" class="panel">
      <h2>确认 Bangumi 资料</h2>
      <form class="anime-filters" @submit.prevent="searchMapping">
        <label class="anime-search"
          >标题<input v-model="mapQuery" maxlength="100" /></label
        ><button class="btn">搜索候选</button>
      </form>
      <p>请核对年份、媒介与续作季数。确认后自动刷新不会替换此映射。</p>
      <p v-if="mappingError" role="alert">{{ mappingError }}</p>
      <article
        v-for="candidate in candidates"
        :key="candidate.bangumi_id"
        class="anime-episode"
      >
        <div>
          <strong>{{ candidate.name }}</strong>
          <p class="hint">
            {{ candidate.original_name }} · {{ candidate.platform }} ·
            {{ candidate.date }} · ID {{ candidate.bangumi_id }}
          </p>
        </div>
        <button class="btn" :disabled="!!busy" @click="mapTo(candidate)">
          确认此条目
        </button>
      </article>
    </section>
    <section v-if="data.subject?.summary" class="panel">
      <h2>简介</h2>
      <p class="anime-summary">
        {{
          expandedSummary || data.subject.summary.length <= 400
            ? data.subject.summary
            : `${data.subject.summary.slice(0, 400)}…`
        }}
      </p>
      <button
        v-if="data.subject.summary.length > 400"
        class="btn btn-sm"
        :aria-expanded="expandedSummary"
        @click="expandedSummary = !expandedSummary"
      >
        {{ expandedSummary ? "收起简介" : "展开简介" }}
      </button>
    </section>
    <section v-if="data.binding" class="panel">
      <h2>剧场版</h2>
      <div class="anime-episode">
        <div>
          <strong>{{ title }}</strong>
          <p>
            {{ animeStatus(data.binding.status) }}
            <span v-if="data.binding.error">· {{ data.binding.error }}</span>
          </p>
          <p v-if="data.binding.checked_at" class="hint">
            上次检查：{{
              new Date(data.binding.checked_at).toLocaleString()
            }}；播放前会复核过期结果。
          </p>
        </div>
        <div class="row-actions">
          <button
            class="btn"
            @click="
              bindingDialog = { target: filmID, title, binding: data.binding }
            "
          >
            关联媒体</button
          ><button
            v-if="data.binding.movie_id"
            class="btn"
            :disabled="!!busy"
            @click="check(filmID)"
          >
            检查资源</button
          ><button
            v-if="data.binding.status === 'playable'"
            class="btn btn-accent"
            :disabled="!!busy"
            @click="play(filmID, title)"
          >
            播放
          </button>
        </div>
      </div>
    </section>
    <section v-else-if="data.subject" class="panel">
      <h2>剧集与特别篇</h2>
      <ReadState
        :error="episodes.error.value"
        :loading="episodes.loading.value"
        :empty="!episodes.data.value"
        @retry="episodes.load"
      />
      <article
        v-for="item in episodes.data.value?.items"
        :key="item.episode.id"
        class="anime-episode"
      >
        <div>
          <strong
            >{{ item.episode.season === 0 ? "特别篇" : "正片" }}
            {{ item.episode.sort }} ·
            {{
              item.episode.name || item.episode.original_name || "标题待补充"
            }}</strong
          >
          <p v-if="!item.episode.active" class="anime-state">
            上游已不再列出此章节，保留已有媒体关联。
          </p>
          <p class="anime-state">
            {{ animeStatus(item.binding.status) }}
            {{ item.binding.movie_id ? `· 媒体 ${item.binding.movie_id}` : "" }}
            {{ item.binding.error ? `· ${item.binding.error}` : "" }}
          </p>
          <p v-if="item.binding.checked_at" class="hint">
            上次检查：{{
              new Date(item.binding.checked_at).toLocaleString()
            }}；播放前会复核过期结果。
          </p>
        </div>
        <div class="row-actions">
          <button
            class="btn btn-sm"
            @click="
              bindingDialog = {
                target: item.item_id,
                title: `${title} / ${item.episode.sort}`,
                binding: item.binding,
              }
            "
          >
            关联媒体</button
          ><button
            v-if="item.binding.movie_id"
            class="btn btn-sm"
            :disabled="!!busy"
            @click="check(item.item_id)"
          >
            检查资源</button
          ><button
            v-if="item.binding.status === 'playable'"
            class="btn btn-accent btn-sm"
            :disabled="!!busy"
            @click="play(item.item_id, `${title} / ${item.episode.sort}`)"
          >
            播放
          </button>
        </div>
      </article>
      <div class="anime-pagination">
        <button
          class="btn"
          :disabled="episodeOffset === 0"
          @click="episodePage(-50)"
        >
          上一页</button
        ><span>{{ episodes.data.value?.total ?? 0 }} 个章节</span
        ><button
          class="btn"
          :disabled="episodeOffset + 50 >= (episodes.data.value?.total ?? 0)"
          @click="episodePage(50)"
        >
          下一页
        </button>
      </div>
    </section>
    <section class="panel">
      <h2>蜜柑发布资源</h2>
      <p class="hint">
        字幕组的发布记录可查看和复制；播放使用上方关联的已有媒体。
      </p>
      <form
        class="anime-filters"
        @submit.prevent="
          releaseOffset = 0;
          releases.load();
        "
      >
        <label class="anime-search"
          >字幕组、分辨率或字幕<input
            v-model="filter"
            placeholder="例如 1080P 或字幕组名称" /></label
        ><button class="btn">筛选</button
        ><button
          type="button"
          class="btn"
          @click="
            filter = '';
            releaseOffset = 0;
            releases.load();
          "
        >
          清除
        </button>
      </form>
      <ReadState
        :error="releases.error.value"
        :loading="releases.loading.value"
        :empty="!releases.data.value"
        @retry="releases.load"
      /><AnimeReleases
        v-if="releases.data.value"
        :items="releases.data.value.items"
      />
      <div class="anime-pagination">
        <button
          class="btn"
          :disabled="releaseOffset === 0"
          @click="releasePage(-50)"
        >
          上一页</button
        ><span>{{ releases.data.value?.total ?? 0 }} 条发布</span
        ><button
          class="btn"
          :disabled="releaseOffset + 50 >= (releases.data.value?.total ?? 0)"
          @click="releasePage(50)"
        >
          下一页
        </button>
      </div>
    </section>
  </template>
  <AnimeBindingDialog
    v-if="bindingDialog"
    v-bind="bindingDialog"
    @close="bindingDialog = null"
    @saved="reload"
  />
</template>
