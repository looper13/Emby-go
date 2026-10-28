<script setup lang="ts">
import { computed } from 'vue';
import { Play, Activity, RefreshCw, Search, Film, Star, ArrowLeft, ChevronLeft, ChevronRight } from '@lucide/vue';
import type { MovieDto, ImageDto, FileDto } from '../../api/contracts';
import { numberUnlessInTitle, statusText, fmtDuration } from '../../lib/media';
import { imageURL } from '../../lib/image-url';
import MediaImage from './MediaImage.vue';
import FavoriteButton from './FavoriteButton.vue';
import { useMediaVisibility } from '../../composables/useMediaVisibility';

const props = defineProps<{
  movie: MovieDto;
  images?: ImageDto[];
  files?: FileDto[];
  playable?: boolean;
  busy?: Set<string>;
  favorite?: boolean;
  canManage?: boolean;
  hasPrev?: boolean;
  hasNext?: boolean;
  prevTitle?: string;
  nextTitle?: string;
}>();

const emit = defineEmits<{
  back: [];
  play: [];
  probe: [];
  reread: [];
  scrape: [];
  entity: [key: string, value: string];
  prev: [];
  next: [];
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

interface TechBadge {
  label: string;
  category: 'res' | 'hdr' | 'codec' | 'audio' | 'sub';
}

const techBadges = computed<TechBadge[]>(() => {
  const list: TechBadge[] = [];
  if (!props.files?.length) {
    if (props.movie.source_container) {
      list.push({ label: props.movie.source_container.toUpperCase(), category: 'codec' });
    }
    return list;
  }

  const streams = props.files.flatMap(f => f.streams || []);
  const video = streams.find(s => s.Type?.toLowerCase() === 'video');
  const audio = streams.find(s => s.Type?.toLowerCase() === 'audio');
  const subs = streams.filter(s => s.Type?.toLowerCase() === 'subtitle');

  if (video?.Width && video?.Height) {
    const w = video.Width;
    const h = video.Height;
    if (w >= 3600 || h >= 2000) list.push({ label: '4K UHD', category: 'res' });
    else if (w >= 1800 || h >= 1000) list.push({ label: '1080p FHD', category: 'res' });
    else if (w >= 1200 || h >= 700) list.push({ label: '720p HD', category: 'res' });
    else list.push({ label: `${h}p`, category: 'res' });
  }

  if (video) {
    const is10Bit = video.BitDepth === 10 || (video.Profile && /10/i.test(video.Profile));
    if (is10Bit) list.push({ label: '10-bit HDR', category: 'hdr' });
  }

  if (video?.Codec) {
    const c = video.Codec.toLowerCase();
    if (c === 'hevc' || c === 'h265') list.push({ label: 'HEVC', category: 'codec' });
    else if (c === 'h264' || c === 'avc') list.push({ label: 'AVC (H.264)', category: 'codec' });
    else if (c === 'av01' || c === 'av1') list.push({ label: 'AV1', category: 'codec' });
    else if (c === 'vp9') list.push({ label: 'VP9', category: 'codec' });
    else list.push({ label: video.Codec.toUpperCase(), category: 'codec' });
  }

  if (audio) {
    let aName = '';
    if (audio.Codec) {
      const ac = audio.Codec.toLowerCase();
      if (ac === 'aac') aName = 'AAC';
      else if (ac === 'ac3') aName = 'Dolby 5.1';
      else if (ac === 'eac3') aName = 'E-AC3';
      else if (ac === 'dts') aName = 'DTS';
      else if (ac === 'truehd') aName = 'TrueHD';
      else if (ac === 'flac') aName = 'FLAC';
      else aName = audio.Codec.toUpperCase();
    }
    if (audio.Channels === 6 && !aName.includes('5.1')) aName += (aName ? ' · ' : '') + '5.1 声道';
    else if (audio.Channels === 8) aName += (aName ? ' · ' : '') + '7.1 声道';
    else if (audio.Channels === 2) aName += (aName ? ' · ' : '') + '立体声';
    if (aName) list.push({ label: aName, category: 'audio' });
  }

  if (subs.length > 0) {
    list.push({ label: `字幕 ×${subs.length}`, category: 'sub' });
  }

  return list;
});
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

    <!-- 顶部悬浮工具栏：返回按钮与前一部/后一部导航 -->
    <div class="item-hero-topbar">
      <button id="item-back" class="btn item-back" title="返回媒体墙 (Esc)" @click="emit('back')">
        <ArrowLeft :size="15" aria-hidden="true" />
        <span>← 返回媒体墙</span>
      </button>

      <div class="item-nav-group">
        <button
          id="item-prev"
          class="btn item-nav-btn"
          :disabled="!hasPrev"
          :title="prevTitle ? `前一部：${prevTitle}` : '前一部'"
          aria-label="前一部"
          @click="emit('prev')"
        >
          <ChevronLeft :size="15" aria-hidden="true" />
          <span>前一部</span>
        </button>
        <button
          id="item-next"
          class="btn item-nav-btn"
          :disabled="!hasNext"
          :title="nextTitle ? `后一部：${nextTitle}` : '后一部'"
          aria-label="后一部"
          @click="emit('next')"
        >
          <span>后一部</span>
          <ChevronRight :size="15" aria-hidden="true" />
        </button>
      </div>
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

        <!-- 技术规格徽章 (Resolution, HDR, Video Codec, Audio, Subtitles) -->
        <div v-if="techBadges.length" class="detail-tech-badges">
          <span
            v-for="b in techBadges"
            :key="b.label"
            class="tech-badge"
            :class="b.category"
          >
            {{ b.label }}
          </span>
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
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.item-nav-group {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.item-nav-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
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
  cursor: pointer;
}

.item-nav-btn:hover:not(:disabled) {
  background: rgba(14, 16, 20, 0.75) !important;
  border-color: rgba(255, 255, 255, 0.28) !important;
  color: #ffffff !important;
  transform: translateY(-1px);
}

.item-nav-btn:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}

[data-theme="light"] .item-nav-btn {
  background: rgba(255, 255, 255, 0.75) !important;
  border-color: rgba(0, 0, 0, 0.12) !important;
  color: #1a1b1e !important;
}

[data-theme="light"] .item-nav-btn:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.95) !important;
  border-color: rgba(0, 0, 0, 0.22) !important;
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

.detail-tech-badges {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 1px;
}

.tech-badge {
  display: inline-flex;
  align-items: center;
  padding: 2px 7px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.03em;
  text-transform: uppercase;
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.16);
  color: #e4e4e7;
  user-select: none;
}

.tech-badge.res {
  background: rgba(59, 130, 246, 0.14);
  border-color: rgba(59, 130, 246, 0.45);
  color: #60a5fa;
}

.tech-badge.hdr {
  background: rgba(245, 158, 11, 0.15);
  border-color: rgba(245, 158, 11, 0.5);
  color: #fbbf24;
}

.tech-badge.codec {
  background: rgba(168, 85, 247, 0.12);
  border-color: rgba(168, 85, 247, 0.4);
  color: #c084fc;
}

.tech-badge.audio {
  background: rgba(16, 185, 129, 0.12);
  border-color: rgba(16, 185, 129, 0.4);
  color: #34d399;
}

.tech-badge.sub {
  background: rgba(255, 255, 255, 0.05);
  border-color: rgba(255, 255, 255, 0.14);
  color: var(--muted);
}

[data-theme="light"] .tech-badge {
  background: rgba(0, 0, 0, 0.04);
  border-color: rgba(0, 0, 0, 0.12);
  color: #27272a;
}

[data-theme="light"] .tech-badge.res {
  background: rgba(37, 99, 235, 0.1);
  border-color: rgba(37, 99, 235, 0.35);
  color: #1d4ed8;
}

[data-theme="light"] .tech-badge.hdr {
  background: rgba(217, 119, 6, 0.12);
  border-color: rgba(217, 119, 6, 0.4);
  color: #b45309;
}

[data-theme="light"] .tech-badge.codec {
  background: rgba(147, 51, 234, 0.1);
  border-color: rgba(147, 51, 234, 0.35);
  color: #7e22ce;
}

[data-theme="light"] .tech-badge.audio {
  background: rgba(5, 150, 105, 0.1);
  border-color: rgba(5, 150, 105, 0.35);
  color: #047857;
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
