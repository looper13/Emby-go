<script setup lang="ts">
import { computed } from 'vue';
import { Play, Activity, RefreshCw, Search, Film, Star, ArrowLeft } from '@lucide/vue';
import type { MovieDto, ImageDto } from '../../api/contracts';
import { numberUnlessInTitle, statusText, fmtDuration } from '../../lib/media';
import { imageURL } from '../../lib/image-url';
import MediaImage from './MediaImage.vue';
import FavoriteButton from './FavoriteButton.vue';
import { useMediaVisibility } from '../../composables/useMediaVisibility';

const props = defineProps<{
  movie: MovieDto;
  images?: ImageDto[];
  playable?: boolean;
  busy?: Set<string>;
  favorite?: boolean;
  canManage?: boolean;
}>();

const emit = defineEmits<{
  back: [];
  play: [];
  probe: [];
  reread: [];
  scrape: [];
  entity: [key: string, value: string];
}>();

const { hideImages } = useMediaVisibility();
const tag = (type: string) => props.images?.find(image => image.ImageType === type)?.ImageTag;
const cover = computed(() => props.movie.cover_url.trim());
const poster = computed(() => props.movie.PosterPath ? imageURL(props.movie.id, 'Primary', 320, tag('Primary')) : cover.value);
const backdrop = computed(() =>
  props.movie.BackdropPath
    ? imageURL(props.movie.id, 'Backdrop', 1280, tag('Backdrop'), 0)
    : props.movie.LandscapePath
      ? imageURL(props.movie.id, 'Thumb', 1280, tag('Thumb'))
      : poster.value
);
</script>

<template>
  <header class="item-hero">
    <!-- 背景层：高斯模糊微光 + 双重黑度渐变阴影（参考 Nowen Video Hills Lite 视觉） -->
    <div class="item-hero-bg">
      <MediaImage v-if="!hideImages && backdrop" :src="backdrop" :fallback="cover" alt="" />
      <div class="hero-scrim-left" />
      <div class="hero-scrim-bottom" />
      <div class="hero-ambient-glow" />
    </div>

    <!-- 顶部悬浮返回按钮（毛玻璃胶囊） -->
    <div class="item-hero-topbar">
      <button id="item-back" class="btn item-back" title="返回媒体墙 (Esc)" @click="emit('back')">
        <ArrowLeft :size="15" aria-hidden="true" />
        <span>← 返回媒体墙</span>
      </button>
    </div>

    <!-- 主体：海报 + 信息区 + 标签 + 操作栏 -->
    <div class="item-hero-body">
      <!-- 封面海报（卡片悬浮与立体阴影） -->
      <div class="item-hero-poster-wrap">
        <MediaImage
          v-if="!hideImages && poster"
          class="item-hero-poster"
          :src="poster"
          :fallback="cover"
          alt=""
        />
        <div v-else-if="hideImages" class="item-hero-poster image-placeholder" aria-label="封面已隐藏">
          <Film :size="40" aria-hidden="true" />
        </div>
      </div>

      <!-- 右侧文本与控制 -->
      <div class="item-hero-text">
        <!-- 电视节目眉标 -->
        <p v-if="movie.television" class="detail-eyebrow">
          <span class="eyebrow-series">{{ movie.television.title }}</span>
          <span class="eyebrow-sep">·</span>
          <span class="eyebrow-ep">{{ movie.television.season === 0 ? '特别篇' : `第 ${movie.television.season} 季` }} · 第 {{ movie.television.episode }} 集</span>
        </p>

        <!-- 标题行与收藏按钮 -->
        <div class="item-title-actions">
          <h1 class="item-hero-title">{{ movie.Title || movie.id }}</h1>
          <FavoriteButton
            v-if="['success','manual'].includes(movie.Status)"
            :item-id="movie.id"
            :favorite="favorite"
            :label="movie.Title"
          />
        </div>

        <!-- 原名与番号 -->
        <p v-if="movie.OriginalTitle && movie.OriginalTitle !== movie.Title" class="detail-orig-title">
          {{ movie.OriginalTitle }}
        </p>

        <!-- 元数据徽标栏 (Rating, Year, Duration, Number) -->
        <div class="detail-meta-pills">
          <span v-if="movie.Rating" class="detail-meta-pill rating">
            <Star :size="12" fill="currentColor" aria-hidden="true" />
            <span>{{ movie.Rating }}</span>
          </span>
          <span v-if="movie.Year" class="detail-meta-pill">{{ movie.Year }}</span>
          <span v-if="movie.RuntimeSeconds" class="detail-meta-pill">{{ fmtDuration(movie.RuntimeSeconds) }}</span>
          <span v-if="numberUnlessInTitle(movie.Title, movie.Number)" class="detail-meta-pill mono">{{ numberUnlessInTitle(movie.Title, movie.Number) }}</span>
          <span v-if="canManage" class="badge" :class="movie.Status">{{ statusText[movie.Status] || movie.Status }}</span>
          <span v-if="movie.collection" class="badge manual">合集 {{ movie.collection }}</span>
        </div>

        <!-- 标签芯片 -->
        <div v-if="movie.Tags?.length" class="detail-badges">
          <button
            v-for="tagName in movie.Tags"
            :key="tagName"
            class="tag-chip"
            data-entity-key="tag"
            :data-entity-value="tagName"
            :title="'按标签筛选：' + tagName"
            @click="emit('entity', 'tag', tagName)"
          >
            #{{ tagName }}
          </button>
        </div>

        <!-- 操作按钮行 (播放、探测、刮削、重读) -->
        <div class="item-hero-actions">
          <button
            v-if="playable ?? ['success', 'manual'].includes(movie.Status)"
            id="detail-play"
            class="btn btn-accent btn-play-large"
            @click="emit('play')"
          >
            <Play :size="17" fill="currentColor" aria-hidden="true" />
            <span>播放</span>
          </button>
          <span v-else class="hint" style="margin:0">该媒体不可播放（待补录 / 协议不兼容）</span>

          <button
            v-if="canManage"
            id="detail-probe"
            class="btn btn-hero-action"
            :disabled="busy?.has('probe:' + movie.id)"
            @click="emit('probe')"
          >
            <Activity :size="15" aria-hidden="true" />
            <span>探测媒体信息</span>
          </button>

          <button
            v-if="canManage && !movie.television"
            id="detail-scrape"
            class="btn btn-hero-action"
            @click="emit('scrape')"
          >
            <Search :size="15" aria-hidden="true" />
            <span>刮削</span>
          </button>

          <button
            v-if="canManage"
            id="detail-reread"
            class="btn btn-hero-action"
            :disabled="busy?.has('reread:' + movie.id)"
            @click="emit('reread')"
          >
            <RefreshCw :size="15" aria-hidden="true" />
            <span>重读源</span>
          </button>
        </div>

        <!-- 播放源及音视频轨道控制插槽 -->
        <slot name="playback" />
      </div>
    </div>
  </header>
</template>

<style scoped>
.item-hero {
  position: relative;
  overflow: hidden;
  border-radius: var(--radius);
  border: 1px solid var(--line);
  background: var(--panel-2);
}

.item-hero-bg {
  position: absolute;
  inset: 0;
  overflow: hidden;
  pointer-events: none;
}

.item-hero-bg img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  transform: scale(1.02);
  filter: saturate(1.1) brightness(0.9);
}

.hero-scrim-left {
  position: absolute;
  inset: 0;
  background: linear-gradient(90deg, rgba(14, 15, 18, 0.95) 0%, rgba(14, 15, 18, 0.72) 38%, rgba(14, 15, 18, 0.15) 75%, transparent 100%);
}

.hero-scrim-bottom {
  position: absolute;
  inset: 0;
  background: linear-gradient(0deg, var(--bg) 0%, rgba(14, 15, 18, 0.82) 20%, rgba(14, 15, 18, 0.2) 50%, transparent 80%);
}

.hero-ambient-glow {
  position: absolute;
  inset: 0;
  background: radial-gradient(circle at 75% 20%, var(--accent-soft), transparent 32rem);
  opacity: 0.8;
}

[data-theme="light"] .hero-scrim-left {
  background: linear-gradient(90deg, rgba(248, 249, 250, 0.96) 0%, rgba(248, 249, 250, 0.75) 40%, rgba(248, 249, 250, 0.18) 75%, transparent 100%);
}

[data-theme="light"] .hero-scrim-bottom {
  background: linear-gradient(0deg, var(--bg) 0%, rgba(248, 249, 250, 0.88) 22%, rgba(248, 249, 250, 0.3) 55%, transparent 85%);
}

.item-hero-topbar {
  position: relative;
  z-index: 10;
  padding: 14px 20px 0;
}

.item-back {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: 32px;
  padding: 0 12px;
  border-radius: 999px;
  background: rgba(14, 16, 20, 0.45) !important;
  border: 1px solid rgba(255, 255, 255, 0.14) !important;
  color: #ededee !important;
  font-size: 12px;
  font-weight: 500;
  backdrop-filter: blur(14px);
  -webkit-backdrop-filter: blur(14px);
  transition: all 0.15s ease;
}

.item-back:hover {
  background: rgba(14, 16, 20, 0.75) !important;
  border-color: rgba(255, 255, 255, 0.28) !important;
  color: #ffffff !important;
  transform: translateY(-1px);
}

[data-theme="light"] .item-back {
  background: rgba(255, 255, 255, 0.75) !important;
  border-color: rgba(0, 0, 0, 0.12) !important;
  color: #1a1b1e !important;
}

[data-theme="light"] .item-back:hover {
  background: rgba(255, 255, 255, 0.95) !important;
  border-color: rgba(0, 0, 0, 0.22) !important;
}

.item-hero-body {
  position: relative;
  z-index: 5;
  display: flex;
  gap: 26px;
  align-items: flex-end;
  padding: 16px 24px 26px;
  min-height: 290px;
}

.item-hero-poster-wrap {
  flex: 0 0 156px;
  aspect-ratio: 2 / 3;
}

.item-hero-poster {
  width: 100%;
  height: 100%;
  object-fit: cover;
  border-radius: var(--radius);
  border: 1px solid rgba(255, 255, 255, 0.16);
  box-shadow: 0 16px 36px rgba(0, 0, 0, 0.55);
  transition: transform 0.2s ease, box-shadow 0.2s ease;
}

.item-hero-poster:hover {
  transform: translateY(-2px);
  box-shadow: 0 20px 42px rgba(0, 0, 0, 0.65);
}

.item-hero-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.detail-eyebrow {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin: 0;
  font-size: 13px;
  color: var(--accent-strong);
  font-weight: 600;
}

.eyebrow-sep {
  opacity: 0.6;
}

.item-title-actions {
  display: flex;
  align-items: center;
  gap: 14px;
  min-width: 0;
}

.item-hero-title {
  margin: 0;
  font-size: clamp(22px, 2.6vw, 32px);
  font-weight: 700;
  line-height: 1.25;
  color: var(--text);
  overflow-wrap: anywhere;
}

.detail-orig-title {
  margin: -4px 0 0;
  font-size: 13px;
  color: var(--dim);
}

.detail-meta-pills {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 7px;
}

.detail-meta-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: 6px;
  background: var(--panel);
  border: 1px solid var(--line);
  color: var(--muted);
  font-size: 12px;
  font-weight: 500;
}

.detail-meta-pill.rating {
  background: rgba(250, 176, 5, 0.14);
  border-color: rgba(250, 176, 5, 0.35);
  color: #fab005;
  font-weight: 700;
}

.detail-meta-pill.mono {
  font-family: var(--font-mono);
  font-size: 11.5px;
}

.detail-badges {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}

.item-hero-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 4px;
}

.btn-play-large {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: 38px;
  padding: 0 20px;
  border-radius: var(--radius-sm);
  font-size: 14px;
  font-weight: 600;
  box-shadow: 0 4px 14px rgba(26, 113, 255, 0.4);
}

.btn-hero-action {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 36px;
  padding: 0 14px;
  font-size: 13px;
}

@media (max-width: 720px) {
  .item-hero-body {
    flex-direction: column;
    align-items: flex-start;
    gap: 16px;
    padding: 16px;
    min-height: 0;
  }
  .item-hero-poster-wrap {
    flex-basis: 120px;
    width: 120px;
  }
  .item-hero-title {
    font-size: 20px;
  }
}
</style>
