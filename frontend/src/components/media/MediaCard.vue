<script setup lang="ts">
import { computed } from 'vue';
import { Activity, RefreshCw, Trash2, Play, Film } from '@lucide/vue';
import type { MovieDto, WallUserData } from '../../api/contracts';
import MediaImage from './MediaImage.vue';
import { imageURL } from '../../lib/image-url';
import { numberUnlessInTitle, statusText } from '../../lib/media';
import { useMediaVisibility } from '../../composables/useMediaVisibility';

const props = defineProps<{ movie: MovieDto; tags?: Record<string, string>; userdata?: WallUserData; busy?: Set<string>; canManage?: boolean }>();
const emit = defineEmits<{ open: [id: number]; quickplay: [id: number]; probe: [id: number]; reread: [id: number]; remove: [id: number] }>();
const { hideImages } = useMediaVisibility();
const displayTitle = computed(() => props.movie.Title || (props.canManage ? props.movie.source_path : `#${props.movie.id}`));
const progress = computed(() => props.userdata && props.movie.RuntimeSeconds > 0 && props.userdata.position_ticks > 0 ? Math.min(100, Math.round(props.userdata.position_ticks / (props.movie.RuntimeSeconds * 10000000) * 100)) : 0);
function key(event: KeyboardEvent, id: number) {
  if (event.target !== event.currentTarget || (event.key !== 'Enter' && event.key !== ' ')) return;
  event.preventDefault(); emit('open', id);
}
</script>
<template>
  <article class="wall-card" :data-play="movie.id" tabindex="0" role="button"
    :aria-label="`查看 ${displayTitle} 详情`" @click="emit('open', movie.id)" @keydown="key($event, movie.id)">
    <div class="wall-poster" :class="{ 'is-media-hidden': hideImages }">
      <MediaImage v-if="!hideImages && (movie.PosterPath || movie.LandscapePath)"
        :src="imageURL(movie.id, movie.PosterPath ? 'Primary' : 'Thumb', 320, movie.PosterPath ? tags?.Primary : tags?.Thumb)"
        :fallback="movie.cover_url" alt="" loading="lazy" />
      <span v-else-if="hideImages" class="wall-path wall-path-hidden" :title="displayTitle">
        <Film class="wall-hidden-icon" :size="28" aria-hidden="true" />
        <span class="wall-hidden-title">{{ displayTitle }}</span>
      </span>
      <span v-else class="wall-path" :title="canManage ? movie.source_path : displayTitle">{{ canManage ? movie.source_path || displayTitle : displayTitle }}</span>
      <div class="wall-badges"><span v-if="canManage && movie.Status !== 'success'" class="wall-badge" :class="movie.Status">{{ statusText[movie.Status] || movie.Status }}</span><span v-if="userdata?.played" class="wall-badge played">已看</span><span v-if="userdata?.favorite" class="wall-badge fav">♥</span><span v-if="movie.AdditionalParts?.length" class="wall-badge multi">CD×{{ movie.AdditionalParts.length + 1 }}</span></div>
      <div v-if="progress" class="wall-progress"><i :style="{width: progress + '%'}"></i></div>
      <div v-if="canManage" class="wall-actions">
        <button class="icon-btn" :data-probe="movie.id" title="探测媒体信息（ffprobe，写回 NFO）" :disabled="busy?.has('probe:' + movie.id)" @click.stop="emit('probe', movie.id)"><Activity :size="15" aria-hidden="true" /></button>
        <button class="icon-btn" :data-reread="movie.id" title="重读 .strm 与 NFO" :disabled="busy?.has('reread:' + movie.id)" @click.stop="emit('reread', movie.id)"><RefreshCw :size="15" aria-hidden="true" /></button>
        <button class="icon-btn danger" :data-delete="movie.id" title="删除索引（不删文件）" :disabled="busy?.has('remove:' + movie.id)" @click.stop="emit('remove', movie.id)"><Trash2 :size="15" aria-hidden="true" /></button>
      </div>
      <button v-if="['success', 'manual'].includes(movie.Status)" class="wall-play" :data-quickplay="movie.id" title="直接播放" @click.stop="emit('quickplay', movie.id)"><Play :size="24" aria-hidden="true" /></button>
    </div>
    <div class="wall-meta"><strong :title="displayTitle">{{ displayTitle }}</strong>
      <small>{{ [movie.television ? `${movie.television.title} · S${movie.television.season}E${movie.television.episode}` : numberUnlessInTitle(movie.Title, movie.Number), movie.Year, movie.OriginalTitle].filter(Boolean).join(' · ') || movie.source_protocol }}</small></div>
  </article>
</template>
<style scoped>
.wall-path { padding-top: 40px; }
.wall-path-hidden {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  gap: 10px;
  padding: 40px 12px 14px;
  color: var(--muted);
}
.wall-hidden-icon {
  opacity: 0.5;
  color: var(--dim);
}
.wall-hidden-title {
  font-size: 13px;
  font-weight: 600;
  line-height: 1.35;
  display: -webkit-box;
  -webkit-line-clamp: 4;
  -webkit-box-orient: vertical;
  overflow: hidden;
  word-break: break-all;
}
</style>
