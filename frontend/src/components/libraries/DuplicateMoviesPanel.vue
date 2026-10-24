<script setup lang="ts">
import { ref, shallowRef, watch } from 'vue';
import { Search } from '@lucide/vue';
import type { LibraryDto } from '../../api/contracts';
import { duplicatesApi, type DuplicateGroups, type DuplicateMembers, type DuplicateGroup, type DuplicateMovie } from '../../api/duplicates';
import DuplicateDeleteDialog from './DuplicateDeleteDialog.vue';
import DuplicateStrmContents from './DuplicateStrmContents.vue';
import { usePageRequest } from '../../composables/usePageRequest';

const props = defineProps<{ libraries: LibraryDto[] }>();
const library = ref(0), groups = shallowRef<DuplicateGroups|null>(null), members = shallowRef<DuplicateMembers|null>(null);
const loading = ref(false), error = ref(''), offset = ref(0), selected = shallowRef<DuplicateGroup|null>(null), memberOffset = ref(0), memberLoading = ref(false), memberError = ref('');
const groupScope = usePageRequest(), memberScope = usePageRequest(), groupLimit = 20, memberLimit = 20;
const deletingMovie = shallowRef<DuplicateMovie|null>(null), deletionGroupKey = ref(''), deletionLibrary = ref(0), notice = ref('');
function deleteMovie(movie:DuplicateMovie, group:DuplicateGroup) { deletingMovie.value=movie;deletionGroupKey.value=group.key;deletionLibrary.value=library.value;notice.value=''; }
async function deleted() { deletingMovie.value=null;notice.value='已删除远程文件、本地 STRM 和影片索引。';await loadGroups(); }
let retryOffset = 0, retryMemberOffset = 0;
function resetMembers() { memberScope.cancel(); selected.value = null; members.value = null; memberLoading.value = false; memberError.value = ''; memberOffset.value = 0; }
function reset() { groupScope.cancel(); resetMembers(); groups.value = null; loading.value = false; error.value = ''; offset.value = 0; }
async function loadGroups(nextOffset = 0) {
  const current = groupScope.begin(); resetMembers(); loading.value = true; error.value = ''; retryOffset = nextOffset;
  try { const result = await duplicatesApi.groups(library.value,groupLimit,nextOffset,current.signal); if (current.isCurrent()) { groups.value = result; offset.value = nextOffset; } }
  catch (cause) { if (current.isCurrent()) error.value = cause instanceof Error ? cause.message : '查重失败'; }
  finally { if (current.isCurrent()) loading.value = false; }
}
async function scan(libraryId?: number) {
  if (libraryId !== undefined && library.value !== libraryId) { library.value = libraryId; reset(); }
  await loadGroups();
}
async function openGroup(group: DuplicateGroup, nextOffset = 0) {
  const current = memberScope.begin();
  if (selected.value?.key !== group.key) members.value = null;
  selected.value = group; memberLoading.value = true; memberError.value = ''; retryMemberOffset = nextOffset;
  try { const result = await duplicatesApi.members(library.value,group.key,memberLimit,nextOffset,current.signal); if (current.isCurrent()) { members.value = result; memberOffset.value = nextOffset; } }
  catch (cause) { if (current.isCurrent()) memberError.value = cause instanceof Error ? cause.message : '读取影片失败'; }
  finally { if (current.isCurrent()) memberLoading.value = false; }
}
function statusLabel(status: string) { return ({success:'已入库',manual:'手动录入',pending:'待补元数据',incompatible:'不兼容'} as Record<string,string>)[status] || status; }
watch(() => props.libraries, libs => { if (library.value && !libs.some(lib => lib.Id === library.value)) {library.value = 0;reset();} });
defineExpose({ scan });
</script>

<template>
  <section id="duplicate-movies" class="panel duplicate-panel" :aria-busy="loading">
    <div class="panel-head"><h2>影片查重</h2><span class="hint">按番号分组</span></div>
    <form class="duplicate-controls" @submit.prevent="scan()">
      <div class="field"><label for="duplicate-library">查重范围</label><select id="duplicate-library" v-model.number="library" @change="reset"><option :value="0">全部媒体库（含跨库重复）</option><option v-for="lib in libraries" :key="lib.Id" :value="lib.Id">{{ lib.Name }}</option></select></div>
      <button id="duplicate-start" class="btn btn-accent" :disabled="loading || !libraries.length"><Search :size="16" aria-hidden="true" />{{ loading ? '正在查重…' : '开始查重' }}</button>
    </form>
    <p class="hint">使用当前已入库番号，忽略大小写、空格和 - _ . 分隔差异；保留前导零及版本后缀。空番号和电视节目不参与，CD 分段算一部。磁盘有变化时，请先增量扫描；查重不会修改文件。“查看位置”会自动读取主 STRM 和分段内容，不访问远程链接。展开分组可删除多余副本，删除前会显示远程与本地路径。</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <p v-if="error" class="auth-error" role="alert">{{ error }} <button class="btn btn-sm" @click="loadGroups(retryOffset)">重试</button></p>
    <p v-if="loading" role="status">正在统计所选范围内的重复番号…</p>
    <template v-if="groups && !error">
      <p id="duplicate-summary" role="status">共 {{ groups.total }} 组重复番号，涉及 {{ groups.total_movies }} 部影片，比每个番号一份多出 {{ groups.extra_copies }} 份。</p>
      <p v-if="!groups.total" class="hint">未发现重复影片。</p>
      <div v-for="group in groups.items" :key="group.key" class="duplicate-group">
        <div class="duplicate-group-head"><strong class="mono">{{ group.number }}</strong><span>{{ group.count }} 份</span><button class="btn btn-sm" :data-duplicate-key="group.key" :disabled="loading || memberLoading && selected?.key === group.key" :aria-expanded="selected?.key === group.key" @click="selected?.key === group.key ? resetMembers() : openGroup(group)">{{ selected?.key === group.key ? '收起' : '查看位置' }}</button></div>
        <div v-if="selected?.key === group.key" class="duplicate-members" :aria-busy="memberLoading">
          <p v-if="memberLoading" role="status">正在读取影片位置…</p>
          <p v-if="memberError" class="auth-error" role="alert">{{ memberError }} <button class="btn btn-sm" @click="openGroup(group, retryMemberOffset)">重试</button></p>
          <template v-if="members && !memberError">
            <p v-if="members.total < 2" class="hint">该分组当前不足两部影片，请重新查重以更新统计。</p>
            <article v-for="movie in members.items" :key="movie.id" class="duplicate-movie">
              <div class="duplicate-movie-head"><RouterLink :to="`/item/${movie.id}`">{{ movie.title || movie.number }}</RouterLink><span class="hint">{{ statusLabel(movie.status) }}</span><button class="btn btn-sm danger" :data-duplicate-delete="movie.id" :disabled="!!deletingMovie || loading || memberLoading || members.total < 2" @click="deleteMovie(movie, group)">删除此副本</button></div>
              <p class="duplicate-location"><span>{{ movie.library_name }}</span><span class="mono">{{ movie.number }}</span></p>
              <DuplicateStrmContents :movie="movie" :library-id="library" :group-key="group.key" />
            </article>
            <div v-if="members.total > memberLimit" class="duplicate-pagination"><button class="btn btn-sm" :disabled="memberLoading || !memberOffset" @click="openGroup(group, memberOffset - memberLimit)">上一页影片</button><span>{{ memberOffset + 1 }}–{{ Math.min(memberOffset + memberLimit, members.total) }} / {{ members.total }}</span><button class="btn btn-sm" :disabled="memberLoading || memberOffset + memberLimit >= members.total" @click="openGroup(group, memberOffset + memberLimit)">下一页影片</button></div>
          </template>
        </div>
      </div>
      <div v-if="groups.total > groupLimit" class="duplicate-pagination"><button class="btn btn-sm" :disabled="loading || !offset" @click="loadGroups(offset - groupLimit)">上一页分组</button><span>{{ offset + 1 }}–{{ Math.min(offset + groupLimit, groups.total) }} / {{ groups.total }}</span><button class="btn btn-sm" :disabled="loading || offset + groupLimit >= groups.total" @click="loadGroups(offset + groupLimit)">下一页分组</button></div>
    </template>
    <p v-else-if="!loading && !error" class="hint">选择范围后点击“开始查重”，可展开每组查看影片所在媒体库、文件路径和 STRM 内容。</p>
  </section>
  <DuplicateDeleteDialog v-if="deletingMovie" :movie="deletingMovie" :group-key="deletionGroupKey" :library-id="deletionLibrary" @close="deletingMovie=null" @deleted="deleted" />
</template>
