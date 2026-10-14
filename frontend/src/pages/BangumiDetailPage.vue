<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { ArrowLeft, ChevronLeft, ChevronRight, RefreshCw, Play, Link, Check, Search } from '@lucide/vue';
import { animeApi, animeStatus, type AnimeBinding } from '../api/anime';
import { apiClient } from '../api/client';
import { useAdminRead } from '../composables/useAdminRead';
import { usePlayerStore } from '../stores/player';
import { useToastStore } from '../stores/toasts';
import ReadState from '../components/common/ReadState.vue';
import MediaImage from '../components/media/MediaImage.vue';
import AnimeBindingDialog from '../components/anime/AnimeBindingDialog.vue';
import ArtworkJob from '../components/anime/ArtworkJob.vue';
import TMDBMappingDialog from '../components/anime/TMDBMappingDialog.vue';
import type { TMDBMapping } from '../api/anime-artwork';
const route = useRoute(), player = usePlayerStore(), toasts = useToastStore();
const id = computed(() => String(route.params.id));
const { data, error, loading, load, active } = useAdminRead(signal => animeApi.bangumiSubject(id.value, signal));
const busy = ref(false), offset = ref(0), section = ref('episodes');
const dialog = ref<{target:string;title:string;binding:AnimeBinding}|null>(null);
const tmdbDialog = ref<{title:string;episode?:TMDBMapping}|null>(null), imageVersion = ref(0);
const episodes = computed(() => data.value?.episodes.slice(offset.value, offset.value + 50) ?? []);
const movie = computed(() => `anime-movie:${data.value?.subject.id}`);
let timer: ReturnType<typeof setInterval>|undefined;
onScopeDispose(() => clearInterval(timer));
watch(id, () => { clearInterval(timer); dialog.value=null;tmdbDialog.value=null;offset.value=0;void load(); });
async function action(fn:()=>Promise<unknown>) { if(busy.value)return;busy.value=true;try{await fn();if(active())await load();}catch(e){toasts.show(e instanceof Error?e.message:'操作失败','error');}finally{busy.value=false;} }
async function refresh() { await action(()=>animeApi.refreshSubject(id.value));clearInterval(timer);timer=setInterval(async()=>{try{const p=await animeApi.progress();if(!p.running){clearInterval(timer);if(p.error)toasts.show(p.error,'error');await load();}}catch{clearInterval(timer);}},1500); }
async function play(target:string,title:string) { await action(async()=>{
  const requested=id.value;const source=await animeApi.playback(target);const detail=await apiClient.request(`/Items/${encodeURIComponent(target)}`,{auth:'required'});
  const ud=typeof detail==='object'&&detail!==null&&'UserData' in detail?detail.UserData:null;
  const ticks=typeof ud==='object'&&ud!==null&&'PlaybackPositionTicks' in ud?Number(ud.PlaybackPositionTicks):0;
  if(active()&&requested===id.value)player.open({title,url:`/Videos/${encodeURIComponent(target)}/stream`,proxyURL:`/Videos/${encodeURIComponent(target)}/proxy`,type:source.Container==='m3u8'?'m3u8':'mp4',itemId:target,resumeTicks:ticks});
}); }
function bind(target:string,title:string,binding:AnimeBinding){dialog.value={target,title,binding};}
function check(target:string){void action(()=>animeApi.check(target));}
</script>
<template>
  <div class="row-actions"><RouterLink class="btn" :to="{path:'/anime',query:route.query}"><ArrowLeft :size="16" />返回动漫库</RouterLink><button class="btn" :disabled="busy||!data" @click="refresh"><RefreshCw :size="16" />刷新 Bangumi 资料</button></div>
  <ReadState :error="error" :loading="loading" :empty="!data" @retry="load" />
  <template v-if="data">
    <section class="panel anime-hero">
      <div class="bangumi-poster"><MediaImage :src="`/api/anime/subjects/${id}/image?size=large&v=${imageVersion}`" :alt="data.subject.name" /></div>
      <div><h2>{{ data.subject.name }}</h2><p>{{ data.subject.original_name }}</p><p>{{ data.subject.platform }} · {{ data.subject.date || '首播日期未知' }} · {{ data.subject.rating || '暂无评分' }}</p><p class="bangumi-summary">{{ data.subject.summary || '暂无简介' }}</p><a :href="`https://bgm.tv/subject/${data.subject.bangumi_id}`" target="_blank" rel="noopener noreferrer">Bangumi 来源</a></div>
    </section>
    <ArtworkJob :subjects="[data.subject.bangumi_id]" people @updated="imageVersion++" />
    <nav class="bangumi-tabs" aria-label="番剧内容"><button v-for="tab in [{id:'episodes',label:'章节'},{id:'people',label:'角色与制作人员'},{id:'stills',label:'每集剧照'},{id:'releases',label:'发布资源'}]" :key="tab.id" :aria-pressed="section===tab.id" :class="{selected:section===tab.id}" @click="section=tab.id">{{ tab.label }}</button></nav>
    <section v-if="section==='episodes'" class="panel">
      <h2>{{ data.binding ? '剧场版' : '章节' }}</h2>
      <div v-if="data.binding" class="anime-episode"><div><strong>{{ data.subject.name }}</strong><p>{{ animeStatus(data.binding.status) }}</p></div><div class="row-actions"><button class="btn" :disabled="busy" @click="bind(movie,data.subject.name,data.binding)"><Link :size="16" />关联媒体</button><button v-if="data.binding.movie_id" class="btn" :disabled="busy" @click="check(movie)"><Check :size="16" />检查资源</button><button v-if="data.binding.status==='playable'" class="btn btn-accent" :disabled="busy" @click="play(movie,data.subject.name)"><Play :size="16" />播放</button></div></div>
      <template v-else>
        <div v-for="item in episodes" :key="item.episode.id" class="anime-episode"><div><strong>{{ item.episode.season===0?'特别篇':'正片' }} {{ item.episode.sort }} · {{ item.episode.name || item.episode.original_name }}</strong><p>{{ animeStatus(item.binding.status) }}<span v-if="!item.episode.active"> · 来源已不再列出</span></p><small v-if="item.binding.error">{{ item.binding.error }}</small></div><div class="row-actions"><button class="btn" :disabled="busy" @click="bind(item.item_id,item.episode.name,item.binding)"><Link :size="16" />关联媒体</button><button v-if="item.binding.movie_id" class="btn" :disabled="busy" @click="check(item.item_id)"><Check :size="16" />检查资源</button><button v-if="item.binding.status==='playable'" class="btn btn-accent" :disabled="busy" @click="play(item.item_id,item.episode.name)"><Play :size="16" />播放</button></div></div>
        <p v-if="!data.episodes.length">暂无章节</p><div class="anime-pagination"><button class="btn" title="上一页" aria-label="上一页" :disabled="offset===0" @click="offset=Math.max(0,offset-50)"><ChevronLeft :size="18" /></button><span>{{ data.episodes.length }} 个章节</span><button class="btn" title="下一页" aria-label="下一页" :disabled="offset+50>=data.episodes.length" @click="offset+=50"><ChevronRight :size="18" /></button></div>
      </template>
    </section>
    <section v-else-if="section==='people'" class="panel"><h2>角色与制作人员</h2><div class="bangumi-people"><article v-for="p in data.people" :key="`${p.kind}:${p.id}:${p.role}`"><div class="bangumi-avatar"><MediaImage :src="`/api/anime/people/${p.kind}/${p.id}/image?v=${imageVersion}`" :alt="p.name" loading="lazy" /></div><a :href="`https://bgm.tv/${p.kind}/${p.id}`" target="_blank" rel="noopener noreferrer"><strong>{{ p.name }}</strong></a><small>{{ p.kind==='character'?'角色':'制作人员' }} · {{ p.id }}</small><p>{{ p.summary }}</p></article></div><p v-if="!data.people.length">暂无人物资料</p></section>
    <section v-else-if="section==='stills'" class="panel">
      <div class="panel-head"><h2>每集剧照</h2><button class="btn" @click="tmdbDialog={title:data.subject.name}"><Link :size="16" />{{ data.tmdb.series_id ? '更改 TMDB 剧集' : '关联 TMDB 剧集' }}</button></div>
      <p v-if="data.tmdb.series_id"><a :href="`https://www.themoviedb.org/tv/${data.tmdb.series_id}`" target="_blank" rel="noopener noreferrer">{{ data.tmdb.name }} · TMDB {{ data.tmdb.series_id }}</a></p>
      <p v-else>尚未关联 TMDB 剧集</p>
      <article v-for="item in episodes" :key="item.episode.id" class="episode-stills">
        <div class="panel-head"><h3>{{ item.episode.sort }} · {{ item.episode.name || item.episode.original_name }}</h3><button class="btn" :disabled="!data.tmdb.series_id" @click="tmdbDialog={title:item.episode.name,episode:item.tmdb}"><Link :size="16" />{{ item.tmdb.series_id ? '更改或刷新剧照' : '关联剧照章节' }}</button></div>
        <p v-if="item.tmdb.series_id">S{{ item.tmdb.season }} E{{ item.tmdb.number }} · {{ item.tmdb.name }}</p>
        <div v-if="item.tmdb.stills.length" class="stills-grid"><a v-for="(_,index) in item.tmdb.stills" :key="`${item.tmdb.revision}:${index}`" :href="`/api/anime/episodes/${item.episode.id}/stills/${item.tmdb.revision}/${index}`" target="_blank" rel="noopener noreferrer"><MediaImage :src="`/api/anime/episodes/${item.episode.id}/stills/${item.tmdb.revision}/${index}`" :alt="`${item.episode.name} 剧照 ${index+1}`" loading="lazy" /></a></div>
        <p v-else class="still-empty">{{ item.tmdb.series_id ? 'TMDB 暂无本集剧照' : '尚未关联剧照章节' }}</p>
      </article>
      <p v-if="!data.episodes.length">暂无章节</p>
      <div class="anime-pagination"><button class="btn" title="上一页" aria-label="上一页" :disabled="offset===0" @click="offset=Math.max(0,offset-50)"><ChevronLeft :size="18" /></button><span>{{ data.episodes.length }} 个章节</span><button class="btn" title="下一页" aria-label="下一页" :disabled="offset+50>=data.episodes.length" @click="offset+=50"><ChevronRight :size="18" /></button></div>
      <small>Images from TMDB</small>
    </section>
    <section v-else class="panel"><h2>发布资源</h2><div v-for="entry in data.entries" :key="entry.id" class="anime-episode"><strong>{{ entry.name }}</strong><RouterLink class="btn" :to="`/anime/entries/${entry.id}`">查看蜜柑发布</RouterLink></div><p v-if="!data.entries.length">尚未关联蜜柑条目</p><RouterLink class="btn" :to="{path:'/anime/mikan',query:{search:data.subject.name}}"><Search :size="16" />搜索蜜柑</RouterLink></section>
  </template>
  <AnimeBindingDialog v-if="dialog" v-bind="dialog" @close="dialog=null" @saved="load" />
  <TMDBMappingDialog v-if="tmdbDialog && data" v-bind="tmdbDialog" :subject="data.subject.bangumi_id" :series="data.tmdb" @close="tmdbDialog=null" @saved="load" />
</template>
<style scoped>
.btn{display:inline-flex;align-items:center;gap:6px}.bangumi-poster{width:100%;aspect-ratio:2/3;overflow:hidden}.bangumi-poster :deep(img){width:100%;height:100%;object-fit:cover}.anime-hero>div:last-child{min-width:0}.anime-hero h2{font-size:24px;overflow-wrap:anywhere}.bangumi-summary{white-space:pre-line;line-height:1.7}.bangumi-tabs{display:flex;border-bottom:1px solid var(--line,#ddd);gap:16px;margin:20px 0;flex-wrap:wrap}.bangumi-tabs button{border:0;background:none;padding:10px 4px;color:inherit;border-bottom:2px solid transparent}.bangumi-tabs .selected{border-bottom-color:var(--accent,#28795e);font-weight:600}.bangumi-people{display:grid;grid-template-columns:repeat(auto-fill,minmax(220px,1fr));gap:16px}.bangumi-people article{border-bottom:1px solid var(--line,#ddd);padding:12px 0;min-width:0}.bangumi-people small{display:block}.bangumi-people p{font-size:13px;line-height:1.6;white-space:pre-line;overflow-wrap:anywhere;max-height:180px;overflow:auto}.anime-episode>div:first-child{min-width:0;overflow-wrap:anywhere}@media(max-width:600px){.anime-hero h2{font-size:20px}.bangumi-poster{max-width:180px}.anime-episode{align-items:flex-start}}
</style>
<style scoped>
.panel{animation:none}
.bangumi-avatar{width:80px;height:100px;margin-bottom:10px;overflow:hidden}.bangumi-avatar :deep(img){width:100%;height:100%;object-fit:cover}.episode-stills{padding:16px 0;border-bottom:1px solid var(--line,#ddd)}.episode-stills h3{font-size:16px;margin:0;min-width:0;overflow-wrap:anywhere}.episode-stills .panel-head{gap:12px;flex-wrap:wrap}.stills-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:12px}.stills-grid a{display:block;aspect-ratio:16/9;overflow:hidden;background:var(--bg)}.stills-grid :deep(img){width:100%;height:100%;object-fit:contain}.still-empty{font-size:13px;padding:12px 0}.bangumi-people a{overflow-wrap:anywhere}
</style>
