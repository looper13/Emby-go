<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue';
import { useRouter } from 'vue-router';
import { Search, Film, Users, Folder, Heart, Tv, Settings, Clock, X, CornerDownLeft } from '@lucide/vue';
import { catalogApi } from '../../api/catalog';
import { adminApi } from '../../api/admin';
import { actorsApi, type ActorEntry } from '../../api/actors';
import type { MovieDto, LibraryDto } from '../../api/contracts';
import { imageURL } from '../../lib/image-url';
import { entityIdOf } from '../../lib/media';
import { useMediaVisibility } from '../../composables/useMediaVisibility';

const props = defineProps<{
  open: boolean;
}>();

const emit = defineEmits<{
  close: [];
}>();

const router = useRouter();
const { hideImages } = useMediaVisibility();

const inputRef = ref<HTMLInputElement | null>(null);
const query = ref('');
const loading = ref(false);

const movies = shallowRef<MovieDto[]>([]);
const imageTags = shallowRef<Record<string, Record<string, string>>>({});
const actors = shallowRef<ActorEntry[]>([]);
const allLibraries = shallowRef<LibraryDto[]>([]);

let debounceTimer: ReturnType<typeof setTimeout> | null = null;
let activeAbortController: AbortController | null = null;

const selectedIndex = ref(0);

interface SearchResultItem {
  key: string;
  category: 'movie' | 'actor' | 'library' | 'nav';
  title: string;
  subtitle?: string;
  badge?: string;
  movie?: MovieDto;
  actor?: ActorEntry;
  library?: LibraryDto;
  action: () => void;
}

const quickNavItems: SearchResultItem[] = [
  { key: 'nav:items', category: 'nav', title: '媒体墙', subtitle: '全部已入库影片与单集', action: () => void router.push('/items') },
  { key: 'nav:favorites', category: 'nav', title: '我的收藏', subtitle: '标有爱心的影片与演员', action: () => void router.push('/favorites') },
  { key: 'nav:actors', category: 'nav', title: '演职人员', subtitle: '演员列表与头像管理', action: () => void router.push('/actors') },
  { key: 'nav:tv', category: 'nav', title: '电视节目', subtitle: '剧集与季集浏览', action: () => void router.push('/items?view=') },
  { key: 'nav:settings', category: 'nav', title: '系统设置', subtitle: '转码、监控与运行状态', action: () => void router.push('/settings') },
  { key: 'nav:scheduled', category: 'nav', title: '计划任务', subtitle: '定时扫描与刮削排程', action: () => void router.push('/scheduled') },
];

const matchedLibraries = computed(() => {
  const q = query.value.trim().toLowerCase();
  if (!q) return allLibraries.value;
  return allLibraries.value.filter(l => l.Name.toLowerCase().includes(q) || l.Path.toLowerCase().includes(q));
});

const flatResults = computed<SearchResultItem[]>(() => {
  const q = query.value.trim();
  if (!q) {
    const list: SearchResultItem[] = [];
    if (allLibraries.value.length > 0) {
      allLibraries.value.slice(0, 4).forEach(lib => {
        list.push({
          key: `lib:${lib.Id}`,
          category: 'library',
          title: lib.Name,
          subtitle: lib.Path,
          library: lib,
          action: () => void router.push(`/items?library_id=${lib.Id}`),
        });
      });
    }
    list.push(...quickNavItems);
    return list;
  }

  const list: SearchResultItem[] = [];

  movies.value.forEach(m => {
    let sub = '';
    if (m.television) sub = `${m.television.title} · S${m.television.season}E${m.television.episode}`;
    else if (m.Year) sub = `${m.Year}`;
    if (m.Rating) sub += (sub ? ' · ' : '') + `★ ${m.Rating}`;

    list.push({
      key: `movie:${m.id}`,
      category: 'movie',
      title: m.Title || `#${m.id}`,
      subtitle: sub,
      movie: m,
      action: () => void router.push({ name: 'item', params: { id: m.id } }),
    });
  });

  actors.value.forEach(a => {
    list.push({
      key: `actor:${a.name}`,
      category: 'actor',
      title: a.name,
      subtitle: a.movie_count ? `${a.movie_count} 部作品` : undefined,
      actor: a,
      action: () => void router.push(`/items?person=${encodeURIComponent(a.name)}`),
    });
  });

  matchedLibraries.value.forEach(l => {
    list.push({
      key: `lib:${l.Id}`,
      category: 'library',
      title: l.Name,
      subtitle: l.Path,
      library: l,
      action: () => void router.push(`/items?library_id=${l.Id}`),
    });
  });

  return list;
});

async function loadLibraries() {
  if (allLibraries.value.length > 0) return;
  try {
    const res = await adminApi.libraries();
    allLibraries.value = res.items || [];
  } catch {
    allLibraries.value = [];
  }
}

async function performSearch(term: string) {
  if (activeAbortController) {
    activeAbortController.abort();
  }
  const ac = new AbortController();
  activeAbortController = ac;

  loading.value = true;
  try {
    const [wallRes, actorRes] = await Promise.allSettled([
      catalogApi.items(new URLSearchParams({ search: term, limit: '6' }), ac.signal),
      actorsApi.list({ library_id: 0, search: term, status: 'all', offset: 0, limit: 4 }, ac.signal),
    ]);

    if (ac.signal.aborted) return;

    if (wallRes.status === 'fulfilled') {
      movies.value = wallRes.value.items || [];
      imageTags.value = wallRes.value.image_tags || {};
    } else {
      movies.value = [];
    }

    if (actorRes.status === 'fulfilled') {
      actors.value = actorRes.value.items || [];
    } else {
      actors.value = [];
    }
  } finally {
    if (!ac.signal.aborted) {
      loading.value = false;
      selectedIndex.value = 0;
    }
  }
}

function handleInput() {
  if (debounceTimer) clearTimeout(debounceTimer);
  const term = query.value.trim();
  if (!term) {
    movies.value = [];
    actors.value = [];
    loading.value = false;
    selectedIndex.value = 0;
    return;
  }
  debounceTimer = setTimeout(() => {
    void performSearch(term);
  }, 160);
}

function executeSelected() {
  const item = flatResults.value[selectedIndex.value];
  if (item) {
    item.action();
    emit('close');
  }
}

function selectItem(index: number) {
  selectedIndex.value = index;
  executeSelected();
}

function onKeyDown(e: KeyboardEvent) {
  if (!props.open) return;

  if (e.key === 'Escape') {
    e.preventDefault();
    emit('close');
    return;
  }

  if (e.key === 'ArrowDown') {
    e.preventDefault();
    if (flatResults.value.length > 0) {
      selectedIndex.value = (selectedIndex.value + 1) % flatResults.value.length;
      scrollActiveIntoView();
    }
    return;
  }

  if (e.key === 'ArrowUp') {
    e.preventDefault();
    if (flatResults.value.length > 0) {
      selectedIndex.value = (selectedIndex.value - 1 + flatResults.value.length) % flatResults.value.length;
      scrollActiveIntoView();
    }
    return;
  }

  if (e.key === 'Enter') {
    e.preventDefault();
    executeSelected();
  }
}

function scrollActiveIntoView() {
  nextTick(() => {
    const el = document.querySelector('.search-dialog-item.is-selected');
    if (el) {
      el.scrollIntoView({ block: 'nearest' });
    }
  });
}

watch(() => props.open, (isOpen) => {
  if (isOpen) {
    query.value = '';
    movies.value = [];
    actors.value = [];
    selectedIndex.value = 0;
    void loadLibraries();
    nextTick(() => {
      inputRef.value?.focus();
    });
    window.addEventListener('keydown', onKeyDown);
  } else {
    window.removeEventListener('keydown', onKeyDown);
    if (activeAbortController) {
      activeAbortController.abort();
    }
  }
}, { immediate: true });

onUnmounted(() => {
  window.removeEventListener('keydown', onKeyDown);
  if (debounceTimer) clearTimeout(debounceTimer);
});
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="search-backdrop"
      role="dialog"
      aria-modal="true"
      aria-label="全局搜索"
      @click.self="emit('close')"
    >
      <div class="search-modal">
        <!-- 搜索头部输入框 -->
        <div class="search-header">
          <Search class="search-header-icon" :size="20" aria-hidden="true" />
          <input
            ref="inputRef"
            v-model="query"
            class="search-input"
            type="text"
            placeholder="搜索影片、剧集、演员、媒体库..."
            autocomplete="off"
            spellcheck="false"
            @input="handleInput"
          />
          <button
            v-if="query"
            class="icon-btn search-clear-btn"
            type="button"
            title="清空搜索"
            aria-label="清空搜索"
            @click="query = ''; handleInput(); inputRef?.focus()"
          >
            <X :size="16" aria-hidden="true" />
          </button>
          <kbd class="search-esc-badge" @click="emit('close')">ESC</kbd>
        </div>

        <!-- 加载动画进度条 -->
        <div class="search-loading-bar" :class="{ 'is-loading': loading }"></div>

        <!-- 结果列表 -->
        <div class="search-body">
          <div v-if="flatResults.length === 0 && !loading" class="search-empty">
            <p>未找到与「<strong>{{ query }}</strong>」相关的结果</p>
            <small>请尝试使用其它关键词、原名或番号</small>
          </div>

          <template v-else>
            <!-- 影片与单集 -->
            <div v-if="query && movies.length > 0" class="search-group">
              <div class="search-group-title">
                <Film :size="13" aria-hidden="true" />
                <span>影片与单集</span>
              </div>
              <div
                v-for="m in movies"
                :key="`m-${m.id}`"
                class="search-dialog-item"
                :class="{ 'is-selected': flatResults[selectedIndex]?.key === `movie:${m.id}` }"
                @click="selectItem(flatResults.findIndex(r => r.key === `movie:${m.id}`))"
                @mouseenter="selectedIndex = flatResults.findIndex(r => r.key === `movie:${m.id}`)"
              >
                <div class="search-item-thumb">
                  <img
                    v-if="!hideImages && (m.PosterPath || m.LandscapePath || m.cover_url)"
                    :src="imageURL(m.id, m.PosterPath ? 'Primary' : 'Thumb', 120, imageTags[m.id]?.Primary || imageTags[m.id]?.Thumb)"
                    alt=""
                    loading="lazy"
                  />
                  <Film v-else :size="18" class="thumb-icon-fallback" aria-hidden="true" />
                </div>
                <div class="search-item-info">
                  <div class="search-item-title">{{ m.Title || `#${m.id}` }}</div>
                  <div class="search-item-sub">
                    <span v-if="m.television">{{ m.television.title }} · S{{ m.television.season }}E{{ m.television.episode }}</span>
                    <span v-else-if="m.Year">{{ m.Year }}</span>
                    <span v-if="m.Rating">★ {{ m.Rating }}</span>
                  </div>
                </div>
                <CornerDownLeft class="item-enter-icon" :size="14" aria-hidden="true" />
              </div>
            </div>

            <!-- 演员 -->
            <div v-if="query && actors.length > 0" class="search-group">
              <div class="search-group-title">
                <Users :size="13" aria-hidden="true" />
                <span>演职人员</span>
              </div>
              <div
                v-for="a in actors"
                :key="`a-${a.name}`"
                class="search-dialog-item"
                :class="{ 'is-selected': flatResults[selectedIndex]?.key === `actor:${a.name}` }"
                @click="selectItem(flatResults.findIndex(r => r.key === `actor:${a.name}`))"
                @mouseenter="selectedIndex = flatResults.findIndex(r => r.key === `actor:${a.name}`)"
              >
                <div class="search-item-avatar">
                  <img
                    v-if="!hideImages && a.has_image"
                    :src="imageURL(entityIdOf('person', a.name), 'Primary', 120, a.image_tag)"
                    alt=""
                    loading="lazy"
                  />
                  <span v-else class="avatar-letter">{{ (a.name || '?').trim().slice(0, 1) }}</span>
                </div>
                <div class="search-item-info">
                  <div class="search-item-title">{{ a.name }}</div>
                  <div class="search-item-sub">{{ a.movie_count ? `${a.movie_count} 部收录影片` : '演员' }}</div>
                </div>
                <CornerDownLeft class="item-enter-icon" :size="14" aria-hidden="true" />
              </div>
            </div>

            <!-- 媒体库 -->
            <div v-if="matchedLibraries.length > 0" class="search-group">
              <div class="search-group-title">
                <Folder :size="13" aria-hidden="true" />
                <span>媒体库</span>
              </div>
              <div
                v-for="lib in matchedLibraries"
                :key="`lib-${lib.Id}`"
                class="search-dialog-item"
                :class="{ 'is-selected': flatResults[selectedIndex]?.key === `lib:${lib.Id}` }"
                @click="selectItem(flatResults.findIndex(r => r.key === `lib:${lib.Id}`))"
                @mouseenter="selectedIndex = flatResults.findIndex(r => r.key === `lib:${lib.Id}`)"
              >
                <div class="search-item-icon-box">
                  <Folder :size="18" aria-hidden="true" />
                </div>
                <div class="search-item-info">
                  <div class="search-item-title">{{ lib.Name }}</div>
                  <div class="search-item-sub">{{ lib.Path }}</div>
                </div>
                <CornerDownLeft class="item-enter-icon" :size="14" aria-hidden="true" />
              </div>
            </div>

            <!-- 快捷导航（当搜索词为空时） -->
            <div v-if="!query" class="search-group">
              <div class="search-group-title">
                <span>常用导航入口</span>
              </div>
              <div
                v-for="nav in quickNavItems"
                :key="nav.key"
                class="search-dialog-item"
                :class="{ 'is-selected': flatResults[selectedIndex]?.key === nav.key }"
                @click="selectItem(flatResults.findIndex(r => r.key === nav.key))"
                @mouseenter="selectedIndex = flatResults.findIndex(r => r.key === nav.key)"
              >
                <div class="search-item-icon-box">
                  <Heart v-if="nav.key.includes('favorites')" :size="18" aria-hidden="true" />
                  <Users v-else-if="nav.key.includes('actors')" :size="18" aria-hidden="true" />
                  <Tv v-else-if="nav.key.includes('tv')" :size="18" aria-hidden="true" />
                  <Settings v-else-if="nav.key.includes('settings')" :size="18" aria-hidden="true" />
                  <Clock v-else-if="nav.key.includes('scheduled')" :size="18" aria-hidden="true" />
                  <Film v-else :size="18" aria-hidden="true" />
                </div>
                <div class="search-item-info">
                  <div class="search-item-title">{{ nav.title }}</div>
                  <div class="search-item-sub">{{ nav.subtitle }}</div>
                </div>
                <CornerDownLeft class="item-enter-icon" :size="14" aria-hidden="true" />
              </div>
            </div>
          </template>
        </div>

        <!-- 底部快捷键提示 -->
        <div class="search-footer">
          <span class="search-shortcut"><kbd>↑</kbd><kbd>↓</kbd> 切换选项</span>
          <span class="search-shortcut"><kbd>↵</kbd> 跳转</span>
          <span class="search-shortcut"><kbd>ESC</kbd> 关闭</span>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.search-backdrop {
  position: fixed;
  inset: 0;
  z-index: 9999;
  background: rgba(0, 0, 0, 0.65);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: clamp(40px, 10vh, 100px) 16px 24px;
  animation: searchFadeIn 0.15s ease-out;
}

@keyframes searchFadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

.search-modal {
  width: 100%;
  max-width: 620px;
  background: var(--bg-card, #18191c);
  border: 1px solid var(--line, rgba(255, 255, 255, 0.12));
  border-radius: 14px;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.65);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  max-height: 80vh;
  animation: searchPopIn 0.18s cubic-bezier(0.16, 1, 0.3, 1);
}

@keyframes searchPopIn {
  from { transform: scale(0.96) translateY(-8px); opacity: 0; }
  to { transform: scale(1) translateY(0); opacity: 1; }
}

.search-header {
  position: relative;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 18px;
  border-bottom: 1px solid var(--line, rgba(255, 255, 255, 0.08));
}

.search-header-icon {
  color: var(--muted);
  flex-shrink: 0;
}

.search-input {
  flex: 1;
  background: transparent;
  border: none;
  outline: none;
  font-size: 16px;
  color: var(--text);
  font-family: inherit;
}

.search-input::placeholder {
  color: var(--dim);
}

.search-clear-btn {
  width: 24px;
  height: 24px;
  padding: 0;
  display: grid;
  place-items: center;
  color: var(--muted);
}

.search-esc-badge {
  font-size: 11px;
  font-family: var(--font-mono, monospace);
  padding: 2px 6px;
  border-radius: 4px;
  background: var(--panel, rgba(255, 255, 255, 0.08));
  border: 1px solid var(--line, rgba(255, 255, 255, 0.14));
  color: var(--muted);
  cursor: pointer;
  user-select: none;
}

.search-loading-bar {
  height: 2px;
  width: 100%;
  background: transparent;
  position: relative;
  overflow: hidden;
}

.search-loading-bar.is-loading {
  background: var(--line, rgba(255, 255, 255, 0.08));
}

.search-loading-bar.is-loading::after {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  bottom: 0;
  width: 35%;
  background: var(--accent, #1a71ff);
  animation: searchLoadingAnim 1s infinite linear;
}

@keyframes searchLoadingAnim {
  0% { transform: translateX(-100%); }
  100% { transform: translateX(350%); }
}

.search-body {
  flex: 1;
  overflow-y: auto;
  padding: 8px 10px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.search-group {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.search-group-title {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px 4px;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--dim);
  text-transform: uppercase;
  letter-spacing: 0.03em;
}

.search-dialog-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 10px;
  border-radius: 8px;
  cursor: pointer;
  transition: background 0.12s ease;
  user-select: none;
}

.search-dialog-item.is-selected {
  background: var(--accent-soft, rgba(26, 113, 255, 0.14));
}

.search-item-thumb {
  width: 34px;
  height: 48px;
  border-radius: 5px;
  overflow: hidden;
  background: var(--panel, #222);
  flex-shrink: 0;
  display: grid;
  place-items: center;
  border: 1px solid var(--line, rgba(255, 255, 255, 0.1));
}

.search-item-thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.thumb-icon-fallback {
  color: var(--dim);
}

.search-item-avatar {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  overflow: hidden;
  background: var(--panel, #222);
  flex-shrink: 0;
  display: grid;
  place-items: center;
  border: 1px solid var(--line, rgba(255, 255, 255, 0.1));
}

.search-item-avatar img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.avatar-letter {
  font-weight: 600;
  font-size: 14px;
  color: var(--muted);
}

.search-item-icon-box {
  width: 38px;
  height: 38px;
  border-radius: 8px;
  background: var(--panel, rgba(255, 255, 255, 0.06));
  border: 1px solid var(--line, rgba(255, 255, 255, 0.08));
  display: grid;
  place-items: center;
  color: var(--accent);
  flex-shrink: 0;
}

.search-item-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.search-item-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.search-dialog-item.is-selected .search-item-title {
  color: var(--accent);
}

.search-item-sub {
  font-size: 12px;
  color: var(--muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.item-enter-icon {
  color: var(--dim);
  opacity: 0;
  flex-shrink: 0;
  transition: opacity 0.12s ease;
}

.search-dialog-item.is-selected .item-enter-icon {
  opacity: 0.8;
  color: var(--accent);
}

.search-empty {
  padding: 36px 20px;
  text-align: center;
  display: flex;
  flex-direction: column;
  gap: 6px;
  color: var(--muted);
}

.search-empty p {
  margin: 0;
  font-size: 15px;
}

.search-empty small {
  color: var(--dim);
  font-size: 12.5px;
}

.search-footer {
  padding: 10px 18px;
  border-top: 1px solid var(--line, rgba(255, 255, 255, 0.08));
  display: flex;
  align-items: center;
  gap: 16px;
  background: rgba(0, 0, 0, 0.15);
}

.search-shortcut {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--dim);
}

.search-shortcut kbd {
  font-family: var(--font-mono, monospace);
  font-size: 11px;
  padding: 1px 5px;
  border-radius: 4px;
  background: var(--panel, rgba(255, 255, 255, 0.08));
  border: 1px solid var(--line, rgba(255, 255, 255, 0.14));
  color: var(--muted);
}

[data-theme="light"] .search-modal {
  background: #ffffff;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.22);
}

[data-theme="light"] .search-footer {
  background: #f8f9fa;
}
</style>
