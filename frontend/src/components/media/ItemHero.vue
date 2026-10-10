<script setup lang="ts">
import { computed } from 'vue';
import { Play, Activity, RefreshCw, Search } from '@lucide/vue';
import type { MovieDto, ImageDto } from '../../api/contracts';
import { numberUnlessInTitle, statusText, fmtDuration } from '../../lib/media';
import { imageURL } from '../../lib/image-url';
import MediaImage from './MediaImage.vue';
const props = defineProps<{ movie: MovieDto; images?: ImageDto[]; playable?: boolean; busy?: Set<string> }>();
const emit = defineEmits<{ back: []; play: []; probe: []; reread: []; scrape: []; entity: [key: string, value: string] }>();
const tag = (type: string) => props.images?.find(image => image.ImageType === type)?.ImageTag;
const cover = computed(() => props.movie.cover_url.trim());
const poster = computed(() => props.movie.PosterPath ? imageURL(props.movie.id, 'Primary', 320, tag('Primary')) : cover.value);
const backdrop = computed(() => props.movie.BackdropPath ? imageURL(props.movie.id, 'Backdrop', 1280, tag('Backdrop'), 0) : props.movie.LandscapePath ? imageURL(props.movie.id, 'Thumb', 1280, tag('Thumb')) : poster.value);
</script>
<template>
  <header class="item-hero">
    <div class="item-hero-bg"><MediaImage v-if="backdrop" :src="backdrop" :fallback="cover" alt="" /></div>
    <div class="item-hero-body">
      <MediaImage v-if="poster" class="item-hero-poster" :src="poster" :fallback="cover" alt="" />
      <div class="item-hero-text"><button id="item-back" class="btn item-back" title="返回媒体墙 (Esc)" @click="emit('back')">← 返回媒体墙</button>
        <h1 class="item-hero-title">{{ movie.Title || movie.id }}</h1>
        <p class="detail-sub">{{ [numberUnlessInTitle(movie.Title, movie.Number), movie.Year, fmtDuration(movie.RuntimeSeconds), movie.Rating ? `★ ${movie.Rating}` : ''].filter(Boolean).join(' · ') || '—' }}</p>
        <div class="detail-badges"><span class="badge" :class="movie.Status">{{ statusText[movie.Status] || movie.Status }}</span><span v-if="movie.collection" class="badge manual">合集 {{ movie.collection }}</span><button v-for="tagName in movie.Tags" :key="tagName" class="tag-chip" data-entity-key="tag" :data-entity-value="tagName" :title="'按标签筛选：' + tagName" @click="emit('entity', 'tag', tagName)">#{{ tagName }}</button></div>
        <div class="item-hero-actions"><button v-if="playable ?? ['success', 'manual'].includes(movie.Status)" id="detail-play" class="btn btn-accent" @click="emit('play')"><Play :size="15" aria-hidden="true" /><span>播放</span></button><span v-else class="hint" style="margin:0">该影片不可播放（待补录 / 协议不兼容）</span><button id="detail-probe" class="btn" :disabled="busy?.has('probe:' + movie.id)" @click="emit('probe')"><Activity :size="15" aria-hidden="true" /><span>探测媒体信息</span></button><button id="detail-scrape" class="btn" @click="emit('scrape')"><Search :size="15" aria-hidden="true" /><span>刮削</span></button><button id="detail-reread" class="btn" :disabled="busy?.has('reread:' + movie.id)" @click="emit('reread')"><RefreshCw :size="15" aria-hidden="true" /><span>重读源</span></button></div>
      </div>
    </div>
  </header>
</template>
