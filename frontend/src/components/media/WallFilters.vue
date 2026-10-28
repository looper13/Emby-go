<script setup lang="ts">
import { computed, ref } from 'vue';
import { Search, SlidersHorizontal, Folder, CheckCircle2, Sparkles, Heart, X, RotateCcw } from '@lucide/vue';
import type { LibraryDto } from '../../api/contracts';
import { entityParams } from '../../lib/media';
import { useCardSize } from '../../composables/useCardSize';

const props = defineProps<{
  query: Record<string, unknown>;
  libraries: LibraryDto[];
  search: string;
  canManage?: boolean;
}>();

const emit = defineEmits<{
  filter: [key: string, value: string, toggle?: boolean];
  search: [value: string];
  clearEntity: [];
  shuffle: [];
}>();

const { cardSize, setSize } = useCardSize();

const value = (key: string) => typeof props.query[key] === 'string' ? props.query[key] : '';
const entity = computed(() => entityParams.map(([key, label]) => [key, label, value(key)]).find(row => row[2]));
const sort = computed(() => value('sort') || 'datecreated');
const order = computed(() => value('order') || (sort.value === 'title' ? 'asc' : 'desc'));

const pills = [
  ['', '全部'],
  ['success', '已入库'],
  ['manual', '手动'],
  ['pending', '待补录'],
  ['incompatible', '不兼容'],
];

const sorts = [
  ['datecreated', '最近入库'],
  ['title', '标题'],
  ['year', '年份'],
  ['communityrating', '评分'],
  ['date', '首映日期'],
  ['random', '随机'],
];

// 活跃筛选数量统计（不含默认值）
const activeCount = computed(() => {
  let count = 0;
  if (value('library_id')) count++;
  if (value('status')) count++;
  if (value('scrape')) count++;
  if (value('favorite') === 'true') count++;
  if (entity.value) count++;
  return count;
});

// 筛选面板展开状态，默认展开以保证平铺浏览效率，支持一键折叠/展开
const showDrawer = ref(true);

function clearAllFilters() {
  if (value('library_id')) emit('filter', 'library_id', '');
  if (value('status')) emit('filter', 'status', '');
  if (value('scrape')) emit('filter', 'scrape', '');
  if (value('favorite')) emit('filter', 'favorite', '');
  if (entity.value) emit('clearEntity');
}
</script>

<template>
  <div class="wall-filters-workspace">
    <!-- 顶部极简工具栏：搜索、筛选器开关、排序与偏好 -->
    <div class="wall-filters-bar">
      <!-- 搜索框（内嵌放大镜与清空按钮） -->
      <div class="search-field-wrap">
        <Search class="search-icon" :size="15" aria-hidden="true" />
        <input
          id="item-search"
          :value="search"
          placeholder="搜索标题 / 番号 / 原名"
          aria-label="搜索标题 / 番号 / 原名"
          @input="emit('search', ($event.target as HTMLInputElement).value)"
        >
        <button
          v-if="search"
          type="button"
          class="search-clear-btn"
          title="清空搜索"
          aria-label="清空搜索"
          @click="emit('search', '')"
        >
          <X :size="13" aria-hidden="true" />
        </button>
      </div>

      <!-- 筛选面板开关按钮 -->
      <button
        type="button"
        class="filter-toggle-btn"
        :class="{ 'is-active': showDrawer || activeCount > 0 }"
        :aria-expanded="showDrawer"
        title="切换筛选面板"
        @click="showDrawer = !showDrawer"
      >
        <SlidersHorizontal :size="14" aria-hidden="true" />
        <span>筛选</span>
        <span v-if="activeCount > 0" class="filter-count-badge">{{ activeCount }}</span>
      </button>

      <!-- 排序下拉与方向 -->
      <div class="wall-filters-right">
        <select
          id="item-sort"
          aria-label="排序"
          :value="sort"
          @change="emit('filter', 'sort', ($event.target as HTMLSelectElement).value)"
        >
          <option v-for="[v, label] in sorts" :key="v" :value="v">{{ label }}</option>
        </select>

        <select
          v-if="sort !== 'random'"
          id="item-order"
          aria-label="排序方向"
          :value="order"
          @change="emit('filter', 'order', ($event.target as HTMLSelectElement).value)"
        >
          <option value="asc">正序</option>
          <option value="desc">倒序</option>
        </select>

        <button
          v-if="sort === 'random'"
          id="wall-shuffle"
          class="btn btn-shuffle"
          title="换一批随机内容"
          @click="emit('shuffle')"
        >
          <Sparkles :size="14" aria-hidden="true" />
          <span>换一批</span>
        </button>

        <label class="favorite-filter" title="仅查看收藏的媒体">
          <input
            type="checkbox"
            :checked="value('favorite') === 'true'"
            @change="emit('filter', 'favorite', ($event.target as HTMLInputElement).checked ? 'true' : '')"
          >
          <Heart :size="13" class="fav-icon" :class="{ 'is-filled': value('favorite') === 'true' }" aria-hidden="true" />
          <span>仅收藏</span>
        </label>

        <!-- 卡片尺寸切换（紧凑/标准/大图） -->
        <div class="card-size-selector" role="group" aria-label="卡片尺寸">
          <button
            type="button"
            class="card-size-btn"
            :class="{ 'is-active': cardSize === 'compact' }"
            title="紧凑卡片 (小图)"
            aria-label="紧凑尺寸"
            @click="setSize('compact')"
          >
            小
          </button>
          <button
            type="button"
            class="card-size-btn"
            :class="{ 'is-active': cardSize === 'normal' }"
            title="标准卡片 (默认)"
            aria-label="标准尺寸"
            @click="setSize('normal')"
          >
            中
          </button>
          <button
            type="button"
            class="card-size-btn"
            :class="{ 'is-active': cardSize === 'large' }"
            title="舒适大图 (放大)"
            aria-label="大图尺寸"
            @click="setSize('large')"
          >
            大
          </button>
        </div>
      </div>
    </div>

    <!-- 活跃实体筛选条（演员/类型/标签/厂商/合集） -->
    <div v-if="entity" class="entity-filter-bar filters">
      <span class="filter-chip">
        <span class="chip-label">{{ entity[1] }}：</span>
        <strong class="chip-value">{{ entity[2] }}</strong>
        <button id="entity-clear" class="chip-x" title="清除该筛选" @click="emit('clearEntity')">✕</button>
      </span>
      <slot name="entity-action" />
    </div>

    <!-- 筛选面板容器（参考 Nowen Video 的 Glassmorphism 筛选工作区） -->
    <div v-show="showDrawer" class="wall-filter-panel">
      <!-- 媒体库分组 -->
      <div v-if="libraries.length" class="nv-filter-group filters">
        <div class="nv-filter-group-label">
          <Folder :size="13" class="group-icon" aria-hidden="true" />
          <span>媒体库</span>
          <span class="nv-filter-group-count">{{ libraries.length }}</span>
        </div>
        <div class="nv-filter-group-content">
          <template v-if="libraries.length <= 8">
            <button
              class="pill"
              :class="{ 'is-active': !value('library_id') }"
              data-lib=""
              @click="emit('filter', 'library_id', '')"
            >
              全部库
            </button>
            <button
              v-for="lib in libraries"
              :key="lib.Id"
              class="pill"
              :class="{ 'is-active': value('library_id') === String(lib.Id) }"
              :data-lib="lib.Id"
              @click="emit('filter', 'library_id', String(lib.Id))"
            >
              {{ lib.Name }}
            </button>
          </template>
          <select
            v-else
            id="item-lib"
            :value="value('library_id')"
            @change="emit('filter', 'library_id', ($event.target as HTMLSelectElement).value)"
          >
            <option value="">全部库</option>
            <option v-for="lib in libraries" :key="lib.Id" :value="lib.Id">{{ lib.Name }}</option>
          </select>
        </div>
      </div>

      <!-- 入库状态分组 -->
      <div v-if="canManage" class="nv-filter-group filters">
        <div class="nv-filter-group-label">
          <CheckCircle2 :size="13" class="group-icon" aria-hidden="true" />
          <span>入库状态</span>
        </div>
        <div class="nv-filter-group-content">
          <button
            v-for="[v, label] in pills"
            :key="v"
            class="pill"
            :class="{ 'is-active': value('status') === v }"
            :data-status="v"
            @click="emit('filter', 'status', v!)"
          >
            {{ label }}
          </button>
        </div>
      </div>

      <!-- 刮削状态分组 -->
      <div v-if="canManage" class="nv-filter-group filters">
        <div class="nv-filter-group-label">
          <Sparkles :size="13" class="group-icon" aria-hidden="true" />
          <span>刮削状态</span>
        </div>
        <div class="nv-filter-group-content">
          <button
            v-for="[v, label] in [['failed', '刮削失败'], ['confirm', '待人工确认']]"
            :key="v"
            class="pill"
            :class="{ 'is-active': value('scrape') === v }"
            :data-scrape="v"
            @click="emit('filter', 'scrape', v!, true)"
          >
            {{ label }}
          </button>
        </div>
      </div>

      <!-- 底部重置按钮 -->
      <div v-if="activeCount > 0" class="filter-panel-footer">
        <button type="button" class="btn-clear-all" @click="clearAllFilters">
          <RotateCcw :size="12" aria-hidden="true" />
          <span>重置所有筛选</span>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.wall-filters-workspace {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-bottom: 20px;
}

/* 顶部搜索与控制栏 */
.wall-filters-bar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  padding: 8px 12px;
  border-radius: var(--radius);
  background: var(--panel);
  border: 1px solid var(--line);
  box-shadow: var(--shadow);
}

/* 搜索框容器 */
.search-field-wrap {
  position: relative;
  display: flex;
  align-items: center;
  flex: 1 1 240px;
  min-width: 200px;
  max-width: 440px;
}

.search-field-wrap .search-icon {
  position: absolute;
  left: 10px;
  color: var(--dim);
  pointer-events: none;
}

.search-field-wrap input {
  width: 100%;
  height: 34px;
  padding: 0 30px 0 32px;
  border-radius: var(--radius-sm);
  background: var(--panel-2);
  border: 1px solid var(--line);
  color: var(--text);
  font-size: 13px;
  transition: all 0.15s ease;
}

.search-field-wrap input:focus {
  border-color: var(--accent);
  background: var(--panel);
  box-shadow: 0 0 0 2px var(--accent-soft);
}

.search-clear-btn {
  position: absolute;
  right: 8px;
  display: grid;
  place-items: center;
  width: 18px;
  height: 18px;
  padding: 0;
  border: 0;
  border-radius: 50%;
  background: var(--line);
  color: var(--muted);
  cursor: pointer;
  transition: all 0.15s ease;
}

.search-clear-btn:hover {
  background: var(--line-strong);
  color: var(--text);
}

/* 筛选开关按钮 */
.filter-toggle-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 34px;
  padding: 0 12px;
  border-radius: var(--radius-sm);
  background: var(--panel-2);
  border: 1px solid var(--line);
  color: var(--muted);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  user-select: none;
  transition: all 0.15s ease;
}

.filter-toggle-btn:hover {
  background: var(--line);
  color: var(--text);
  border-color: var(--line-strong);
}

.filter-toggle-btn.is-active {
  background: var(--accent-soft);
  border-color: var(--accent);
  color: var(--accent);
  font-weight: 600;
}

.filter-count-badge {
  display: inline-grid;
  place-items: center;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--accent);
  color: #ffffff;
  font-size: 10.5px;
  font-weight: 700;
  font-family: var(--font-mono);
}

/* 右侧排序与偏好 */
.wall-filters-right {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-left: auto;
}

.wall-filters-right select {
  height: 34px;
  padding: 0 10px;
  border-radius: var(--radius-sm);
  background: var(--panel-2);
  border: 1px solid var(--line);
  color: var(--text);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.wall-filters-right select:hover {
  border-color: var(--line-strong);
}

.wall-filters-right select:focus {
  border-color: var(--accent);
}

.btn-shuffle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 34px;
  padding: 0 12px;
  border-radius: var(--radius-sm);
  background: var(--panel-2);
  border: 1px solid var(--line);
  color: var(--accent);
  font-weight: 600;
  font-size: 12.5px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-shuffle:hover {
  background: var(--accent-soft);
  border-color: var(--accent);
}

.favorite-filter {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 34px;
  padding: 0 10px;
  border-radius: var(--radius-sm);
  background: var(--panel-2);
  border: 1px solid var(--line);
  color: var(--muted);
  font-size: 12.5px;
  font-weight: 500;
  cursor: pointer;
  user-select: none;
  white-space: nowrap;
  transition: all 0.15s ease;
}

.favorite-filter:hover {
  border-color: var(--line-strong);
  color: var(--text);
}

.favorite-filter input {
  width: 15px;
  height: 15px;
  margin: 0;
  accent-color: var(--accent);
  cursor: pointer;
}

.fav-icon {
  color: var(--dim);
  transition: color 0.15s ease;
}

.fav-icon.is-filled {
  color: #ff5277;
  fill: #ff5277;
}

/* 实体筛选条 */
.entity-filter-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
}

.chip-label {
  opacity: 0.8;
  font-weight: 400;
}

.chip-value {
  font-weight: 600;
}

/* 筛选面板容器（玻璃质感面板） */
.wall-filter-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 14px 16px;
  border-radius: var(--radius);
  background: color-mix(in srgb, var(--panel) 92%, transparent);
  border: 1px solid var(--line);
  box-shadow: var(--shadow);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
}

/* 分组布局（类似 Nowen Video nv-filter-group） */
.nv-filter-group {
  display: grid;
  grid-template-columns: 88px minmax(0, 1fr);
  gap: 10px;
  align-items: center;
  margin: 0 !important;
}

.nv-filter-group-label {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--dim);
  font-size: 12px;
  font-weight: 600;
  user-select: none;
}

.nv-filter-group-count {
  display: inline-grid;
  place-items: center;
  min-width: 17px;
  height: 17px;
  padding: 0 4px;
  border-radius: 999px;
  background: var(--accent-soft);
  color: var(--accent);
  font-size: 10px;
  font-weight: 700;
  font-family: var(--font-mono);
}

.nv-filter-group-content {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 0;
}

/* 重置筛选按钮 */
.filter-panel-footer {
  display: flex;
  justify-content: flex-end;
  padding-top: 8px;
  border-top: 1px solid var(--line);
}

.btn-clear-all {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 4px 10px;
  border-radius: var(--radius-sm);
  background: transparent;
  border: 1px solid var(--line);
  color: var(--dim);
  font-size: 11.5px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-clear-all:hover {
  background: var(--danger-soft);
  border-color: var(--danger);
  color: var(--danger);
}

@media (max-width: 768px) {
  .nv-filter-group {
    grid-template-columns: 1fr;
    gap: 6px;
  }
  .wall-filters-bar {
    flex-direction: column;
    align-items: stretch;
  }
  .search-field-wrap {
    max-width: none;
    width: 100%;
  }
  .wall-filters-right {
    margin-left: 0;
    justify-content: space-between;
  }
}
</style>
