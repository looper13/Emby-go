<script setup lang="ts">
import { computed } from 'vue';
import type { LibraryDto } from '../../api/contracts';
import { entityParams } from '../../lib/media';
const props = defineProps<{ query: Record<string, unknown>; libraries: LibraryDto[]; search: string }>();
const emit = defineEmits<{ filter: [key: string, value: string, toggle?: boolean]; search: [value: string]; clearEntity: []; shuffle: [] }>();
const value = (key: string) => typeof props.query[key] === 'string' ? props.query[key] : '';
const entity = computed(() => entityParams.map(([key, label]) => [key, label, value(key)]).find(row => row[2]));
const pills = [['', '全部'], ['success', '已入库'], ['manual', '手动'], ['pending', '待补录'], ['incompatible', '不兼容']];
const sorts = [['datecreated', '最近入库'], ['title', '标题'], ['year', '年份'], ['communityrating', '评分'], ['date', '首映日期'], ['random', '随机']];
</script>
<template>
  <div v-if="libraries.length" class="filters">
    <template v-if="libraries.length <= 8"><button class="pill" :class="{ 'is-active': !value('library_id') }" data-lib="" @click="emit('filter', 'library_id', '')">全部库</button>
      <button v-for="lib in libraries" :key="lib.Id" class="pill" :class="{ 'is-active': value('library_id') === String(lib.Id) }" :data-lib="lib.Id" @click="emit('filter', 'library_id', String(lib.Id))">{{ lib.Name }}</button></template>
    <select v-else id="item-lib" :value="value('library_id')" @change="emit('filter', 'library_id', ($event.target as HTMLSelectElement).value)"><option value="">全部库</option><option v-for="lib in libraries" :key="lib.Id" :value="lib.Id">{{ lib.Name }}</option></select>
  </div>
  <div v-if="entity" class="filters"><span class="filter-chip">{{ entity[1] }}：{{ entity[2] }}<button id="entity-clear" class="chip-x" title="清除该筛选" @click="emit('clearEntity')">✕</button></span><slot name="entity-action" /></div>
  <div class="filters">
    <button v-for="[v, label] in pills" :key="v" class="pill" :class="{ 'is-active': value('status') === v }" :data-status="v" @click="emit('filter', 'status', v!)">{{ label }}</button>
    <button v-for="[v, label] in [['failed', '刮削失败'], ['confirm', '待人工确认']]" :key="v" class="pill" :class="{ 'is-active': value('scrape') === v }" :data-scrape="v" @click="emit('filter', 'scrape', v!, true)">{{ label }}</button>
    <input id="item-search" :value="search" placeholder="搜索标题 / 番号 / 原名" aria-label="搜索标题 / 番号 / 原名" style="min-width:200px" @input="emit('search', ($event.target as HTMLInputElement).value)">
    <select id="item-sort" aria-label="排序" :value="value('sort') || 'datecreated'" @change="emit('filter', 'sort', ($event.target as HTMLSelectElement).value)"><option v-for="[v, label] in sorts" :key="v" :value="v">{{ label }}</option></select>
    <label class="favorite-filter"><input type="checkbox" :checked="value('favorite') === 'true'" @change="emit('filter','favorite',($event.target as HTMLInputElement).checked ? 'true' : '')">仅收藏</label>
    <button v-if="value('sort') === 'random'" id="wall-shuffle" class="btn" @click="emit('shuffle')">换一批</button>
  </div>
</template>
<style scoped>
.favorite-filter { display: inline-flex; align-items: center; gap: 6px; white-space: nowrap; }
.favorite-filter input { width: 16px; height: 16px; }
</style>
