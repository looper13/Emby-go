<script setup lang="ts">
import { onMounted, onScopeDispose, ref } from 'vue';
import { Search, X, Link, Unlink } from '@lucide/vue';
import { tmdbApi, type TMDBMapping, type TMDBSeriesMapping } from '../../api/anime-artwork';
const props = defineProps<{ subject: number; title: string; series: TMDBSeriesMapping; episode?: TMDBMapping }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const dialog = ref<HTMLDialogElement>(), search = ref(props.title), busy = ref(false), message = ref('');
const configured = ref(false), selected = ref<number | null>(null), season = ref<number | null>(null);
const candidates = ref<Awaited<ReturnType<typeof tmdbApi.search>>>([]);
const seasons = ref<Awaited<ReturnType<typeof tmdbApi.series>>['seasons']>([]);
const episodes = ref<Awaited<ReturnType<typeof tmdbApi.season>>>([]);
const controller = new AbortController();
onScopeDispose(() => { controller.abort(); dialog.value?.close(); });
async function action(fn: () => Promise<void>) {
  if (busy.value) return;
  busy.value = true; message.value = '';
  try { await fn(); } catch (e) { if (!controller.signal.aborted) message.value = e instanceof Error ? e.message : 'TMDB 请求失败'; }
  finally { busy.value = false; }
}
onMounted(() => {
  dialog.value?.showModal();
  void action(async () => {
    configured.value = (await tmdbApi.config(controller.signal)).configured;
    if (!configured.value) return;
    if (props.episode) seasons.value = (await tmdbApi.series(props.series.series_id, controller.signal)).seasons;
    else candidates.value = await tmdbApi.search(search.value, controller.signal);
  });
});
function find() { void action(async () => { selected.value = null; candidates.value = await tmdbApi.search(search.value, controller.signal); }); }
function chooseSeason() { void action(async () => { selected.value = null; episodes.value = []; if (season.value != null) episodes.value = await tmdbApi.season(props.series.series_id, season.value, controller.signal); }); }
function save(remove = false) {
  void action(async () => {
    if (props.episode) {
      if (!remove && (season.value == null || selected.value == null)) return;
      await tmdbApi.mapEpisode(props.episode, props.series.revision, season.value ?? 0, selected.value ?? 0, remove);
    } else { if (!remove && selected.value == null) return; await tmdbApi.mapSeries(props.subject, remove ? 0 : selected.value!, props.series.revision); }
    if (!controller.signal.aborted) { emit('saved'); emit('close'); }
  });
}
</script>
<template>
  <dialog ref="dialog" class="anime-dialog tmdb-dialog" aria-labelledby="tmdb-map-title" @cancel.prevent="!busy && emit('close')">
    <div class="panel-head"><h2 id="tmdb-map-title">{{ episode ? '关联剧照章节' : '关联 TMDB 剧集' }} · {{ title }}</h2><button class="btn" title="关闭" aria-label="关闭" :disabled="busy" @click="emit('close')"><X :size="18" /></button></div>
    <p v-if="episode?.series_id">当前：S{{ episode.season }} E{{ episode.number }} · {{ episode.name }}</p>
    <p v-else-if="series.series_id">当前：{{ series.name }} · TMDB {{ series.series_id }}</p>
    <p v-if="!configured && !busy">TMDB 凭据未配置</p>
    <template v-if="configured">
      <form v-if="!episode" class="anime-filters" @submit.prevent="find"><label class="anime-search">剧集名称<input v-model="search" maxlength="100" :disabled="busy" /></label><button class="btn" :disabled="busy || !search.trim()"><Search :size="16" />搜索</button></form>
      <label v-else class="season-field">TMDB 季<select v-model="season" :disabled="busy" @change="chooseSeason"><option :value="null">选择季</option><option v-for="s in seasons" :key="s.number" :value="s.number">{{ s.number }} · {{ s.name }}</option></select></label>
      <div class="tmdb-candidates">
        <template v-if="!episode"><label v-for="item in candidates" :key="item.id" class="anime-candidate"><input v-model="selected" type="radio" name="tmdb-selection" :value="item.id" :disabled="busy" /><span>{{ item.name }} · {{ item.date || '日期未知' }}<small>{{ item.original_name }} · TMDB {{ item.id }}</small><small>{{ item.overview }}</small></span></label><p v-if="!busy && !candidates.length">没有匹配剧集</p></template>
        <template v-else><label v-for="item in episodes" :key="item.id" class="anime-candidate"><input v-model="selected" type="radio" name="tmdb-selection" :value="item.number" :disabled="busy" /><span>{{ item.number }} · {{ item.name }}<small>{{ item.date }} · TMDB {{ item.id }}</small><small>{{ item.overview }}</small></span></label><p v-if="season != null && !busy && !episodes.length">该季暂无章节</p></template>
      </div>
    </template>
    <p v-if="message" role="alert">{{ message }}</p><p v-if="busy" role="status">处理中</p>
    <div class="row-actions"><button v-if="episode ? episode.series_id : series.series_id" class="btn" :disabled="busy" @click="save(true)"><Unlink :size="16" />解除关联</button><button class="btn btn-accent" :disabled="busy || selected == null || !configured" @click="save()"><Link :size="16" />确认关联</button></div>
  </dialog>
</template>
<style scoped>
.tmdb-dialog h2{font-size:18px;overflow-wrap:anywhere}.panel-head h2{min-width:0}.panel-head>button{flex:none}.tmdb-candidates{max-height:45vh;overflow:auto}.anime-candidate{align-items:flex-start}.anime-candidate span{min-width:0;overflow-wrap:anywhere}.anime-candidate small{display:block;max-height:100px;overflow:auto;line-height:1.6}.btn{display:inline-flex;align-items:center;gap:6px}.season-field{display:grid;gap:6px;margin:12px 0}
</style>
