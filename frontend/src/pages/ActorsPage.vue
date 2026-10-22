<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { Users, Sparkles, Film } from '@lucide/vue';
import { adminApi } from '../api/admin';
import { actorsApi, type ActorStatus } from '../api/actors';
import { scrapeApi, type ScrapeSettings, type SourceTest, type GFriendsStatus } from '../api/scrape';
import { useAdminRead } from '../composables/useAdminRead';
import { useTasksStore } from '../stores/tasks';
import { useToastStore } from '../stores/toasts';
import { entityIdOf } from '../lib/media';
import { imageURL } from '../lib/image-url';
import ActorSettingsForm from '../components/scrape/ActorSettingsForm.vue';
import ActorImportPanel from '../components/scrape/ActorImportPanel.vue';
import BackgroundTaskProgress from '../components/tasks/BackgroundTaskProgress.vue';
import MediaImage from '../components/media/MediaImage.vue';
import FavoriteButton from '../components/media/FavoriteButton.vue';
import ReadState from '../components/common/ReadState.vue';

const tasks = useTasksStore(), toasts = useToastStore();
const library = ref(0), status = ref<ActorStatus>('all'), search = ref(''), term = ref(''), offset = ref(0), limit = 48;
const favoritesOnly = ref(false);
const saving = ref(false), dirty = ref(false), testing = ref(new Set<string>()), indexStatus = ref<GFriendsStatus>();
const { data, error, loading, load, active } = useAdminRead(async signal => {
  const [settings, libs] = await Promise.all([scrapeApi.settings(signal), adminApi.libraries(signal)]);
  return { settings, libraries: libs.items || [] };
});
const { data: actors, error: actorsError, loading: actorsLoading, load: loadActors } = useAdminRead(signal =>
  actorsApi.list({ library_id: library.value, status: status.value, search: term.value, offset: offset.value, limit, ...(favoritesOnly.value ? {favorite:'true'} : {}) }, signal));
const sourceLabel = computed(() => ({ metatube: 'MetaTube', gfriends: 'GFriends', gfriends_then_metatube: 'GFriends 优先，MetaTube 回退' })[data.value?.settings.avatar_source ?? 'metatube']);
const scopeLabel = computed(() => data.value?.libraries.find(lib => lib.Id === library.value)?.Name || '全部媒体库');
watch([library, status, term, favoritesOnly], () => { offset.value = 0; void loadActors(); });
watch(() => tasks.revision, () => { void loadActors(); });
watch(actors, result => {
  if (result && offset.value > 0 && result.items.length === 0) {
    offset.value = Math.max(0, Math.ceil(result.total / limit) - 1) * limit;
    void loadActors();
  }
});
function searchActors() { term.value = search.value.trim(); }
function page(delta: number) { offset.value = Math.max(0, offset.value + delta * limit); void loadActors(); }
async function save(value: Partial<ScrapeSettings>) {
  if (saving.value) return;
  saving.value = true;
  try {
    const settings = await scrapeApi.save(value);
    toasts.show('头像设置已保存并即时生效');
    if (active() && data.value) { data.value = { ...data.value, settings }; indexStatus.value = undefined; void loadActors(); }
  } catch (cause) { toasts.show(cause instanceof Error ? cause.message : '保存失败', 'error'); }
  finally { saving.value = false; }
}
async function test(target: SourceTest) {
  if (testing.value.has(target)) return;
  testing.value.add(target);
  try {
    const result = await scrapeApi.test(target);
    if (active() && result.status) indexStatus.value = result.status;
    toasts.show(result.ok ? `${result.detail}（${result.elapsed_ms}ms）` : `连接失败：${result.error}`, result.ok ? 'ok' : 'error');
  } catch (cause) { toasts.show(cause instanceof Error ? cause.message : '测试失败', 'error'); }
  finally { testing.value.delete(target); }
}
</script>

<template>
  <ReadState :error="error" :loading="loading" :empty="!data" @retry="load" />
  <template v-if="data">
    <section class="panel actor-overview">
      <div class="panel-head"><div><h2>演员头像管理</h2><p class="hint">集中查看演员，一次补齐整个媒体库的缺失头像。</p></div><span class="source-badge">{{ sourceLabel }}</span></div>
      <div class="actor-actions">
        <div class="field"><label for="actor-library">处理范围</label><select id="actor-library" v-model.number="library"><option :value="0">全部媒体库</option><option v-for="lib in data.libraries" :key="lib.Id" :value="lib.Id">{{ lib.Name }}</option></select></div>
        <button id="avatar-start" class="btn btn-accent" :disabled="tasks.mutationBusy || dirty || saving || !data.settings.avatar_configured || !actors || actorsLoading || !!actorsError || actors.stats.total === 0" @click="tasks.scrape.run({library_id:library}, true)"><Sparkles :size="16" aria-hidden="true" />{{ tasks.scrape.submitting ? '正在启动…' : '一键批量补齐头像' }}</button>
        <button class="btn" :disabled="actorsLoading" @click="loadActors">刷新列表</button>
      </div>
      <p v-if="!data.settings.avatar_configured" class="hint">请先在下方保存可用的头像来源。GFriends 无需 MetaTube Token。</p>
      <p v-else-if="dirty" class="hint" role="status">头像设置有未保存修改，请保存或恢复后再开始任务。</p>
      <p class="hint">批量范围：{{ scopeLabel }}的全部演员，不受下方搜索、头像筛选和分页影响。保留已有头像及非空 NFO 头像地址。</p>
      <div v-if="actors" id="actor-stats" class="actor-stats" :aria-busy="actorsLoading">
        <div><span>演员总数</span><strong>{{ actorsLoading ? '…' : actors.stats.total }}</strong></div><div><span>已有头像</span><strong>{{ actorsLoading ? '…' : actors.stats.ready }}</strong></div><div><span>缺少头像</span><strong>{{ actorsLoading ? '…' : actors.stats.missing }}</strong></div>
      </div>
      <BackgroundTaskProgress kind="scrape" :progress="tasks.scrape.progress" :visible="tasks.scrape.visible" :busy="tasks.scrape.busy" :unavailable="tasks.scrape.unavailable" :cancelling="tasks.scrape.cancelling" @cancel="tasks.scrape.cancel()" />
      <p class="hint">头像全局共享；所选媒体库只限定候选演员，补写空头像地址可能涉及其他库。任务也会修复未完成的资料写入，实际任务数可能与缺图数不同。</p>
    </section>

    <details id="actor-config" class="actor-config">
      <summary>头像来源与存储设置 <span>{{ sourceLabel }}{{ dirty ? ' · 未保存' : '' }}</span></summary>
      <ActorSettingsForm :settings="data.settings" :saving="saving" :testing="testing" :index-status="indexStatus" @save="save" @test="test" @dirty="dirty=$event" />
      <ActorImportPanel :source="data.settings.avatars_dir" :disabled="dirty || saving" />
    </details>

    <section class="panel">
      <div class="panel-head"><h2>演员列表</h2><span v-if="actors && !actorsLoading" class="hint">共 {{ actors.total }} 位</span></div>
      <form class="actor-filters" @submit.prevent="searchActors">
        <div class="field"><label for="actor-search">搜索演员</label><input id="actor-search" v-model="search" type="search" placeholder="输入演员姓名"></div>
        <div class="field"><label for="actor-status">头像状态</label><select id="actor-status" v-model="status"><option value="all">全部演员</option><option value="missing">缺少头像</option><option value="ready">已有头像</option></select></div>
        <button class="btn" type="submit">搜索</button>
        <label class="actor-favorite-filter"><input v-model="favoritesOnly" type="checkbox">仅收藏</label>
      </form>
      <ReadState :error="actorsError" :loading="actorsLoading" :empty="!actors" @retry="loadActors" />
      <p v-if="actorsLoading && actors" role="status">正在更新演员列表…</p>
      <template v-if="actors && !actorsError && !actorsLoading">
        <div v-if="actors.items.length" class="actor-grid">
          <article v-for="actor in actors.items" :key="actor.name" class="actor-card"><RouterLink :to="{path:'/items',query:{person:actor.name,...(library ? {library_id:String(library)} : {})}}">
            <div class="actor-portrait">
              <MediaImage v-if="actor.has_image" :src="imageURL(entityIdOf('person',actor.name),'Primary',240,actor.image_tag)" loading="lazy" :alt="actor.name + '的头像'" />
              <span v-else class="actor-placeholder"><Users :size="32" aria-hidden="true" /></span>
              <span class="portrait-status" :class="{missing:!actor.has_image}">{{ actor.has_image ? '已有头像' : '缺少头像' }}</span>
              <span v-if="actor.movie_count" class="portrait-count"><Film :size="11" aria-hidden="true" />{{ actor.movie_count }}</span>
            </div>
            <strong>{{ actor.name }}</strong><small>{{ actor.movie_count }} 部影片 · 查看作品</small>
          </RouterLink><div class="actor-favorite-action"><FavoriteButton :item-id="entityIdOf('person',actor.name)" :favorite="actor.favorite" :label="actor.name" @changed="loadActors" /></div></article>
        </div>
        <p v-else class="actor-empty">{{ actors.stats.total === 0 ? '当前媒体库还没有演员。扫描带有演员信息的 NFO 后，会显示在这里。' : '没有符合筛选条件的演员。可调整姓名或头像状态后重试。' }}</p>
        <nav v-if="actors.total > limit || offset > 0" class="actor-pagination" aria-label="演员分页"><button class="btn btn-sm" :disabled="offset===0" @click="page(-1)">上一页</button><span>{{ Math.floor(offset/limit)+1 }} / {{ Math.max(1,Math.ceil(actors.total/limit)) }}</span><button class="btn btn-sm" :disabled="offset+limit>=actors.total" @click="page(1)">下一页</button></nav>
      </template>
    </section>
  </template>
</template>

<style scoped>
.actor-overview .panel-head { align-items: flex-start; }
.actor-overview .hint { line-height: 1.7; color: var(--muted); }
.source-badge { color: var(--accent); background: var(--accent-soft); border: 1px solid rgba(43, 122, 254, 0.25); border-radius: 999px; padding: 4px 12px; font-size: 12px; font-weight: 600; }
.actor-actions, .actor-filters { display: flex; flex-wrap: wrap; align-items: flex-end; gap: 12px; }
.actor-actions .field { min-width: 200px; flex: 1; max-width: 340px; }
.actor-stats { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 14px; margin: 20px 0; }
.actor-stats > div { border: 1px solid var(--line); border-radius: 8px; padding: 16px 20px; background: var(--panel-2); transition: border-color 0.15s ease; }
.actor-stats span { display: block; font-size: 11.5px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.04em; color: var(--dim); }
.actor-stats strong { display: block; font-family: var(--font-mono); font-size: 28px; font-weight: 700; margin-top: 6px; color: var(--text); }
.actor-config { margin: 20px 0 22px; }
.actor-config > summary { padding: 14px 18px; cursor: pointer; border: 1px solid var(--line); border-radius: 8px; font-weight: 600; background: var(--panel-2); transition: border-color 0.15s ease, background-color 0.15s ease; }
.actor-config > summary:hover { border-color: var(--accent); }
.actor-config > summary span { margin-left: 12px; font-size: 12px; font-weight: normal; color: var(--muted); }
.actor-config[open] > summary { margin-bottom: 16px; }
.actor-filters { margin-bottom: 22px; }
.actor-filters input, .actor-filters select { width: 100%; min-width: 0; }
.actor-filters .field:first-child { flex: 1; min-width: 160px; }
.actor-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(148px, 1fr)); gap: 22px 16px; }
.actor-card { min-width: 0; text-decoration: none; color: inherit; transition: transform 0.18s cubic-bezier(0.2, 0, 0, 1); }
.actor-card:hover { transform: translateY(-3px); }
.actor-card > a { display: block; text-decoration: none; color: inherit; }
.actor-favorite-action { display: flex; justify-content: flex-end; margin-top: 8px; }
.actor-favorite-filter { display: inline-flex; align-items: center; gap: 6px; padding: 8px 0; font-size: 13px; color: var(--muted); cursor: pointer; }
.actor-favorite-filter input { width: 16px; height: 16px; accent-color: var(--accent); }
.actor-card:hover strong { color: var(--accent); }
.actor-portrait { position: relative; aspect-ratio: 3/4; border-radius: 8px; overflow: hidden; background: var(--panel-2); border: 1px solid var(--line); transition: border-color 0.18s ease, box-shadow 0.18s ease; }
.actor-card:hover .actor-portrait { border-color: var(--accent); box-shadow: 0 6px 18px rgba(43, 122, 254, 0.25); }
.actor-portrait :deep(img) { width: 100%; height: 100%; object-fit: cover; transition: transform 0.28s cubic-bezier(0.2, 0, 0, 1); }
.actor-card:hover .actor-portrait :deep(img) { transform: scale(1.03); }
.actor-placeholder { display: grid; place-items: center; width: 100%; height: 100%; color: var(--dim); }
.portrait-status { position: absolute; bottom: 8px; left: 8px; z-index: 2; display: inline-flex; align-items: center; gap: 4px; background: rgba(0, 0, 0, 0.72); backdrop-filter: blur(6px); -webkit-backdrop-filter: blur(6px); border-radius: 999px; border: 1px solid rgba(255, 255, 255, 0.15); color: #20c997; font-size: 11px; font-weight: 700; padding: 2px 8px; }
.portrait-status.missing { color: #fab005; border-color: rgba(250, 176, 5, 0.3); }
.portrait-count { position: absolute; top: 8px; right: 8px; z-index: 2; display: inline-flex; align-items: center; gap: 4px; background: rgba(0, 0, 0, 0.72); backdrop-filter: blur(6px); -webkit-backdrop-filter: blur(6px); border-radius: 999px; border: 1px solid rgba(255, 255, 255, 0.15); color: #d0bfff; font-size: 11px; font-weight: 700; padding: 2px 7px; }
.actor-card strong, .actor-card small { display: block; overflow-wrap: anywhere; margin-top: 6px; }
.actor-card strong { font-size: 13.5px; font-weight: 600; color: var(--text); }
.actor-card small { color: var(--dim); font-size: 11.5px; }
.actor-empty { padding: 36px 0; color: var(--muted); text-align: center; }
.actor-pagination { display: flex; justify-content: center; align-items: center; gap: 16px; margin-top: 28px; }
@media(max-width:600px) {
  .actor-actions .field { max-width: none; width: 100%; }
  .actor-actions #avatar-start { flex: 1; justify-content: center; }
  .actor-stats { gap: 8px; }
  .actor-stats > div { padding: 12px; }
  .actor-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px 12px; }
  .actor-overview .panel-head { flex-direction: column; gap: 12px; }
  .actor-config > summary span { display: block; margin: 8px 0 0; }
}
</style>
